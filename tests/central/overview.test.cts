const { loadFeature } = require('../helpers/scripts.cts');
const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');

// rendered: whether the namespaces list already shows a previous overview.
function overviewContext(fetch, { rendered = false, ...overrides } = {}) {
  const messages = [];
  const elements = { meta: {}, 'storage-meta': {}, namespaces: { children: rendered ? [{}] : [] } };
  const ctx = vm.createContext({
    window: {}, document: { getElementById: id => elements[id] || null, querySelectorAll: () => [] },
    fetch, setActionStatus: (...args) => messages.push(args),
    setPanelReady() {}, applyTheme() {}, renderHelpHint: () => '', fillSettings() {},
    ...overrides,
  });
  for (const feature of ['utils', 'keys', 'verification', 'overview']) {
    loadFeature(ctx, 'central', feature);
  }
  ctx.updateOverviewDom = () => {};
  return { ctx, elements, messages };
}

test('Central reports failure without a success toast for HTTP and network errors', async () => {
  for (const fetch of [async () => ({ ok: false }), async () => { throw new Error('Network unavailable'); }]) {
    const { ctx, messages } = overviewContext(fetch, { rendered: true });
    await ctx.manualRefresh();
    assert.equal(messages.length, 1);
    assert.equal(messages[0][1], 'error');
  }
});

test('Central reports success only after a successful overview refresh', async () => {
  const { ctx, messages } = overviewContext(async () => ({ ok: true, json: async () => ({ edges: [], settings: {} }) }), { rendered: true });
  await ctx.manualRefresh();
  assert.deepEqual(messages, [['Refreshed.', 'success']]);
});

test('Central does not claim success when a refresh is already in progress', async () => {
  let finish;
  const { ctx, messages } = overviewContext(() => new Promise(resolve => { finish = resolve; }), { rendered: true });
  const first = ctx.loadOverview({ silent: true });
  await ctx.manualRefresh();
  assert.deepEqual(messages, []);
  finish({ ok: false });
  assert.equal(await first, false);
});

test('Central renders snapshots while storage probes remain pending', async () => {
  let finishStorage;
  let rendered = false;
  const { ctx, elements } = overviewContext(async url => url.includes('section=storage')
    ? new Promise(resolve => { finishStorage = resolve; })
    : { ok: true, json: async () => ({ edges: [], settings: { theme: 'dark' } }) });
  ctx.updateOverviewDom = () => { rendered = true; };
  ctx.loadVerifyStatus = () => {};
  assert.equal(await ctx.loadOverview(), true);
  assert.equal(rendered, true);
  assert.equal(elements['storage-meta'].innerHTML, undefined);
  finishStorage({ ok: true, json: async () => ({ status: 'ok', disk_total_bytes: 1024 }) });
  await new Promise(setImmediate);
  assert.match(elements['storage-meta'].innerHTML, /1.0 KB/);
});

test('Central settings unlock without waiting for the snapshot list', async () => {
  const ready = {};
  let finishSnapshots;
  const { ctx } = overviewContext(async url => {
    if (url.includes('section=settings')) return { ok: true, json: async () => ({ settings: { theme: 'dark' } }) };
    if (url.includes('section=snapshots')) return new Promise(resolve => { finishSnapshots = resolve; });
    return { ok: true, json: async () => ({}) };
  }, { setPanelReady: (name, value) => { ready[name] = value; } });
  ctx.loadVerifyStatus = () => {};
  const overview = ctx.loadOverview();
  await new Promise(setImmediate);
  assert.equal(ready.settings, true, 'settings are editable while snapshots load');
  finishSnapshots({ ok: true, json: async () => ({ edges: [] }) });
  assert.equal(await overview, true);
});

test('Central keeps loaded settings editable when a later poll fails', async () => {
  const ready = {};
  let failing = false;
  const { ctx } = overviewContext(async () => {
    if (failing) throw new Error('offline');
    return { ok: true, json: async () => ({ edges: [], settings: { theme: 'dark' } }) };
  }, { setPanelReady: (name, value) => { ready[name] = value; } });
  ctx.loadVerifyStatus = () => {};
  assert.equal(await ctx.loadOverview(), true);
  await new Promise(setImmediate);
  failing = true;
  assert.equal(await ctx.loadOverview({ silent: true }), false);
  await new Promise(setImmediate);
  assert.equal(ready.settings, true);
});
