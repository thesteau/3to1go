const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const fs = require("node:fs");
const path = require("node:path");
const { loadFeature } = require("../helpers/scripts.cts");

function monitoringContext(name = "<script>untrusted</script>") {
  const elements = new Map([
    ["monitoring-token-section", { hidden: true }],
    ["monitoring-token-list", { innerHTML: "" }],
    ["monitoring_token_output", { value: "old-secret" }],
    ["monitoring_token_name", { value: "prometheus" }],
    ["monitoring_token_days", { value: "90" }],
    ["monitoring_scope_metrics", { checked: true }],
    ["monitoring_scope_report", { checked: false }],
  ]);
  const requests = [];
  const statuses = [];
  const info = { id: "token-id", name, routes: ["metrics"], expires_at: "2027-01-01" };
  const ctx = vm.createContext({
    currentUser: { id: 1, is_admin: true },
    document: { getElementById: (id) => elements.get(id) },
    escapeHtml: (value) =>
      String(value).replaceAll("&", "&amp;").replaceAll('"', "&quot;").replaceAll("<", "&lt;").replaceAll(">", "&gt;"),
    readJson: async (response) => response.body,
    fetch: async (url, options) => {
      requests.push({ url, options });
      return { ok: true, body: options?.method === "POST" ? { token: "header.claims.signature" } : { tokens: [info] } };
    },
    setStatus: (...args) => statuses.push(args),
    confirmApp: async () => true,
  });
  loadFeature(ctx, "station", "users");
  return { ctx, elements, requests, statuses };
}

test("monitoring token UI lists routes, escapes names, and keeps secrets out of the list", async () => {
  const { ctx, elements, requests } = monitoringContext();
  await ctx.loadMonitoringTokens();
  assert.equal(requests[0].url, "/api/monitoring-tokens");
  assert.equal(elements.get("monitoring-token-section").hidden, false);
  const html = elements.get("monitoring-token-list").innerHTML;
  assert.match(html, /&lt;script&gt;untrusted&lt;\/script&gt;/);
  assert.match(html, /metrics · expires 2027-01-01/);
  assert.doesNotMatch(html, /old-secret|header\.claims\.signature|<script\b/i);
});

test("monitoring token revoke handler receives the exact identifier", () => {
  const { ctx } = monitoringContext();
  const id = "token');throw Error('unexpected');//";
  const rendered = ctx.renderMonitoringTokens([{ id, name: "example", routes: ["report"], expires_at: "tomorrow" }]);
  const handler = rendered.match(/onclick="([^"]*)"/)[1].replaceAll("&quot;", '"');
  let received;
  ctx.revokeMonitoringToken = (value) => {
    received = value;
  };
  vm.runInContext(handler, ctx);
  assert.equal(received, id);
});

test("monitoring token creation sends selected routes and reveals the token once", async () => {
  const { ctx, elements, requests } = monitoringContext();
  await ctx.createMonitoringToken();
  assert.equal(requests[0].url, "/api/monitoring-tokens");
  assert.deepEqual(JSON.parse(requests[0].options.body), { name: "prometheus", ttl_days: 90, routes: ["metrics"] });
  assert.equal(elements.get("monitoring_token_output").value, "header.claims.signature");
  assert.doesNotMatch(elements.get("monitoring-token-list").innerHTML, /header\.claims\.signature/);
});

test("monitoring token creation needs a route and clears a stale secret", async () => {
  const { ctx, elements, requests, statuses } = monitoringContext();
  elements.get("monitoring_scope_metrics").checked = false;
  await ctx.createMonitoringToken();
  assert.equal(requests.length, 0);
  assert.equal(elements.get("monitoring_token_output").value, "");
  assert.equal(statuses.at(-1)[2], "error");
});

test("monitoring token failures stay visible and revoke uses the monitoring endpoint", async () => {
  const { ctx, elements, requests, statuses } = monitoringContext();
  await ctx.revokeMonitoringToken("token/id");
  assert.equal(requests[0].url, "/api/monitoring-tokens/token%2Fid");
  assert.equal(requests[0].options.method, "DELETE");
  ctx.fetch = async () => ({ ok: false, body: { detail: "server unavailable" } });
  await ctx.createMonitoringToken();
  assert.equal(elements.get("monitoring_token_output").value, "");
  assert.equal(statuses.at(-1)[1], "server unavailable");
  await ctx.revokeMonitoringToken("token-id");
  assert.equal(statuses.at(-1)[1], "server unavailable");
});

test("non-admins get no monitoring token controls or requests", async () => {
  const { ctx, elements, requests } = monitoringContext();
  ctx.currentUser.is_admin = false;
  await ctx.loadMonitoringTokens();
  assert.equal(elements.get("monitoring-token-section").hidden, true);
  assert.equal(requests.length, 0);
});

test("an app without the monitoring section makes no requests", async () => {
  const { ctx, elements, requests } = monitoringContext();
  elements.delete("monitoring-token-section");
  await ctx.loadMonitoringTokens();
  assert.equal(requests.length, 0);
});

test("the monitoring scope is sent only where the app offers it", async () => {
  const elements = new Map([
    ["automation_token_output", { value: "" }],
    ["automation_token_name", { value: "prometheus" }],
    ["automation_token_days", { value: "90" }],
    ["automation_scope_read", { checked: false }],
    ["automation_scope_monitoring", { checked: true }],
  ]);
  const requests = [];
  const ctx = vm.createContext({
    currentUser: { id: 1, is_admin: true },
    document: { getElementById: (id) => elements.get(id) },
    escapeHtml: String,
    readJson: async (response) => response.body,
    fetch: async (url, options) => {
      requests.push({ url, options });
      return { ok: true, body: { token: "3to1go_api_once" } };
    },
    setStatus() {},
    confirmApp: async () => true,
  });
  loadFeature(ctx, "scout", "users");
  await ctx.createAutomationToken();
  assert.deepEqual(JSON.parse(requests[0].options.body).scopes, ["monitoring"]);
});

test("each app's dialog offers the right monitoring token controls", () => {
  const html = (app, file) => fs.readFileSync(path.join(__dirname, "../../app", app, "static/html", file), "utf8");
  const station = html("station", "users-dialog.html");
  const scout = html("scout", "users-dialog.html");
  assert.match(station, /id="monitoring-token-section"/);
  assert.doesNotMatch(scout, /id="monitoring-token-section"/);
  assert.match(scout, /id="automation_scope_monitoring"/);
  assert.doesNotMatch(station, /id="automation_scope_monitoring"/);
});

for (const app of ["scout", "station"]) {
  test(`${app} require token toggle follows saved settings and survives edits`, () => {
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
      window: {},
      latestData: { settings: {} },
      applyTheme() {},
      updateCronScheduleHint() {},
    });
    loadFeature(ctx, app, "admin");
    ctx.fillSettings({ monitoring_token_required: true });
    assert.equal(ctx.collectSettingsPayload().monitoring_token_required, true);
    ctx.toggleSettingSwitch(nodes.get("settings_monitoring_token_required"));
    assert.equal(ctx.collectSettingsPayload().monitoring_token_required, false);
    ctx.fillSettings({ monitoring_token_required: false });
    assert.equal(ctx.getToggle("settings_monitoring_token_required"), false);
    const html = fs.readFileSync(path.join(__dirname, "../../app", app, "static/html/settings-dialog.html"), "utf8");
    assert.match(
      html,
      /id="settings_monitoring_token_required"[^>]*role="switch"[^>]*aria-checked="false"[^>]*data-requires="settings"[^>]*disabled/,
    );
  });
}
