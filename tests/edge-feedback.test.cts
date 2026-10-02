const { loadFeature } = require('./helpers/scripts.cts');
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function load(ctx, file) {
  loadFeature(ctx, 'edge', file.replace(/\.js$/, ''));
}

function element() {
  return { children: [], textContent: '', disabled: false, open: true,
    appendChild(child) { this.children.push(child); },
    replaceChildren() { this.children = []; },
  };
}

test('excluding a file shows progress, then updates its row without reloading the folder', async () => {
  let finishExclude;
  let browses = 0;
  const messages = [];
  const elements = Object.fromEntries(['files-path', 'files-up', 'files-size', 'files-list', 'files-dialog'].map(id => [id, element()]));
  const ctx = vm.createContext({
    document: { getElementById: id => elements[id], createElement: element },
    currentUser: { is_admin: true }, loadData: async () => {},
    setStatus() {}, setActionStatus: (...args) => messages.push(args),
    fetch: async (url) => {
      if (url.includes('/exclude')) return new Promise(resolve => { finishExclude = resolve; });
      browses++;
      return { ok: true, json: async () => ({ entries: [{ name: 'a.log', relative_path: 'job/a.log', kind: 'file', size: 1, job_path: 'job' }] }) };
    },
  });
  load(ctx, 'utils.js');
  load(ctx, 'files.js');
  await ctx.browseFiles('job');
  const row = elements['files-list'].children[0];
  const button = row.children[4].children[0];
  const pending = button.onclick();
  assert.equal(button.textContent, 'Excluding…');
  assert.equal(button.disabled, true);
  finishExclude({ ok: true, json: async () => ({ status: 'ok' }) });
  await pending;
  assert.equal(row.children[3].textContent, 'Excluded by job settings');
  assert.equal(row.children[4].children.length, 0);
  assert.equal(browses, 1);
  assert.deepEqual(messages.map(([, kind]) => kind), ['success']);
});

function lazyTreeContext(children, latestData = null) {
  const tree = { innerHTML: '' };
  const details = [];
  const requests = [];
  const ctx = vm.createContext({
    document: {
      getElementById: id => id === 'directory-tree' ? tree : null,
      querySelectorAll: () => details,
    },
    currentUser: { is_admin: true }, latestData, setActionStatus() {},
    fetch: async url => {
      const relativePath = decodeURIComponent(url.split('relative_path=')[1]);
      requests.push(relativePath);
      return { ok: true, json: async () => ({ directories: children[relativePath] || [] }) };
    },
  });
  load(ctx, 'utils.js');
  load(ctx, 'directories.js');
  return { ctx, tree, details, requests };
}

test('directory tree loads only the top level and starts every folder collapsed', async () => {
  const { ctx, tree, requests } = lazyTreeContext({
    '.': [
      { relative_path: 'music', selected: true, child_count: 2 },
      { relative_path: 'docs', child_count: 0 },
    ],
  });
  await ctx.reloadDirectoryTree();
  assert.deepEqual(requests, ['.']);
  assert.doesNotMatch(tree.innerHTML, /Scan Root|data-path="\."/);
  assert.match(tree.innerHTML, /<details class="dir-branch" data-path="music">/);
  assert.match(tree.innerHTML, /2 nested/);
  assert.match(tree.innerHTML, /class="dir-leaf" data-path="docs"/);
});

test('opening a folder fetches its children once and keeps it open across reloads', async () => {
  const { ctx, tree, details, requests } = lazyTreeContext({
    '.': [{ relative_path: 'music', selected: true, child_count: 2 }],
    music: [
      { relative_path: 'music/old', blocked_by_parent: 'music', excluded: true },
      { relative_path: 'music/new', blocked_by_parent: 'music' },
    ],
  });
  await ctx.reloadDirectoryTree();
  const listeners = {};
  details.push({ dataset: { path: 'music' }, open: true, addEventListener: (name, fn) => { listeners[name] = fn; } });
  ctx.bindDirectoryTreeEvents();
  listeners.toggle();
  await new Promise(setImmediate);
  assert.deepEqual(requests, ['.', 'music']);
  assert.match(tree.innerHTML, /<details class="dir-branch" data-path="music" open>/);
  assert.match(tree.innerHTML, /class="dir-leaf dir-excluded" data-path="music\/old"/);
  assert.match(tree.innerHTML, /excluded from music/);
  const excludeButtons = tree.innerHTML.match(/Exclude folder/g) || [];
  assert.equal(excludeButtons.length, 1, 'only the folder that is not yet excluded offers Exclude');

  listeners.toggle();
  await new Promise(setImmediate);
  assert.deepEqual(requests, ['.', 'music'], 'reopening uses the loaded children');
  await ctx.reloadDirectoryTree();
  assert.deepEqual(requests, ['.', 'music', '.', 'music'], 'a reload refreshes open folders only');
  assert.equal(ctx.findEntry('music/new').blocked_by_parent, 'music');
});

test('a folder response requested before a tree reload cannot overwrite it', async () => {
  const pending = [];
  const tree = { innerHTML: '' };
  const details = [];
  let parentIsJob = true;
  const ctx = vm.createContext({
    document: { getElementById: id => id === 'directory-tree' ? tree : null, querySelectorAll: () => details },
    currentUser: { is_admin: true }, latestData: null, setActionStatus() {},
    fetch: async url => {
      const relativePath = decodeURIComponent(url.split('relative_path=')[1]);
      const body = relativePath === '.'
        ? { directories: [{ relative_path: 'music', selected: parentIsJob, child_count: 1 }] }
        : { directories: [{ relative_path: 'music/old', blocked_by_parent: parentIsJob ? 'music' : null }] };
      const response = { ok: true, json: async () => body };
      // Hold the first folder request open; answer everything else at once.
      if (relativePath === 'music' && pending.length === 0) return new Promise(resolve => pending.push(() => resolve(response)));
      return response;
    },
  });
  load(ctx, 'utils.js');
  load(ctx, 'directories.js');
  await ctx.reloadDirectoryTree();
  const listeners = {};
  details.push({ dataset: { path: 'music' }, open: true, addEventListener: (name, fn) => { listeners[name] = fn; } });
  ctx.bindDirectoryTreeEvents();
  listeners.toggle();
  await new Promise(setImmediate);

  parentIsJob = false;
  await ctx.reloadDirectoryTree();
  assert.equal(ctx.findEntry('music/old').blocked_by_parent, null);
  pending.shift()();
  await new Promise(setImmediate);
  assert.equal(ctx.findEntry('music/old').blocked_by_parent, null, 'the stale response is discarded');
  assert.doesNotMatch(tree.innerHTML, /Covered by parent job/);
});

test('a folder opened during a tree reload keeps its children', async () => {
  let finishTopLevel;
  const { ctx, tree, details } = lazyTreeContext({
    '.': [{ relative_path: 'music', child_count: 1 }],
    music: [{ relative_path: 'music/new' }],
  });
  await ctx.reloadDirectoryTree();
  const originalFetch = ctx.fetch;
  ctx.fetch = async url => url.endsWith('relative_path=.')
    ? new Promise(resolve => { finishTopLevel = () => resolve(originalFetch(url)); })
    : originalFetch(url);
  const reload = ctx.reloadDirectoryTree();
  const listeners = {};
  details.push({ dataset: { path: 'music' }, open: true, addEventListener: (name, fn) => { listeners[name] = fn; } });
  ctx.bindDirectoryTreeEvents();
  listeners.toggle();
  await new Promise(setImmediate);
  finishTopLevel();
  await reload;
  assert.match(tree.innerHTML, /data-path="music\/new"/);
});

test('unopened folders show that they contain a selected job', async () => {
  const { ctx, tree } = lazyTreeContext(
    { '.': [{ relative_path: 'media', child_count: 3, hidden_child_count: 1 }] },
    { directories: [{ relative_path: 'media/photos', selected: true }] },
  );
  await ctx.reloadDirectoryTree();
  assert.match(tree.innerHTML, /contains selected job/);
  assert.match(tree.innerHTML, /2 nested/, 'hidden folders are not counted until shown');
});

test('a refresh requested during a poll waits and fetches fresh data', async () => {
  const pendingDirectories = [];
  let directoryRequests = 0;
  const ctx = vm.createContext({
    window: { setTimeout: () => 0, clearTimeout() {} }, document: { getElementById: () => null, querySelector: () => null },
    fetch: async url => {
      if (url === '/api/directories') {
        directoryRequests++;
        return new Promise(resolve => pendingDirectories.push(resolve));
      }
      return { ok: true, json: async () => ({ settings: { theme: 'dark' } }) };
    },
    setHtmlIfChanged() {}, applyTheme() {}, setActionStatus() {}, setPanelReady() {}, fillMetaFromDir() {}, fillMetaEncKey() {},
    renderSelectedJobs() {}, renderDirectoryTree() {}, directoryTreeLoaded: () => true, reloadDirectoryTree: async () => {},
  });
  load(ctx, 'refresh.js');
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

test('restore preview keeps full folder totals when rows are capped', () => {
  const list = { innerHTML: '' };
  const ctx = vm.createContext({ document: { getElementById: id => id === 'recover-preview-list' ? list : null } });
  load(ctx, 'utils.js');
  load(ctx, 'recovery.js');
  const entries = [
    ...Array.from({ length: 450 }, (_, i) => ({ path: `a/${i}.txt`, size: 1, action: 'replace' })),
    ...Array.from({ length: 10 }, (_, i) => ({ path: `b/${i}.txt`, size: 2, action: 'add' })),
  ];
  ctx.renderRecoverPreviewList(entries, { query: '', action: 'all' });
  assert.equal((list.innerHTML.match(/class="recover-preview-row"/g) || []).length, 400);
  assert.match(list.innerHTML, /450 files · 450 B/);
  assert.match(list.innerHTML, /50 files in this folder not listed/);
  assert.match(list.innerHTML, /10 files · 20 B/, 'folders past the cap still show their totals');
  assert.match(list.innerHTML, /10 files in this folder not listed/);
});

test('restore preview caps rendered folders and summarizes the rest', () => {
  const list = { innerHTML: '' };
  const ctx = vm.createContext({ document: { getElementById: id => id === 'recover-preview-list' ? list : null } });
  load(ctx, 'utils.js');
  load(ctx, 'recovery.js');
  const entries = Array.from({ length: 3000 }, (_, i) => ({ path: `dir${String(i).padStart(4, '0')}/f.txt`, size: 1, action: 'replace' }));
  ctx.renderRecoverPreviewList(entries, { query: '', action: 'all' });
  assert.equal((list.innerHTML.match(/class="recover-preview-group"/g) || []).length, 150);
  assert.match(list.innerHTML, /2850 more folders with 2850 files · 2\.8 KB not listed/);
});
