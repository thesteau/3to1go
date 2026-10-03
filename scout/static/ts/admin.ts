let _settingsSnapshot: string | null = null;

const SCOUT_SETTINGS_HELP: Record<string, string> = {
  settings_scout_id: "A friendly name Station uses to group this Scout with related installations.",
  settings_station_url: "The Station server URL Scout uploads backups to.",
  settings_advertised_url: "Optional URL Station displays as a link to this Scout instance.",
  settings_scout_credential: "JWT credential minted from Station. Scout includes this when uploading.",
  settings_cron_schedule: "Five cron fields: minute, hour, day of month, month, day of week.",
  settings_state_dir: "Where Scout keeps retry state, progress, and other local bookkeeping.",
  settings_spool_dir: "Where Scout stages local archive files before and during upload.",
  settings_log_level: "How chatty Scout logs should be.",
  settings_max_depth: "How many nested folders below the scan root Scout will inspect.",
  settings_keep_local_pending: "Keep unfinished local archives on disk so Scout can retry later after a failure.",
  settings_upload_chunk_size_mb: "Preferred chunk size Scout asks Station to accept for each upload part.",
  settings_min_upload_chunk_size_mb: "Smallest chunk Scout will shrink down to when adapting to network conditions.",
  settings_max_upload_chunk_size_mb: "Largest chunk Scout will grow up to when uploads are healthy.",
  settings_upload_retry_max_attempts: "How many times Scout retries a failed upload before requiring manual attention.",
  settings_upload_retry_base_delay_seconds: "Starting delay before retry backoff grows.",
  settings_upload_retry_max_delay_seconds: "Longest delay Scout will wait between upload retries.",
  settings_upload_connect_timeout_seconds: "How long Scout waits to establish a connection to Station.",
  settings_upload_read_timeout_padding_seconds: "Extra read timeout buffer added while upload chunks are streaming.",
  settings_upload_min_throughput_bytes_per_second:
    "Minimum upload speed Scout expects before treating the connection as stalled.",
  settings_circuit_breaker_failure_threshold:
    "How many consecutive upload failures cause Scout to pause uploads temporarily.",
  settings_circuit_breaker_cooldown_seconds: "How long Scout waits before trying again after the upload circuit opens.",
};

async function manualRefresh(): Promise<void> {
  await loadData();
  setActionStatus("Refreshed.", "success");
}

async function openSettingsDialog(): Promise<void> {
  if (!requirePanelReady("settings")) return;
  fillSettings(latestData?.settings || {});
  _settingsSnapshot = JSON.stringify(collectSettingsPayload());
  clearStatus("settings-status");
  clearStatus("certificates-status");
  const httpWarning = document.getElementById("settings-http-warning");
  if (httpWarning) {
    httpWarning.style.display = window.location.protocol === "http:" ? "" : "none";
  }
  openDialog("settings-dialog");
  try {
    await loadCertificateConfig();
  } catch (error) {
    setActionStatus((error as Error).message || "Failed to load certificates.", "error");
  }
}

function fillSettings(settings: ScoutSettings | null | undefined): void {
  const data = settings || {};
  const input = (id: string) => document.getElementById(id) as HTMLInputElement;
  input("settings_scout_id").value = data.scout_id || "";
  input("settings_station_url").value = data.station_url || "";
  input("settings_advertised_url").value = data.advertised_url || "";
  input("settings_scout_credential").value = data.scout_credential || "";
  input("settings_cron_schedule").value = data.cron_schedule || "";
  input("settings_state_dir").value = data.state_dir || "";
  input("settings_spool_dir").value = data.spool_dir || "";
  (document.getElementById("settings_log_level") as HTMLSelectElement).value = data.log_level || "INFO";
  applyTheme(data.theme || "dark");
  input("settings_max_depth").value = String(data.max_depth ?? 10);
  setToggle("settings_keep_local_pending", data.keep_local_pending ?? true);
  setToggle("settings_uploads_paused", data.uploads_paused || false);
  (document.getElementById("settings_anomaly_mode") as HTMLSelectElement).value = data.anomaly_mode || "hold";
  input("settings_upload_chunk_size_mb").value = String(data.upload_chunk_size_mb ?? 8);
  input("settings_min_upload_chunk_size_mb").value = String(data.min_upload_chunk_size_mb ?? 1);
  input("settings_max_upload_chunk_size_mb").value = String(data.max_upload_chunk_size_mb ?? 16);
  input("settings_upload_retry_max_attempts").value = String(data.upload_retry_max_attempts ?? 5);
  input("settings_upload_retry_base_delay_seconds").value = String(data.upload_retry_base_delay_seconds ?? 5);
  input("settings_upload_retry_max_delay_seconds").value = String(data.upload_retry_max_delay_seconds ?? 300);
  input("settings_upload_connect_timeout_seconds").value = String(data.upload_connect_timeout_seconds ?? 10);
  input("settings_upload_read_timeout_padding_seconds").value = String(data.upload_read_timeout_padding_seconds ?? 30);
  input("settings_upload_min_throughput_bytes_per_second").value = String(
    data.upload_min_throughput_bytes_per_second ?? 262144,
  );
  input("settings_circuit_breaker_failure_threshold").value = String(data.circuit_breaker_failure_threshold ?? 5);
  input("settings_circuit_breaker_cooldown_seconds").value = String(data.circuit_breaker_cooldown_seconds ?? 300);
  updateCronScheduleHint();
}

function collectSettingsPayload(overrides: Partial<ScoutSettings> = {}): ScoutSettings {
  const value = (id: string) => (document.getElementById(id) as HTMLInputElement | HTMLSelectElement).value;
  return {
    scout_id: value("settings_scout_id").trim(),
    station_url: value("settings_station_url").trim(),
    advertised_url: value("settings_advertised_url").trim(),
    scout_credential: value("settings_scout_credential"),
    cron_schedule: value("settings_cron_schedule").trim(),
    state_dir: value("settings_state_dir").trim(),
    spool_dir: value("settings_spool_dir").trim(),
    log_level: value("settings_log_level"),
    theme: getToggle("settings_theme_dark") ? "dark" : "light",
    max_depth: Number(value("settings_max_depth") || 0),
    keep_local_pending: getToggle("settings_keep_local_pending"),
    uploads_paused: getToggle("settings_uploads_paused"),
    anomaly_mode: value("settings_anomaly_mode"),
    upload_chunk_size_mb: Number(value("settings_upload_chunk_size_mb") || 1),
    min_upload_chunk_size_mb: Number(value("settings_min_upload_chunk_size_mb") || 1),
    max_upload_chunk_size_mb: Number(value("settings_max_upload_chunk_size_mb") || 1),
    upload_retry_max_attempts: Number(value("settings_upload_retry_max_attempts") || 1),
    upload_retry_base_delay_seconds: Number(value("settings_upload_retry_base_delay_seconds") || 1),
    upload_retry_max_delay_seconds: Number(value("settings_upload_retry_max_delay_seconds") || 1),
    upload_connect_timeout_seconds: Number(value("settings_upload_connect_timeout_seconds") || 1),
    upload_read_timeout_padding_seconds: Number(value("settings_upload_read_timeout_padding_seconds") || 5),
    upload_min_throughput_bytes_per_second: Number(value("settings_upload_min_throughput_bytes_per_second") || 1024),
    circuit_breaker_failure_threshold: Number(value("settings_circuit_breaker_failure_threshold") || 1),
    circuit_breaker_cooldown_seconds: Number(value("settings_circuit_breaker_cooldown_seconds") || 1),
    ntfy_url: latestData?.settings?.ntfy_url || "",
    ntfy_topic: latestData?.settings?.ntfy_topic || "",
    ntfy_message_template: latestData?.settings?.ntfy_message_template || "",
    hook_pre_command: latestData?.settings?.hook_pre_command || "",
    hook_post_command: latestData?.settings?.hook_post_command || "",
    ...overrides,
  };
}

async function postSettings(payload: ScoutSettings): Promise<{ response: Response; body: SettingsResponse }> {
  const response = await fetch("/api/settings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body: SettingsResponse = await response.json();
  return { response, body };
}

async function saveSettings(): Promise<void> {
  if (!requirePanelReady("settings")) return;
  setStatus("settings-status", "Saving...", "info");
  const cronInput = document.getElementById("settings_cron_schedule") as HTMLInputElement | null;
  if (cronInput) {
    const cronError = validateCronSchedule(cronInput.value);
    cronInput.setCustomValidity(cronError);
    if (cronError) {
      cronInput.reportValidity();
      cronInput.focus();
      setStatus("settings-status", cronError, "error");
      setActionStatus(cronError, "error");
      return;
    }
  }
  const payload = collectSettingsPayload();
  const { response, body } = await postSettings(payload);
  setStatus(
    "settings-status",
    response.ok ? "Saved." : body.detail || "Settings save failed.",
    response.ok ? "success" : "error",
  );
  if (response.ok) {
    if (latestData && body.settings) {
      latestData.settings = body.settings;
    }
    applyTheme(latestData?.settings?.theme || payload.theme);
    _settingsSnapshot = null;
    setActionStatus("Scout settings saved.", "success");
    await pause(350);
    closeDialog("settings-dialog");
    loadData({ silent: true, refreshDirectoryTree: true });
  } else {
    const detail = body.detail || "Settings save failed.";
    if (cronInput && String(detail).includes("cron_schedule")) {
      cronInput.setCustomValidity(detail);
      cronInput.reportValidity();
      cronInput.focus();
    }
    setActionStatus(detail, "error");
  }
}
