const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

test("large-upload auto-approval defaults on and its toggle is included in saved settings", () => {
  const nodes = new Map();
  const ctx = vm.createContext({
    document: {
      getElementById(id) {
        if (!nodes.has(id)) {
          const attributes = new Map();
          nodes.set(id, {
            value: "",
            setAttribute: (name, value) => attributes.set(name, value),
            getAttribute: (name) => attributes.get(name),
            classList: { toggle() {} },
          });
        }
        return nodes.get(id);
      },
    },
    applyTheme() {},
    updateCronScheduleHint() {},
    latestData: { settings: {} },
  });
  loadFeature(ctx, "scout", "admin");
  ctx.fillSettings({});
  assert.equal(ctx.getToggle("settings_auto_approve_large_uploads"), true);
  assert.equal(ctx.collectSettingsPayload().auto_approve_large_uploads, true);
  ctx.fillSettings({ auto_approve_large_uploads: false });
  assert.equal(ctx.getToggle("settings_auto_approve_large_uploads"), false);
  assert.equal(ctx.collectSettingsPayload().auto_approve_large_uploads, false);
});

test("remembered approvals show job details safely and Remove restores the prompt", async () => {
  const container = { innerHTML: "", textContent: "" };
  const calls = [];
  const messages = [];
  let failRemove = true;
  let approvals = [
    {
      id: "approval-id",
      job_name: "<img src=x onerror=alert(1)>",
      job_path: "/scan/docs",
      scout_id: "scout-1",
      station_url: "https://station.example",
      approved_at: "2026-10-13T00:00:00Z",
    },
  ];
  const ctx = vm.createContext({
    currentUser: { is_admin: true },
    document: { getElementById: () => container },
    requirePanelReady: () => true,
    setActionStatus: (...args) => messages.push(args),
    loadData: async () => {},
    fetch: async (url, options) => {
      calls.push([url, options?.method || "GET"]);
      if (options?.method === "DELETE") {
        if (failRemove) return { ok: false, json: async () => ({ detail: "A backup is running" }) };
        approvals = [];
        return { ok: true, json: async () => ({ status: "ok" }) };
      }
      return { ok: true, json: async () => ({ approvals }) };
    },
  });
  loadFeature(ctx, "scout", "utils");
  loadFeature(ctx, "scout", "admin");
  await ctx.loadLargeUploadApprovals();
  assert.match(container.innerHTML, /&lt;img/);
  assert.doesNotMatch(container.innerHTML, /<img/);
  assert.match(container.innerHTML, /\/scan\/docs/);
  assert.match(container.innerHTML, /https:\/\/station.example/);
  assert.match(container.innerHTML, />Remove<\/button>/);
  const button = { disabled: false };
  await ctx.removeLargeUploadApproval("approval-id", button);
  assert.equal(messages.at(-1)[1], "error");
  assert.match(container.innerHTML, /approval-id/);
  assert.equal(button.disabled, false);
  failRemove = false;
  await ctx.removeLargeUploadApproval("approval-id", button);
  assert.match(container.innerHTML, /No remembered approvals/);
  assert.match(messages.at(-1)[0], /next oversized upload will need approval again/);
  assert.deepEqual(calls, [
    ["/api/upload-approvals", "GET"],
    ["/api/upload-approvals/approval-id", "DELETE"],
    ["/api/upload-approvals/approval-id", "DELETE"],
    ["/api/upload-approvals", "GET"],
  ]);
});

test("an approval list response finishing after sign-out does not restore job details", async () => {
  const container = { innerHTML: "", textContent: "" };
  let resolve;
  const response = new Promise((done) => {
    resolve = done;
  });
  const ctx = vm.createContext({
    currentUser: { is_admin: true },
    document: { getElementById: () => container },
    fetch: () => response,
  });
  loadFeature(ctx, "scout", "admin");
  const loading = ctx.loadLargeUploadApprovals();
  ctx.currentUser = null;
  container.innerHTML = "Signed out";
  resolve({ ok: true, json: async () => ({ approvals: [{ job_name: "private" }] }) });
  await loading;
  assert.equal(container.innerHTML, "Signed out");
});
