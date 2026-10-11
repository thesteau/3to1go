const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

// Stubs for the features refresh.js renders into; tests override what they observe.
function refreshContext(overrides) {
  const ctx = vm.createContext({
    currentUser: { is_admin: true },
    window: {},
    document: { getElementById: () => null },
    setHtmlIfChanged() {},
    applyTheme() {},
    setActionStatus() {},
    setPanelReady() {},
    fillMetaFromDir() {},
    fillMetaEncKey() {},
    renderSelectedJobs() {},
    renderDirectoryTree() {},
    directoryTreeLoaded: () => true,
    reloadDirectoryTree: async () => {},
    loadRestoreRequests: async () => {},
    ...overrides,
  });
  loadFeature(ctx, "scout", "refresh");
  return ctx;
}

test("a reload shows the last jobs and folders at once, without settings, until sign-out", async () => {
  const stored = new Map();
  const sessionStorage = {
    getItem: (key) => stored.get(key) ?? null,
    setItem: (key, value) => stored.set(key, value),
    removeItem: (key) => stored.delete(key),
  };
  const jobs = [{ relative_path: "docs", selected: true, state: {} }];
  const folders = [{ relative_path: "docs", child_count: 0 }];
  const first = refreshContext({
    sessionStorage,
    directoryChildren: new Map([[".", folders]]),
    fetch: async (url) => ({
      ok: true,
      json: async () =>
        url === "/api/status"
          ? { settings: { scout_credential: "secret-jwt" } }
          : { directories: jobs, discovering: false },
    }),
  });
  await first.loadData({ refreshDirectoryTree: false });
  const saved = [...stored.values()].join("");
  assert.match(saved, /docs/);
  assert.doesNotMatch(saved, /secret-jwt/, "settings are not stored");

  // The next page load in this tab renders the stored view before any request.
  const rendered = [];
  let treeRendered = false;
  const second = refreshContext({
    sessionStorage,
    directoryChildren: new Map(),
    directoryTreeLoaded: () => false,
    renderSelectedJobs: (dirs, discovering) => rendered.push([dirs.length, discovering]),
    renderDirectoryTree: () => {
      treeRendered = true;
    },
  });
  second.restoreScoutView();
  assert.deepEqual(rendered, [[1, false]]);
  assert.equal(treeRendered, true);
  assert.equal(second.directoryChildren.get(".").length, 1);

  second.clearScoutView();
  assert.equal(stored.size, 0, "sign-out clears the stored view");
});

test("Scout keeps polling while idle, accelerates for work, and pauses for dialogs", async () => {
  let timer;
  let dialogOpen = false;
  const requests = [];
  const ctx = refreshContext({
    window: {
      setTimeout(callback, delay) {
        timer = { callback, delay };
        return 1;
      },
      clearTimeout() {},
    },
    document: { hidden: false, querySelector: () => (dialogOpen ? {} : null), getElementById: () => null },
    fetch: async (url) => {
      requests.push(url);
      return {
        ok: true,
        json: async () => (url === "/api/status" ? { scheduler: { state: "running" } } : { directories: [] }),
      };
    },
  });
  vm.runInContext(
    '_scoutAutoRefreshStarted = true; latestData = { scheduler: { state: "waiting" }, directories: [] }; scheduleScoutRefresh()',
    ctx,
  );
  assert.equal(timer.delay, 15000);
  timer.callback();
  await new Promise(setImmediate);
  assert.deepEqual(requests, ["/api/status", "/api/directories"]);
  assert.equal(timer.delay, 2500);
  dialogOpen = true;
  timer.callback();
  assert.equal(requests.length, 2);
  assert.equal(timer.delay, 2000);
  dialogOpen = false;
  ctx.document.hidden = true;
  timer.callback();
  assert.equal(requests.length, 2);
});

test("a refresh requested during a poll waits and fetches fresh data", async () => {
  const pendingDirectories = [];
  let directoryRequests = 0;
  const ctx = refreshContext({
    window: { setTimeout: () => 0, clearTimeout() {} },
    document: { getElementById: () => null, querySelector: () => null },
    fetch: async (url) => {
      if (url === "/api/directories") {
        directoryRequests++;
        return new Promise((resolve) => pendingDirectories.push(resolve));
      }
      return { ok: true, json: async () => ({ settings: { theme: "dark" } }) };
    },
  });
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

test("Scout renders status and key before slow job and folder loads complete", async () => {
  let finishDirectories;
  let finishTree;
  let treeLoaded = false;
  const rendered = [];
  const ready = {};
  const ctx = refreshContext({
    fetch: async (url) =>
      url === "/api/directories"
        ? new Promise((resolve) => {
            finishDirectories = resolve;
          })
        : {
            ok: true,
            json: async () =>
              url === "/api/status" ? { settings: { theme: "dark" } } : { key_base64: "key", fingerprint: "fp" },
          },
    setPanelReady: (name, value) => {
      ready[name] = value;
    },
    fillMetaFromDir: () => rendered.push("status"),
    fillMetaEncKey: () => rendered.push("key"),
    renderSelectedJobs: () => rendered.push("jobs"),
    directoryTreeLoaded: () => treeLoaded,
    reloadDirectoryTree: () =>
      new Promise((resolve) => {
        finishTree = resolve;
      }).then(() => {
        treeLoaded = true;
        rendered.push("tree");
      }),
  });
  const pending = ctx.loadData();
  await new Promise(setImmediate);
  assert.deepEqual(rendered, ["status", "key"]);
  assert.equal(ready.settings, true);
  finishTree();
  await new Promise(setImmediate);
  assert.deepEqual(rendered, ["status", "key", "tree"], "the folder tree does not wait for job discovery");
  finishDirectories({ ok: true, json: async () => ({ directories: [] }) });
  await pending;
  assert.deepEqual(rendered, ["status", "key", "tree", "jobs"]);
});

test("Scout polls skip the folder tree once it has loaded", async () => {
  const requests = [];
  let reloads = 0;
  const ctx = refreshContext({
    fetch: async (url) => {
      requests.push(url);
      return { ok: true, json: async () => ({ directories: [] }) };
    },
    reloadDirectoryTree: async () => {
      reloads++;
    },
  });
  await ctx.loadData({ silent: true, includeKey: false });
  assert.equal(reloads, 0);
  assert.deepEqual(requests, ["/api/status", "/api/directories"]);
  await ctx.loadData({ silent: true, includeKey: false, refreshDirectoryTree: true });
  assert.equal(reloads, 1);
});

test("each refresh checks restore requests after the job list, quietly when polling", async () => {
  const order = [];
  const ctx = refreshContext({
    fetch: async (url) => {
      order.push(url);
      return { ok: true, json: async () => ({ directories: [] }) };
    },
    renderSelectedJobs: () => order.push("jobs"),
    loadRestoreRequests: async (options) => order.push(["restore", options.silent]),
  });
  await ctx.loadData({ silent: true, includeKey: false });
  assert.deepEqual(order.slice(-2), ["jobs", ["restore", true]]);
  await ctx.loadData({ includeKey: false, refreshDirectoryTree: false });
  assert.deepEqual(order.at(-1), ["restore", false]);
});

test("Scout keeps loaded settings editable when a later poll fails", async () => {
  const ready = {};
  let failing = false;
  const ctx = refreshContext({
    fetch: async (url) => {
      if (failing) throw new Error("offline");
      return {
        ok: true,
        json: async () =>
          url === "/api/status"
            ? { settings: { theme: "dark" } }
            : url === "/api/directories"
              ? { directories: [] }
              : { key_base64: "k", fingerprint: "f" },
      };
    },
    setPanelReady: (name, value) => {
      ready[name] = value;
    },
  });
  await ctx.loadData();
  assert.equal(ready.settings, true);
  failing = true;
  await ctx.loadData({ silent: true, includeKey: false });
  assert.equal(ready.settings, true);
});
