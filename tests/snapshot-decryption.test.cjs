const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const nodeCrypto = require('node:crypto');

// Fixture written by Edge's encryption.EncryptFile (minio/sio DARE 2.0): two packages, the second final.
const fixture = fs.readFileSync(path.join(__dirname, 'fixtures', 'dare-v2-aes-gcm.bin'));
const key = Buffer.from(Array.from({ length: 32 }, (_, i) => i));
const keyB64 = key.toString('base64url');
const plaintext = Buffer.from(Array.from({ length: (1 << 16) + 1000 }, (_, i) => (i * 31 + 7) & 0xff));

function cryptoContext() {
  const ctx = vm.createContext({ crypto: globalThis.crypto, atob, Blob });
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../central/static/js/crypto.js'), 'utf8'), ctx);
  return ctx;
}

const arrayBuffer = (buffer) => buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength);

test('Central decrypts snapshots Edge encrypts with minio/sio DARE 2.0', async () => {
  const ctx = cryptoContext();
  assert.equal(ctx.isEncrypted(arrayBuffer(fixture)), true);
  const blob = await ctx.decryptBuffer(arrayBuffer(fixture), keyB64);
  assert.deepEqual(Buffer.from(await blob.arrayBuffer()), plaintext);
});

test('Central rejects tampered, truncated or extended DARE snapshots', async () => {
  const ctx = cryptoContext();
  const tampered = Buffer.from(fixture);
  tampered[100] ^= 1;
  const truncated = fixture.subarray(0, 16 + 65536 + 16);
  const extended = Buffer.concat([fixture, Buffer.from([0])]);
  for (const buffer of [tampered, truncated, extended]) {
    await assert.rejects(ctx.decryptBuffer(arrayBuffer(buffer), keyB64));
  }
  const wrongKey = Buffer.alloc(32, 7).toString('base64url');
  await assert.rejects(ctx.decryptBuffer(arrayBuffer(fixture), wrongKey));
});

test('Central still decrypts legacy RCENC1 snapshots and leaves plain archives alone', async () => {
  const ctx = cryptoContext();
  const iv = nodeCrypto.randomBytes(12);
  const cipher = nodeCrypto.createCipheriv('aes-256-gcm', key, iv);
  const legacy = Buffer.concat([Buffer.from('RCENC1\0\0'), iv, cipher.update(plaintext), cipher.final(), cipher.getAuthTag()]);
  assert.equal(ctx.isEncrypted(arrayBuffer(legacy)), true);
  const blob = await ctx.decryptBuffer(arrayBuffer(legacy), keyB64);
  assert.deepEqual(Buffer.from(await blob.arrayBuffer()), plaintext);

  const zstd = Buffer.concat([Buffer.from([0x28, 0xb5, 0x2f, 0xfd]), Buffer.alloc(64)]);
  assert.equal(ctx.isEncrypted(arrayBuffer(zstd)), false);
});
