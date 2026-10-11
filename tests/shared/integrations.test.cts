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
    "payload-template",
    "timing",
    "detail",
    "timeout",
  ]) {
    fields.set(`integration-${name}`, { value: "", checked: false });
  }
  fields.set("integration-select", { value: "saved-id", replaceChildren() {} });
  fields.set("integrations-button", { hidden: true });
  fields.set("hook_pre_command", { value: "" });
  fields.set("hook_post_command", { value: "" });
  fields.set("hook-view-content", { value: "" });
  for (const name of [
    "events",
    "payload-editor",
    "test",
    "delete",
    "notifications-panel",
    "scripts-panel",
    "notifications-tab",
    "scripts-tab",
  ]) {
    fields.set(`integration-${name}`, {
      children: [],
      replaceChildren() {
        this.children = [];
      },
      appendChild(child) {
        this.children.push(child);
      },
      setAttribute() {},
    });
  }
  fields.get("integration-name").value = "Backup alerts";
  fields.get("integration-format").value = "json";
  fields.get("integration-timeout").value = "5";
  fields.get("integration-enabled").checked = true;
  fields.get("integration-timing").value = "post";
  const ctx = vm.createContext({
    currentUser: { id: 1, is_admin: true },
    document: {
      getElementById: (id) => fields.get(id),
      querySelectorAll: () => [{ value: "upload-finished" }],
      createElement: () => ({
        dataset: {},
        children: [],
        append(...children) {
          this.children.push(...children);
        },
      }),
      createTextNode: (text) => text,
    },
    requirePanelReady: () => true,
    setPanelReady: () => {},
    closeDialog: () => {},
    setStatus: () => {},
  });
  loadFeature(ctx, app, "integrations");
  return { ctx, fields };
}

for (const app of ["scout", "station"]) {
  test(`${app} only admins can open integrations or load its script panel`, async () => {
    const { ctx, fields } = editor(app);
    let opened = 0;
    let loaded = 0;
    ctx.openDialog = () => opened++;
    ctx.clearStatus = () => {};
    ctx.setActionStatus = () => {};
    ctx.loadIntegrations = async () => loaded++;
    ctx.loadHookConfig = async () => loaded++;
    for (const user of [null, { is_admin: false }, { is_admin: true, must_change_password: true }]) {
      ctx.currentUser = user;
      await ctx.openIntegrationsDialog();
      await ctx.showIntegrationPanel("scripts");
      assert.equal(fields.get("integrations-button").hidden, true);
    }
    assert.equal(opened, 0);
    assert.equal(loaded, 0);
    ctx.currentUser = { is_admin: true };
    ctx.updateIntegrationAccess();
    assert.equal(fields.get("integrations-button").hidden, false);
    await ctx.openIntegrationsDialog();
    assert.equal(opened, 1);
    assert.equal(loaded, 1);
  });

  test(`${app} losing admin access clears private commands, templates and script contents`, () => {
    const { ctx, fields } = editor(app);
    const privateFields = [
      "integration-url",
      "integration-headers",
      "integration-template",
      "integration-payload-template",
      "hook_pre_command",
      "hook_post_command",
      "hook-view-content",
    ];
    for (const id of privateFields) fields.get(id).value = "private-server-address";
    vm.runInContext('integrationDestinations = [{ name: "private-server" }]', ctx);
    ctx.currentUser.is_admin = false;
    ctx.updateIntegrationAccess();
    for (const id of privateFields) assert.equal(fields.get(id).value, "");
    assert.equal(vm.runInContext("integrationDestinations.length", ctx), 0);
    assert.equal(fields.get("integrations-button").hidden, true);
  });

  test(`${app} a pending integration response cannot restore private data after sign-out`, async () => {
    const { ctx } = editor(app);
    let finish;
    ctx.loadEditorPanel = async (_name, task) => task();
    ctx.fetch = () => new Promise((resolve) => (finish = resolve));
    const loading = ctx.loadIntegrations();
    ctx.currentUser = null;
    ctx.updateIntegrationAccess();
    finish({ ok: true, json: async () => ({ destinations: [{ name: "private-server" }] }) });
    await assert.rejects(loading, /Admin access required/);
    assert.equal(vm.runInContext("integrationDestinations.length", ctx), 0);
  });

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

  test(`${app} new integrations and reset use generic defaults while edits are preserved`, () => {
    const { ctx, fields } = editor(app);
    vm.runInContext(
      'integrationDefaultMessage = "{{ job_name }}: {{ status }}"; integrationDefaultPayload = \'{"message":"{{ message }}"}\';',
      ctx,
    );
    fields.get("integration-select").value = "";
    ctx.editIntegration();
    assert.equal(fields.get("integration-template").value, "{{ job_name }}: {{ status }}");
    assert.equal(fields.get("integration-payload-template").value, '{"message":"{{ message }}"}');
    assert.equal(fields.get("integration-payload-editor").hidden, true);
    fields.get("integration-template").value = "Edited message";
    fields.get("integration-payload-template").value = '{"text":"{{ message }}"}';
    fields.get("integration-format").value = "custom-json";
    ctx.updateIntegrationFormat();
    assert.equal(fields.get("integration-payload-editor").hidden, false);
    assert.equal(ctx.collectIntegrationPayload().payload_template, '{"text":"{{ message }}"}');
    ctx.resetIntegrationTemplate("message");
    assert.equal(fields.get("integration-template").value, "{{ job_name }}: {{ status }}");
    assert.equal(fields.get("integration-payload-template").value, '{"text":"{{ message }}"}');
    ctx.resetIntegrationTemplate("payload");
    assert.equal(fields.get("integration-payload-template").value, '{"message":"{{ message }}"}');
  });

  test(`${app} custom JSON rejects malformed templates before saving`, () => {
    const { ctx, fields } = editor(app);
    fields.get("integration-format").value = "custom-json";
    fields.get("integration-payload-template").value = '{"job": {{ job_name }}}';
    assert.throws(() => ctx.collectIntegrationPayload(), /Payload template must be valid JSON/);
  });

  test(`${app} PRE and POST show separate events for independent destinations`, () => {
    const { ctx, fields } = editor(app);
    const preEvent = app === "scout" ? "job-started" : "upload-started";
    const postEvent = app === "scout" ? "upload-finished" : "upload-received";
    vm.runInContext(`integrationEvents = ${JSON.stringify([preEvent, postEvent])};`, ctx);
    ctx.renderIntegrationEvents();
    const postRows = fields.get("integration-events").children;
    assert.equal(postRows.length, 1);
    assert.equal(postRows[0].children[0].value, postEvent);
    fields.get("integration-timing").value = "pre";
    ctx.renderIntegrationEvents();
    const preRows = fields.get("integration-events").children;
    assert.equal(preRows.length, 1);
    assert.equal(preRows[0].children[0].value, preEvent);
    assert.equal(preRows[0].children[0].checked, true);
    assert.equal(ctx.isIntegrationPreEvent(preEvent), true);
    assert.equal(ctx.isIntegrationPreEvent(postEvent), false);
  });

  test(`${app} switching integration panels preserves separate action drafts`, async () => {
    const { ctx, fields } = editor(app);
    let scriptLoads = 0;
    let notificationLoads = 0;
    ctx.loadHookConfig = async () => {
      scriptLoads++;
    };
    ctx.loadIntegrations = async () => {
      notificationLoads++;
    };
    fields.get("integration-template").value = "Notification draft";
    await ctx.showIntegrationPanel("scripts");
    assert.equal(fields.get("integration-notifications-panel").hidden, true);
    await ctx.showIntegrationPanel("notifications");
    await ctx.showIntegrationPanel("scripts");
    assert.equal(scriptLoads, 1);
    assert.equal(notificationLoads, 1);
    assert.equal(fields.get("integration-template").value, "Notification draft");
  });
}
