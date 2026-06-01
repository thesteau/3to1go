let latestData = null;
let directoryExpansionState = new Set(["."]);
let autoRefreshTimer = null;
let isLoadingData = false;
let hooksRefreshTimer = null;
let edgeNtfyConfig = null;
let edgeHookConfig = null;
let hookDraftDirty = { pre: false, post: false };
const AUTO_REFRESH_INTERVAL_MS = 5000;

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function encodedPath(value) {
  return encodeURIComponent(value ?? ".");
}

function statusBadge(entry) {
  if (entry.config_error) {
    return '<span class="badge error">invalid config</span>';
  }
  if (entry.selected) {
    return '<span class="badge">selected</span>';
  }
  if (entry.blocked_by_parent) {
    return `<span class="badge warn">nested under ${escapeHtml(entry.blocked_by_parent)}</span>`;
  }
  return '<span class="badge warn">available</span>';
}

function shortFingerprint(value) {
  return value ? String(value).slice(0, 12) : "unknown";
}

function clipMiddle(value, maxLength = 32) {
  const text = String(value ?? "");
  if (text.length <= maxLength) return text;
  const head = Math.max(10, Math.floor((maxLength - 1) / 2));
  const tail = Math.max(8, maxLength - head - 1);
  return `${text.slice(0, head)}…${text.slice(-tail)}`;
}

function renderClipValue(label, value, { className = "", clipLength = 32 } = {}) {
  const full = String(value ?? "").trim();
  if (!full) return "—";
  return renderStaticClipValue(label, full, { className, clipLength });
}

function renderStaticClipValue(label, value, { className = "", clipLength = 32 } = {}) {
  const full = String(value ?? "").trim();
  if (!full) return "—";
  const short = clipMiddle(full, clipLength);
  const classes = className ? ` ${className}` : "";
  return `<span class="clip-static${classes}" title="${escapeHtml(full)}">${label ? `<span class="clip-label">${escapeHtml(label)}</span>` : ""}<span class="clip-value">${escapeHtml(short)}</span></span>`;
}

function setActionStatus(message, kind = "info") {
  const element = document.getElementById("action-status");
  if (!element) return;
  element.textContent = message || "";
  element.dataset.kind = kind;
}

function clearStatus(id) {
  const element = document.getElementById(id);
  if (element) {
    element.textContent = "";
  }
}

function openDialog(id) {
  const dialog = document.getElementById(id);
  if (!dialog?.showModal) return;
  if (dialog.open) return;
  dialog.showModal();
}

function closeDialog(id) {
  const dialog = document.getElementById(id);
  if (dialog?.open) {
    dialog.close();
  }
  if (id === "hooks-dialog" && hooksRefreshTimer) {
    clearInterval(hooksRefreshTimer);
    hooksRefreshTimer = null;
  }
}

function openSettingsDialog() {
  fillSettings(latestData?.settings || {});
  clearStatus("settings-status");
  openDialog("settings-dialog");
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
  try {
    await loadNtfyConfig();
    openDialog("ntfy-dialog");
  } catch (error) {
    alert(error.message || "Failed to load ntfy settings.");
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
  document.getElementById("ntfy_url").value = "";
  document.getElementById("ntfy_topic").value = "";
  document.getElementById("ntfy_message_template").value = edgeNtfyConfig?.default_message_template || "";
}

async function saveNtfyConfig() {
  const response = await fetch("/api/ntfy", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(collectNtfyPayload()),
  });
  const body = await response.json();
  document.getElementById("ntfy-status").textContent = response.ok ? "Saved." : (body.detail || "Save failed.");
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
  document.getElementById("ntfy-status").textContent = response.ok
    ? "Connection test succeeded."
    : (body.detail || "Test failed.");
  if (!response.ok) {
    setActionStatus(body.detail || "ntfy test failed.", "error");
  }
}

function formatBytes(bytes) {
  if (!bytes) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
  return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
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
  try {
    await loadHookConfig({ preserveDrafts: false });
    openDialog("hooks-dialog");
  } catch (error) {
    alert(error.message || "Failed to load hook settings.");
    return;
  }
  if (hooksRefreshTimer) {
    clearInterval(hooksRefreshTimer);
  }
  hooksRefreshTimer = setInterval(() => {
    if (document.getElementById("hooks-dialog")?.open) {
      loadHookConfig({ preserveDrafts: true }).catch(() => {});
    }
  }, AUTO_REFRESH_INTERVAL_MS);
}

function clearHookCommand(kind) {
  const input = document.getElementById(kind === "pre" ? "hook_pre_command" : "hook_post_command");
  if (!input) return;
  input.value = "";
  hookDraftDirty[kind] = true;
}

async function saveHookCommands() {
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
  document.getElementById("hooks-status").textContent = response.ok ? "Commands saved." : (body.detail || "Save failed.");
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
    document.getElementById("hooks-status").textContent = "Choose a file first.";
    return;
  }
  const formData = new FormData();
  formData.append("hook_file", file);
  const response = await fetch("/api/hooks/files", { method: "POST", body: formData });
  const body = await response.json();
  document.getElementById("hooks-status").textContent = response.ok ? "File uploaded." : (body.detail || "Upload failed.");
  if (response.ok) {
    input.value = "";
    await loadHookConfig({ preserveDrafts: true });
  } else {
    setActionStatus(body.detail || "Hook upload failed.", "error");
  }
}

async function viewHookFile(filename, viewable) {
  if (!viewable) {
    alert("This file cannot be viewed.");
    return;
  }
  const response = await fetch(`/api/hooks/files/${encodeURIComponent(filename)}`);
  const body = await response.json();
  if (!response.ok) {
    alert(body.detail || "View failed.");
    return;
  }
  document.getElementById("hook-view-filename").textContent = body.filename || filename;
  document.getElementById("hook-view-content").value = body.content || "";
  openDialog("hook-view-dialog");
}

async function deleteHookFile(filename) {
  if (!confirm(`Delete ${filename}?`)) {
    return;
  }
  const response = await fetch(`/api/hooks/files/${encodeURIComponent(filename)}`, { method: "DELETE" });
  const body = await response.json();
  document.getElementById("hooks-status").textContent = response.ok ? "File deleted." : (body.detail || "Delete failed.");
  if (response.ok) {
    await loadHookConfig({ preserveDrafts: true });
  } else {
    setActionStatus(body.detail || "Hook delete failed.", "error");
  }
}

function openJobDialog(relativePath = ".") {
  editPath(relativePath);
  openDialog("job-dialog");
}

function openJobDialogFromEvent(event, relativePath) {
  event.preventDefault();
  event.stopPropagation();
  openJobDialog(relativePath);
  return false;
}

function fillMeta(data, encKey, encFingerprint) {
  const scheduler = data.scheduler || {};
  const uploadCircuit = data.upload_circuit || {};
  const settingsStatus = data.settings_status || {};
  document.getElementById("meta").innerHTML = `
    <div><strong>Edge ID</strong><br>${renderClipValue("", data.edge_id, { className: "clip-code", clipLength: 28 })}</div>
    <div><strong>Instance ID</strong><br>${renderClipValue("", data.edge_instance_id || "—", { className: "clip-code", clipLength: 28 })}</div>
    <div><strong>Scan Root</strong><br>${renderClipValue("", data.scan_root, { className: "clip-code", clipLength: 34 })}</div>
    <div><strong>Central URL</strong><br>${renderClipValue("", data.central_url, { className: "clip-code", clipLength: 34 })}</div>
    <div><strong>Advertised URL</strong><br>${renderClipValue("", data.advertised_url || "—", { className: "clip-code", clipLength: 34 })}</div>
    <div><strong>Edge UI</strong><br>${renderClipValue("", data.http_url, { className: "clip-code", clipLength: 30 })}</div>
    <div><strong>Settings File</strong><br>${renderClipValue("", data.settings_path || "n/a", { className: "clip-code", clipLength: 34 })}</div>
    <div><strong>Cron Schedule</strong><br><code>${escapeHtml(data.cron_schedule)}</code></div>
    <div><strong>Minimum Gap</strong><br>${escapeHtml(`${data.minimum_cycle_gap_minutes} minutes`)}</div>
    <div><strong>Scheduler</strong><br>${escapeHtml(scheduler.state || "idle")}</div>
    <div><strong>Next Run</strong><br>${escapeHtml(scheduler.next_run_at || "waiting for first cycle")}</div>
    <div><strong>Upload Circuit</strong><br>${escapeHtml(uploadCircuit.state || "closed")}</div>
    <div><strong>Auth Token</strong><br>${escapeHtml(settingsStatus.auth_token_configured ? "configured" : "missing")}</div>
    <div class="enc-key-cell">
      <strong>Encryption Key</strong>
      <div class="enc-key-row">
        <code id="enc-key-value">${escapeHtml(encKey || "—")}</code>
        <button type="button" class="secondary enc-key-copy" onclick="copyEncKey()">Copy</button>
      </div>
      <span class="hint">Fingerprint ${escapeHtml(shortFingerprint(encFingerprint))}. Central uses this to confirm you pasted the right key for this Edge before decrypting.</span>
    </div>
  `;
}

async function copyEncKey() {
  const key = document.getElementById("enc-key-value")?.textContent;
  if (!key || key === "—") return;
  await navigator.clipboard.writeText(key);
  const btn = document.querySelector(".enc-key-copy");
  if (btn) {
    btn.textContent = "Copied!";
    setTimeout(() => { btn.textContent = "Copy"; }, 2000);
  }
}

function fillSettings(settings) {
  const data = settings || {};
  document.getElementById("settings_edge_id").value = data.edge_id || "";
  document.getElementById("settings_scan_root").value = data.scan_root || "";
  document.getElementById("settings_central_url").value = data.central_url || "";
  document.getElementById("settings_advertised_url").value = data.advertised_url || "";
  document.getElementById("settings_auth_token").value = data.auth_token || "";
  document.getElementById("settings_cron_schedule").value = data.cron_schedule || "";
  document.getElementById("settings_state_dir").value = data.state_dir || "";
  document.getElementById("settings_spool_dir").value = data.spool_dir || "";
  document.getElementById("settings_log_level").value = data.log_level || "INFO";
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
}

function rememberDirectoryExpansion() {
  const openPaths = Array.from(document.querySelectorAll("#directory-tree details[data-path][open]"))
    .map((element) => element.dataset.path)
    .filter(Boolean);
  directoryExpansionState = new Set(openPaths.length ? openPaths : ["."]);
}

function buildDirectoryIndex(directories) {
  const entriesByPath = new Map();
  const childrenByParent = new Map();

  directories.forEach((entry) => {
    entriesByPath.set(entry.relative_path, entry);
    childrenByParent.set(entry.relative_path, []);
  });

  directories.forEach((entry) => {
    if (entry.relative_path === ".") {
      return;
    }
    const segments = entry.relative_path.split("/");
    const parentPath = segments.length > 1 ? segments.slice(0, -1).join("/") : ".";
    if (!childrenByParent.has(parentPath)) {
      childrenByParent.set(parentPath, []);
    }
    childrenByParent.get(parentPath).push(entry.relative_path);
  });

  return { entriesByPath, childrenByParent };
}

function formatDirectoryProgress(entry) {
  return entry.state?.pending_archive_size
    ? `${entry.state?.upload_offset || 0}/${entry.state.pending_archive_size} bytes`
    : "n/a";
}

function directoryDisplayName(entry) {
  if (entry.relative_path === ".") {
    return "Scan Root";
  }
  return entry.relative_path.split("/").pop() || entry.relative_path;
}

function renderDirectoryHeader(entry, childCount, hasSelectedDescendant) {
  const relativePath = entry.relative_path;
  const lastState = entry.state?.last_status || "none";
  const absolutePath = renderClipValue("", entry.absolute_path, { className: "clip-hint", clipLength: 52 });
  const pathValue = relativePath === "."
    ? renderClipValue("", latestData?.scan_root || entry.absolute_path, { className: "clip-code", clipLength: 52 })
    : renderClipValue("", entry.relative_path, { className: "clip-code", clipLength: 44 });

  return `
    <div class="dir-row">
      <div class="dir-main">
        <div class="dir-title">
          ${childCount ? '<span class="dir-caret" aria-hidden="true"></span>' : '<span class="dir-caret dir-caret-placeholder" aria-hidden="true"></span>'}
          <span class="dir-name">${escapeHtml(directoryDisplayName(entry))}</span>
          ${statusBadge(entry)}
          ${childCount ? `<span class="dir-count">${escapeHtml(String(childCount))} nested</span>` : ""}
          ${hasSelectedDescendant && !entry.selected ? '<span class="dir-count">contains selected job</span>' : ""}
        </div>
        <div class="hint">${pathValue}</div>
        <div class="hint">${absolutePath}</div>
        <div class="dir-state">Last state: ${escapeHtml(lastState)} <span class="hint">${escapeHtml(formatDirectoryProgress(entry))}</span></div>
      </div>
      <div class="dir-actions">
        <button type="button" class="secondary" onclick="return openJobDialogFromEvent(event, decodeURIComponent('${encodedPath(relativePath)}'))">Edit</button>
      </div>
    </div>
  `;
}

function renderDirectoryNode(relativePath, index) {
  const entry = index.entriesByPath.get(relativePath);
  if (!entry) {
    return { html: "", hasSelectedDescendant: false };
  }

  const childPaths = index.childrenByParent.get(relativePath) || [];
  const renderedChildren = childPaths.map((childPath) => renderDirectoryNode(childPath, index));
  const hasSelectedDescendant = entry.selected || renderedChildren.some((child) => child.hasSelectedDescendant);
  const header = renderDirectoryHeader(entry, childPaths.length, renderedChildren.some((child) => child.hasSelectedDescendant));

  if (!childPaths.length) {
    return {
      hasSelectedDescendant,
      html: `<div class="dir-leaf" data-path="${escapeHtml(relativePath)}">${header}</div>`,
    };
  }

  const shouldOpen = directoryExpansionState.has(relativePath) || hasSelectedDescendant || relativePath === ".";
  return {
    hasSelectedDescendant,
    html: `
      <details class="dir-branch" data-path="${escapeHtml(relativePath)}"${shouldOpen ? " open" : ""}>
        <summary class="dir-summary">${header}</summary>
        <div class="dir-children">
          ${renderedChildren.map((child) => child.html).join("")}
        </div>
      </details>
    `,
  };
}

function bindDirectoryTreeEvents() {
  document.querySelectorAll("#directory-tree details[data-path]").forEach((element) => {
    element.addEventListener("toggle", () => {
      const path = element.dataset.path;
      if (!path) return;
      if (element.open) {
        directoryExpansionState.add(path);
      } else {
        directoryExpansionState.delete(path);
      }
    });
  });
}

function renderDirectories(data) {
  const selected = data.directories.filter((entry) => entry.selected);
  document.getElementById("selected-jobs").innerHTML = selected.length
    ? selected.map((entry) => `
      <div class="job-card">
        <div class="job-card-title">${renderStaticClipValue("", entry.config?.job_name || entry.relative_path, { className: "clip-title", clipLength: 34 })}</div>
        <div class="hint">${renderClipValue("", entry.relative_path, { className: "clip-code", clipLength: 42 })}</div>
        <div class="hint">Last state: ${escapeHtml(entry.state?.last_status || "none")}</div>
        ${entry.state?.pending_archive_size ? `<div class="hint">Progress: ${escapeHtml(`${entry.state?.upload_offset || 0}/${entry.state.pending_archive_size} bytes`)}</div>` : ""}
        ${entry.state?.next_retry_at ? `<div class="hint">Next retry: ${escapeHtml(entry.state.next_retry_at)}</div>` : ""}
        ${entry.state?.last_error_detail ? `<div class="hint" style="color:#b42318;">${renderClipValue("", entry.state.last_error_detail, { className: "clip-hint", clipLength: 68 })}</div>` : ""}
        ${entry.blocked_by_parent ? `<div class="hint">Nested under existing job ${renderClipValue("", entry.blocked_by_parent, { className: "clip-code", clipLength: 36 })}</div>` : ""}
        ${entry.config_error ? `<div class="hint" style="color:#b42318;">${renderClipValue("", entry.config_error, { className: "clip-hint", clipLength: 68 })}</div>` : ""}
        <div class="toolbar">
          <span class="hint-with-help">
            <button type="button" class="secondary" onclick="forceUpload(decodeURIComponent('${encodeURIComponent(entry.config?.job_name || entry.relative_path)}'), this)">Force Upload</button>
            <span class="hover-hint" title="Use this when you want Edge to upload again even if the folder looks unchanged locally. Central may still reject the upload if it already has the same snapshot.">?</span>
          </span>
          <button type="button" class="secondary" onclick="recoverLatest(decodeURIComponent('${encodedPath(entry.relative_path)}'), decodeURIComponent('${encodeURIComponent(entry.config?.job_name || entry.relative_path)}'), this)">Recover Latest</button>
          <button type="button" class="secondary" onclick="openJobDialog(decodeURIComponent('${encodedPath(entry.relative_path)}'))">Edit</button>
          <button type="button" class="danger" onclick="deleteByPath(decodeURIComponent('${encodedPath(entry.relative_path)}'))">Delete</button>
        </div>
      </div>
    `).join("")
    : '<p class="hint">No directories are selected yet.</p>';

  rememberDirectoryExpansion();
  const tree = renderDirectoryNode(".", buildDirectoryIndex(data.directories || []));
  document.getElementById("directory-tree").innerHTML = tree.html || '<p class="hint">No directories were found under the scan root.</p>';
  bindDirectoryTreeEvents();
}

function findEntry(relativePath) {
  return latestData?.directories.find((entry) => entry.relative_path === relativePath);
}

function editPath(relativePath) {
  const entry = findEntry(relativePath);
  document.getElementById("relative_path").value = relativePath;
  document.getElementById("job_name").value = entry?.config?.job_name || (relativePath === "." ? "" : relativePath.split("/").pop());
  document.getElementById("exclude").value = (entry?.config?.exclude || []).join("\n");
  document.getElementById("include_hidden").checked = entry?.config?.include_hidden ?? true;
  document.getElementById("follow_symlinks").checked = entry?.config?.follow_symlinks ?? false;
  document.getElementById("is_docker_composed").checked = entry?.config?.is_docker_composed ?? false;
  document.getElementById("update_container_on_packup").checked = entry?.config?.update_container_on_packup ?? false;
  clearStatus("form-status");
  document.getElementById("form-status").textContent = entry?.selected
    ? "Editing existing .upload_dir"
    : "Creating a new .upload_dir";
}

function resetForm() {
  document.getElementById("relative_path").value = ".";
  document.getElementById("job_name").value = "";
  document.getElementById("exclude").value = "";
  document.getElementById("include_hidden").checked = true;
  document.getElementById("follow_symlinks").checked = false;
  document.getElementById("is_docker_composed").checked = false;
  document.getElementById("update_container_on_packup").checked = false;
  document.getElementById("form-status").textContent = "Choose a directory to create or update its .upload_dir file.";
}

function isDialogOpen(id) {
  return Boolean(document.getElementById(id)?.open);
}

async function loadData({ silent = false } = {}) {
  if (isLoadingData) {
    return;
  }

  isLoadingData = true;
  const [dirRes, keyRes] = await Promise.all([
    fetch("/api/directories"),
    fetch("/api/encryption-key"),
  ]);
  try {
    if (!dirRes.ok || !keyRes.ok) {
      throw new Error("Refresh failed.");
    }

    latestData = await dirRes.json();
    const keyData = await keyRes.json();
    fillMeta(latestData, keyData.key || "", keyData.fingerprint || latestData.encryption_key_fingerprint || "");
    if (!isDialogOpen("settings-dialog")) {
      fillSettings(latestData.settings || {});
    }
    renderDirectories(latestData);
    if (!silent) {
      document.getElementById("run-status").textContent = "";
    }
  } catch (error) {
    if (!silent) {
      setActionStatus(error.message || "Refresh failed.", "error");
    }
  } finally {
    isLoadingData = false;
  }
}

function startAutoRefresh() {
  if (autoRefreshTimer) {
    clearInterval(autoRefreshTimer);
  }

  autoRefreshTimer = setInterval(() => {
    if (document.hidden) {
      return;
    }
    loadData({ silent: true });
  }, AUTO_REFRESH_INTERVAL_MS);

  document.addEventListener("visibilitychange", () => {
    if (!document.hidden) {
      loadData({ silent: true });
    }
  });
}

async function saveSettings() {
  const payload = {
    edge_id: document.getElementById("settings_edge_id").value.trim(),
    scan_root: document.getElementById("settings_scan_root").value.trim(),
    central_url: document.getElementById("settings_central_url").value.trim(),
    advertised_url: document.getElementById("settings_advertised_url").value.trim(),
    auth_token: document.getElementById("settings_auth_token").value,
    cron_schedule: document.getElementById("settings_cron_schedule").value.trim(),
    state_dir: document.getElementById("settings_state_dir").value.trim(),
    spool_dir: document.getElementById("settings_spool_dir").value.trim(),
    log_level: document.getElementById("settings_log_level").value,
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
  };

  const response = await fetch("/api/settings", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await response.json();
  document.getElementById("settings-status").textContent = response.ok
    ? "Settings saved."
    : (body.detail || "Settings save failed.");
  if (response.ok) {
    await loadData();
    setActionStatus("Edge settings saved.", "success");
    closeDialog("settings-dialog");
  } else {
    setActionStatus(body.detail || "Settings save failed.", "error");
  }
}

async function saveJob() {
  const relativePath = document.getElementById("relative_path").value || ".";
  const payload = {
    relative_path: relativePath,
    job_name: document.getElementById("job_name").value.trim() || null,
    exclude: document.getElementById("exclude").value.split("\n").map((value) => value.trim()).filter(Boolean),
    include_hidden: document.getElementById("include_hidden").checked,
    follow_symlinks: document.getElementById("follow_symlinks").checked,
    is_docker_composed: document.getElementById("is_docker_composed").checked,
    update_container_on_packup: document.getElementById("update_container_on_packup").checked,
  };

  const response = await fetch("/api/jobs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await response.json();
  document.getElementById("form-status").textContent = response.ok
    ? "Saved successfully."
    : (body.detail || "Save failed.");
  if (response.ok) {
    await loadData();
    setActionStatus(`Saved .upload_dir for ${relativePath}.`, "success");
    closeDialog("job-dialog");
  } else {
    setActionStatus(body.detail || "Save failed.", "error");
  }
}

async function deleteByPath(relativePath) {
  if (!confirm(`Delete .upload_dir from ${relativePath}?`)) {
    return;
  }
  const response = await fetch(`/api/jobs?relative_path=${encodeURIComponent(relativePath)}`, {
    method: "DELETE",
  });
  const body = await response.json();
  document.getElementById("form-status").textContent = response.ok
    ? "Deleted successfully."
    : (body.detail || "Delete failed.");
  if (response.ok) {
    await loadData();
    setActionStatus(`Deleted .upload_dir from ${relativePath}.`, "success");
    closeDialog("job-dialog");
  } else {
    setActionStatus(body.detail || "Delete failed.", "error");
  }
}

async function forceUpload(jobName, btn) {
  const label = jobName || "this job";
  if (!confirm(
    `Force an upload for ${label}?\n\nThis bypasses the unchanged check. Central may still reject it as a duplicate if that snapshot already exists.`,
  )) {
    return;
  }

  btn.disabled = true;
  try {
    const response = await fetch(`/api/jobs/force-send?job_name=${encodeURIComponent(jobName)}`, {
      method: "POST",
    });
    const body = await response.json();
    if (!response.ok) {
      setActionStatus(body.detail || `Force upload failed for ${label}.`, "error");
      return;
    }
    if (body.status === "already_running") {
      setActionStatus("A backup or recovery operation is already running on this Edge.", "error");
      return;
    }

    setActionStatus(
      body.manual_retry_cleared
        ? `Forced upload for ${label}. A manual block was cleared first.`
        : `Forced upload for ${label}. Central may still reject it as a duplicate.`,
      "success",
    );
    await loadData({ silent: true });
  } catch (error) {
    setActionStatus(error.message || `Force upload failed for ${label}.`, "error");
  } finally {
    btn.disabled = false;
  }
}

async function recoverLatest(relativePath, jobName, btn) {
  const label = jobName || relativePath;
  if (!confirm(
    `Recover the latest Central backup for ${label}?\n\nFiles included in that backup will be overwritten in this folder. Files not present in the backup will stay untouched.`,
  )) {
    return;
  }

  btn.disabled = true;
  try {
    const response = await fetch(`/api/jobs/recover-latest?relative_path=${encodeURIComponent(relativePath)}`, {
      method: "POST",
    });
    const body = await response.json();
    if (!response.ok) {
      setActionStatus(body.detail || `Recovery failed for ${label}.`, "error");
      return;
    }
    if (body.status === "already_running") {
      setActionStatus("A backup or recovery operation is already running on this Edge.", "error");
      return;
    }

    const restoredFiles = Number(body.restored_files || 0);
    const snapshotName = body.snapshot_filename || "latest snapshot";
    setActionStatus(
      `Recovered ${label} from ${snapshotName} and replaced ${restoredFiles} backed-up file${restoredFiles === 1 ? "" : "s"}.`,
      "success",
    );
    await loadData({ silent: true });
  } catch (error) {
    setActionStatus(error.message || `Recovery failed for ${label}.`, "error");
  } finally {
    btn.disabled = false;
  }
}

async function deleteJob() {
  const relativePath = document.getElementById("relative_path").value || ".";
  await deleteByPath(relativePath);
}

async function runNow() {
  const response = await fetch("/api/run-now", { method: "POST" });
  const body = await response.json();
  if (body.status === "queued" || body.status === "started") {
    const cleared = body.manual_retries_cleared || 0;
    document.getElementById("run-status").textContent = cleared > 0
      ? `Backup cycle requested. ${cleared} manual block(s) cleared for retry.`
      : "Backup cycle requested.";
    await loadData({ silent: true });
    return;
  }
  document.getElementById("run-status").textContent = "A cycle is already running.";
}

document.getElementById("hook_pre_command")?.addEventListener("input", () => {
  hookDraftDirty.pre = true;
});
document.getElementById("hook_post_command")?.addEventListener("input", () => {
  hookDraftDirty.post = true;
});

resetForm();
loadData();
startAutoRefresh();
