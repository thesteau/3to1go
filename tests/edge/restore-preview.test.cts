const { loadFeature } = require('../helpers/scripts.cts');
const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');

function renderPreview(entries) {
  const list = { innerHTML: '' };
  const ctx = vm.createContext({ document: { getElementById: id => id === 'recover-preview-list' ? list : null } });
  loadFeature(ctx, 'edge', 'utils');
  loadFeature(ctx, 'edge', 'recovery');
  ctx.renderRecoverPreviewList(entries, { query: '', action: 'all' });
  return list.innerHTML;
}

test('restore preview keeps full folder totals when rows are capped', () => {
  const html = renderPreview([
    ...Array.from({ length: 450 }, (_, i) => ({ path: `a/${i}.txt`, size: 1, action: 'replace' })),
    ...Array.from({ length: 10 }, (_, i) => ({ path: `b/${i}.txt`, size: 2, action: 'add' })),
  ]);
  assert.equal((html.match(/class="recover-preview-row"/g) || []).length, 400);
  assert.match(html, /450 files · 450 B/);
  assert.match(html, /50 files in this folder not listed/);
  assert.match(html, /10 files · 20 B/, 'folders past the cap still show their totals');
  assert.match(html, /10 files in this folder not listed/);
});

test('restore preview caps rendered folders and summarizes the rest', () => {
  const html = renderPreview(Array.from({ length: 3000 }, (_, i) => ({ path: `dir${String(i).padStart(4, '0')}/f.txt`, size: 1, action: 'replace' })));
  assert.equal((html.match(/class="recover-preview-group"/g) || []).length, 150);
  assert.match(html, /2850 more folders with 2850 files · 2\.8 KB not listed/);
});
