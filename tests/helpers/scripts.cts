const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const loaded = new WeakMap<object, Set<string>>();

// Use the page's actual script order and paths, including shared dependencies.
// Tests may load a feature in isolation without running application startup.
function loadFeature(context: object, app: string, feature: string): void {
  const base = path.join(__dirname, '../..', app, 'static');
  const html = fs.readFileSync(path.join(base, 'index.html'), 'utf8');
  const files = [...html.matchAll(/<script defer src="\/static\/js\/([^"]+)"/g)]
    .map(match => match[1])
    .filter(file => path.basename(file) === `${feature}.js`);
  if (!files.length) throw new Error(`No scripts for ${app}/${feature}`);
  let seen = loaded.get(context);
  if (!seen) loaded.set(context, seen = new Set());
  for (const file of files) {
    const filename = path.join(base, 'js', file);
    if (seen.has(filename)) continue;
    vm.runInContext(fs.readFileSync(filename, 'utf8'), context, { filename });
    seen.add(filename);
  }
}

module.exports = { loadFeature };
