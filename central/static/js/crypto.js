const ENC_MAGIC = new Uint8Array([82, 67, 69, 78, 67, 49, 0, 0]); // "RCENC1\x00\x00"
const ENC_MAGIC_LEN = 8;
const ENC_IV_LEN = 12;

function isEncrypted(buffer) {
  if (buffer.byteLength < ENC_MAGIC_LEN + ENC_IV_LEN) return false;
  const view = new Uint8Array(buffer, 0, ENC_MAGIC_LEN);
  return ENC_MAGIC.every((b, i) => b === view[i]);
}

function base64UrlToBytes(b64) {
  const normalized = String(b64 || "").trim();
  if (!normalized) throw new Error("missing key");
  const std = normalized.replace(/-/g, "+").replace(/_/g, "/");
  const padded = std.padEnd(std.length + ((4 - (std.length % 4)) % 4), "=");
  return Uint8Array.from(atob(padded), (c) => c.charCodeAt(0));
}

function bytesToHex(bytes) {
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
}

async function fingerprintKey(keyB64) {
  const keyBytes = base64UrlToBytes(keyB64);
  const digest = await crypto.subtle.digest("SHA-256", keyBytes);
  return bytesToHex(new Uint8Array(digest));
}

async function decryptBuffer(buffer, keyB64) {
  const keyBytes = base64UrlToBytes(keyB64);
  const iv = new Uint8Array(buffer, ENC_MAGIC_LEN, ENC_IV_LEN);
  const ciphertext = buffer.slice(ENC_MAGIC_LEN + ENC_IV_LEN);
  const key = await crypto.subtle.importKey("raw", keyBytes, { name: "AES-GCM" }, false, ["decrypt"]);
  return crypto.subtle.decrypt({ name: "AES-GCM", iv }, key, ciphertext);
}

function triggerBlobDownload(buffer, filename) {
  const url = URL.createObjectURL(new Blob([buffer]));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
