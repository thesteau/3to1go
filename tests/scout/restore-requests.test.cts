const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

function restoreContext(overrides = {}) {
  const elements = {
    "restore-requests-tab": { hidden: true, textContent: "" },
    "restore-requests-list": { innerHTML: "", querySelectorAll: () => [] },
    "restore-destination-request": { value: "deleted/photos" },
  };
  const messages = [];
  const calls = [];
  const ctx = vm.createContext({
    document: { getElementById: (id) => elements[id] || null },
    latestData: { directories: [] },
    setStatus: (...args) => messages.push(args),
    setButtonBusy: () => () => {},
    confirmApp: async () => true,
    fetch: async (url, options) => {
      calls.push([url, options]);
      return { ok: true, json: async () => (options ? { status: "rejected" } : []) };
    },
    ...overrides,
  });
  loadFeature(ctx, "scout", "utils");
  loadFeature(ctx, "scout", "restore-requests");
  return { ctx, elements, messages, calls };
}

test("Scout reveals restore requests, escapes archive labels, and offers both decisions", () => {
  const { ctx, elements } = restoreContext();
  ctx.renderRestoreRequests([]);
  assert.equal(elements["restore-requests-tab"].hidden, true);
  ctx.renderRestoreRequests([
    {
      id: "request",
      job_name: "<photos>",
      filename: "older<&.tar.zst",
      status: "pending",
      created_at: "2026-10-03T00:00:00Z",
    },
  ]);
  assert.equal(elements["restore-requests-tab"].hidden, false);
  const html = elements["restore-requests-list"].innerHTML;
  assert.match(html, /&lt;photos&gt;/);
  assert.match(html, /older&lt;&amp;\.tar\.zst/);
  assert.match(html, />Accept<\/button>/);
  assert.match(html, />Reject<\/button>/);
  ctx.renderRestoreRequests([]);
  assert.match(elements["restore-requests-list"].innerHTML, /No pending restore requests/);
});

test("reject sends the decision without requiring a destination or confirming restore", async () => {
  const { ctx, elements, calls } = restoreContext({
    confirmApp: async () => {
      throw new Error("reject must not confirm restoration");
    },
  });
  elements["restore-destination-request"].value = "";
  await ctx.decideStationRestore("request", "reject", {});
  const [url, options] = calls[0];
  assert.equal(url, "/api/restore-requests/decision");
  assert.deepEqual(JSON.parse(options.body), { id: "request", decision: "reject", relative_path: "" });
});

test("accept requires a destination and confirmation before contacting Station", async () => {
  const { ctx, elements, calls } = restoreContext({ confirmApp: async () => false });
  await ctx.decideStationRestore("request", "accept", {});
  assert.equal(calls.length, 0);
  ctx.confirmApp = async () => true;
  await ctx.decideStationRestore("request", "accept", {});
  assert.deepEqual(JSON.parse(calls[0][1].body), {
    id: "request",
    decision: "accept",
    relative_path: "deleted/photos",
  });
  elements["restore-destination-request"].value = "";
  elements["restore-destination-request"].focus = () => {};
  calls.length = 0;
  await ctx.decideStationRestore("request", "accept", {});
  assert.equal(calls.length, 0);
});
