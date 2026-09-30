const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

function loadPageScripts(app) {
  const base = path.join(__dirname, '..', app, 'static');
  const html = fs.readFileSync(path.join(base, 'index.html'), 'utf8');
  const ctx = vm.createContext({ window: { fetch() {} } });
  // App startup needs a live DOM; load its dependencies in the real page order.
  for (const [, file] of html.matchAll(/<script defer src="\/static\/js\/([^"]+)"/g)) {
    if (file === 'app.js') continue;
    vm.runInContext(fs.readFileSync(path.join(base, 'js', file), 'utf8'), ctx, { filename: file });
  }
  return { ctx, base, html };
}

// Browsers decode HTML entities before compiling event attributes.
function handlers(html) {
  const entities = { '&quot;': '"', '&#39;': "'", '&lt;': '<', '&gt;': '>', '&amp;': '&' };
  return [...html.matchAll(/onclick="([^"]*)"/g)].map(([, value]) =>
    value.replace(/&quot;|&#39;|&lt;|&gt;|&amp;/g, entity => entities[entity]));
}

for (const app of ['central', 'edge']) {
  test(`${app} page loads feature scripts and exposes its inline actions`, () => {
    const { ctx, base, html } = loadPageScripts(app);
    const templates = fs.readdirSync(path.join(base, 'html'))
      .filter(file => file.endsWith('.html'))
      .map(file => fs.readFileSync(path.join(base, 'html', file), 'utf8'));
    for (const handler of handlers([html, ...templates].join('\n'))) {
      const name = handler.match(/^\s*([A-Za-z_$][\w$]*)\(/)?.[1];
      if (name && name !== 'connectApp') assert.equal(typeof ctx[name], 'function', name);
    }
  });

  test(`${app} uploaded file actions preserve quotes and special characters`, () => {
    const { ctx } = loadPageScripts(app);
    for (const name of ["operator's.pem", 'quotes" & <日本語>.sh', "x');throw new Error('unexpected');//", 'back\\slash.sh']) {
      const html = ctx.renderHookFiles([{ name, viewable: true }]) + ctx.renderCertificateFiles([{ name }]);
      const calls = [];
      for (const action of ['viewHookFile', 'deleteHookFile', 'deleteCertificateFile']) {
        ctx[action] = value => calls.push(value);
      }
      for (const handler of handlers(html)) vm.runInContext(handler, ctx);
      assert.deepEqual(calls, [name, name, name]);
    }
  });
}

test('Central snapshot and instance actions preserve quoted identifiers', () => {
  const { ctx } = loadPageScripts('central');
  const edge = "edge's\\id";
  const instance = 'instance"<&';
  const job = "job');throw new Error('unexpected');//";
  const name = "snapshot's.tar.zst";
  const calls = [];
  for (const action of ['downloadSnapshot', 'deleteSnapshot', 'deleteInstance', 'revokeInstanceCredential', 'rememberEncKey', 'clearEncKey']) {
    ctx[action] = (...args) => calls.push([action, ...args.slice(0, action.endsWith('Snapshot') ? 4 : 2)]);
  }
  const html = ctx.renderInstanceCard(edge, {
    edge_instance_id: instance, credential_configured: true,
    jobs: [{ job_name: job, snapshots: [{ name }] }],
  });
  for (const handler of handlers(html)) vm.runInContext(handler, ctx);
  assert.equal(calls.length, 6);
  for (const [action, ...args] of calls) {
    assert.deepEqual(args, action.endsWith('Snapshot') ? [edge, instance, job, name] : [edge, instance]);
  }
});
