const { loadFeature } = require('../helpers/scripts.cts');
const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');

function keyContext() {
  const storage = new Map([['unrelated', 'keep']]);
  const input = { value: 'unsaved-key' };
  const status = { textContent: 'Key saved' };
  const messages = [];
  const ctx = vm.createContext({
    sessionStorage: {
      get length() { return storage.size; }, key: index => [...storage.keys()][index],
      getItem: key => storage.get(key), setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key),
    },
    document: { querySelectorAll: selector => selector.includes('input') ? [input] : [status], querySelector: () => status },
    window: { fetch: async () => ({ ok: true }), setTimeout() {} },
    closeDialog() {}, openDialog() {}, clearStatus() {}, resolveAppDialog() {},
    setActionStatus: (...args) => messages.push(args), escapeSelectorValue: value => value,
  });
  loadFeature(ctx, 'central', 'keys');
  loadFeature(ctx, 'central', 'auth');
  return { ctx, storage, input, status, messages };
}

test('logout clears saved keys, drafts and status while retaining unrelated storage', async () => {
  const { ctx, storage, input, status } = keyContext();
  ctx.setEncKey('edge', 'instance', 'secret');
  storage.set('3to1go_enc_storage-only', 'another-secret');
  await ctx.logoutUser();
  assert.equal(ctx.getEncKey('edge', 'instance'), null);
  assert.deepEqual([...storage], [['unrelated', 'keep']]);
  assert.equal(input.value, '');
  assert.equal(status.textContent, '');
});

test('expired sessions also clear keys', () => {
  const { ctx } = keyContext();
  ctx.setEncKey('edge', 'instance', 'secret');
  ctx.openLoginDialog();
  assert.equal(ctx.getEncKey('edge', 'instance'), null);
});

test('key validation finishing after logout cannot restore a key', async () => {
  const { ctx, storage } = keyContext();
  let finish;
  ctx.fingerprintKey = () => new Promise(resolve => { finish = resolve; });
  const pending = ctx.storeEncKey('edge', 'instance', 'secret');
  await ctx.logoutUser();
  finish('fingerprint');
  assert.equal(await pending, null);
  assert.equal(ctx.getEncKey('edge', 'instance'), null);
  assert.equal(storage.size, 1);
});

test('a key prompt started before logout cannot save a key afterwards', async () => {
  const { ctx } = keyContext();
  ctx.document.querySelector = () => null;
  let finish;
  ctx.appDialog = () => new Promise(resolve => { finish = resolve; });
  const pending = ctx.resolveEncKey('edge', 'instance');
  await ctx.logoutUser();
  finish('secret');
  assert.equal(await pending, null);
  assert.equal(ctx.getEncKey('edge', 'instance'), null);
});
