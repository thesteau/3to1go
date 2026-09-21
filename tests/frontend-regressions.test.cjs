const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function load(context, file) {
  vm.runInContext(fs.readFileSync(path.join(__dirname, '..', file), 'utf8'), context);
}

test('Edge keeps polling while idle, accelerates for work, and pauses for dialogs', async () => {
  let timer;
  let dialogOpen = false;
  const requests = [];
  const context = vm.createContext({
    window: { setTimeout(callback, delay) { timer = { callback, delay }; return 1; }, clearTimeout() {} },
    document: { hidden: false, querySelector: () => dialogOpen ? {} : null, getElementById: () => null },
    fetch: async (url) => {
      requests.push(url);
      return { ok: true, json: async () => url === '/api/status' ? { scheduler: { state: 'running' } } : { directories: [] } };
    },
    applyTheme() {}, fillMetaFromDir() {}, renderSelectedJobs() {},
  });
  load(context, 'edge/static/js/refresh.js');
  vm.runInContext('_edgeAutoRefreshStarted = true; latestData = { scheduler: { state: "waiting" }, directories: [] }; scheduleEdgeRefresh()', context);
  assert.equal(timer.delay, 15000);
  timer.callback();
  await new Promise(setImmediate);
  assert.deepEqual(requests, ['/api/status', '/api/directories']);
  assert.equal(timer.delay, 2500);
  dialogOpen = true;
  timer.callback();
  assert.equal(requests.length, 2);
  assert.equal(timer.delay, 2000);
  dialogOpen = false;
  context.document.hidden = true;
  timer.callback();
  assert.equal(requests.length, 2);
});

test('folder paths round-trip through executable inline action handlers', () => {
  const context = vm.createContext({});
  load(context, 'edge/static/js/utils.js');
  for (const folder of ["Family's photos", 'quotes" & spaces', 'photos/日本語', "x');throw new Error('unexpected');//"]) {
    const encoded = context.encodedPath(folder);
    for (const action of ['openJobDialogFromEvent', 'forceUploadFromEvent', 'openRecoverDialogFromEvent']) {
      const handler = new Function(action, 'event', `return ${action}(event, decodeURIComponent('${encoded}'))`);
      assert.equal(handler((event, actualPath) => actualPath, {}), folder);
    }
  }
});

function overviewContext(fetch) {
  const messages = [];
  const elements = { namespaces: { children: [{}] }, meta: {} };
  const context = vm.createContext({
    window: {}, document: { getElementById: id => elements[id] || null, querySelectorAll: () => [] },
    fetch, setActionStatus: (...args) => messages.push(args),
    applyTheme() {}, renderHelpHint: () => '', fillSettings() {},
  });
  load(context, 'central/static/js/utils.js');
  load(context, 'central/static/js/overview.js');
  context.updateOverviewDom = () => {};
  return { context, messages };
}

test('Central reports failure without a success toast for HTTP and network errors', async () => {
  for (const fetch of [async () => ({ ok: false }), async () => { throw new Error('Network unavailable'); }]) {
    const { context, messages } = overviewContext(fetch);
    await context.manualRefresh();
    assert.equal(messages.length, 1);
    assert.equal(messages[0][1], 'error');
  }
});

test('Central reports success only after a successful overview refresh', async () => {
  const { context, messages } = overviewContext(async () => ({ ok: true, json: async () => ({ edges: [], settings: {} }) }));
  await context.manualRefresh();
  assert.deepEqual(messages, [['Refreshed.', 'success']]);
});

test('Central does not claim success when a refresh is already in progress', async () => {
  let finish;
  const { context, messages } = overviewContext(() => new Promise(resolve => { finish = resolve; }));
  const first = context.loadOverview({ silent: true });
  await context.manualRefresh();
  assert.deepEqual(messages, []);
  finish({ ok: false });
  assert.equal(await first, false);
});

function keyContext() {
  const storage = new Map([['unrelated', 'keep']]);
  const input = { value: 'unsaved-key' };
  const status = { textContent: 'Key saved' };
  const messages = [];
  const context = vm.createContext({
    sessionStorage: {
      get length() { return storage.size; }, key: index => [...storage.keys()][index],
      getItem: key => storage.get(key), setItem: (key, value) => storage.set(key, value), removeItem: key => storage.delete(key),
    },
    document: { querySelectorAll: selector => selector.includes('input') ? [input] : [status], querySelector: () => status },
    window: { fetch: async () => ({ ok: true }), setTimeout() {} },
    closeDialog() {}, openDialog() {}, clearStatus() {}, resolveAppDialog() {},
    setActionStatus: (...args) => messages.push(args), escapeSelectorValue: value => value,
  });
  load(context, 'central/static/js/keys.js');
  load(context, 'central/static/js/auth.js');
  return { context, storage, input, status, messages };
}

test('logout clears saved keys, drafts and status while retaining unrelated storage', async () => {
  const { context, storage, input, status } = keyContext();
  context.setEncKey('edge', 'instance', 'secret');
  storage.set('3to1go_enc_storage-only', 'another-secret');
  await context.logoutUser();
  assert.equal(context.getEncKey('edge', 'instance'), null);
  assert.deepEqual([...storage], [['unrelated', 'keep']]);
  assert.equal(input.value, '');
  assert.equal(status.textContent, '');
});

test('expired sessions also clear keys', () => {
  const { context } = keyContext();
  context.setEncKey('edge', 'instance', 'secret');
  context.openLoginDialog();
  assert.equal(context.getEncKey('edge', 'instance'), null);
});

test('key validation finishing after logout cannot restore a key', async () => {
  const { context, storage } = keyContext();
  let finish;
  context.fingerprintKey = () => new Promise(resolve => { finish = resolve; });
  const pending = context.storeEncKey('edge', 'instance', 'secret');
  await context.logoutUser();
  finish('fingerprint');
  assert.equal(await pending, null);
  assert.equal(context.getEncKey('edge', 'instance'), null);
  assert.equal(storage.size, 1);
});

test('a key prompt started before logout cannot save a key afterwards', async () => {
  const { context } = keyContext();
  context.document.querySelector = () => null;
  let finish;
  context.appDialog = () => new Promise(resolve => { finish = resolve; });
  const pending = context.resolveEncKey('edge', 'instance');
  await context.logoutUser();
  finish('secret');
  assert.equal(await pending, null);
  assert.equal(context.getEncKey('edge', 'instance'), null);
});
