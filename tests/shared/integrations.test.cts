const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

function editor(app: string) {
  const fields = new Map<string, any>();
  for (const name of [
    "name",
    "enabled",
    "format",
    "url",
    "headers",
    "clear-headers",
    "scout",
    "instance",
    "job",
    "source",
    "template",
    "detail",
    "timeout",
  ]) {
    fields.set(`integration-${name}`, { value: "", checked: false });
  }
  fields.set("integration-select", { value: "saved-id" });
  fields.get("integration-name").value = "Backup alerts";
  fields.get("integration-format").value = "json";
  fields.get("integration-timeout").value = "5";
  fields.get("integration-enabled").checked = true;
  const ctx = vm.createContext({
    document: {
      getElementById: (id) => fields.get(id),
      querySelectorAll: () => [{ value: "upload-finished" }],
    },
    requirePanelReady: () => true,
    setStatus: () => {},
  });
  loadFeature(ctx, app, "integrations");
  return { ctx, fields };
}

for (const app of ["scout", "station"]) {
  test(`${app} requesting sign-in clears draft integration secrets`, () => {
    const { ctx, fields } = editor(app);
    ctx.window = { fetch: () => {}, setTimeout: () => {} };
    ctx.clearStatus = () => {};
    ctx.openDialog = () => {};
    ctx.closeDialog = () => {};
    ctx.clearSessionEncKeys = () => {};
    ctx.resolveAppDialog = () => {};
    loadFeature(ctx, app, "auth");
    fields.get("integration-url").value = "https://example.invalid/private-token";
    fields.get("integration-headers").value = '{"Authorization":"Bearer private-token"}';
    ctx.openLoginDialog();
    assert.equal(fields.get("integration-url").value, "");
    assert.equal(fields.get("integration-headers").value, "");
  });

  test(`${app} integration edits preserve omitted secrets and explicitly clear headers`, () => {
    const { ctx, fields } = editor(app);
    let payload = ctx.collectIntegrationPayload();
    assert.equal(Object.hasOwn(payload, "url"), false);
    assert.equal(Object.hasOwn(payload, "headers"), false);
    fields.get("integration-clear-headers").checked = true;
    payload = ctx.collectIntegrationPayload();
    assert.equal(JSON.stringify(payload.headers), "{}");
    fields.get("integration-clear-headers").checked = false;
    fields.get("integration-url").value = "https://example.invalid/private-token";
    fields.get("integration-headers").value = '{"Authorization":"Bearer private-token"}';
    payload = ctx.collectIntegrationPayload();
    assert.equal(payload.url, "https://example.invalid/private-token");
    assert.equal(payload.headers.Authorization, "Bearer private-token");
  });

  test(`${app} invalid integration headers report an error without echoing secret input`, () => {
    const { ctx, fields } = editor(app);
    for (const value of ["private-token", '["private-token"]', '{"Authorization":42}']) {
      fields.get("integration-headers").value = value;
      assert.throws(
        () => ctx.collectIntegrationPayload(),
        (error) => error.message === "Headers must be a JSON object containing string values.",
      );
    }
  });

  test(`${app} saved integration tests never transmit draft credentials`, async () => {
    const { ctx, fields } = editor(app);
    fields.get("integration-url").value = "https://example.invalid/private-token";
    fields.get("integration-headers").value = '{"Authorization":"Bearer private-token"}';
    let request: any;
    ctx.fetch = async (url, options) => {
      request = { url, options };
      return { ok: true, json: async () => ({ status: "ok" }) };
    };
    await ctx.testIntegration();
    assert.equal(request.url, "/api/integrations/saved-id/test");
    assert.equal(Object.hasOwn(request.options, "body"), false);
    assert.equal(JSON.stringify(request).includes("private-token"), false);
  });

  test(`${app} successful integration saves clear secret inputs`, async () => {
    const { ctx, fields } = editor(app);
    fields.get("integration-url").value = "https://example.invalid/private-token";
    fields.get("integration-headers").value = '{"Authorization":"Bearer private-token"}';
    ctx.fetch = async () => ({ ok: true, json: async () => ({ destination: { id: "saved-id" } }) });
    let selectedID: string;
    ctx.loadIntegrations = async (id) => {
      selectedID = id;
    };
    await ctx.saveIntegration();
    assert.equal(fields.get("integration-url").value, "");
    assert.equal(fields.get("integration-headers").value, "");
    assert.equal(selectedID, "saved-id");
  });
}
