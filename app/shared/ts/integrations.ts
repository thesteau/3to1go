let integrationDestinations: IntegrationDestination[] = [];
let integrationEvents: string[] = [];
let integrationDefaultMessage = "";
let integrationDefaultPayload = "";
let integrationLoadedPanels = new Set<string>();

function canManageIntegrations(): boolean {
  return Boolean(currentUser?.is_admin && !currentUser.must_change_password);
}

function updateIntegrationAccess(): void {
  const allowed = canManageIntegrations();
  const button = document.getElementById("integrations-button");
  if (button) button.hidden = !allowed;
  if (!allowed) clearIntegrationState();
}

function clearIntegrationState(): void {
  clearIntegrationSecrets();
  integrationDestinations = [];
  integrationEvents = [];
  integrationDefaultMessage = "";
  integrationDefaultPayload = "";
  integrationLoadedPanels.clear();
  if (typeof clearHookState === "function") clearHookState();
  for (const id of [
    "integration-name",
    "integration-scout",
    "integration-instance",
    "integration-job",
    "integration-source",
    "integration-template",
    "integration-payload-template",
    "hook_pre_command",
    "hook_post_command",
    "hook_file_input",
    "hook-view-content",
  ]) {
    const input = document.getElementById(id) as HTMLInputElement | HTMLTextAreaElement | null;
    if (input) input.value = "";
  }
  for (const id of [
    "integration-select",
    "integration-events",
    "hook-files",
    "hook-view-filename",
    "hook-script-dir",
  ]) {
    document.getElementById(id)?.replaceChildren();
  }
  setPanelReady("integrations", false);
  setPanelReady("hooks", false);
  closeDialog("integrations-dialog");
  closeDialog("hook-view-dialog");
}

async function openIntegrationsDialog(panel: "notifications" | "scripts" = "notifications"): Promise<void> {
  if (!canManageIntegrations()) {
    updateIntegrationAccess();
    setActionStatus("Admin access required.", "error");
    return;
  }
  clearStatus("integrations-status");
  clearStatus("hooks-status");
  integrationLoadedPanels.clear();
  openDialog("integrations-dialog");
  await showIntegrationPanel(panel);
}

async function showIntegrationPanel(panel: "notifications" | "scripts"): Promise<void> {
  if (!canManageIntegrations()) return;
  for (const name of ["notifications", "scripts"]) {
    const selected = name === panel;
    document.getElementById(`integration-${name}-panel`)!.hidden = !selected;
    const tab = document.getElementById(`integration-${name}-tab`)!;
    tab.setAttribute("aria-selected", String(selected));
    tab.tabIndex = selected ? 0 : -1;
  }
  if (integrationLoadedPanels.has(panel)) return;
  try {
    if (panel === "scripts") await loadHookConfig({ preserveDrafts: false });
    else await loadIntegrations();
    integrationLoadedPanels.add(panel);
  } catch (error) {
    setStatus(panel === "scripts" ? "hooks-status" : "integrations-status", (error as Error).message, "error");
  }
}

function handleIntegrationTabKey(event: KeyboardEvent): void {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  const current = (event.target as HTMLElement).id === "integration-scripts-tab";
  const panel =
    event.key === "Home" ? "notifications" : event.key === "End" ? "scripts" : current ? "notifications" : "scripts";
  void showIntegrationPanel(panel);
  document.getElementById(`integration-${panel}-tab`)!.focus();
}

async function loadIntegrations(selectedID = ""): Promise<void> {
  const user = currentUser;
  await loadEditorPanel("integrations", async () => {
    const response = await fetch("/api/integrations", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    const body: IntegrationsResponse = await response.json();
    if (!response.ok) throw new Error(body.detail || "Failed to load integrations.");
    if (currentUser !== user || !canManageIntegrations()) throw new Error("Admin access required.");
    integrationDestinations = body.destinations || [];
    integrationEvents = body.events || [];
    integrationDefaultMessage = body.default_message_template || "";
    integrationDefaultPayload = body.default_payload_template || "";
    const select = document.getElementById("integration-select") as HTMLSelectElement;
    select.replaceChildren(new Option("New integration", ""));
    for (const destination of integrationDestinations) {
      select.add(new Option(`${destination.name}${destination.enabled ? "" : " (disabled)"}`, destination.id));
    }
    select.value = selectedID;
    editIntegration();
  });
}

function clearIntegrationSecrets(): void {
  for (const id of ["integration-url", "integration-headers"]) {
    const input = document.getElementById(id) as HTMLInputElement | null;
    if (input) input.value = "";
  }
}

function editIntegration(): void {
  const id = (document.getElementById("integration-select") as HTMLSelectElement).value;
  const destination = integrationDestinations.find((entry) => entry.id === id);
  const input = (name: string) => document.getElementById(`integration-${name}`) as HTMLInputElement;
  input("name").value = destination?.name || "";
  input("enabled").checked = destination?.enabled ?? true;
  (document.getElementById("integration-format") as HTMLSelectElement).value = destination?.format || "json";
  input("scout").value = destination?.match_scout_id || "";
  input("instance").value = destination?.match_instance_id || "";
  input("job").value = destination?.match_job_name || "";
  input("source").value = destination?.match_source_address || "";
  input("detail").checked = destination?.include_detail || false;
  input("timeout").value = String(destination?.timeout_seconds || 5);
  (document.getElementById("integration-template") as HTMLTextAreaElement).value =
    destination?.message_template || integrationDefaultMessage;
  (document.getElementById("integration-payload-template") as HTMLTextAreaElement).value =
    destination?.payload_template || integrationDefaultPayload;
  (document.getElementById("integration-timing") as HTMLSelectElement).value = destination?.events.some(
    isIntegrationPreEvent,
  )
    ? "pre"
    : "post";
  updateIntegrationFormat();
  clearIntegrationSecrets();
  input("clear-headers").checked = false;
  input("url").placeholder = destination?.url_configured
    ? "Configured. Leave blank to keep; enter a URL to replace."
    : "https://…";
  input("headers").placeholder = destination?.headers_configured
    ? "Configured. Leave blank to keep; enter JSON to replace."
    : '{"Authorization":"Bearer …"}';
  renderIntegrationEvents(destination?.events);
  for (const name of ["test", "delete"]) {
    (document.getElementById(`integration-${name}`) as HTMLButtonElement).hidden = !destination;
  }
}

function isIntegrationPreEvent(event: string): boolean {
  return event === "job-started" || event === "upload-started";
}

function renderIntegrationEvents(selectedEvents?: string[]): void {
  const pre = (document.getElementById("integration-timing") as HTMLSelectElement).value === "pre";
  const labels: Record<string, string> = {
    "job-started": "Before processing a job",
    "upload-started": "Before storing an upload (after checksum verification)",
    "job-finished": "After any job outcome",
    "upload-finished": "After a successful upload",
    "upload-received": "After storage handling succeeds or fails",
    "unusual-backup": "When an unusual backup is detected",
    "unusual-upload": "When an unusual stored archive is detected",
  };
  const events = document.getElementById("integration-events")!;
  events.replaceChildren();
  for (const event of integrationEvents) {
    if (isIntegrationPreEvent(event) !== pre) continue;
    const label = document.createElement("label");
    label.className = "integration-check";
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.value = event;
    checkbox.name = "integration-event";
    checkbox.checked = selectedEvents ? selectedEvents.includes(event) : event !== "job-finished";
    checkbox.dataset.requires = "integrations";
    label.append(checkbox, document.createTextNode(` ${labels[event] || event}`));
    events.appendChild(label);
  }
}

function updateIntegrationFormat(): void {
  document.getElementById("integration-payload-editor")!.hidden =
    (document.getElementById("integration-format") as HTMLSelectElement).value !== "custom-json";
}

function resetIntegrationTemplate(kind: "message" | "payload"): void {
  if (!requirePanelReady("integrations")) return;
  const id = kind === "message" ? "integration-template" : "integration-payload-template";
  (document.getElementById(id) as HTMLTextAreaElement).value =
    kind === "message" ? integrationDefaultMessage : integrationDefaultPayload;
}

function collectIntegrationPayload(): IntegrationUpdate {
  const input = (name: string) => document.getElementById(`integration-${name}`) as HTMLInputElement;
  const payload: IntegrationUpdate = {
    id: (document.getElementById("integration-select") as HTMLSelectElement).value,
    name: input("name").value.trim(),
    enabled: input("enabled").checked,
    format: (document.getElementById("integration-format") as HTMLSelectElement).value,
    events: [...document.querySelectorAll<HTMLInputElement>('input[name="integration-event"]:checked')].map(
      (field) => field.value,
    ),
    match_scout_id: input("scout").value.trim(),
    match_instance_id: input("instance").value.trim(),
    match_job_name: input("job").value.trim(),
    match_source_address: input("source").value.trim(),
    message_template: (document.getElementById("integration-template") as HTMLTextAreaElement).value,
    payload_template: (document.getElementById("integration-payload-template") as HTMLTextAreaElement).value,
    include_detail: input("detail").checked,
    timeout_seconds: Number(input("timeout").value),
  };
  if (payload.format === "custom-json") {
    try {
      JSON.parse(payload.payload_template || integrationDefaultPayload);
    } catch {
      throw new Error("Payload template must be valid JSON with placeholders inside string values.");
    }
  }
  if (input("url").value.trim()) payload.url = input("url").value.trim();
  const headers = input("headers").value.trim();
  if (headers && input("clear-headers").checked) throw new Error("Choose replacement headers or clear headers.");
  if (headers) {
    try {
      const parsed = JSON.parse(headers);
      if (
        !parsed ||
        Array.isArray(parsed) ||
        typeof parsed !== "object" ||
        Object.values(parsed).some((value) => typeof value !== "string")
      ) {
        throw new Error();
      }
      payload.headers = parsed;
    } catch {
      throw new Error("Headers must be a JSON object containing string values.");
    }
  } else if (input("clear-headers").checked) payload.headers = {};
  return payload;
}

async function saveIntegration(): Promise<void> {
  if (!requirePanelReady("integrations")) return;
  let payload: IntegrationUpdate;
  try {
    payload = collectIntegrationPayload();
  } catch (error) {
    setStatus("integrations-status", (error as Error).message, "error");
    return;
  }
  const response = await fetch("/api/integrations", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body: IntegrationsResponse = await response.json();
  if (!response.ok) {
    setStatus("integrations-status", body.detail || "Save failed.", "error");
    return;
  }
  clearIntegrationSecrets();
  await loadIntegrations(body.destination?.id);
  setStatus("integrations-status", "Saved. Secret values are never returned.", "success");
}

async function testIntegration(): Promise<void> {
  if (!requirePanelReady("integrations")) return;
  const id = (document.getElementById("integration-select") as HTMLSelectElement).value;
  if (!id) return;
  setStatus("integrations-status", "Testing the saved destination…", "info");
  const response = await fetch(`/api/integrations/${encodeURIComponent(id)}/test`, { method: "POST" });
  const body: ApiBody = await response.json();
  setStatus(
    "integrations-status",
    response.ok ? "Test delivered. Tests use saved values, including when disabled." : body.detail || "Test failed.",
    response.ok ? "success" : "error",
  );
}

async function deleteIntegration(): Promise<void> {
  if (!requirePanelReady("integrations")) return;
  const id = (document.getElementById("integration-select") as HTMLSelectElement).value;
  if (
    !id ||
    !(await confirmApp({
      title: "Delete integration",
      message: "Remove this destination and its stored secrets?",
      confirmLabel: "Delete",
      danger: true,
    }))
  )
    return;
  const response = await fetch(`/api/integrations/${encodeURIComponent(id)}`, { method: "DELETE" });
  const body: ApiBody = await response.json();
  if (!response.ok) {
    setStatus("integrations-status", body.detail || "Delete failed.", "error");
    return;
  }
  await loadIntegrations();
  setStatus("integrations-status", "Integration deleted.", "success");
}

globalThis.addEventListener?.("DOMContentLoaded", () => {
  document.getElementById("integrations-dialog")?.addEventListener("close", clearIntegrationSecrets);
});
globalThis.addEventListener?.("pagehide", clearIntegrationSecrets);
