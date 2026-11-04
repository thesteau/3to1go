const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const fs = require("node:fs");
const path = require("node:path");
const { loadFeature } = require("../helpers/scripts.cts");

function tokenRequests(app, checked) {
  const elements = new Map([
    ["automation_token_output", { value: "" }],
    ["automation_token_name", { value: "prometheus" }],
    ["automation_token_days", { value: "90" }],
    ...Object.entries(checked).map(([scope, value]) => [`automation_scope_${scope}`, { checked: value }]),
  ]);
  const requests = [];
  const ctx = vm.createContext({
    currentUser: { id: 1, is_admin: true },
    document: { getElementById: (id) => elements.get(id) },
    escapeHtml: String,
    readJson: async (response) => response.body,
    fetch: async (url, options) => {
      requests.push({ url, options });
      return { ok: true, body: { token: "header.claims.signature" } };
    },
    setStatus() {},
    confirmApp: async () => true,
  });
  loadFeature(ctx, app, "users");
  return { ctx, elements, requests };
}

const dialog = (app, file) => fs.readFileSync(path.join(__dirname, "../../app", app, "static/html", file), "utf8");

test("route scopes are sent, and a scope the app doesn't offer is skipped", async () => {
  const station = tokenRequests("station", { read: false, metrics: true, report: true });
  await station.ctx.createAutomationToken();
  assert.deepEqual(JSON.parse(station.requests[0].options.body).scopes, ["metrics", "report"]);
  assert.equal(station.elements.get("automation_token_output").value, "header.claims.signature");

  const scout = tokenRequests("scout", { read: false, metrics: true });
  await scout.ctx.createAutomationToken();
  assert.deepEqual(JSON.parse(scout.requests[0].options.body).scopes, ["metrics"]);
});

test("each app's dialog offers only its own route scopes, off by default", () => {
  const station = dialog("station", "users-dialog.html");
  const scout = dialog("scout", "users-dialog.html");
  assert.match(station, /id="automation_scope_metrics" type="checkbox">/);
  assert.match(station, /id="automation_scope_report" type="checkbox">/);
  assert.match(scout, /id="automation_scope_metrics" type="checkbox">/);
  assert.doesNotMatch(scout, /automation_scope_report/);
});

test("there is one token section, named for its app, with a hint about the Station token", () => {
  for (const [app, name] of [
    ["station", "Station Auth Tokens"],
    ["scout", "Scout Auth Tokens"],
  ]) {
    const html = dialog(app, "users-dialog.html");
    assert.doesNotMatch(html, /monitoring-token-section|Automation Tokens/);
    const heading = html.match(/<h3>([^<]*)<span class="field-help hover-hint" tabindex="0" aria-label="([^"]*)" title="([^"]*)">\?<\/span><\/h3>/);
    assert.ok(heading, `${app} heading has a hint`);
    assert.equal(heading[1].trim(), name);
    assert.match(heading[2], /^Not the same as the Station token/);
    assert.equal(heading[2], heading[3]);
    assert.match(dialog(app, "settings-dialog.html"), /Require auth token/);
  }
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
    const html = dialog(app, "settings-dialog.html");
    assert.match(
      html,
      /id="settings_monitoring_token_required"[^>]*role="switch"[^>]*aria-checked="false"[^>]*data-requires="settings"[^>]*disabled/,
    );
    assert.doesNotMatch(html, /On for new installs/);
  });
}
