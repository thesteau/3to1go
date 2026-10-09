const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { loadFeature } = require("../helpers/scripts.cts");

function tokenContext() {
  const elements = new Map([
    ["automation-token-section", { hidden: true }],
    ["automation-token-list", { innerHTML: "" }],
    ["automation_token_output", { value: "old-secret" }],
    ["automation_token_name", { value: "nightly" }],
    ["automation_token_days", { value: "90" }],
    ...["read", "backup", "restore", "manage"].map((scope) => [
      `automation_scope_${scope}`,
      { checked: scope === "read" },
    ]),
  ]);
  const requests = [];
  const statuses = [];
  const info = { id: "token-id", name: "<script>untrusted</script>", scopes: ["read"], expires_at: "2027-01-01" };
  const ctx = vm.createContext({
    currentUser: { id: 1, is_admin: true },
    document: { getElementById: (id) => elements.get(id) },
    escapeHtml: (value) =>
      String(value).replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;").replaceAll(">", "&gt;"),
    readJson: async (response) => response.body,
    fetch: async (url, options) => {
      requests.push({ url, options });
      return { ok: true, body: options?.method === "POST" ? { token: "3to1go_api_once" } : { tokens: [info] } };
    },
    setStatus: (...args) => statuses.push(args),
    confirmApp: async () => true,
  });
  loadFeature(ctx, "station", "users");
  return { ctx, elements, requests, statuses };
}

test("automation token UI keeps secrets out of metadata and escapes names and identifiers", async () => {
  const { ctx, elements } = tokenContext();
  await ctx.loadAutomationTokens();
  const html = elements.get("automation-token-list").innerHTML;
  assert.match(html, /&lt;script&gt;untrusted&lt;\/script&gt;/);
  assert.doesNotMatch(html, /old-secret|3to1go_api_once|<script>/);
  const id = "token');throw Error('unexpected');//";
  const rendered = ctx.renderAutomationTokens([{ id, name: "example", scopes: ["read"], expires_at: "tomorrow" }]);
  const handler = rendered.match(/onclick="([^"]*)"/)[1].replaceAll("&quot;", '"');
  let received;
  ctx.revokeAutomationToken = (value) => {
    received = value;
  };
  vm.runInContext(handler, ctx);
  assert.equal(received, id);
});

test("creation uses selected permissions and reveals only the new secret", async () => {
  const { ctx, elements, requests } = tokenContext();
  elements.get("automation_scope_backup").checked = true;
  await ctx.createAutomationToken();
  assert.deepEqual(JSON.parse(requests[0].options.body), { name: "nightly", ttl_days: 90, scopes: ["read", "backup"] });
  assert.equal(elements.get("automation_token_output").value, "3to1go_api_once");
  assert.doesNotMatch(elements.get("automation-token-list").innerHTML, /3to1go_api_once/);
});

test("failed creation clears a stale secret and revocation errors remain visible", async () => {
  const { ctx, elements, statuses } = tokenContext();
  ctx.fetch = async () => ({ ok: false, body: { detail: "server unavailable" } });
  await ctx.createAutomationToken();
  assert.equal(elements.get("automation_token_output").value, "");
  assert.equal(statuses.at(-1)[1], "server unavailable");
  await ctx.revokeAutomationToken("token-id");
  assert.equal(statuses.at(-1)[1], "server unavailable");
});

test("non-admin token controls stay hidden and perform no requests", async () => {
  const { ctx, elements, requests } = tokenContext();
  ctx.currentUser.is_admin = false;
  await ctx.loadAutomationTokens();
  assert.equal(elements.get("automation-token-section").hidden, true);
  assert.equal(requests.length, 0);
});
