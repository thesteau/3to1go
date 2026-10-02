const { loadFeature } = require('./helpers/scripts.cts');
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function load(ctx, file) {
  const [app, feature] = file.match(/^(central|edge)\/static\/js\/(.+)\.js$/).slice(1);
  loadFeature(ctx, app, feature);
}

test('Edge renders status and key before slow job and folder loads complete', async () => {
  let finishDirectories;
  let finishTree;
  let treeLoaded = false;
  const rendered = [];
  const ready = {};
  const ctx = vm.createContext({
    window: {}, document: { getElementById: () => null },
    fetch: async url => url === '/api/directories'
      ? new Promise(resolve => { finishDirectories = resolve; })
      : { ok: true, json: async () => url === '/api/status' ? { settings: { theme: 'dark' } } : { key_base64: 'key', fingerprint: 'fp' } },
    setHtmlIfChanged() {}, applyTheme() {}, setActionStatus() {},
    setPanelReady: (name, value) => { ready[name] = value; },
    fillMetaFromDir: () => rendered.push('status'),
    fillMetaEncKey: () => rendered.push('key'),
    renderSelectedJobs: () => rendered.push('jobs'),
    renderDirectoryTree() {},
    directoryTreeLoaded: () => treeLoaded,
    reloadDirectoryTree: () => new Promise(resolve => { finishTree = resolve; }).then(() => { treeLoaded = true; rendered.push('tree'); }),
  });
  load(ctx, 'edge/static/js/refresh.js');
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
  const ctx = vm.createContext({
    window: {}, document: { getElementById: () => null },
    fetch: async url => { requests.push(url); return { ok: true, json: async () => ({ directories: [] }) }; },
    setHtmlIfChanged() {}, applyTheme() {}, setActionStatus() {}, setPanelReady() {}, fillMetaFromDir() {},
    renderSelectedJobs() {}, renderDirectoryTree() {}, directoryTreeLoaded: () => true,
    reloadDirectoryTree: async () => { reloads++; },
  });
  load(ctx, 'edge/static/js/refresh.js');
  await ctx.loadData({ silent: true, includeKey: false });
  assert.equal(reloads, 0);
  assert.deepEqual(requests, ['/api/status', '/api/directories']);
  await ctx.loadData({ silent: true, includeKey: false, refreshDirectoryTree: true });
  assert.equal(reloads, 1);
});

test('Central renders snapshots while storage probes remain pending', async () => {
  let finishStorage;
  let rendered = false;
  const elements = { meta: {}, 'storage-meta': {}, namespaces: { children: [] } };
  const ctx = vm.createContext({
    window: {}, document: { getElementById: id => elements[id] || null, querySelectorAll: () => [] },
    fetch: async url => url.includes('section=storage')
      ? new Promise(resolve => { finishStorage = resolve; })
      : { ok: true, json: async () => ({ edges: [], settings: { theme: 'dark' } }) },
    setActionStatus() {}, setPanelReady() {}, applyTheme() {}, renderHelpHint: () => '', fillSettings() {},
  });
  load(ctx, 'central/static/js/utils.js');
  load(ctx, 'central/static/js/keys.js');
  load(ctx, 'central/static/js/verification.js');
  load(ctx, 'central/static/js/overview.js');
  ctx.updateOverviewDom = () => { rendered = true; };
  ctx.loadVerifyStatus = () => {};
  assert.equal(await ctx.loadOverview(), true);
  assert.equal(rendered, true);
  assert.equal(elements['storage-meta'].innerHTML, undefined);
  finishStorage({ ok: true, json: async () => ({ status: 'ok', disk_total_bytes: 1024 }) });
  await new Promise(setImmediate);
  assert.match(elements['storage-meta'].innerHTML, /1.0 KB/);
});

for (const app of ['edge', 'central']) {
  test(`${app} cannot save unloaded settings and unlocks controls only for ready panels`, async () => {
    const settingsButton = { disabled: true };
    const keyButton = { disabled: true };
    let fetches = 0;
    const ctx = vm.createContext({
      document: { querySelectorAll: selector => selector.includes('encryption-key') ? [keyButton] : [settingsButton] },
      fetch: async () => { fetches++; throw new Error('Unexpected request'); },
    });
    load(ctx, `${app}/static/js/ui.js`);
    for (const file of ["admin", "notifications", "certificates", "hooks"]) {
      load(ctx, `${app}/static/js/${file}.js`);
    }
    ctx.setActionStatus = () => {};
    await ctx.saveSettings();
    assert.equal(fetches, 0);
    ctx.setPanelReady('settings', true);
    assert.equal(settingsButton.disabled, false);
    assert.equal(keyButton.disabled, true);
    ctx.setPanelReady('settings', false);
    assert.equal(settingsButton.disabled, true);
    await ctx.saveSettings();
    assert.equal(fetches, 0);
  });
}

test('Edge keeps loaded settings editable when a later poll fails', async () => {
  const ready = {};
  let failing = false;
  const ctx = vm.createContext({
    window: {}, document: { getElementById: () => null },
    fetch: async url => {
      if (failing) throw new Error('offline');
      return { ok: true, json: async () => url === '/api/status' ? { settings: { theme: 'dark' } } : url === '/api/directories' ? { directories: [] } : { key_base64: 'k', fingerprint: 'f' } };
    },
    setHtmlIfChanged() {}, applyTheme() {}, setActionStatus() {}, fillMetaFromDir() {}, fillMetaEncKey() {},
    renderSelectedJobs() {}, renderDirectoryTree() {}, directoryTreeLoaded: () => true, reloadDirectoryTree: async () => {},
    setPanelReady: (name, value) => { ready[name] = value; },
  });
  load(ctx, 'edge/static/js/refresh.js');
  await ctx.loadData();
  assert.equal(ready.settings, true);
  failing = true;
  await ctx.loadData({ silent: true, includeKey: false });
  assert.equal(ready.settings, true);
});

test('Central keeps loaded settings editable when a later poll fails', async () => {
  const ready = {};
  let failing = false;
  const elements = { meta: {}, 'storage-meta': {}, namespaces: { children: [] } };
  const ctx = vm.createContext({
    window: {}, document: { getElementById: id => elements[id] || null, querySelectorAll: () => [] },
    fetch: async () => {
      if (failing) throw new Error('offline');
      return { ok: true, json: async () => ({ edges: [], settings: { theme: 'dark' } }) };
    },
    setActionStatus() {}, applyTheme() {}, renderHelpHint: () => '', fillSettings() {},
    setPanelReady: (name, value) => { ready[name] = value; },
  });
  load(ctx, 'central/static/js/utils.js');
  load(ctx, 'central/static/js/keys.js');
  load(ctx, 'central/static/js/verification.js');
  load(ctx, 'central/static/js/overview.js');
  ctx.updateOverviewDom = () => {};
  ctx.loadVerifyStatus = () => {};
  assert.equal(await ctx.loadOverview(), true);
  failing = true;
  assert.equal(await ctx.loadOverview({ silent: true }), false);
  assert.equal(ready.settings, true);
});

for (const app of ['edge', 'central']) {
  test(`${app} editor panels that fail to load offer a working retry`, async () => {
    const control = { disabled: true };
    const status = {
      children: [], innerHTML: '', textContent: '',
      replaceChildren() { this.children = []; this.textContent = ''; },
      appendChild(child) { this.children.push(child); },
    };
    const ctx = vm.createContext({
      document: {
        getElementById: id => id === 'ntfy-load-status' ? status : null,
        querySelectorAll: () => [control],
        createElement: () => ({}),
      },
    });
    load(ctx, `${app}/static/js/ui.js`);
    let attempts = 0;
    const task = async () => { if (++attempts === 1) throw new Error('down'); return 'ok'; };
    await assert.rejects(ctx.loadEditorPanel('ntfy', task));
    assert.equal(control.disabled, true);
    assert.match(status.textContent, /Could not load/);
    const retry = status.children.find(child => child.textContent === 'Retry');
    await retry.onclick();
    assert.equal(control.disabled, false);
    assert.equal(attempts, 2);
  });
}

test('Edge cannot rotate a key before it has loaded', async () => {
  let requests = 0;
  const ctx = vm.createContext({ fetch: () => requests++, document: { querySelectorAll: () => [] } });
  load(ctx, 'edge/static/js/ui.js');
  load(ctx, 'edge/static/js/meta.js');
  ctx.setActionStatus = () => {};
  await ctx.rotateEncKey();
  assert.equal(requests, 0);
});
