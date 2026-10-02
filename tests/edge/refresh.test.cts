const { loadFeature } = require('../helpers/scripts.cts');
const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');

// Stubs for the features refresh.js renders into; tests override what they observe.
function refreshContext(overrides) {
  const ctx = vm.createContext({
    window: {}, document: { getElementById: () => null },
    setHtmlIfChanged() {}, applyTheme() {}, setActionStatus() {}, setPanelReady() {},
    fillMetaFromDir() {}, fillMetaEncKey() {}, renderSelectedJobs() {}, renderDirectoryTree() {},
    directoryTreeLoaded: () => true, reloadDirectoryTree: async () => {},
    ...overrides,
  });
  loadFeature(ctx, 'edge', 'refresh');
  return ctx;
}

test('Edge keeps polling while idle, accelerates for work, and pauses for dialogs', async () => {
  let timer;
  let dialogOpen = false;
  const requests = [];
  const ctx = refreshContext({
    window: { setTimeout(callback, delay) { timer = { callback, delay }; return 1; }, clearTimeout() {} },
    document: { hidden: false, querySelector: () => dialogOpen ? {} : null, getElementById: () => null },
    fetch: async (url) => {
      requests.push(url);
      return { ok: true, json: async () => url === '/api/status' ? { scheduler: { state: 'running' } } : { directories: [] } };
    },
  });
  vm.runInContext('_edgeAutoRefreshStarted = true; latestData = { scheduler: { state: "waiting" }, directories: [] }; scheduleEdgeRefresh()', ctx);
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
  ctx.document.hidden = true;
  timer.callback();
  assert.equal(requests.length, 2);
});

test('a refresh requested during a poll waits and fetches fresh data', async () => {
  const pendingDirectories = [];
  let directoryRequests = 0;
  const ctx = refreshContext({
    window: { setTimeout: () => 0, clearTimeout() {} }, document: { getElementById: () => null, querySelector: () => null },
    fetch: async url => {
      if (url === '/api/directories') {
        directoryRequests++;
        return new Promise(resolve => pendingDirectories.push(resolve));
      }
      return { ok: true, json: async () => ({ settings: { theme: 'dark' } }) };
    },
  });
  const poll = ctx.loadData({ silent: true, includeKey: false });
  const afterAction = ctx.loadData({ silent: true, includeKey: false });
  await new Promise(setImmediate);
  assert.equal(directoryRequests, 1);
  pendingDirectories.shift()({ ok: true, json: async () => ({ directories: [] }) });
  await poll;
  await new Promise(setImmediate);
  assert.equal(directoryRequests, 2);
  pendingDirectories.shift()({ ok: true, json: async () => ({ directories: [] }) });
  await afterAction;
});

test('Edge renders status and key before slow job and folder loads complete', async () => {
  let finishDirectories;
  let finishTree;
  let treeLoaded = false;
  const rendered = [];
  const ready = {};
  const ctx = refreshContext({
    fetch: async url => url === '/api/directories'
      ? new Promise(resolve => { finishDirectories = resolve; })
      : { ok: true, json: async () => url === '/api/status' ? { settings: { theme: 'dark' } } : { key_base64: 'key', fingerprint: 'fp' } },
    setPanelReady: (name, value) => { ready[name] = value; },
    fillMetaFromDir: () => rendered.push('status'),
    fillMetaEncKey: () => rendered.push('key'),
    renderSelectedJobs: () => rendered.push('jobs'),
    directoryTreeLoaded: () => treeLoaded,
    reloadDirectoryTree: () => new Promise(resolve => { finishTree = resolve; }).then(() => { treeLoaded = true; rendered.push('tree'); }),
  });
  const pending = ctx.loadData();
  await new Promise(setImmediate);
  assert.deepEqual(rendered, ['status', 'key']);
  assert.equal(ready.settings, true);
  finishTree();
  await new Promise(setImmediate);
  assert.deepEqual(rendered, ['status', 'key', 'tree'], 'the folder tree does not wait for job discovery');
  finishDirectories({ ok: true, json: async () => ({ directories: [] }) });
  await pending;
  assert.deepEqual(rendered, ['status', 'key', 'tree', 'jobs']);
});

test('Edge polls skip the folder tree once it has loaded', async () => {
  const requests = [];
  let reloads = 0;
  const ctx = refreshContext({
    fetch: async url => { requests.push(url); return { ok: true, json: async () => ({ directories: [] }) }; },
    reloadDirectoryTree: async () => { reloads++; },
  });
  await ctx.loadData({ silent: true, includeKey: false });
  assert.equal(reloads, 0);
  assert.deepEqual(requests, ['/api/status', '/api/directories']);
  await ctx.loadData({ silent: true, includeKey: false, refreshDirectoryTree: true });
  assert.equal(reloads, 1);
});

test('Edge keeps loaded settings editable when a later poll fails', async () => {
  const ready = {};
  let failing = false;
  const ctx = refreshContext({
    fetch: async url => {
      if (failing) throw new Error('offline');
      return { ok: true, json: async () => url === '/api/status' ? { settings: { theme: 'dark' } } : url === '/api/directories' ? { directories: [] } : { key_base64: 'k', fingerprint: 'f' } };
    },
    setPanelReady: (name, value) => { ready[name] = value; },
  });
  await ctx.loadData();
  assert.equal(ready.settings, true);
  failing = true;
  await ctx.loadData({ silent: true, includeKey: false });
  assert.equal(ready.settings, true);
});
