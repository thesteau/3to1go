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

function isLegacyEncrypted(buffer: ArrayBuffer): boolean {
  if (buffer.byteLength < ENC_MAGIC_LEN + ENC_IV_LEN) return false;
  const view = new Uint8Array(buffer, 0, ENC_MAGIC_LEN);
  return ENC_MAGIC.every((b, i) => b === view[i]);
}

function isDareEncrypted(buffer: ArrayBuffer): boolean {
  if (buffer.byteLength <= DARE_HEADER_LEN + DARE_TAG_LEN) return false;
  const view = new Uint8Array(buffer, 0, 2);
  return view[0] === DARE_VERSION_20 && view[1] === DARE_AES_GCM;
}

function isEncrypted(buffer: ArrayBuffer): boolean {
  return isDareEncrypted(buffer) || isLegacyEncrypted(buffer);
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
async function decryptDare(buffer: ArrayBuffer, key: CryptoKey): Promise<Blob> {
  const bytes = new Uint8Array(buffer);
  const plaintext: ArrayBuffer[] = [];
  let firstNonce: Uint8Array<ArrayBuffer> | null = null;
  let offset = 0;
  for (let sequence = 0; ; sequence += 1) {
    if (offset + DARE_HEADER_LEN + DARE_TAG_LEN >= bytes.length) throw new Error("Encrypted snapshot is truncated.");
    const header = bytes.subarray(offset, offset + DARE_HEADER_LEN);
    if (header[0] !== DARE_VERSION_20 || header[1] !== DARE_AES_GCM) throw new Error("Unsupported snapshot encryption format.");
    const length = (header[2] | (header[3] << 8)) + 1;
    const end = offset + DARE_HEADER_LEN + length + DARE_TAG_LEN;
    if (end > bytes.length) throw new Error("Encrypted snapshot is truncated.");
    const final = (header[4] & 0x80) !== 0;
    if (!final && length !== DARE_MAX_PAYLOAD) throw new Error("Encrypted snapshot has an invalid package size.");

    const headerNonce = header.subarray(4, DARE_HEADER_LEN);
    firstNonce ??= headerNonce.slice();
    const expected = firstNonce.slice();
    expected[0] = (expected[0] & 0x7f) | (final ? 0x80 : 0);
    if (!expected.every((value, index) => value === headerNonce[index])) throw new Error("Encrypted snapshot packages do not belong together.");

    const nonce = headerNonce.slice();
    const view = new DataView(nonce.buffer);
    view.setUint32(8, (view.getUint32(8, true) ^ sequence) >>> 0, true);
    // Keep only one WebCrypto operation in flight, even for multi-gigabyte archives.
    plaintext.push(await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: nonce, additionalData: header.subarray(0, 4) },
      key, bytes.subarray(offset + DARE_HEADER_LEN, end),
    ));
    offset = end;
    if (final) break;
  }
  if (offset !== bytes.length) throw new Error("Encrypted snapshot has data after its final package.");

  return new Blob(plaintext);
}

async function decryptBuffer(buffer: ArrayBuffer, keyB64: string): Promise<Blob> {
  const keyBytes = base64UrlToBytes(keyB64);
  const key = await crypto.subtle.importKey("raw", keyBytes, { name: "AES-GCM" }, false, ["decrypt"]);
  if (isDareEncrypted(buffer)) return decryptDare(buffer, key);
  const iv = new Uint8Array(buffer, ENC_MAGIC_LEN, ENC_IV_LEN);
  const ciphertext = buffer.slice(ENC_MAGIC_LEN + ENC_IV_LEN);
  return new Blob([await crypto.subtle.decrypt({ name: "AES-GCM", iv }, key, ciphertext)]);
}

function triggerBlobDownload(data: ArrayBuffer | Blob, filename: string): void {
  const url = URL.createObjectURL(data instanceof Blob ? data : new Blob([data]));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
