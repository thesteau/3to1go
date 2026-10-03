let _centralNtfyConfig: NtfyConfig | null = null;

// --- Ntfy ---

function fillNtfyForm(config: NtfyConfig | null | undefined): void {
  const data = config || {};
  (document.getElementById("ntfy_url") as HTMLInputElement).value = data.ntfy_url || "";
  (document.getElementById("ntfy_topic") as HTMLInputElement).value = data.ntfy_topic || "";
  (document.getElementById("ntfy_match_edge_id") as HTMLInputElement).value = data.ntfy_match_edge_id || "";
  (document.getElementById("ntfy_match_edge_instance_id") as HTMLInputElement).value =
    data.ntfy_match_edge_instance_id || "";
  (document.getElementById("ntfy_match_source") as HTMLInputElement).value = data.ntfy_match_source || "";
  (document.getElementById("ntfy_message_template") as HTMLTextAreaElement).value =
    data.ntfy_message_template || data.default_message_template || "";
}

async function loadNtfyConfig(): Promise<NtfyConfig> {
  return loadEditorPanel("ntfy", async () => {
    const response = await fetch("/api/ntfy", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    const body: NtfyConfig = await response.json();
    if (!response.ok) {
      throw new Error(body.detail || "Failed to load ntfy settings.");
    }
    _centralNtfyConfig = body;
    fillNtfyForm(body);
    return body;
  });
}

function collectNtfyPayload(): NtfyConfig {
  return {
    ntfy_url: (document.getElementById("ntfy_url") as HTMLInputElement).value.trim(),
    ntfy_topic: (document.getElementById("ntfy_topic") as HTMLInputElement).value.trim(),
    ntfy_match_edge_id: (document.getElementById("ntfy_match_edge_id") as HTMLInputElement).value.trim(),
    ntfy_match_edge_instance_id: (
      document.getElementById("ntfy_match_edge_instance_id") as HTMLInputElement
    ).value.trim(),
    ntfy_match_source: (document.getElementById("ntfy_match_source") as HTMLInputElement).value.trim(),
    ntfy_message_template: (document.getElementById("ntfy_message_template") as HTMLTextAreaElement).value.trim(),
  };
}

function resetNtfyDefaults(): void {
  const defaults = _centralNtfyConfig || {};
  (document.getElementById("ntfy_url") as HTMLInputElement).value = "https://ntfy.sh";
  (document.getElementById("ntfy_topic") as HTMLInputElement).value = "";
  (document.getElementById("ntfy_match_edge_id") as HTMLInputElement).value = "";
  (document.getElementById("ntfy_match_edge_instance_id") as HTMLInputElement).value = "";
  (document.getElementById("ntfy_match_source") as HTMLInputElement).value = "";
  (document.getElementById("ntfy_message_template") as HTMLTextAreaElement).value =
    defaults.default_message_template || "";
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
  setStatus("ntfy-status", response.ok ? "Saved." : body.detail || "Save failed.", response.ok ? "success" : "error");
  if (response.ok) {
    await loadCentralSettings();
    await loadNtfyConfig();
    setActionStatus("Central ntfy settings saved.", "success");
  } else {
    setActionStatus(body.detail || "ntfy save failed.", "error");
  }
}
