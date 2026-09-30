const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function setup(fetch) {
  function element() {
    return { children: [], textContent: '', disabled: false, open: true,
      appendChild(child) { this.children.push(child); },
      replaceChildren() { this.children = []; },
    };
  }
  const elements = Object.fromEntries(['files-path', 'files-up', 'files-size', 'files-list', 'files-dialog'].map(id => [id, element()]));
  const messages = [];
  const ctx = vm.createContext({
    fetch, document: { getElementById: id => elements[id], createElement: element },
    currentUser: { is_admin: true },
    setStatus: (...args) => messages.push(args), setActionStatus: (...args) => messages.push(args),
    loadData: async () => {}, requestEdgeActiveRefreshBurst() {}, confirmApp: async () => true,
  });
  for (const file of ['utils.js', 'files.js']) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, '../edge/static/js', file), 'utf8'), ctx);
  }
  return { ctx, elements, messages, element };
}

test('file browser shows file sizes, calculates folder totals, and saves the exact exclusion path', async () => {
  const calls = [];
  const filename = "nested/<photo>[1]'s.jpg";
  let excluded = false;
  const { ctx, elements } = setup(async (url, options) => {
    calls.push([url, options]);
    if (url.includes('/exclude')) {
      assert.equal(JSON.parse(options.body).relative_path, filename);
      excluded = true;
      return { ok: true, json: async () => ({ status: 'ok' }) };
    }
    if (url.includes('/size')) return { ok: true, json: async () => ({ size: 2048, files: 2 }) };
    return { ok: true, json: async () => ({ entries: [
      { name: 'subfolder', relative_path: 'nested/subfolder', kind: 'directory', job_path: '.' },
      { name: "<photo>[1]'s.jpg", relative_path: filename, kind: 'file', size: 0, job_path: '.', excluded },
    ] }) };
  });
  await ctx.browseFiles('nested');
  let rows = elements['files-list'].children;
  assert.equal(rows[1].children[0].textContent, "<photo>[1]'s.jpg");
  assert.equal(rows[1].children[2].textContent, '0 B');
  const sizeButton = rows[0].children[2].children[0];
  await sizeButton.onclick();
  assert.equal(sizeButton.textContent, '2.0 KB (2 files)');
  await rows[1].children[4].children[0].onclick();
  rows = elements['files-list'].children;
  assert.equal(rows[1].children[4].children.length, 0);
  assert.ok(calls.some(([url]) => url.includes('size?relative_path=nested%2Fsubfolder')));
});

test('clear staged backup and cancellation use separate endpoints and report errors', async () => {
  const calls = [];
  const { ctx, messages, element } = setup(async (url, options) => {
    calls.push([url, options]);
    if (url.includes('clear-staged')) return { ok: false, json: async () => ({ detail: 'Backup is still running' }) };
    return { ok: true, json: async () => ({ status: 'cancelling' }) };
  });
  const btn = element();
  await ctx.clearStagedBackup('job', btn);
  assert.equal(messages.at(-1)[1], 'error');
  assert.equal(btn.disabled, false);
  await ctx.cancelOperation(btn);
  assert.deepEqual(calls.map(([url]) => url), ['/api/directories/clear-staged', '/api/cancel-operation']);
  assert.match(messages.at(-1)[0], /Cancellation requested/);
});
