// Legacy snapshots (before Edge moved to minio/sio): "RCENC1\0\0" + 12-byte IV + one AES-GCM ciphertext.
const ENC_MAGIC = new Uint8Array([82, 67, 69, 78, 67, 49, 0, 0]); // "RCENC1\x00\x00"
const ENC_MAGIC_LEN = 8;
const ENC_IV_LEN = 12;

// Current snapshots: minio/sio DARE 2.0 with AES-256-GCM, as written by Edge's encryption package.
// Each package is a 16-byte header, up to 64 KiB of ciphertext, and a 16-byte tag.
const DARE_VERSION_20 = 0x20;
const DARE_AES_GCM = 0x00;
const DARE_HEADER_LEN = 16;
const DARE_TAG_LEN = 16;
const DARE_MAX_PAYLOAD = 1 << 16;

// Enough leading bytes to tell plain, DARE and legacy snapshots apart.
const SNAPSHOT_HEAD_LEN = DARE_HEADER_LEN + DARE_TAG_LEN + 1;
// Downloaded bytes are folded into a Blob in batches of this size, so the browser can keep them
// outside the JavaScript heap instead of the tab retaining every chunk until the download ends.
const SNAPSHOT_BLOB_BATCH_BYTES = 16 * 1024 * 1024;

type SnapshotEncryption = "dare" | "legacy" | null;

// The connection failed while reading a snapshot, as opposed to the snapshot failing to decrypt.
class SnapshotReadError extends Error {}

// Reads a snapshot incrementally, buffering only the bytes a caller has asked for but not consumed.
class SnapshotReader {
  private chunks: Uint8Array<ArrayBuffer>[] = [];
  private buffered = 0;
  private done = false;

  constructor(
    private readonly next: () => Promise<Uint8Array<ArrayBuffer> | null>,
    private readonly stop: () => void = () => {},
  ) {}

  private async fill(count: number): Promise<void> {
    while (this.buffered < count && !this.done) {
      const chunk = await this.next();
      if (chunk === null) {
        this.done = true;
      } else if (chunk.length) {
        this.chunks.push(chunk);
        this.buffered += chunk.length;
      }
    }
  }

  // Returns up to `count` leading bytes without consuming them.
  async peek(count: number): Promise<Uint8Array<ArrayBuffer>> {
    await this.fill(count);
    const head = new Uint8Array(Math.min(count, this.buffered));
    let filled = 0;
    for (const chunk of this.chunks) {
      if (filled === head.length) break;
      const part = chunk.subarray(0, head.length - filled);
      head.set(part, filled);
      filled += part.length;
    }
    return head;
  }

  // Consumes up to `count` bytes; fewer are returned only at the end of the snapshot.
  async take(count: number): Promise<Uint8Array<ArrayBuffer>> {
    await this.fill(count);
    const out = new Uint8Array(Math.min(count, this.buffered));
    let filled = 0;
    while (filled < out.length) {
      const chunk = this.chunks[0];
      const used = Math.min(chunk.length, out.length - filled);
      out.set(chunk.subarray(0, used), filled);
      filled += used;
      if (used === chunk.length) this.chunks.shift();
      else this.chunks[0] = chunk.subarray(used);
    }
    this.buffered -= out.length;
    return out;
  }

  // Passes every remaining byte to `onChunk` as it arrives.
  async drain(onChunk: (chunk: Uint8Array<ArrayBuffer>) => void): Promise<void> {
    this.chunks.forEach(onChunk);
    this.chunks = [];
    this.buffered = 0;
    while (!this.done) {
      const chunk = await this.next();
      if (chunk === null) this.done = true;
      else if (chunk.length) onChunk(chunk);
    }
  }

  cancel(): void {
    this.done = true;
    this.stop();
  }
}

class SnapshotBlobBuilder {
  private blob = new Blob();
  private parts: BlobPart[] = [];
  private size = 0;

  add(part: ArrayBuffer | Uint8Array<ArrayBuffer>): void {
    this.parts.push(part);
    this.size += part.byteLength;
    if (this.size >= SNAPSHOT_BLOB_BATCH_BYTES) this.flush();
  }

  private flush(): void {
    this.blob = new Blob([this.blob, ...this.parts]);
    this.parts = [];
    this.size = 0;
  }

  finish(): Blob {
    this.flush();
    return this.blob;
  }
}

function snapshotReaderFromResponse(response: Response): SnapshotReader {
  const body = response.body?.getReader();
  if (!body) return new SnapshotReader(async () => null);
  return new SnapshotReader(
    async () => {
      try {
        const { done, value } = await body.read();
        return done ? null : value;
      } catch (error) {
        throw new SnapshotReadError("The download was interrupted.", { cause: error });
      }
    },
    () => {
      body.cancel().catch(() => {});
    },
  );
}

function snapshotReaderFromBuffer(buffer: ArrayBuffer): SnapshotReader {
  let sent = false;
  return new SnapshotReader(async () => {
    if (sent) return null;
    sent = true;
    return new Uint8Array(buffer);
  });
}

function snapshotEncryption(head: Uint8Array): SnapshotEncryption {
  if (head.length > DARE_HEADER_LEN + DARE_TAG_LEN && head[0] === DARE_VERSION_20 && head[1] === DARE_AES_GCM)
    return "dare";
  if (head.length >= ENC_MAGIC_LEN + ENC_IV_LEN && ENC_MAGIC.every((b, i) => b === head[i])) return "legacy";
  return null;
}

function isEncrypted(buffer: ArrayBuffer): boolean {
  return snapshotEncryption(new Uint8Array(buffer, 0, Math.min(buffer.byteLength, SNAPSHOT_HEAD_LEN))) !== null;
}

function base64UrlToBytes(b64: string): Uint8Array<ArrayBuffer> {
  const normalized = String(b64 || "").trim();
  if (!normalized) throw new Error("missing key");
  const std = normalized.replace(/-/g, "+").replace(/_/g, "/");
  const padded = std.padEnd(std.length + ((4 - (std.length % 4)) % 4), "=");
  return Uint8Array.from(atob(padded), (c) => c.charCodeAt(0));
}

function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
}

async function fingerprintKey(keyB64: string): Promise<string> {
  const keyBytes = base64UrlToBytes(keyB64);
  const digest = await crypto.subtle.digest("SHA-256", keyBytes);
  return bytesToHex(new Uint8Array(digest));
}

// Mirrors minio/sio's DARE 2.0 reader: every package must share the first package's nonce (with the
// final flag set only on the last one), and a stream without a final package is rejected as truncated.
// Packages are read and decrypted one at a time, so only one package of ciphertext is ever buffered.
async function decryptDare(reader: SnapshotReader, key: CryptoKey, output: SnapshotBlobBuilder): Promise<void> {
  let firstNonce: Uint8Array<ArrayBuffer> | null = null;
  for (let sequence = 0; ; sequence += 1) {
    const header = await reader.take(DARE_HEADER_LEN);
    if (header.length < DARE_HEADER_LEN) throw new Error("Encrypted snapshot is truncated.");
    if (header[0] !== DARE_VERSION_20 || header[1] !== DARE_AES_GCM)
      throw new Error("Unsupported snapshot encryption format.");
    const length = (header[2] | (header[3] << 8)) + 1;
    const final = (header[4] & 0x80) !== 0;
    if (!final && length !== DARE_MAX_PAYLOAD) throw new Error("Encrypted snapshot has an invalid package size.");
    const sealed = await reader.take(length + DARE_TAG_LEN);
    if (sealed.length < length + DARE_TAG_LEN) throw new Error("Encrypted snapshot is truncated.");

    const headerNonce = header.subarray(4, DARE_HEADER_LEN);
    firstNonce ??= headerNonce.slice();
    const expected = firstNonce.slice();
    expected[0] = (expected[0] & 0x7f) | (final ? 0x80 : 0);
    if (!expected.every((value, index) => value === headerNonce[index]))
      throw new Error("Encrypted snapshot packages do not belong together.");

    const nonce = headerNonce.slice();
    const view = new DataView(nonce.buffer);
    view.setUint32(8, (view.getUint32(8, true) ^ sequence) >>> 0, true);
    output.add(
      await crypto.subtle.decrypt({ name: "AES-GCM", iv: nonce, additionalData: header.subarray(0, 4) }, key, sealed),
    );
    if (final) break;
  }
  if ((await reader.take(1)).length) throw new Error("Encrypted snapshot has data after its final package.");
}

// Returns the snapshot's plaintext: decrypted with `keyB64` when it is encrypted, as downloaded otherwise.
async function readSnapshot(reader: SnapshotReader, keyB64: string | null): Promise<Blob> {
  const encryption = snapshotEncryption(await reader.peek(SNAPSHOT_HEAD_LEN));
  const output = new SnapshotBlobBuilder();
  if (!encryption) {
    await reader.drain((chunk) => output.add(chunk));
    return output.finish();
  }
  if (!keyB64) throw new Error("This snapshot is encrypted and needs its Edge key.");
  const key = await crypto.subtle.importKey("raw", base64UrlToBytes(keyB64), { name: "AES-GCM" }, false, ["decrypt"]);
  if (encryption === "dare") {
    await decryptDare(reader, key, output);
    return output.finish();
  }
  // Legacy snapshots are a single AES-GCM message, which WebCrypto can only decrypt whole.
  const header = await reader.take(ENC_MAGIC_LEN + ENC_IV_LEN);
  const sealed = new SnapshotBlobBuilder();
  await reader.drain((chunk) => sealed.add(chunk));
  const iv = header.subarray(ENC_MAGIC_LEN);
  output.add(await crypto.subtle.decrypt({ name: "AES-GCM", iv }, key, await sealed.finish().arrayBuffer()));
  return output.finish();
}

async function decryptBuffer(buffer: ArrayBuffer, keyB64: string): Promise<Blob> {
  return readSnapshot(snapshotReaderFromBuffer(buffer), keyB64);
}

function triggerBlobDownload(data: ArrayBuffer | Blob, filename: string): void {
  const url = URL.createObjectURL(data instanceof Blob ? data : new Blob([data]));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
