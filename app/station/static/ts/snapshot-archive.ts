interface SnapshotArchiveEntry {
  path: string;
  directory: boolean;
  size: number;
  offset: number;
  modified: number | null;
}

interface SnapshotArchive {
  tar: Blob;
  entries: SnapshotArchiveEntry[];
}

const TAR_BLOCK_BYTES = 512;
const TAR_METADATA_LIMIT = 1024 * 1024;

// Yield to input/painting between batches, including when Blob reads resolve immediately.
async function yieldSnapshotWork(signal?: AbortSignal): Promise<void> {
  signal?.throwIfAborted();
  await new Promise<void>((resolve) => setTimeout(resolve, 0));
  signal?.throwIfAborted();
}

async function decompressSnapshotArchive(compressed: Blob, signal?: AbortSignal): Promise<Blob> {
  const output = new SnapshotBlobBuilder();
  const decoder = new fzstd.Decompress((chunk) => {
    // The decoder can reuse its window after this callback, so own the queued bytes.
    output.add(new Uint8Array(chunk));
  });
  for (let offset = 0; offset < compressed.size; offset += 256 * 1024) {
    await yieldSnapshotWork(signal);
    decoder.push(new Uint8Array(await compressed.slice(offset, offset + 256 * 1024).arrayBuffer()));
  }
  signal?.throwIfAborted();
  decoder.push(new Uint8Array(), true);
  return output.finish();
}

function tarText(bytes: Uint8Array): string {
  const end = bytes.indexOf(0);
  return new TextDecoder("utf-8", { fatal: true }).decode(end < 0 ? bytes : bytes.subarray(0, end));
}

function tarNumber(bytes: Uint8Array): number {
  let value = 0;
  if (bytes[0] & 0x80) {
    // GNU/base-256 positive numbers, used when an octal field cannot hold a large size.
    if (bytes[0] & 0x40) throw new Error("The snapshot has a negative archive size.");
    value = bytes[0] & 0x7f;
    for (const byte of bytes.subarray(1)) value = value * 256 + byte;
  } else {
    const text = tarText(bytes).trim();
    if (text && !/^[0-7]+$/.test(text)) throw new Error("The snapshot has an invalid archive header.");
    value = text ? Number.parseInt(text, 8) : 0;
  }
  if (!Number.isSafeInteger(value)) throw new Error("The snapshot contains a file too large to view.");
  return value;
}

function snapshotArchivePath(name: string): string {
  if (name.startsWith("/") || name.includes("\\") || name.includes("\0") || /^[a-z]:/i.test(name)) {
    throw new Error("The snapshot contains an unsafe file path.");
  }
  const parts = name.split("/").filter((part) => part && part !== ".");
  if (parts.includes("..")) throw new Error("The snapshot contains an unsafe file path.");
  return parts.join("/");
}

function parseTarPax(bytes: Uint8Array): Record<string, string> {
  const fields: Record<string, string> = Object.create(null);
  for (let offset = 0; offset < bytes.length; ) {
    const space = bytes.indexOf(32, offset);
    if (space < 0) throw new Error("The snapshot has invalid file metadata.");
    const lengthText = tarText(bytes.subarray(offset, space));
    const length = Number(lengthText);
    const end = offset + length;
    if (
      !/^\d+$/.test(lengthText) ||
      !Number.isSafeInteger(length) ||
      end <= space + 1 ||
      end > bytes.length ||
      bytes[end - 1] !== 10
    ) {
      throw new Error("The snapshot has invalid file metadata.");
    }
    const record = new TextDecoder("utf-8", { fatal: true }).decode(bytes.subarray(space + 1, end - 1));
    const equals = record.indexOf("=");
    if (equals <= 0) throw new Error("The snapshot has invalid file metadata.");
    fields[record.slice(0, equals)] = record.slice(equals + 1);
    offset = end;
  }
  return fields;
}

async function indexSnapshotTar(tar: Blob, signal?: AbortSignal): Promise<SnapshotArchive> {
  const entries = new Map<string, SnapshotArchiveEntry>();
  let pax: Record<string, string> = Object.create(null);
  let globalPax: Record<string, string> = Object.create(null);
  let longName: string | null = null;
  let yielded = Date.now();
  for (let offset = 0; offset < tar.size; ) {
    signal?.throwIfAborted();
    if (Date.now() - yielded > 16) {
      await yieldSnapshotWork(signal);
      yielded = Date.now();
    }
    const header = new Uint8Array(await tar.slice(offset, offset + TAR_BLOCK_BYTES).arrayBuffer());
    if (header.length !== TAR_BLOCK_BYTES) throw new Error("The snapshot archive is truncated.");
    if (header.every((byte) => byte === 0)) {
      const end = new Uint8Array(await tar.slice(offset + TAR_BLOCK_BYTES, offset + 2 * TAR_BLOCK_BYTES).arrayBuffer());
      if (
        end.length !== TAR_BLOCK_BYTES ||
        end.some((byte) => byte !== 0) ||
        Object.keys(pax).length ||
        longName !== null
      ) {
        throw new Error("The snapshot archive is truncated.");
      }
      return { tar, entries: [...entries.values()] };
    }
    const checksum = header.reduce((sum, byte, i) => sum + (i >= 148 && i < 156 ? 32 : byte), 0);
    if (tarNumber(header.subarray(148, 156)) !== checksum)
      throw new Error("The snapshot has a damaged archive header.");
    const type = header[156];
    const metadata = type === 120 || type === 103 || type === 76; // PAX local/global, GNU long name.
    const fields = { ...globalPax, ...pax };
    if (!metadata && fields.size !== undefined && !/^\d+$/.test(fields.size)) {
      throw new Error("The snapshot has an invalid file size.");
    }
    const size = !metadata && fields.size !== undefined ? Number(fields.size) : tarNumber(header.subarray(124, 136));
    if (!Number.isSafeInteger(size) || size < 0) throw new Error("The snapshot has an invalid file size.");
    const dataOffset = offset + TAR_BLOCK_BYTES;
    const nextOffset = dataOffset + Math.ceil(size / TAR_BLOCK_BYTES) * TAR_BLOCK_BYTES;
    if (!Number.isSafeInteger(nextOffset) || nextOffset > tar.size)
      throw new Error("The snapshot archive is truncated.");
    if (metadata) {
      if (size > TAR_METADATA_LIMIT) throw new Error("The snapshot's file metadata is too large to view.");
      const bytes = new Uint8Array(await tar.slice(dataOffset, dataOffset + size).arrayBuffer());
      if (type === 76) longName = tarText(bytes).replace(/\n$/, "");
      else if (type === 103) globalPax = { ...globalPax, ...parseTarPax(bytes) };
      else pax = { ...pax, ...parseTarPax(bytes) };
    } else {
      if (Object.keys(fields).some((key) => key.startsWith("GNU.sparse."))) {
        throw new Error("Sparse archive entries cannot be viewed. Download the full snapshot instead.");
      }
      // Scout archives contain regular files and folders. Never expose links or special files.
      if (type === 0 || type === 48 || type === 53) {
        const ustar = tarText(header.subarray(257, 263)) === "ustar" && header[263] === 48 && header[264] === 48;
        const prefix = ustar ? tarText(header.subarray(345, 500)) : "";
        const name = tarText(header.subarray(0, 100));
        const path = snapshotArchivePath(fields.path ?? longName ?? (prefix ? `${prefix}/${name}` : name));
        const directory = type === 53;
        if ((!path && !directory) || (directory && size !== 0))
          throw new Error("The snapshot has an invalid file entry.");
        if (path) {
          const modified = fields.mtime !== undefined ? Number(fields.mtime) : tarNumber(header.subarray(136, 148));
          const entry = {
            path,
            directory,
            size,
            offset: dataOffset,
            modified: Number.isFinite(modified) ? modified * 1000 : null,
          };
          const existing = entries.get(path);
          if (existing && (!existing.directory || !directory))
            throw new Error("The snapshot contains conflicting file paths.");
          entries.set(path, entry);
          const parts = path.split("/");
          parts.pop();
          for (let count = 1; count <= parts.length; count++) {
            const parent = parts.slice(0, count).join("/");
            if (entries.get(parent)?.directory === false)
              throw new Error("The snapshot contains conflicting file paths.");
            if (!entries.has(parent))
              entries.set(parent, { path: parent, directory: true, size: 0, offset: 0, modified: null });
          }
        }
      }
      pax = Object.create(null);
      longName = null;
    }
    offset = nextOffset;
  }
  throw new Error("The snapshot archive is truncated.");
}

function snapshotEntryBlob(archive: SnapshotArchive, entry: SnapshotArchiveEntry): Blob {
  return archive.tar.slice(entry.offset, entry.offset + entry.size);
}

async function zipSnapshotFiles(
  archive: SnapshotArchive,
  entries: SnapshotArchiveEntry[],
  signal?: AbortSignal,
): Promise<Blob> {
  // zip.js streams Blob reads and automatically uses ZIP64 when required. Store entries without
  // compression to keep large selections responsive; the downloaded ZIP preserves relative paths.
  const writer = new zip.ZipWriter(new zip.BlobWriter("application/zip"), { level: 0, useWebWorkers: false });
  for (const entry of entries) {
    signal?.throwIfAborted();
    await writer.add(entry.path, new zip.BlobReader(snapshotEntryBlob(archive, entry)), {
      lastModDate: entry.modified === null ? undefined : new Date(entry.modified),
      signal,
    });
  }
  signal?.throwIfAborted();
  return writer.close();
}
