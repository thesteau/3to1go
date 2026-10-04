const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

test("Station Restore requires a saved key and sends the exact filename without a fingerprint", async () => {
  let key = null;
  const calls = [];
  const ctx = vm.createContext({
    sessionStorage: { getItem: () => key },
    document: { querySelectorAll: () => [] },
    setActionStatus() {},
    setButtonBusy: () => () => {},
    fetch: async (url, options) => {
      calls.push([url, options]);
      return { ok: true, json: async () => ({ notified: true }) };
    },
  });
  for (const feature of ["utils", "keys", "snapshots"]) loadFeature(ctx, "station", feature);
  const filename = "photos__2026-09-01T00-00-00Z__abcdef12.tar.zst";
  await ctx.requestSnapshotRestore("scout", "instance", "photos", filename, {});
  assert.equal(calls.length, 0);
  key = "saved-key";
  await ctx.requestSnapshotRestore("scout", "instance", "photos", filename, {});
  assert.equal(calls.length, 1);
  assert.equal(calls[0][0], `/api/snapshots/scout/instance/photos/${filename}/restore`);
  assert.equal(calls[0][1].method, "POST");
  assert.deepEqual(JSON.parse(calls[0][1].body), { target_scout_id: "scout", target_instance_id: "instance" });
});

test("Station sends another target without sending an encryption key", async () => {
  const calls = [];
  const ctx = vm.createContext({
    setActionStatus() {},
    setButtonBusy: () => () => {},
    getEncKey: () => {
      throw new Error("target restore should request the key on Scout");
    },
    fetch: async (url, options) => {
      calls.push([url, options]);
      return { ok: true, json: async () => ({}) };
    },
  });
  loadFeature(ctx, "station", "snapshots");
  await ctx.requestSnapshotRestore("source", "old", "photos", "exact.tar.zst", {}, "target", "new");
  assert.deepEqual(JSON.parse(calls[0][1].body), { target_scout_id: "target", target_instance_id: "new" });
});
