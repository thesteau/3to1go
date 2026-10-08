const fs = require("node:fs");
const path = require("node:path");

// These classic browser scripts are build output, just like the compiled UI.
// Serve pinned npm packages locally so opening a snapshot never needs a CDN.
const dependencies = path.resolve(__dirname, "../../node_modules");
const destination = path.join(__dirname, "js/vendor");
fs.mkdirSync(destination, { recursive: true });
for (const [source, target] of [
  ["fzstd/umd/index.js", "fzstd.js"],
  ["fzstd/LICENSE", "fzstd.LICENSE"],
  ["@zip.js/zip.js/dist/zip-native.min.js", "zip.js"],
  ["@zip.js/zip.js/LICENSE", "zip.LICENSE"],
]) {
  fs.copyFileSync(path.join(dependencies, source), path.join(destination, target));
}
