let edgeNtfyConfig = null;
let edgeHookConfig = null;
let hookDraftDirty = { pre: false, post: false };

const EDGE_SETTINGS_HELP = {
  settings_edge_id: "A friendly name Central uses to group this Edge with related installations.",
  settings_scan_root: "Edge scans this folder tree for .upload_dir files and available directories.",
  settings_central_url: "The Central server URL Edge uploads backups to.",
  settings_advertised_url: "Optional URL Central displays as a link to this Edge instance.",
  settings_edge_credential: "JWT credential minted from Central. Edge includes this when uploading.",
  settings_cron_schedule: "Five cron fields: minute, hour, day of month, month, day of week.",
  settings_state_dir: "Where Edge keeps retry state, progress, and other local bookkeeping.",
  settings_spool_dir: "Where Edge stages local archive files before and during upload.",
  settings_log_level: "How chatty Edge logs should be.",
  settings_max_depth: "How many nested folders below the scan root Edge will inspect.",
  settings_keep_local_pending: "Keep unfinished local archives on disk so Edge can retry later after a failure.",
  settings_upload_chunk_size_mb: "Preferred chunk size Edge asks Central to accept for each upload part.",
  settings_min_upload_chunk_size_mb: "Smallest chunk Edge will shrink down to when adapting to network conditions.",
  settings_max_upload_chunk_size_mb: "Largest chunk Edge will grow up to when uploads are healthy.",
  settings_upload_retry_max_attempts: "How many times Edge retries a failed upload before requiring manual attention.",
  settings_upload_retry_base_delay_seconds: "Starting delay before retry backoff grows.",
  settings_upload_retry_max_delay_seconds: "Longest delay Edge will wait between upload retries.",
  settings_upload_connect_timeout_seconds: "How long Edge waits to establish a connection to Central.",
  settings_upload_read_timeout_padding_seconds: "Extra read timeout buffer added while upload chunks are streaming.",
  settings_upload_min_throughput_bytes_per_second: "Minimum upload speed Edge expects before treating the connection as stalled.",
  settings_circuit_breaker_failure_threshold: "How many consecutive upload failures cause Edge to pause uploads temporarily.",
  settings_circuit_breaker_cooldown_seconds: "How long Edge waits before trying again after the upload circuit opens.",
};

async function manualRefresh() {
  await loadData();
  setActionStatus("Refreshed.", "success");
}

async function openSettingsDialog() {
  fillSettings(latestData?.settings || {});
  clearStatus("settings-status");
  clearStatus("certificates-status");
  openDialog("settings-dialog");
  try {
    await loadCertificateConfig();
  } catch (error) {
    setActionStatus(error.message || "Failed to load certificates.", "error");
  }
}

function fillNtfyForm(config) {
  const data = config || {};
  document.getElementById("ntfy_url").value = data.ntfy_url || "";
  document.getElementById("ntfy_topic").value = data.ntfy_topic || "";
  document.getElementById("ntfy_message_template").value = data.ntfy_message_template || data.default_message_template || "";
}

async function loadNtfyConfig() {
  const response = await fetch("/api/ntfy");
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.detail || "Failed to load ntfy settings.");
  }
  edgeNtfyConfig = body;
  fillNtfyForm(body);
  return body;
}

async function openNtfyDialog() {
  clearStatus("ntfy-status");
  openDialog("ntfy-dialog");
  try {
    await loadNtfyConfig();
  } catch (error) {
    setActionStatus(error.message || "Failed to load ntfy settings.", "error");
  }
}

function collectNtfyPayload() {
  return {
    ntfy_url: document.getElementById("ntfy_url").value.trim(),
    ntfy_topic: document.getElementById("ntfy_topic").value.trim(),
    ntfy_message_template: document.getElementById("ntfy_message_template").value.trim(),
  };
}

function resetNtfyDefaults() {
  document.getElementById("ntfy_url").value = "https://ntfy.sh";
  document.getElementById("ntfy_topic").value = "";
  document.getElementById("ntfy_message_template").value = edgeNtfyConfig?.default_message_template || "";
}

async function saveNtfyConfig() {
  setStatus("ntfy-status", "Saving...", "info");
  const response = await fetch("/api/ntfy", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(collectNtfyPayload()),
  });
  const body = await response.json();
  setStatus("ntfy-status", response.ok ? "Saved." : (body.detail || "Save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadData({ silent: true });
    await loadNtfyConfig();
    setActionStatus("Edge ntfy settings saved.", "success");
  } else {
    setActionStatus(body.detail || "ntfy save failed.", "error");
  }
}

async function testNtfyConfig() {
  const response = await fetch("/api/ntfy/test", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(collectNtfyPayload()),
  });
  const body = await response.json();
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

function renderCertificateFiles(files) {
  const items = files || [];
  if (!items.length) {
    return '<p class="hint">No certificates saved yet.</p>';
  }
  return items.map((file) => `
    <div class="hook-file-row">
      <div class="hook-file-main">
        <strong>${escapeHtml(file.name)}</strong>
        <span class="hint">${escapeHtml(formatBytes(file.size_bytes))}</span>
      </div>
      <div class="hook-file-actions">
        <button type="button" class="danger" onclick="deleteCertificateFile(decodeURIComponent('${encodeURIComponent(file.name)}'))">Delete</button>
      </div>
    </div>
  `).join("");
}

function fillCertificateForm(config) {
  const data = config || {};
  document.getElementById("certificate-dir").textContent = data.cert_dir || "n/a";
  document.getElementById("certificate-files").innerHTML = renderCertificateFiles(data.files || []);
}

async function loadCertificateConfig() {
  const response = await fetch("/api/certificates");
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.detail || "Failed to load certificates.");
  }
  fillCertificateForm(body);
  return body;
}

async function uploadCertificateFile() {
  const input = document.getElementById("certificate_file_input");
  const file = input?.files?.[0];
  if (!file) {
    setStatus("certificates-status", "Choose a certificate first.", "error");
    return;
  }
  const formData = new FormData();
  formData.append("certificate_file", file);
  const response = await fetch("/api/certificates/files", { method: "POST", body: formData });
  const body = await response.json();
  setStatus("certificates-status", response.ok ? "Certificate uploaded." : (body.detail || "Upload failed."), response.ok ? "success" : "error");
  if (response.ok) {
    input.value = "";
    await loadCertificateConfig();
    setActionStatus(`Uploaded ${file.name}.`, "success");
  } else {
    setActionStatus(body.detail || "Certificate upload failed.", "error");
  }
}

async function deleteCertificateFile(filename) {
  if (!await confirmApp({
    title: "Delete Certificate",
    message: `Delete ${filename}?`,
    confirmLabel: "Delete",
    danger: true,
  })) {
    return;
  }
  const response = await fetch(`/api/certificates/files/${encodeURIComponent(filename)}`, { method: "DELETE" });
  const body = await response.json();
  setStatus("certificates-status", response.ok ? "Certificate deleted." : (body.detail || "Delete failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadCertificateConfig();
    setActionStatus(`Deleted ${filename}.`, "success");
  } else {
    setActionStatus(body.detail || "Certificate delete failed.", "error");
  }
}

function renderHookFiles(files) {
  const items = files || [];
  if (!items.length) {
    return '<p class="hint">No files saved yet.</p>';
  }
  return items.map((file) => `
    <div class="hook-file-row">
      <div class="hook-file-main">
        <strong>${escapeHtml(file.name)}</strong>
        <span class="hint">${escapeHtml(formatBytes(file.size_bytes))}</span>
      </div>
      <div class="hook-file-actions">
        <button type="button" class="secondary" onclick="viewHookFile(decodeURIComponent('${encodeURIComponent(file.name)}'), ${file.viewable ? "true" : "false"})">View</button>
        <button type="button" class="danger" onclick="deleteHookFile(decodeURIComponent('${encodeURIComponent(file.name)}'))">Delete</button>
      </div>
    </div>
  `).join("");
}

function fillHookForm(config, { preserveDrafts = true } = {}) {
  const data = config || {};
  document.getElementById("hook-script-dir").textContent = data.script_dir || "n/a";
  if (!preserveDrafts || !hookDraftDirty.pre) {
    document.getElementById("hook_pre_command").value = data.pre_command || "";
    hookDraftDirty.pre = false;
  }
  if (!preserveDrafts || !hookDraftDirty.post) {
    document.getElementById("hook_post_command").value = data.post_command || "";
    hookDraftDirty.post = false;
  }
  document.getElementById("hook-files").innerHTML = renderHookFiles(data.files || []);
}

async function loadHookConfig({ preserveDrafts = true } = {}) {
  const response = await fetch("/api/hooks");
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.detail || "Failed to load hook settings.");
  }
  edgeHookConfig = body;
  fillHookForm(body, { preserveDrafts });
  return body;
}

async function openHooksDialog() {
  clearStatus("hooks-status");
  openDialog("hooks-dialog");
  try {
    await loadHookConfig({ preserveDrafts: false });
  } catch (error) {
    setActionStatus(error.message || "Failed to load hook settings.", "error");
  }
}

function clearHookCommand(kind) {
  const input = document.getElementById(kind === "pre" ? "hook_pre_command" : "hook_post_command");
  if (!input) return;
  input.value = "";
  hookDraftDirty[kind] = true;
}

async function saveHookCommands() {
  setStatus("hooks-status", "Saving...", "info");
  const payload = {
    hook_pre_command: document.getElementById("hook_pre_command").value.trim(),
    hook_post_command: document.getElementById("hook_post_command").value.trim(),
  };
  const response = await fetch("/api/hooks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await response.json();
  setStatus("hooks-status", response.ok ? "Commands saved." : (body.detail || "Save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    hookDraftDirty = { pre: false, post: false };
    await loadData({ silent: true });
    await loadHookConfig({ preserveDrafts: false });
    setActionStatus("Edge hook commands saved.", "success");
  } else {
    setActionStatus(body.detail || "Hook save failed.", "error");
  }
}

async function uploadHookFile() {
  const input = document.getElementById("hook_file_input");
  const file = input?.files?.[0];
  if (!file) {
    setStatus("hooks-status", "Choose a file first.", "error");
    return;
  }
  const formData = new FormData();
  formData.append("hook_file", file);
  const response = await fetch("/api/hooks/files", { method: "POST", body: formData });
  const body = await response.json();
  setStatus("hooks-status", response.ok ? "File uploaded." : (body.detail || "Upload failed."), response.ok ? "success" : "error");
  if (response.ok) {
    input.value = "";
    await loadHookConfig({ preserveDrafts: true });
    setActionStatus(`Uploaded ${file.name}.`, "success");
  } else {
    setActionStatus(body.detail || "Hook upload failed.", "error");
  }
}

async function viewHookFile(filename, viewable) {
  if (!viewable) {
    setActionStatus("This file cannot be viewed.", "error");
    return;
  }
  const response = await fetch(`/api/hooks/files/${encodeURIComponent(filename)}`);
  const body = await response.json();
  if (!response.ok) {
    setActionStatus(body.detail || "View failed.", "error");
    return;
  }
  document.getElementById("hook-view-filename").textContent = body.filename || filename;
  document.getElementById("hook-view-content").value = body.content || "";
  openDialog("hook-view-dialog");
}

async function deleteHookFile(filename) {
  if (!await confirmApp({
    title: "Delete Hook File",
    message: `Delete ${filename}?`,
    confirmLabel: "Delete",
    danger: true,
  })) {
    return;
  }
  const response = await fetch(`/api/hooks/files/${encodeURIComponent(filename)}`, { method: "DELETE" });
  const body = await response.json();
  setStatus("hooks-status", response.ok ? "File deleted." : (body.detail || "Delete failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadHookConfig({ preserveDrafts: true });
    setActionStatus(`Deleted ${filename}.`, "success");
  } else {
    setActionStatus(body.detail || "Hook delete failed.", "error");
  }
}

function fillSettings(settings) {
  const data = settings || {};
  document.getElementById("settings_edge_id").value = data.edge_id || "";
  document.getElementById("settings_scan_root").value = data.scan_root || "";
  document.getElementById("settings_central_url").value = data.central_url || "";
  document.getElementById("settings_advertised_url").value = data.advertised_url || "";
  document.getElementById("settings_edge_credential").value = data.edge_credential || "";
  document.getElementById("settings_cron_schedule").value = data.cron_schedule || "";
  document.getElementById("settings_state_dir").value = data.state_dir || "";
  document.getElementById("settings_spool_dir").value = data.spool_dir || "";
  document.getElementById("settings_log_level").value = data.log_level || "INFO";
  applyTheme(data.theme || "dark");
  document.getElementById("settings_max_depth").value = data.max_depth ?? 10;
  document.getElementById("settings_keep_local_pending").checked = data.keep_local_pending ?? true;
  document.getElementById("settings_upload_chunk_size_mb").value = data.upload_chunk_size_mb ?? 8;
  document.getElementById("settings_min_upload_chunk_size_mb").value = data.min_upload_chunk_size_mb ?? 1;
  document.getElementById("settings_max_upload_chunk_size_mb").value = data.max_upload_chunk_size_mb ?? 16;
  document.getElementById("settings_upload_retry_max_attempts").value = data.upload_retry_max_attempts ?? 5;
  document.getElementById("settings_upload_retry_base_delay_seconds").value = data.upload_retry_base_delay_seconds ?? 5;
  document.getElementById("settings_upload_retry_max_delay_seconds").value = data.upload_retry_max_delay_seconds ?? 300;
  document.getElementById("settings_upload_connect_timeout_seconds").value = data.upload_connect_timeout_seconds ?? 10;
  document.getElementById("settings_upload_read_timeout_padding_seconds").value = data.upload_read_timeout_padding_seconds ?? 30;
  document.getElementById("settings_upload_min_throughput_bytes_per_second").value = data.upload_min_throughput_bytes_per_second ?? 262144;
  document.getElementById("settings_circuit_breaker_failure_threshold").value = data.circuit_breaker_failure_threshold ?? 5;
  document.getElementById("settings_circuit_breaker_cooldown_seconds").value = data.circuit_breaker_cooldown_seconds ?? 300;
  updateCronScheduleHint();
}

function collectSettingsPayload(overrides = {}) {
  return {
    edge_id: document.getElementById("settings_edge_id").value.trim(),
    scan_root: document.getElementById("settings_scan_root").value.trim(),
    central_url: document.getElementById("settings_central_url").value.trim(),
    advertised_url: document.getElementById("settings_advertised_url").value.trim(),
    edge_credential: document.getElementById("settings_edge_credential").value,
    cron_schedule: document.getElementById("settings_cron_schedule").value.trim(),
    state_dir: document.getElementById("settings_state_dir").value.trim(),
    spool_dir: document.getElementById("settings_spool_dir").value.trim(),
    log_level: document.getElementById("settings_log_level").value,
    theme: document.getElementById("settings_theme_dark")?.checked ? "dark" : "light",
    max_depth: Number(document.getElementById("settings_max_depth").value || 0),
    keep_local_pending: document.getElementById("settings_keep_local_pending").checked,
    upload_chunk_size_mb: Number(document.getElementById("settings_upload_chunk_size_mb").value || 1),
    min_upload_chunk_size_mb: Number(document.getElementById("settings_min_upload_chunk_size_mb").value || 1),
    max_upload_chunk_size_mb: Number(document.getElementById("settings_max_upload_chunk_size_mb").value || 1),
    upload_retry_max_attempts: Number(document.getElementById("settings_upload_retry_max_attempts").value || 1),
    upload_retry_base_delay_seconds: Number(document.getElementById("settings_upload_retry_base_delay_seconds").value || 1),
    upload_retry_max_delay_seconds: Number(document.getElementById("settings_upload_retry_max_delay_seconds").value || 1),
    upload_connect_timeout_seconds: Number(document.getElementById("settings_upload_connect_timeout_seconds").value || 1),
    upload_read_timeout_padding_seconds: Number(document.getElementById("settings_upload_read_timeout_padding_seconds").value || 5),
    upload_min_throughput_bytes_per_second: Number(document.getElementById("settings_upload_min_throughput_bytes_per_second").value || 1024),
    circuit_breaker_failure_threshold: Number(document.getElementById("settings_circuit_breaker_failure_threshold").value || 1),
    circuit_breaker_cooldown_seconds: Number(document.getElementById("settings_circuit_breaker_cooldown_seconds").value || 1),
    ntfy_url: latestData?.settings?.ntfy_url || "",
    ntfy_topic: latestData?.settings?.ntfy_topic || "",
    ntfy_message_template: latestData?.settings?.ntfy_message_template || "",
    hook_pre_command: latestData?.settings?.hook_pre_command || "",
    hook_post_command: latestData?.settings?.hook_post_command || "",
    ...overrides,
  };
}

async function postSettings(payload) {
  const response = await fetch("/api/settings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await response.json();
  return { response, body };
}

async function saveSettings() {
  setStatus("settings-status", "Saving...", "info");
  const payload = collectSettingsPayload();
  const { response, body } = await postSettings(payload);
  setStatus("settings-status", response.ok ? "Saved." : (body.detail || "Settings save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    if (latestData && body.settings) {
      latestData.settings = body.settings;
    }
    applyTheme(latestData?.settings?.theme || payload.theme);
    setActionStatus("Edge settings saved.", "success");
    await pause(350);
    closeDialog("settings-dialog");
    loadData({ silent: true, refreshDirectoryTree: true });
  } else {
    setActionStatus(body.detail || "Settings save failed.", "error");
  }
}
