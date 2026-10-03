const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

for (const app of ["edge", "central"]) {
  test(`${app} Admin shows the version and links to the docs`, async () => {
    const dialog = fs.readFileSync(path.join(__dirname, "../..", app, "static/html/users-dialog.html"), "utf8");
    assert.match(dialog, /id="build-info"/);
    assert.match(dialog, /href="https:\/\/3to1go\.docs\.thesteau\.com\/"/);

    const buildInfo = { textContent: "", title: "" };
    const sha = "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b";
    let build = { version: "v1.2.0", commit: sha, summary: "v1.2.0" };
    const ctx = vm.createContext({
      document: {
        getElementById: (id: string) => (id === "build-info" ? buildInfo : { hidden: false, innerHTML: "" }),
      },
      fetch: async () => ({ ok: true, json: async () => ({ users: [], build }) }),
      currentUser: null,
      setStatus: (_id: string, message: string) => {
        throw new Error(message);
      },
    });
    loadFeature(ctx, app, "utils");
    loadFeature(ctx, app, "users");

    await ctx.loadUsers();
    assert.equal(buildInfo.textContent, "Version v1.2.0");
    assert.equal(buildInfo.title, `Commit ${sha}`);

    build = { version: "", commit: sha, summary: "1a2b3c4" };
    await ctx.loadUsers();
    assert.equal(buildInfo.textContent, "Version 1a2b3c4", "main builds show the commit hash");
  });
}
