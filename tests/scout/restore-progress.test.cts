const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

function progressContext(overrides = {}) {
  const container = { hidden: true, innerHTML: "" };
  const timers = new Map();
  let nextTimer = 0;
  const ctx = vm.createContext({
    currentUser: { is_admin: true },
    crypto: { randomUUID: () => "current-restore" },
    document: { getElementById: () => container },
    window: {
      setTimeout: (callback) => {
        timers.set(++nextTimer, callback);
        return nextTimer;
      },
      clearTimeout: (id) => timers.delete(id),
    },
    fetch: async () => ({
      ok: true,
      json: async () => ({
        operation_id: "current-restore",
        status: "running",
        phase: "downloading",
        percent: 50,
        bytes_done: 1024,
        bytes_total: 2048,
      }),
    }),
    ...overrides,
  });
  loadFeature(ctx, "scout", "utils");
  loadFeature(ctx, "scout", "restore-progress");
  return { ctx, container, timers };
}

test("restore polling shows real bytes, then indeterminate extraction with files restored", async () => {
  const { ctx, container, timers } = progressContext();
  const operation = ctx.startRestoreProgress("progress");
  await new Promise(setImmediate);
  assert.equal(container.hidden, false);
  assert.match(container.innerHTML, /Downloading snapshot/);
  assert.match(container.innerHTML, /aria-valuenow="50"/);
  assert.match(container.innerHTML, /1.0 KB \/ 2.0 KB/);
  ctx.fetch = async () => ({
    ok: true,
    json: async () => ({
      operation_id: operation.operationID,
      status: "running",
      phase: "extracting",
      percent: null,
      restored_files: 7,
    }),
  });
  await [...timers.values()][0]();
  assert.match(container.innerHTML, /Extracting files/);
  assert.match(container.innerHTML, /7 files restored/);
  assert.match(container.innerHTML, /restore-indeterminate/);
  assert.doesNotMatch(container.innerHTML, /aria-valuenow/);
  operation.finish(true);
  assert.match(container.innerHTML, /Restore complete/);
  assert.match(container.innerHTML, /aria-valuenow="100"/);
});

test("finishing ignores a late progress response and does not schedule more polls", async () => {
  let resolve;
  const pending = new Promise((done) => {
    resolve = done;
  });
  const { ctx, container, timers } = progressContext({ fetch: () => pending });
  const operation = ctx.startRestoreProgress("progress");
  operation.finish(false);
  const finished = container.innerHTML;
  resolve({
    ok: true,
    json: async () => ({ operation_id: operation.operationID, status: "running", phase: "decrypting", percent: 60 }),
  });
  await new Promise(setImmediate);
  assert.equal(container.innerHTML, finished);
  assert.match(finished, /Restore failed/);
  assert.equal(timers.size, 0);
});

test("a fast restore uses the response's final file count before any poll finishes", async () => {
  let resolve;
  const pending = new Promise((done) => {
    resolve = done;
  });
  const { ctx, container } = progressContext({ fetch: () => pending });
  const operation = ctx.startRestoreProgress("progress");
  operation.finish(true, 12);
  assert.match(container.innerHTML, /12 files restored/);
  assert.match(container.innerHTML, /aria-valuenow="100"/);
  resolve({ ok: false });
  await new Promise(setImmediate);
});

test("unrelated operations and polls completing after sign-out cannot populate progress", async () => {
  const { ctx, container, timers } = progressContext({
    fetch: async () => ({
      ok: true,
      json: async () => ({ operation_id: "older-restore", status: "completed", percent: 100 }),
    }),
  });
  const operation = ctx.startRestoreProgress("progress");
  await new Promise(setImmediate);
  assert.match(container.innerHTML, /Starting restore/);
  ctx.currentUser = null;
  container.innerHTML = "Signed out";
  await [...timers.values()][0]();
  operation.finish(true);
  assert.equal(container.innerHTML, "Signed out");
});

test("local restore sends the progress ID, blocks duplicate clicks, and refreshes folders", async () => {
  const nodes = { "recover-fingerprint": { value: "", disabled: false }, "recover-confirm-btn": { disabled: false } };
  const calls = [],
    refreshes = [],
    finishes = [];
  let resolve;
  const response = new Promise((done) => {
    resolve = done;
  });
  const ctx = vm.createContext({
    document: { getElementById: (id) => nodes[id] || null },
    setStatus() {},
    setActionStatus() {},
    closeDialog() {},
    startRestoreProgress: () => ({ operationID: "local-restore", finish: (success) => finishes.push(success) }),
    fetch: (url, options) => {
      calls.push([url, JSON.parse(options.body)]);
      return response;
    },
    loadData: async (options) => refreshes.push(options),
  });
  loadFeature(ctx, "scout", "recovery");
  vm.runInContext(
    '_recoverContext = { relativePath: "photos", jobName: "photos", preview: { snapshot_fingerprint: "abcdef12" }, previewFingerprint: "" }',
    ctx,
  );
  const pending = ctx.confirmRecover();
  await ctx.confirmRecover();
  assert.equal(calls.length, 1);
  assert.equal(nodes["recover-fingerprint"].disabled, true);
  assert.deepEqual(calls[0], [
    "/api/recovery/restore",
    { relative_path: "photos", fingerprint: "abcdef12", operation_id: "local-restore" },
  ]);
  resolve({ ok: true, json: async () => ({ status: "recovered", restored_files: 2 }) });
  await pending;
  assert.deepEqual(finishes, [true]);
  assert.deepEqual(JSON.parse(JSON.stringify(refreshes)), [
    { silent: true, includeKey: false, refreshDirectoryTree: true },
  ]);
  assert.equal(nodes["recover-fingerprint"].disabled, false);
  assert.equal(nodes["recover-confirm-btn"].disabled, false);
});
