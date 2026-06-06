let _centralNtfyConfig = null;
let _centralHookConfig = null;
let _hookDraftDirty = { pre: false, post: false };

// --- Settings ---

function fillSettings(settings) {
  const data = settings || {};
  document.getElementById("settings_retention_keep_last").value = data.retention_keep_last ?? 3;
  document.getElementById("settings_log_level").value = data.log_level || "INFO";
  applyTheme(data.theme || "dark");
  document.getElementById("settings_max_upload_size_mb").value = data.max_upload_size_mb ?? 2048;
  document.getElementById("settings_upload_chunk_size_mb").value = data.upload_chunk_size_mb ?? 8;
  document.getElementById("settings_upload_session_ttl_hours").value = data.upload_session_ttl_hours ?? 24;
  document.getElementById("settings_upload_cleanup_interval_seconds").value = data.upload_cleanup_interval_seconds ?? 300;
}

function collectSettingsPayload(overrides = {}) {
  return {
    retention_keep_last: Number(document.getElementById("settings_retention_keep_last").value || 1),
    log_level: document.getElementById("settings_log_level").value,
    theme: document.getElementById("settings_theme_dark")?.checked ? "dark" : "light",
    max_upload_size_mb: Number(document.getElementById("settings_max_upload_size_mb").value || 1),
    upload_chunk_size_mb: Number(document.getElementById("settings_upload_chunk_size_mb").value || 1),
    upload_session_ttl_hours: Number(document.getElementById("settings_upload_session_ttl_hours").value || 1),
    upload_cleanup_interval_seconds: Number(document.getElementById("settings_upload_cleanup_interval_seconds").value || 10),
    ntfy_url: window.__centralSettings?.ntfy_url || "",
    ntfy_topic: window.__centralSettings?.ntfy_topic || "",
    ntfy_message_template: window.__centralSettings?.ntfy_message_template || "",
    ntfy_match_edge_id: window.__centralSettings?.ntfy_match_edge_id || "",
    ntfy_match_edge_instance_id: window.__centralSettings?.ntfy_match_edge_instance_id || "",
    ntfy_match_source: window.__centralSettings?.ntfy_match_source || "",
    hook_pre_command: window.__centralSettings?.hook_pre_command || "",
    hook_post_command: window.__centralSettings?.hook_post_command || "",
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

async function openSettingsDialog() {
  fillSettings(window.__centralSettings || {});
  clearStatus("settings-status");
  clearStatus("certificates-status");
  openDialog("settings-dialog");
  try {
    await loadCertificateConfig();
  } catch (error) {
    setActionStatus(error.message || "Failed to load certificates.", "error");
  }
}

async function saveSettings() {
  setStatus("settings-status", "Saving...", "info");
  const payload = collectSettingsPayload();
  const { response, body } = await postSettings(payload);
  setStatus("settings-status", response.ok ? "Settings saved. Closing..." : (body.detail || "Settings save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    window.__centralSettings = body.settings || { ...window.__centralSettings, ...payload };
    applyTheme(window.__centralSettings.theme);
    await loadOverview({ silent: true, force: true });
    setActionStatus("Central settings saved.", "success");
    await pause(450);
    closeDialog("settings-dialog");
  } else {
    setActionStatus(body.detail || "Settings save failed.", "error");
  }
}

// --- Credentials ---

function openCredentialDialog() {
  document.getElementById("credential_ttl_days").value = "365";
  document.getElementById("credential_output").value = "";
  clearStatus("credential-status");
  openDialog("credential-dialog");
}

async function mintCredential() {
  const ttlDays = Number(document.getElementById("credential_ttl_days").value || 365);
  setStatus("credential-status", "Minting...", "info");
  const response = await fetch(`/api/credentials/mint?ttl_days=${encodeURIComponent(ttlDays)}`, {
    method: "POST",
  });
  const body = await readJson(response);
  if (!response.ok) {
    setStatus("credential-status", body.detail || "Mint failed.", "error");
    setActionStatus(body.detail || "Mint failed.", "error");
    return;
  }
  document.getElementById("credential_output").value = body.credential || "";
  setStatus("credential-status", "Credential minted. Copy it before closing.", "success");
  setActionStatus("Edge credential minted.", "success");
}

async function copyMintedCredential() {
  const value = document.getElementById("credential_output").value.trim();
  if (!value) {
    setStatus("credential-status", "Mint a credential first.", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    const output = document.getElementById("credential_output");
    output.focus();
    output.select();
    document.execCommand("copy");
  }
  setStatus("credential-status", "Copied.", "success");
}

// --- Ntfy ---

function fillNtfyForm(config) {
  const data = config || {};
  document.getElementById("ntfy_url").value = data.ntfy_url || "";
  document.getElementById("ntfy_topic").value = data.ntfy_topic || "";
  document.getElementById("ntfy_match_edge_id").value = data.ntfy_match_edge_id || "";
  document.getElementById("ntfy_match_edge_instance_id").value = data.ntfy_match_edge_instance_id || "";
  document.getElementById("ntfy_match_source").value = data.ntfy_match_source || "";
  document.getElementById("ntfy_message_template").value = data.ntfy_message_template || data.default_message_template || "";
}

async function loadNtfyConfig() {
  const response = await fetch("/api/ntfy");
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.detail || "Failed to load ntfy settings.");
  }
  _centralNtfyConfig = body;
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
    ntfy_match_edge_id: document.getElementById("ntfy_match_edge_id").value.trim(),
    ntfy_match_edge_instance_id: document.getElementById("ntfy_match_edge_instance_id").value.trim(),
    ntfy_match_source: document.getElementById("ntfy_match_source").value.trim(),
    ntfy_message_template: document.getElementById("ntfy_message_template").value.trim(),
  };
}

function resetNtfyDefaults() {
  const defaults = _centralNtfyConfig || {};
  document.getElementById("ntfy_url").value = "";
  document.getElementById("ntfy_topic").value = "";
  document.getElementById("ntfy_match_edge_id").value = "";
  document.getElementById("ntfy_match_edge_instance_id").value = "";
  document.getElementById("ntfy_match_source").value = "";
  document.getElementById("ntfy_message_template").value = defaults.default_message_template || "";
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
    await loadOverview({ silent: true, force: true });
    await loadNtfyConfig();
    setActionStatus("Central ntfy settings saved.", "success");
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

// --- Certificates ---

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
        <button type="button" class="btn btn-del" onclick="deleteCertificateFile(decodeURIComponent('${encodeURIComponent(file.name)}'))">Delete</button>
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

// --- Hooks ---

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
        <button type="button" class="btn btn-del" onclick="deleteHookFile(decodeURIComponent('${encodeURIComponent(file.name)}'))">Delete</button>
      </div>
    </div>
  `).join("");
}

function fillHookForm(config, { preserveDrafts = true } = {}) {
  const data = config || {};
  document.getElementById("hook-script-dir").textContent = data.script_dir || "n/a";
  if (!preserveDrafts || !_hookDraftDirty.pre) {
    document.getElementById("hook_pre_command").value = data.pre_command || "";
    _hookDraftDirty.pre = false;
  }
  if (!preserveDrafts || !_hookDraftDirty.post) {
    document.getElementById("hook_post_command").value = data.post_command || "";
    _hookDraftDirty.post = false;
  }
  document.getElementById("hook-files").innerHTML = renderHookFiles(data.files || []);
}

async function loadHookConfig({ preserveDrafts = true } = {}) {
  const response = await fetch("/api/hooks");
  const body = await response.json();
  if (!response.ok) {
    throw new Error(body.detail || "Failed to load hook settings.");
  }
  _centralHookConfig = body;
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
  _hookDraftDirty[kind] = true;
}

async function saveHookCommands() {
  setStatus("hooks-status", "Saving...", "info");
  const payload = {
    pre_command: document.getElementById("hook_pre_command").value.trim(),
    post_command: document.getElementById("hook_post_command").value.trim(),
  };
  const response = await fetch("/api/hooks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await response.json();
  setStatus("hooks-status", response.ok ? "Commands saved." : (body.detail || "Save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    _hookDraftDirty = { pre: false, post: false };
    await loadOverview({ silent: true, force: true });
    await loadHookConfig({ preserveDrafts: false });
    setActionStatus("Central hook commands saved.", "success");
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
