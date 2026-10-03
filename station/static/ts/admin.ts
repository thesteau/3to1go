let _settingsSnapshot: string | null = null;

// --- Settings ---

function fillSettings(settings: StationSettings | null | undefined): void {
  const data = settings || {};
  (document.getElementById("settings_retention_keep_last") as HTMLInputElement).value = String(
    data.retention_keep_last ?? 3,
  );
  (document.getElementById("settings_log_level") as HTMLSelectElement).value = data.log_level || "INFO";
  applyTheme(data.theme || "dark");
  (document.getElementById("settings_max_upload_size_mb") as HTMLInputElement).value = String(
    data.max_upload_size_mb ?? 2048,
  );
  (document.getElementById("settings_upload_chunk_size_mb") as HTMLInputElement).value = String(
    data.upload_chunk_size_mb ?? 8,
  );
  (document.getElementById("settings_upload_session_ttl_hours") as HTMLInputElement).value = String(
    data.upload_session_ttl_hours ?? 24,
  );
  (document.getElementById("settings_upload_cleanup_interval_seconds") as HTMLInputElement).value = String(
    data.upload_cleanup_interval_seconds ?? 300,
  );
  (document.getElementById("settings_snapshot_verify_interval_hours") as HTMLInputElement).value = String(
    data.snapshot_verify_interval_hours ?? 0,
  );
  setToggle("settings_uploads_paused", data.uploads_paused || false);
  (document.getElementById("settings_anomaly_mode") as HTMLSelectElement).value = data.anomaly_mode || "alert";
}

function collectSettingsPayload(overrides: Partial<StationSettings> = {}): StationSettings {
  return {
    retention_keep_last: Number(
      (document.getElementById("settings_retention_keep_last") as HTMLInputElement).value || 1,
    ),
    log_level: (document.getElementById("settings_log_level") as HTMLSelectElement).value,
    theme: getToggle("settings_theme_dark") ? "dark" : "light",
    max_upload_size_mb: Number((document.getElementById("settings_max_upload_size_mb") as HTMLInputElement).value || 1),
    upload_chunk_size_mb: Number(
      (document.getElementById("settings_upload_chunk_size_mb") as HTMLInputElement).value || 1,
    ),
    upload_session_ttl_hours: Number(
      (document.getElementById("settings_upload_session_ttl_hours") as HTMLInputElement).value || 1,
    ),
    upload_cleanup_interval_seconds: Number(
      (document.getElementById("settings_upload_cleanup_interval_seconds") as HTMLInputElement).value || 10,
    ),
    snapshot_verify_interval_hours: Number(
      (document.getElementById("settings_snapshot_verify_interval_hours") as HTMLInputElement).value || 0,
    ),
    uploads_paused: getToggle("settings_uploads_paused"),
    anomaly_mode: (document.getElementById("settings_anomaly_mode") as HTMLSelectElement).value,
    ntfy_url: window.__stationSettings?.ntfy_url || "",
    ntfy_topic: window.__stationSettings?.ntfy_topic || "",
    ntfy_message_template: window.__stationSettings?.ntfy_message_template || "",
    ntfy_match_scout_id: window.__stationSettings?.ntfy_match_scout_id || "",
    ntfy_match_scout_instance_id: window.__stationSettings?.ntfy_match_scout_instance_id || "",
    ntfy_match_source: window.__stationSettings?.ntfy_match_source || "",
    hook_pre_command: window.__stationSettings?.hook_pre_command || "",
    hook_post_command: window.__stationSettings?.hook_post_command || "",
    ...overrides,
  };
}

async function postSettings(payload: StationSettings): Promise<{ response: Response; body: SettingsResponse }> {
  const response = await fetch("/api/settings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body: SettingsResponse = await response.json();
  return { response, body };
}

async function openSettingsDialog(): Promise<void> {
  if (!requirePanelReady("settings")) return;
  fillSettings(window.__stationSettings || {});
  _settingsSnapshot = JSON.stringify(collectSettingsPayload());
  clearStatus("settings-status");
  clearStatus("certificates-status");
  openDialog("settings-dialog");
  try {
    await loadCertificateConfig();
  } catch (error) {
    setActionStatus((error as Error).message || "Failed to load certificates.", "error");
  }
}

async function saveSettings(): Promise<void> {
  if (!requirePanelReady("settings")) return;
  setStatus("settings-status", "Saving...", "info");
  const payload = collectSettingsPayload();
  const { response, body } = await postSettings(payload);
  setStatus(
    "settings-status",
    response.ok ? "Settings saved. Closing..." : body.detail || "Settings save failed.",
    response.ok ? "success" : "error",
  );
  if (response.ok) {
    window.__stationSettings = body.settings || { ...window.__stationSettings, ...payload };
    applyTheme(window.__stationSettings.theme);
    _settingsSnapshot = null;
    setActionStatus("Station settings saved.", "success");
    await pause(450);
    closeDialog("settings-dialog");
    // Refresh in the background; the snapshot list can take a while.
    loadOverview({ silent: true, force: true });
  } else {
    setActionStatus(body.detail || "Settings save failed.", "error");
  }
}
