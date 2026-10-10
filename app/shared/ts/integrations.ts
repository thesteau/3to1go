let integrationDestinations: IntegrationDestination[] = [];
let integrationEvents: string[] = [];

async function openIntegrationsDialog(): Promise<void> {
  clearStatus("integrations-status");
  openDialog("integrations-dialog");
  await loadIntegrations();
}

async function loadIntegrations(selectedID = ""): Promise<void> {
  await loadEditorPanel("integrations", async () => {
    const response = await fetch("/api/integrations", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    const body: IntegrationsResponse = await response.json();
    if (!response.ok) throw new Error(body.detail || "Failed to load integrations.");
    integrationDestinations = body.destinations || [];
    integrationEvents = body.events || [];
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
  (document.getElementById("integration-template") as HTMLTextAreaElement).value = destination?.message_template || "";
  clearIntegrationSecrets();
  input("clear-headers").checked = false;
  input("url").placeholder = destination?.url_configured
    ? "Configured. Leave blank to keep; enter a URL to replace."
    : "https://…";
  input("headers").placeholder = destination?.headers_configured
    ? "Configured. Leave blank to keep; enter JSON to replace."
    : '{"Authorization":"Bearer …"}';
  const events = document.getElementById("integration-events")!;
  events.replaceChildren();
  for (const event of integrationEvents) {
    const label = document.createElement("label");
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox";
    checkbox.value = event;
    checkbox.name = "integration-event";
    checkbox.checked = destination ? destination.events.includes(event) : event !== "job-finished";
    label.append(checkbox, document.createTextNode(` ${event}`));
    events.appendChild(label);
  }
  for (const name of ["test", "delete"]) {
    (document.getElementById(`integration-${name}`) as HTMLButtonElement).hidden = !destination;
  }
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
    include_detail: input("detail").checked,
    timeout_seconds: Number(input("timeout").value),
  };
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
