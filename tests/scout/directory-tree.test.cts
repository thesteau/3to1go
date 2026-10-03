const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

function treeContext(fetch, latestData = null) {
  const tree = { innerHTML: "" };
  const details = [];
  const ctx = vm.createContext({
    document: {
      getElementById: (id) => (id === "directory-tree" ? tree : null),
      querySelectorAll: () => details,
    },
    currentUser: { is_admin: true },
    latestData,
    setActionStatus() {},
    fetch,
  });
  loadFeature(ctx, "scout", "utils");
  loadFeature(ctx, "scout", "directories");
  return { ctx, tree, details };
}

function lazyTreeContext(children, latestData = null) {
  const requests = [];
  const context = treeContext(async (url) => {
    const relativePath = decodeURIComponent(url.split("relative_path=")[1]);
    requests.push(relativePath);
    return { ok: true, json: async () => ({ directories: children[relativePath] || [] }) };
  }, latestData);
  return { ...context, requests };
}

// Simulates the user opening the rendered <details> for path.
function openFolder(ctx, details, path) {
  const listeners = {};
  details.push({
    dataset: { path },
    open: true,
    addEventListener: (name, fn) => {
      listeners[name] = fn;
    },
  });
  ctx.bindDirectoryTreeEvents();
  listeners.toggle();
  return listeners;
}

test("directory tree loads only the top level and starts every folder collapsed", async () => {
  const { ctx, tree, requests } = lazyTreeContext({
    ".": [
      { relative_path: "music", selected: true, child_count: 2 },
      { relative_path: "docs", child_count: 0 },
    ],
  });
  await ctx.reloadDirectoryTree();
  assert.deepEqual(requests, ["."]);
  assert.doesNotMatch(tree.innerHTML, /Scan Root|data-path="\."/);
  assert.match(tree.innerHTML, /<details class="dir-branch" data-path="music">/);
  assert.match(tree.innerHTML, /2 nested/);
  assert.match(tree.innerHTML, /class="dir-leaf" data-path="docs"/);
});

test("opening a folder fetches its children once and keeps it open across reloads", async () => {
  const { ctx, tree, details, requests } = lazyTreeContext({
    ".": [{ relative_path: "music", selected: true, child_count: 2 }],
    music: [
      { relative_path: "music/old", blocked_by_parent: "music", excluded: true },
      { relative_path: "music/new", blocked_by_parent: "music" },
    ],
  });
  await ctx.reloadDirectoryTree();
  const listeners = openFolder(ctx, details, "music");
  await new Promise(setImmediate);
  assert.deepEqual(requests, [".", "music"]);
  assert.match(tree.innerHTML, /<details class="dir-branch" data-path="music" open>/);
  assert.match(tree.innerHTML, /class="dir-leaf dir-excluded" data-path="music\/old"/);
  assert.match(tree.innerHTML, /excluded from music/);
  const excludeButtons = tree.innerHTML.match(/Exclude folder/g) || [];
  assert.equal(excludeButtons.length, 1, "only the folder that is not yet excluded offers Exclude");

  listeners.toggle();
  await new Promise(setImmediate);
  assert.deepEqual(requests, [".", "music"], "reopening uses the loaded children");
  await ctx.reloadDirectoryTree();
  assert.deepEqual(requests, [".", "music", ".", "music"], "a reload refreshes open folders only");
  assert.equal(ctx.findEntry("music/new").blocked_by_parent, "music");
});

test("a folder response requested before a tree reload cannot overwrite it", async () => {
  const pending = [];
  let parentIsJob = true;
  const { ctx, tree, details } = treeContext(async (url) => {
    const relativePath = decodeURIComponent(url.split("relative_path=")[1]);
    const body =
      relativePath === "."
        ? { directories: [{ relative_path: "music", selected: parentIsJob, child_count: 1 }] }
        : { directories: [{ relative_path: "music/old", blocked_by_parent: parentIsJob ? "music" : null }] };
    const response = { ok: true, json: async () => body };
    // Hold the first folder request open; answer everything else at once.
    if (relativePath === "music" && pending.length === 0)
      return new Promise((resolve) => pending.push(() => resolve(response)));
    return response;
  });
  await ctx.reloadDirectoryTree();
  openFolder(ctx, details, "music");
  await new Promise(setImmediate);

  parentIsJob = false;
  await ctx.reloadDirectoryTree();
  assert.equal(ctx.findEntry("music/old").blocked_by_parent, null);
  pending.shift()();
  await new Promise(setImmediate);
  assert.equal(ctx.findEntry("music/old").blocked_by_parent, null, "the stale response is discarded");
  assert.doesNotMatch(tree.innerHTML, /Covered by parent job/);
});

test("a folder opened during a tree reload keeps its children", async () => {
  let finishTopLevel;
  const { ctx, tree, details } = lazyTreeContext({
    ".": [{ relative_path: "music", child_count: 1 }],
    music: [{ relative_path: "music/new" }],
  });
  await ctx.reloadDirectoryTree();
  const originalFetch = ctx.fetch;
  ctx.fetch = async (url) =>
    url.endsWith("relative_path=.")
      ? new Promise((resolve) => {
          finishTopLevel = () => resolve(originalFetch(url));
        })
      : originalFetch(url);
  const reload = ctx.reloadDirectoryTree();
  openFolder(ctx, details, "music");
  await new Promise(setImmediate);
  finishTopLevel();
  await reload;
  assert.match(tree.innerHTML, /data-path="music\/new"/);
});

// Answers each folder request from `children` when the test releases it.
function heldTreeContext(children) {
  const held = [];
  const requests = [];
  const context = treeContext((url) => {
    const relativePath = decodeURIComponent(url.split("relative_path=")[1]);
    requests.push(relativePath);
    return new Promise((resolve) => held.push({ relativePath, resolve }));
  });
  const release = (relativePath, body = { directories: children[relativePath] || [] }, ok = true) => {
    const index = held.findIndex((request) => request.relativePath === relativePath);
    assert.notEqual(index, -1, `no pending request for ${relativePath}`);
    held.splice(index, 1)[0].resolve({ ok, json: async () => body });
  };
  return { ...context, requests, release };
}

const settle = () => new Promise(setImmediate);

test("a folder that fails to reload is fetched again when reopened", async () => {
  const { ctx, tree, details, requests, release } = heldTreeContext({
    ".": [{ relative_path: "music", child_count: 1 }],
    music: [{ relative_path: "music/old", blocked_by_parent: "music" }],
  });
  const first = ctx.reloadDirectoryTree();
  release(".");
  await first;
  const listeners = openFolder(ctx, details, "music");
  await settle();
  release("music");
  await settle();
  assert.match(tree.innerHTML, /Covered by parent job/);

  const failed = ctx.reloadDirectoryTree();
  await settle();
  release(".");
  release("music", { detail: "Folders could not load." }, false);
  await failed;
  assert.doesNotMatch(tree.innerHTML, /data-path="music" open/, "the folder closes");

  listeners.toggle();
  await settle();
  assert.deepEqual(requests, [".", "music", ".", "music", "music"], "reopening fetches the folder again");
  release("music", { directories: [{ relative_path: "music/old", blocked_by_parent: null }] });
  await settle();
  assert.doesNotMatch(tree.innerHTML, /Covered by parent job/);
});

test("a folder closed mid-request and reopened during a reload still loads", async () => {
  const { ctx, tree, details, requests, release } = heldTreeContext({
    ".": [{ relative_path: "music", child_count: 1 }],
    music: [{ relative_path: "music/new" }],
  });
  const first = ctx.reloadDirectoryTree();
  release(".");
  await first;
  const listeners = openFolder(ctx, details, "music");
  await settle();
  details[0].open = false;
  listeners.toggle();

  const reload = ctx.reloadDirectoryTree();
  await settle();
  details[0].open = true;
  listeners.toggle();
  await settle();
  assert.deepEqual(requests, [".", "music", ".", "music"], "reopening is not blocked by the older request");

  release(".");
  await reload;
  release("music", { directories: [{ relative_path: "music/stale" }] });
  await settle();
  assert.doesNotMatch(tree.innerHTML, /music\/stale/, "the older response is discarded");
  release("music");
  await settle();
  assert.match(tree.innerHTML, /data-path="music\/new"/);
  assert.doesNotMatch(tree.innerHTML, /Loading…/);
});

test("unopened folders show that they contain a selected job", async () => {
  const { ctx, tree } = lazyTreeContext(
    { ".": [{ relative_path: "media", child_count: 3, hidden_child_count: 1 }] },
    { directories: [{ relative_path: "media/photos", selected: true }] },
  );
  await ctx.reloadDirectoryTree();
  assert.match(tree.innerHTML, /contains selected job/);
  assert.match(tree.innerHTML, /2 nested/, "hidden folders are not counted until shown");
});
