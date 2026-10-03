const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const nodeCrypto = require("node:crypto");

// Fixture written by Scout's encryption.EncryptFile (minio/sio DARE 2.0): two packages, the second final.
const fixture = fs.readFileSync(path.join(__dirname, "..", "fixtures", "dare-v2-aes-gcm.bin"));
const key = Buffer.from(Array.from({ length: 32 }, (_, i) => i));
const keyB64 = key.toString("base64url");
const plaintext = Buffer.from(Array.from({ length: (1 << 16) + 1000 }, (_, i) => (i * 31 + 7) & 0xff));

function cryptoContext(crypto = globalThis.crypto) {
  const ctx = vm.createContext({ crypto, atob, Blob });
  loadFeature(ctx, "station", "crypto");
  return ctx;
}

const arrayBuffer = (buffer) => buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength);

test("Station decrypts snapshots Scout encrypts with minio/sio DARE 2.0", async () => {
  const ctx = cryptoContext();
  assert.equal(ctx.isEncrypted(arrayBuffer(fixture)), true);
  const blob = await ctx.decryptBuffer(arrayBuffer(fixture), keyB64);
  assert.deepEqual(Buffer.from(await blob.arrayBuffer()), plaintext);
});

test("Station bounds decryption work instead of queuing the entire archive", async () => {
  let active = 0;
  let peak = 0;
  const subtle = globalThis.crypto.subtle;
  const ctx = cryptoContext({
    subtle: {
      importKey: (...args) => subtle.importKey(...args),
      async decrypt(...args) {
        peak = Math.max(peak, ++active);
        try {
          return await subtle.decrypt(...args);
        } finally {
          active--;
        }
      },
    },
  });
  const blob = await ctx.decryptBuffer(arrayBuffer(fixture), keyB64);
  assert.deepEqual(Buffer.from(await blob.arrayBuffer()), plaintext);
  assert.equal(peak, 1);
});

// Encrypts `data` the way minio/sio DARE 2.0 does, to build streams larger than the Go fixture.
function dareEncrypt(data) {
  const random = nodeCrypto.randomBytes(12);
  random[0] &= 0x7f;
  const packages = [];
  for (let offset = 0, sequence = 0; offset < data.length; offset += 1 << 16, sequence += 1) {
    const payload = data.subarray(offset, offset + (1 << 16));
    const header = Buffer.alloc(16);
    header[0] = 0x20;
    header.writeUInt16LE(payload.length - 1, 2);
    random.copy(header, 4);
    if (offset + payload.length >= data.length) header[4] |= 0x80;
    const nonce = Buffer.from(header.subarray(4));
    nonce.writeUInt32LE((nonce.readUInt32LE(8) ^ sequence) >>> 0, 8);
    const cipher = nodeCrypto.createCipheriv("aes-256-gcm", key, nonce).setAAD(header.subarray(0, 4));
    packages.push(header, cipher.update(payload), cipher.final(), cipher.getAuthTag());
  }
  return Buffer.concat(packages);
}

test("Station decrypts a DARE snapshot as it streams instead of buffering the whole response", async () => {
  const data = nodeCrypto.randomBytes(3 * (1 << 16) + 123);
  const encrypted = dareEncrypt(data);
  const chunkSize = 16 * 1024;
  let reads = 0;
  let readsAtFirstDecrypt = null;
  const subtle = globalThis.crypto.subtle;
  const ctx = cryptoContext({
    subtle: {
      importKey: (...args) => subtle.importKey(...args),
      decrypt(...args) {
        readsAtFirstDecrypt ??= reads;
        return subtle.decrypt(...args);
      },
    },
  });
  const reader = vm.runInContext(
    "(next) => new SnapshotReader(next)",
    ctx,
  )(async () => {
    const chunk = encrypted.subarray(reads * chunkSize, (reads + 1) * chunkSize);
    if (!chunk.length) return null;
    reads += 1;
    return new Uint8Array(chunk);
  });
  const blob = await ctx.readSnapshot(reader, keyB64);
  assert.deepEqual(Buffer.from(await blob.arrayBuffer()), data);
  const totalReads = Math.ceil(encrypted.length / chunkSize);
  assert.ok(
    readsAtFirstDecrypt <= Math.ceil((16 + (1 << 16) + 16) / chunkSize),
    `first package decrypted after ${readsAtFirstDecrypt} reads`,
  );
  assert.ok(readsAtFirstDecrypt < totalReads);
});

function downloadContext({ savedKey }) {
  const storage = new Map(savedKey ? [["3to1go_enc_scout-a::inst-1", keyB64]] : []);
  const requests = [];
  const downloads = [];
  const prompts = [];
  const ctx = vm.createContext({
    crypto: globalThis.crypto,
    atob,
    Blob,
    fetch: async (url) => {
      requests.push(url);
      return new Response(fixture);
    },
    sessionStorage: {
      getItem: (k) => storage.get(k) ?? null,
      setItem: (k, v) => storage.set(k, v),
      removeItem: (k) => storage.delete(k),
      key: (i) => Array.from(storage.keys())[i] ?? null,
      get length() {
        return storage.size;
      },
    },
    document: { querySelector: () => null, querySelectorAll: () => [], createElement: () => ({ click() {} }) },
    URL: {
      createObjectURL: (blob) => {
        downloads.push(blob);
        return "blob:snapshot";
      },
      revokeObjectURL() {},
    },
    appDialog: async (options) => {
      prompts.push(options.title);
      return keyB64;
    },
    setActionStatus() {},
    loadOverview: async () => true,
  });
  for (const file of ["utils", "crypto", "keys", "snapshots"]) {
    loadFeature(ctx, "station", file);
  }
  return { ctx, requests, downloads, prompts };
}

test("Station downloads with a saved key in one request, and re-fetches after prompting for a key", async () => {
  for (const savedKey of [true, false]) {
    const { ctx, requests, downloads, prompts } = downloadContext({ savedKey });
    await ctx.downloadSnapshot("scout-a", "inst-1", "photos", "photos.tar.zst", null);
    assert.equal(requests.length, savedKey ? 1 : 2);
    assert.deepEqual(prompts, savedKey ? [] : ["Encryption Key Required"]);
    assert.equal(downloads.length, 1);
    assert.deepEqual(Buffer.from(await downloads[0].arrayBuffer()), plaintext);
  }
});

test("Station rejects tampered, truncated or extended DARE snapshots", async () => {
  const ctx = cryptoContext();
  const tampered = Buffer.from(fixture);
  tampered[100] ^= 1;
  const truncated = fixture.subarray(0, 16 + 65536 + 16);
  const extended = Buffer.concat([fixture, Buffer.from([0])]);
  for (const buffer of [tampered, truncated, extended]) {
    await assert.rejects(ctx.decryptBuffer(arrayBuffer(buffer), keyB64));
  }
  const wrongKey = Buffer.alloc(32, 7).toString("base64url");
  await assert.rejects(ctx.decryptBuffer(arrayBuffer(fixture), wrongKey));
});

test("Station still decrypts legacy RCENC1 snapshots and leaves plain archives alone", async () => {
  const ctx = cryptoContext();
  const iv = nodeCrypto.randomBytes(12);
  const cipher = nodeCrypto.createCipheriv("aes-256-gcm", key, iv);
  const legacy = Buffer.concat([
    Buffer.from("RCENC1\0\0"),
    iv,
    cipher.update(plaintext),
    cipher.final(),
    cipher.getAuthTag(),
  ]);
  assert.equal(ctx.isEncrypted(arrayBuffer(legacy)), true);
  const blob = await ctx.decryptBuffer(arrayBuffer(legacy), keyB64);
  assert.deepEqual(Buffer.from(await blob.arrayBuffer()), plaintext);

  const zstd = Buffer.concat([Buffer.from([0x28, 0xb5, 0x2f, 0xfd]), Buffer.alloc(64)]);
  assert.equal(ctx.isEncrypted(arrayBuffer(zstd)), false);
});
