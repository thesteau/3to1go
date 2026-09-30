const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function load(ctx, file) {
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../edge/static/js', file), 'utf8'), ctx);
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

test('directory tree hides the scan root and starts every folder collapsed', () => {
  const tree = { innerHTML: '' };
  const ctx = vm.createContext({
    document: { getElementById: id => id === 'directory-tree' ? tree : null, querySelectorAll: () => [] },
    currentUser: { is_admin: true }, latestData: null,
  });
  load(ctx, 'utils.js');
  load(ctx, 'directories.js');
  ctx.renderDirectoryTree([
    { relative_path: '.', selected: false },
    { relative_path: 'music', selected: true },
    { relative_path: 'music/old', blocked_by_parent: 'music', excluded: true },
    { relative_path: 'music/new', blocked_by_parent: 'music' },
  ]);
  assert.doesNotMatch(tree.innerHTML, /Scan Root|data-path="\."/);
  assert.match(tree.innerHTML, /<details class="dir-branch" data-path="music">/);
  assert.match(tree.innerHTML, /class="dir-leaf dir-excluded" data-path="music\/old"/);
  assert.match(tree.innerHTML, /excluded from music/);
  const excludeButtons = tree.innerHTML.match(/Exclude folder/g) || [];
  assert.equal(excludeButtons.length, 1, 'only the folder that is not yet excluded offers Exclude');
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
    renderSelectedJobs() {}, renderDirectoryTree() {}, requestAnimationFrame: fn => fn(),
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
