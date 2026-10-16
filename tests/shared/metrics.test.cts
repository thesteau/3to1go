const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const fs = require("node:fs");
const path = require("node:path");

for (const app of ["scout", "station"]) {
  test(`${app} metrics toggle defaults off and survives settings edits`, () => {
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
    ctx.fillSettings({});
    assert.equal(ctx.collectSettingsPayload().metrics_enabled, false);
    ctx.toggleSettingSwitch(nodes.get("settings_metrics_enabled"));
    assert.equal(ctx.collectSettingsPayload().metrics_enabled, true);
    ctx.fillSettings({ metrics_enabled: true });
    assert.equal(ctx.getToggle("settings_metrics_enabled"), true);
    ctx.toggleSettingSwitch(nodes.get("settings_metrics_enabled"));
    assert.equal(ctx.collectSettingsPayload().metrics_enabled, false);
    ctx.fillSettings({});
    assert.equal(ctx.getToggle("settings_metrics_enabled"), false);
    const html = fs.readFileSync(path.join(__dirname, "../../app", app, "static/html/settings-dialog.html"), "utf8");
    assert.match(
      html,
      /id="settings_metrics_enabled"[^>]*role="switch"[^>]*aria-checked="false"[^>]*data-requires="settings"[^>]*disabled/,
    );
  });
}
