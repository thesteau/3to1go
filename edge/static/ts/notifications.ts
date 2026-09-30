let edgeNtfyConfig: NtfyConfig | null = null;

function fillNtfyForm(config: NtfyConfig | null | undefined): void {
  const data = config || {};
  (document.getElementById("ntfy_url") as HTMLInputElement).value = data.ntfy_url || "";
  (document.getElementById("ntfy_topic") as HTMLInputElement).value = data.ntfy_topic || "";
  (document.getElementById("ntfy_message_template") as HTMLTextAreaElement).value = data.ntfy_message_template || data.default_message_template || "";
}

async function loadNtfyConfig(): Promise<NtfyConfig> {
  return loadEditorPanel("ntfy", async () => {
  const response = await fetch("/api/ntfy", { signal: globalThis.AbortSignal?.timeout?.(30000) });
  const body: NtfyConfig = await response.json();
  if (!response.ok) {
    throw new Error(body.detail || "Failed to load ntfy settings.");
  }
  edgeNtfyConfig = body;
  fillNtfyForm(body);
  return body;
  });
}

async function openNtfyDialog(): Promise<void> {
  clearStatus("ntfy-status");
  openDialog("ntfy-dialog");
  try {
    await loadNtfyConfig();
  } catch (error) {
    setActionStatus((error as Error).message || "Failed to load ntfy settings.", "error");
  }
}

function collectNtfyPayload(): NtfyConfig {
  return {
    ntfy_url: (document.getElementById("ntfy_url") as HTMLInputElement).value.trim(),
    ntfy_topic: (document.getElementById("ntfy_topic") as HTMLInputElement).value.trim(),
    ntfy_message_template: (document.getElementById("ntfy_message_template") as HTMLTextAreaElement).value.trim(),
  };
}

function resetNtfyDefaults(): void {
  (document.getElementById("ntfy_url") as HTMLInputElement).value = "https://ntfy.sh";
  (document.getElementById("ntfy_topic") as HTMLInputElement).value = "";
  (document.getElementById("ntfy_message_template") as HTMLTextAreaElement).value = edgeNtfyConfig?.default_message_template || "";
}

async function saveNtfyConfig(): Promise<void> {
  if (!requirePanelReady("ntfy")) return;
  setStatus("ntfy-status", "Saving...", "info");
  const response = await fetch("/api/ntfy", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(collectNtfyPayload()),
  });
  const body: ApiBody = await response.json();
  setStatus("ntfy-status", response.ok ? "Saved." : (body.detail || "Save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadData({ silent: true });
    await loadNtfyConfig();
    setActionStatus("Edge ntfy settings saved.", "success");
  } else {
    setActionStatus(body.detail || "ntfy save failed.", "error");
  }
}

async function testNtfyConfig(): Promise<void> {
  if (!requirePanelReady("ntfy")) return;
  const response = await fetch("/api/ntfy/test", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(collectNtfyPayload()),
  });
  const body: ApiBody = await response.json();
  setStatus(
    "ntfy-status",
    response.ok ? "Connection test succeeded." : (body.detail || "Test failed."),
    response.ok ? "success" : "error",
  );
  if (response.ok) {
    setActionStatus("ntfy connection test succeeded.", "success");
  } else {
    setActionStatus(body.detail || "ntfy test failed.", "error");
  }
}
