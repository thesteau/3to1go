const { loadFeature } = require('../helpers/scripts.cts');
const { test } = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');

for (const app of ['edge', 'central']) {
  test(`${app} cannot save unloaded settings and unlocks controls only for ready panels`, async () => {
    const settingsButton = { disabled: true };
    const keyButton = { disabled: true };
    let fetches = 0;
    const ctx = vm.createContext({
      document: { querySelectorAll: selector => selector.includes('encryption-key') ? [keyButton] : [settingsButton] },
      fetch: async () => { fetches++; throw new Error('Unexpected request'); },
    });
    for (const feature of ['ui', 'admin', 'notifications', 'certificates', 'hooks']) {
      loadFeature(ctx, app, feature);
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
    loadFeature(ctx, app, 'ui');
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
  loadFeature(ctx, 'edge', 'ui');
  loadFeature(ctx, 'edge', 'meta');
  ctx.setActionStatus = () => {};
  await ctx.rotateEncKey();
  assert.equal(requests, 0);
});
