const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

function element() {
  return {
    children: [],
    parent: null,
    textContent: "",
    disabled: false,
    open: true,
    style: {},
    attributes: {},
    setAttribute(name, value) {
      this.attributes[name] = value;
    },
    appendChild(child) {
      child.parent = this;
      this.children.push(child);
    },
    replaceChildren() {
      this.children = [];
    },
    after(...nodes) {
      const siblings = this.parent.children;
      for (const node of nodes) node.parent = this.parent;
      siblings.splice(siblings.indexOf(this) + 1, 0, ...nodes);
    },
    remove() {
      const siblings = this.parent.children;
      siblings.splice(siblings.indexOf(this), 1);
    },
  };
}

function fileBrowserContext(fetch) {
  const elements = Object.fromEntries(
    ["files-path", "files-size", "files-list", "files-dialog"].map((id) => [id, element()]),
  );
  const messages = [];
  const actions = [];
  const ctx = vm.createContext({
    fetch,
    document: { getElementById: (id) => elements[id], createElement: element },
    currentUser: { is_admin: true },
    setStatus: (...args) => messages.push(args),
    setActionStatus: (...args) => {
      messages.push(args);
      actions.push(args);
    },
    loadData: async () => {},
    requestScoutActiveRefreshBurst() {},
    confirmApp: async () => true,
  });
  loadFeature(ctx, "scout", "utils");
  loadFeature(ctx, "scout", "files");
  return { ctx, elements, messages, actions };
}

test("file browser shows file sizes, calculates folder totals, and saves the exact exclusion path", async () => {
  const calls = [];
  const filename = "nested/<photo>[1]'s.jpg";
  let excluded = false;
  const { ctx, elements } = fileBrowserContext(async (url, options) => {
    calls.push([url, options]);
    if (url.includes("/exclude")) {
      assert.equal(JSON.parse(options.body).relative_path, filename);
      excluded = true;
      return { ok: true, json: async () => ({ status: "ok" }) };
    }
    if (url.includes("/size")) return { ok: true, json: async () => ({ size: 2048, files: 2 }) };
    return {
      ok: true,
      json: async () => ({
        entries: [
          { name: "subfolder", relative_path: "nested/subfolder", kind: "directory", job_path: "." },
          { name: "<photo>[1]'s.jpg", relative_path: filename, kind: "file", size: 0, job_path: ".", excluded },
        ],
      }),
    };
  });
  await ctx.browseFiles("nested");
  let rows = elements["files-list"].children;
  assert.equal(rows[1].children[0].textContent, "<photo>[1]'s.jpg");
  assert.equal(rows[1].children[2].textContent, "0 B");
  const sizeButton = rows[0].children[2].children[0];
  await sizeButton.onclick();
  assert.equal(sizeButton.textContent, "2.0 KB (2 files)");
  await rows[1].children[4].children[0].onclick();
  rows = elements["files-list"].children;
  assert.equal(rows[1].children[4].children.length, 0);
  assert.ok(calls.some(([url]) => url.includes("size?relative_path=nested%2Fsubfolder")));
});

test("clicking a folder expands its contents below it, and clicking again collapses them", async () => {
  const listings = {
    job: [
      { name: "docs", relative_path: "job/docs", kind: "directory", job_path: "job" },
      { name: "z.txt", relative_path: "job/z.txt", kind: "file", size: 1, job_path: "job" },
    ],
    "job/docs": [
      { name: "drafts", relative_path: "job/docs/drafts", kind: "directory", job_path: "job" },
      { name: "a.txt", relative_path: "job/docs/a.txt", kind: "file", size: 2, job_path: "job" },
    ],
    "job/docs/drafts": [
      { name: "b.txt", relative_path: "job/docs/drafts/b.txt", kind: "file", size: 3, job_path: "job" },
    ],
  };
  const { ctx, elements } = fileBrowserContext(async (url) => {
    const path = decodeURIComponent(url.split("relative_path=")[1]);
    return { ok: true, json: async () => ({ entries: listings[path] }) };
  });
  const names = () =>
    elements["files-list"].children.map(
      (row) => row.children[0].children[0]?.textContent ?? row.children[0].textContent,
    );
  await ctx.browseFiles("job");
  assert.deepEqual(names(), ["▸ docs", "z.txt"]);

  const docs = elements["files-list"].children[0].children[0].children[0];
  await docs.onclick();
  assert.deepEqual(names(), ["▾ docs", "▸ drafts", "a.txt", "z.txt"]);
  assert.equal(docs.attributes["aria-expanded"], "true");
  assert.equal(elements["files-list"].children[1].children[0].style.paddingLeft, "2rem");

  await elements["files-list"].children[1].children[0].children[0].onclick();
  assert.deepEqual(names(), ["▾ docs", "▾ drafts", "b.txt", "a.txt", "z.txt"]);

  // Collapsing a folder also collapses everything expanded inside it.
  await docs.onclick();
  assert.deepEqual(names(), ["▸ docs", "z.txt"]);
  assert.equal(docs.attributes["aria-expanded"], "false");
  await docs.onclick();
  assert.deepEqual(names(), ["▾ docs", "▸ drafts", "a.txt", "z.txt"]);
});

test("excluding a file shows progress, then updates its row without reloading the folder", async () => {
  let finishExclude;
  let browses = 0;
  const { ctx, elements, actions } = fileBrowserContext(async (url) => {
    if (url.includes("/exclude"))
      return new Promise((resolve) => {
        finishExclude = resolve;
      });
    browses++;
    return {
      ok: true,
      json: async () => ({
        entries: [{ name: "a.log", relative_path: "job/a.log", kind: "file", size: 1, job_path: "job" }],
      }),
    };
  });
  await ctx.browseFiles("job");
  const row = elements["files-list"].children[0];
  const button = row.children[4].children[0];
  const pending = button.onclick();
  assert.equal(button.textContent, "Excluding…");
  assert.equal(button.disabled, true);
  finishExclude({ ok: true, json: async () => ({ status: "ok" }) });
  await pending;
  assert.equal(row.children[3].textContent, "Excluded by job settings");
  assert.equal(row.children[4].children.length, 0);
  assert.equal(browses, 1);
  assert.deepEqual(
    actions.map(([, kind]) => kind),
    ["success"],
  );
});
