async function openNtfyDialog(): Promise<void> {
  clearStatus("ntfy-status");
  openDialog("ntfy-dialog");
  try {
    await loadNtfyConfig();
  } catch (error) {
    setActionStatus((error as Error).message || "Failed to load ntfy settings.", "error");
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
    response.ok ? "Connection test succeeded." : body.detail || "Test failed.",
    response.ok ? "success" : "error",
  );
  if (response.ok) {
    setActionStatus("ntfy connection test succeeded.", "success");
  } else {
    setActionStatus(body.detail || "ntfy test failed.", "error");
  }
}
