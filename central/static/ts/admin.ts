let _centralNtfyConfig: NtfyConfig | null = null;
let _settingsSnapshot: string | null = null;
let _centralHookConfig: HookConfig | null = null;
let _hookDraftDirty = { pre: false, post: false };

// --- Settings ---

function toggleSettingSwitch(btn: HTMLElement): void {
  const on = btn.getAttribute("aria-checked") !== "true";
  btn.setAttribute("aria-checked", on ? "true" : "false");
  btn.classList.toggle("toggle-on", on);
}

function setToggle(id: string, on: boolean): void {
  const btn = document.getElementById(id);
  if (!btn) return;
  btn.setAttribute("aria-checked", on ? "true" : "false");
  btn.classList.toggle("toggle-on", on);
}

function getToggle(id: string): boolean {
  return document.getElementById(id)?.getAttribute("aria-checked") === "true";
}

function fillSettings(settings: CentralSettings | null | undefined): void {
  const data = settings || {};
  (document.getElementById("settings_retention_keep_last") as HTMLInputElement).value = String(data.retention_keep_last ?? 3);
  (document.getElementById("settings_log_level") as HTMLSelectElement).value = data.log_level || "INFO";
  applyTheme(data.theme || "dark");
  (document.getElementById("settings_max_upload_size_mb") as HTMLInputElement).value = String(data.max_upload_size_mb ?? 2048);
  (document.getElementById("settings_upload_chunk_size_mb") as HTMLInputElement).value = String(data.upload_chunk_size_mb ?? 8);
  (document.getElementById("settings_upload_session_ttl_hours") as HTMLInputElement).value = String(data.upload_session_ttl_hours ?? 24);
  (document.getElementById("settings_upload_cleanup_interval_seconds") as HTMLInputElement).value = String(data.upload_cleanup_interval_seconds ?? 300);
  (document.getElementById("settings_snapshot_verify_interval_hours") as HTMLInputElement).value = String(data.snapshot_verify_interval_hours ?? 0);
  setToggle("settings_uploads_paused", data.uploads_paused || false);
}

function collectSettingsPayload(overrides: Partial<CentralSettings> = {}): CentralSettings {
  return {
    retention_keep_last: Number((document.getElementById("settings_retention_keep_last") as HTMLInputElement).value || 1),
    log_level: (document.getElementById("settings_log_level") as HTMLSelectElement).value,
    theme: getToggle("settings_theme_dark") ? "dark" : "light",
    max_upload_size_mb: Number((document.getElementById("settings_max_upload_size_mb") as HTMLInputElement).value || 1),
    upload_chunk_size_mb: Number((document.getElementById("settings_upload_chunk_size_mb") as HTMLInputElement).value || 1),
    upload_session_ttl_hours: Number((document.getElementById("settings_upload_session_ttl_hours") as HTMLInputElement).value || 1),
    upload_cleanup_interval_seconds: Number((document.getElementById("settings_upload_cleanup_interval_seconds") as HTMLInputElement).value || 10),
    snapshot_verify_interval_hours: Number((document.getElementById("settings_snapshot_verify_interval_hours") as HTMLInputElement).value || 0),
    uploads_paused: getToggle("settings_uploads_paused"),
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

async function postSettings(payload: CentralSettings): Promise<{ response: Response; body: SettingsResponse }> {
  const response = await fetch("/api/settings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body: SettingsResponse = await response.json();
  return { response, body };
}

async function cancelSettings(): Promise<void> {
  if (_settingsSnapshot !== null && JSON.stringify(collectSettingsPayload()) !== _settingsSnapshot) {
    const confirmed = await confirmApp({
      title: "Unsaved Changes",
      message: "You have unsaved changes. Use the Save button to apply them, or discard and close.",
      confirmLabel: "Discard & Close",
    });
    if (!confirmed) return;
  }
  _settingsSnapshot = null;
  closeDialog("settings-dialog");
}

async function openSettingsDialog(): Promise<void> {
  if (!requirePanelReady("settings")) return;
  fillSettings(window.__centralSettings || {});
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
  setStatus("settings-status", response.ok ? "Settings saved. Closing..." : (body.detail || "Settings save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    window.__centralSettings = body.settings || { ...window.__centralSettings, ...payload };
    applyTheme(window.__centralSettings.theme);
    _settingsSnapshot = null;
    await loadOverview({ silent: true, force: true });
    setActionStatus("Central settings saved.", "success");
    await pause(450);
    closeDialog("settings-dialog");
  } else {
    setActionStatus(body.detail || "Settings save failed.", "error");
  }
}

// --- Credentials ---

function openCredentialDialog(): void {
  (document.getElementById("credential_ttl_days") as HTMLInputElement).value = "365";
  (document.getElementById("credential_shared") as HTMLInputElement).checked = false;
  (document.getElementById("credential_max_registrations") as HTMLInputElement).value = "1";
  (document.getElementById("credential_max_registrations") as HTMLInputElement).disabled = true;
  (document.getElementById("credential_output") as HTMLTextAreaElement).value = "";
  clearStatus("credential-status");
  openDialog("credential-dialog");
}

async function handleCredentialSharedToggle(): Promise<void> {
  const sharedInput = document.getElementById("credential_shared") as HTMLInputElement;
  const limitInput = document.getElementById("credential_max_registrations") as HTMLInputElement;
  if (!sharedInput.checked) {
    limitInput.disabled = true;
    return;
  }
  const confirmed = await confirmApp({
    title: "Shared Credential",
    message: "A shared credential can authenticate multiple Edge instances until its limit is reached. Only use this when you intentionally want those instances to share revocation.",
    confirmLabel: "Use Shared",
    danger: true,
  });
  if (!confirmed) {
    sharedInput.checked = false;
    limitInput.disabled = true;
    return;
  }
  limitInput.disabled = false;
  limitInput.focus();
}

async function mintCredential(): Promise<void> {
  const ttlDays = Number((document.getElementById("credential_ttl_days") as HTMLInputElement).value || 365);
  const shared = (document.getElementById("credential_shared") as HTMLInputElement).checked;
  const maxRegistrations = shared ? Number((document.getElementById("credential_max_registrations") as HTMLInputElement).value || 1) : 1;
  if (shared && (maxRegistrations < 2 || maxRegistrations > 10000)) {
    setStatus("credential-status", "Shared instance limit must be between 2 and 10000.", "error");
    return;
  }
  setStatus("credential-status", "Minting...", "info");
  const response = await fetch("/api/credentials/mint", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      ttl_days: ttlDays,
      shared,
      max_registrations: maxRegistrations,
    }),
  });
  const body = await readJson<MintCredentialResponse>(response);
  if (!response.ok) {
    setStatus("credential-status", body.detail || "Mint failed.", "error");
    setActionStatus(body.detail || "Mint failed.", "error");
    return;
  }
  (document.getElementById("credential_output") as HTMLTextAreaElement).value = body.credential || "";
  setStatus("credential-status", body.message || "Credential minted. Copy it before closing.", "success");
  setActionStatus("Edge credential minted.", "success");
}

async function copyMintedCredential(): Promise<void> {
  const value = (document.getElementById("credential_output") as HTMLTextAreaElement).value.trim();
  if (!value) {
    setStatus("credential-status", "Mint a credential first.", "error");
    return;
  }
  try {
    await navigator.clipboard.writeText(value);
  } catch {
    const output = document.getElementById("credential_output") as HTMLTextAreaElement;
    output.focus();
    output.select();
    document.execCommand("copy");
  }
  setStatus("credential-status", "Copied.", "success");
}

// --- Ntfy ---

function fillNtfyForm(config: NtfyConfig | null | undefined): void {
  const data = config || {};
  (document.getElementById("ntfy_url") as HTMLInputElement).value = data.ntfy_url || "";
  (document.getElementById("ntfy_topic") as HTMLInputElement).value = data.ntfy_topic || "";
  (document.getElementById("ntfy_match_edge_id") as HTMLInputElement).value = data.ntfy_match_edge_id || "";
  (document.getElementById("ntfy_match_edge_instance_id") as HTMLInputElement).value = data.ntfy_match_edge_instance_id || "";
  (document.getElementById("ntfy_match_source") as HTMLInputElement).value = data.ntfy_match_source || "";
  (document.getElementById("ntfy_message_template") as HTMLTextAreaElement).value = data.ntfy_message_template || data.default_message_template || "";
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
    ntfy_match_edge_id: (document.getElementById("ntfy_match_edge_id") as HTMLInputElement).value.trim(),
    ntfy_match_edge_instance_id: (document.getElementById("ntfy_match_edge_instance_id") as HTMLInputElement).value.trim(),
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
  (document.getElementById("ntfy_message_template") as HTMLTextAreaElement).value = defaults.default_message_template || "";
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
    await loadOverview({ silent: true, force: true });
    await loadNtfyConfig();
    setActionStatus("Central ntfy settings saved.", "success");
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

// --- Certificates ---

function renderCertificateFiles(files: StoredFile[] | undefined): string {
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

function fillCertificateForm(config: CertificateConfig | null | undefined): void {
  const data = config || {};
  document.getElementById("certificate-dir")!.textContent = data.cert_dir || "n/a";
  document.getElementById("certificate-files")!.innerHTML = renderCertificateFiles(data.files || []);
}

async function loadCertificateConfig(): Promise<CertificateConfig> {
  return loadEditorPanel("certificates", async () => {
  const response = await fetch("/api/certificates", { signal: globalThis.AbortSignal?.timeout?.(30000) });
  const body: CertificateConfig = await response.json();
  if (!response.ok) {
    throw new Error(body.detail || "Failed to load certificates.");
  }
  fillCertificateForm(body);
  return body;
  });
}

async function uploadCertificateFile(): Promise<void> {
  if (!requirePanelReady("certificates")) return;
  const input = document.getElementById("certificate_file_input") as HTMLInputElement | null;
  const file = input?.files?.[0];
  if (!input || !file) {
    setStatus("certificates-status", "Choose a certificate first.", "error");
    return;
  }
  const formData = new FormData();
  formData.append("certificate_file", file);
  const response = await fetch("/api/certificates/files", { method: "POST", body: formData });
  const body: ApiBody = await response.json();
  setStatus("certificates-status", response.ok ? "Certificate uploaded." : (body.detail || "Upload failed."), response.ok ? "success" : "error");
  if (response.ok) {
    input.value = "";
    await loadCertificateConfig();
    setActionStatus(`Uploaded ${file.name}.`, "success");
  } else {
    setActionStatus(body.detail || "Certificate upload failed.", "error");
  }
}

async function deleteCertificateFile(filename: string): Promise<void> {
  if (!requirePanelReady("certificates")) return;
  if (!await confirmApp({
    title: "Delete Certificate",
    message: `Delete ${filename}?`,
    confirmLabel: "Delete",
    danger: true,
  })) {
    return;
  }
  const response = await fetch(`/api/certificates/files/${encodeURIComponent(filename)}`, { method: "DELETE" });
  const body: ApiBody = await response.json();
  setStatus("certificates-status", response.ok ? "Certificate deleted." : (body.detail || "Delete failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadCertificateConfig();
    setActionStatus(`Deleted ${filename}.`, "success");
  } else {
    setActionStatus(body.detail || "Certificate delete failed.", "error");
  }
}

// --- Hooks ---

function renderHookFiles(files: StoredFile[] | undefined): string {
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

function fillHookForm(config: HookConfig | null | undefined, { preserveDrafts = true } = {}): void {
  const data = config || {};
  document.getElementById("hook-script-dir")!.textContent = data.script_dir || "n/a";
  if (!preserveDrafts || !_hookDraftDirty.pre) {
    (document.getElementById("hook_pre_command") as HTMLTextAreaElement).value = data.pre_command || "";
    _hookDraftDirty.pre = false;
  }
  if (!preserveDrafts || !_hookDraftDirty.post) {
    (document.getElementById("hook_post_command") as HTMLTextAreaElement).value = data.post_command || "";
    _hookDraftDirty.post = false;
  }
  document.getElementById("hook-files")!.innerHTML = renderHookFiles(data.files || []);
}

async function loadHookConfig({ preserveDrafts = true } = {}): Promise<HookConfig> {
  return loadEditorPanel("hooks", async () => {
  const response = await fetch("/api/hooks", { signal: globalThis.AbortSignal?.timeout?.(30000) });
  const body: HookConfig = await response.json();
  if (!response.ok) {
    throw new Error(body.detail || "Failed to load hook settings.");
  }
  _centralHookConfig = body;
  fillHookForm(body, { preserveDrafts });
  return body;
  });
}

async function openHooksDialog(): Promise<void> {
  clearStatus("hooks-status");
  openDialog("hooks-dialog");
  try {
    await loadHookConfig({ preserveDrafts: false });
  } catch (error) {
    setActionStatus((error as Error).message || "Failed to load hook settings.", "error");
  }
}

function clearHookCommand(kind: "pre" | "post"): void {
  const input = document.getElementById(kind === "pre" ? "hook_pre_command" : "hook_post_command") as HTMLTextAreaElement | null;
  if (!input) return;
  input.value = "";
  _hookDraftDirty[kind] = true;
}

async function saveHookCommands(): Promise<void> {
  if (!requirePanelReady("hooks")) return;
  setStatus("hooks-status", "Saving...", "info");
  const payload = {
    pre_command: (document.getElementById("hook_pre_command") as HTMLTextAreaElement).value.trim(),
    post_command: (document.getElementById("hook_post_command") as HTMLTextAreaElement).value.trim(),
  };
  const response = await fetch("/api/hooks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body: ApiBody = await response.json();
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

async function uploadHookFile(): Promise<void> {
  if (!requirePanelReady("hooks")) return;
  const input = document.getElementById("hook_file_input") as HTMLInputElement | null;
  const file = input?.files?.[0];
  if (!input || !file) {
    setStatus("hooks-status", "Choose a file first.", "error");
    return;
  }
  const formData = new FormData();
  formData.append("hook_file", file);
  const response = await fetch("/api/hooks/files", { method: "POST", body: formData });
  const body: ApiBody = await response.json();
  setStatus("hooks-status", response.ok ? "File uploaded." : (body.detail || "Upload failed."), response.ok ? "success" : "error");
  if (response.ok) {
    input.value = "";
    await loadHookConfig({ preserveDrafts: true });
    setActionStatus(`Uploaded ${file.name}.`, "success");
  } else {
    setActionStatus(body.detail || "Hook upload failed.", "error");
  }
}

async function viewHookFile(filename: string, viewable: boolean): Promise<void> {
  if (!viewable) {
    setActionStatus("This file cannot be viewed.", "error");
    return;
  }
  const response = await fetch(`/api/hooks/files/${encodeURIComponent(filename)}`);
  const body: HookFileResponse = await response.json();
  if (!response.ok) {
    setActionStatus(body.detail || "View failed.", "error");
    return;
  }
  document.getElementById("hook-view-filename")!.textContent = body.filename || filename;
  (document.getElementById("hook-view-content") as HTMLTextAreaElement).value = body.content || "";
  openDialog("hook-view-dialog");
}

async function deleteHookFile(filename: string): Promise<void> {
  if (!requirePanelReady("hooks")) return;
  if (!await confirmApp({
    title: "Delete Hook File",
    message: `Delete ${filename}?`,
    confirmLabel: "Delete",
    danger: true,
  })) {
    return;
  }
  const response = await fetch(`/api/hooks/files/${encodeURIComponent(filename)}`, { method: "DELETE" });
  const body: ApiBody = await response.json();
  setStatus("hooks-status", response.ok ? "File deleted." : (body.detail || "Delete failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadHookConfig({ preserveDrafts: true });
    setActionStatus(`Deleted ${filename}.`, "success");
  } else {
    setActionStatus(body.detail || "Hook delete failed.", "error");
  }
}
