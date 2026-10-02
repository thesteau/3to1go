const { loadFeature } = require('../helpers/scripts.cts');
const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');

test('clear staged backup and cancellation use separate endpoints and report errors', async () => {
  const calls = [];
  const messages = [];
  const ctx = vm.createContext({
    fetch: async (url, options) => {
      calls.push([url, options]);
      if (url.includes('clear-staged')) return { ok: false, json: async () => ({ detail: 'Backup is still running' }) };
      return { ok: true, json: async () => ({ status: 'cancelling' }) };
    },
    document: { getElementById: () => null },
    setActionStatus: (...args) => messages.push(args),
    loadData: async () => {}, requestEdgeActiveRefreshBurst() {}, confirmApp: async () => true,
  });
  loadFeature(ctx, 'edge', 'utils');
  loadFeature(ctx, 'edge', 'files');
  const btn = { textContent: '', disabled: false };
  await ctx.clearStagedBackup('job', btn);
  assert.equal(messages.at(-1)[1], 'error');
  assert.equal(btn.disabled, false);
  await ctx.cancelOperation(btn);
  assert.deepEqual(calls.map(([url]) => url), ['/api/directories/clear-staged', '/api/cancel-operation']);
  assert.match(messages.at(-1)[0], /Cancellation requested/);
});

test('folder paths round-trip through executable inline action handlers', () => {
  const ctx = vm.createContext({});
  loadFeature(ctx, 'edge', 'utils');
  for (const folder of ["Family's photos", 'quotes" & spaces', 'photos/日本語', "x');throw new Error('unexpected');//"]) {
    const encoded = ctx.encodedPath(folder);
    for (const action of ['openJobDialogFromEvent', 'forceUploadFromEvent', 'openRecoverDialogFromEvent']) {
      const handler = new Function(action, 'event', `return ${action}(event, decodeURIComponent('${encoded}'))`);
      assert.equal(handler((event, actualPath) => actualPath, {}), folder);
    }
  }
});
