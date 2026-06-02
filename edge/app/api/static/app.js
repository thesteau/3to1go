let latestData = null;
let directoryExpansionState = new Set(["."]);
let isLoadingData = false;
let edgeNtfyConfig = null;
let edgeHookConfig = null;
let hookDraftDirty = { pre: false, post: false };
let showHiddenDirs = false;
let _recoverContext = null;
const TOAST_DURATION_MS = 8000;
let _appDialogResolve = null;
const EDGE_SETTINGS_HELP = {
  settings_edge_id: "A friendly name Central uses to group this Edge with related installations.",
  settings_scan_root: "Edge scans this folder tree for .upload_dir files and available directories.",
  settings_central_url: "The Central server URL Edge uploads backups to.",
  settings_advertised_url: "Optional URL Central displays as a link to this Edge instance.",
  settings_auth_token: "Shared secret Edge includes when it talks to Central.",
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
  if (entry.blocked_by_parent) {
    return `<span class="badge warn" title="Nested folders under an already-selected parent are backed up through that parent job instead of continuing as separate jobs.">managed by ${escapeHtml(entry.blocked_by_parent)}</span>`;
  }
  if (entry.selected) {
    return '<span class="badge">selected</span>';
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

function showToast(message, kind = "info", { duration = TOAST_DURATION_MS, title = "" } = {}) {
  if (!message) return;
  const region = document.getElementById("toast-region");
  if (!region) return;

  const defaultTitle = kind === "error" ? "Something needs attention" : kind === "success" ? "Done" : "Notice";
  const toast = document.createElement("div");
  toast.className = `toast ${kind}`;
  toast.setAttribute("role", "status");
  toast.innerHTML = `<strong class="toast-title">${escapeHtml(title || defaultTitle)}</strong><span>${escapeHtml(message)}</span>`;
  region.appendChild(toast);
  requestAnimationFrame(() => toast.classList.add("visible"));

  window.setTimeout(() => {
    toast.classList.remove("visible");
    window.setTimeout(() => toast.remove(), 180);
  }, duration);
}

function setActionStatus(message, kind = "info") {
  showToast(message, kind);
}

function setStatus(id, message, kind = "info") {
  const element = document.getElementById(id);
  if (!element) return;
  element.textContent = message || "";
  if (message) {
    element.dataset.kind = kind;
  } else {
    delete element.dataset.kind;
  }
}

function clearStatus(id) {
  setStatus(id, "", "info");
}

function pause(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function renderHelpHint(message) {
  return `<span class="hover-hint" tabindex="0" aria-label="${escapeHtml(message)}" title="${escapeHtml(message)}">?</span>`;
}

function initializeFieldHelp(helpEntries) {
  Object.entries(helpEntries).forEach(([id, helpText]) => {
    const label = document.querySelector(`label[for="${id}"]`);
    if (!label || label.querySelector(".field-help")) {
      return;
    }
    label.insertAdjacentHTML("beforeend", ` <span class="field-help hover-hint" tabindex="0" aria-label="${escapeHtml(helpText)}" title="${escapeHtml(helpText)}">?</span>`);
  });
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
}

function appDialog({ title, message, input = false, inputLabel = "", inputType = "text", confirmLabel = "Continue", danger = false } = {}) {
  const dialog = document.getElementById("app-dialog");
  if (!dialog?.showModal) {
    return Promise.resolve(input ? null : false);
  }
  if (_appDialogResolve) {
    resolveAppDialog(false);
  }

  document.getElementById("app-dialog-title").textContent = title || "Confirm";
  document.getElementById("app-dialog-message").textContent = message || "";
  const inputWrap = document.getElementById("app-dialog-input-wrap");
  const inputElement = document.getElementById("app-dialog-input");
  document.getElementById("app-dialog-input-label").textContent = inputLabel || "";
  inputWrap.hidden = !input;
  inputElement.type = inputType;
  inputElement.value = "";
  const confirmButton = document.getElementById("app-dialog-confirm");
  confirmButton.textContent = confirmLabel;
  confirmButton.className = danger ? "danger" : "";
  dialog.oncancel = (event) => {
    event.preventDefault();
    resolveAppDialog(false);
  };
  inputElement.onkeydown = (event) => {
    if (event.key === "Enter") {
      event.preventDefault();
      resolveAppDialog(true);
    }
  };

  dialog.showModal();
  if (input) {
    window.setTimeout(() => inputElement.focus(), 0);
  }

  return new Promise((resolve) => {
    _appDialogResolve = (confirmed) => {
      const value = input ? inputElement.value.trim() : confirmed;
      _appDialogResolve = null;
      closeDialog("app-dialog");
      resolve(confirmed ? value : null);
    };
  });
}

function resolveAppDialog(confirmed) {
  if (_appDialogResolve) {
    _appDialogResolve(confirmed);
  }
}

function confirmApp(options) {
  return appDialog(options).then(Boolean);
}

async function manualRefresh() {
  await loadData();
  setActionStatus("Refreshed.", "success");
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
  document.getElementById("ntfy_url").value = "";
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

function openJobDialog(relativePath = ".") {
  const entry = findEntry(relativePath);
  if (entry?.blocked_by_parent) {
    setActionStatus(
      `That folder is nested under ${entry.blocked_by_parent}, so Edge follows the parent job instead of opening separate upload settings here.`,
      "error",
    );
    return;
  }
  editPath(relativePath);
  openDialog("job-dialog");
}

function stopActionEvent(event) {
  event?.preventDefault();
  event?.stopPropagation();
}

function openJobDialogFromEvent(event, relativePath) {
  stopActionEvent(event);
  openJobDialog(relativePath);
  return false;
}

function formatLocalDateTime(value) {
  const text = String(value || "").trim();
  if (!text) return "—";
  const parsed = new Date(text);
  if (Number.isNaN(parsed.getTime())) {
    return text;
  }
  return parsed.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

function formatClock(hourText, minuteText) {
  const hour = Number(hourText);
  const minute = Number(minuteText);
  if (!Number.isInteger(hour) || !Number.isInteger(minute)) {
    return null;
  }
  const parsed = new Date();
  parsed.setHours(hour, minute, 0, 0);
  return parsed.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

function describeDayOfWeek(field) {
  const dayNames = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
  if (field === "1-5") return "weekdays";
  if (/^\d$/.test(field)) return dayNames[Number(field) % 7];
  if (/^\d-\d$/.test(field)) {
    const [start, end] = field.split("-").map(Number);
    return `${dayNames[start % 7]} through ${dayNames[end % 7]}`;
  }
  if (/^\d(?:,\d)+$/.test(field)) {
    return field.split(",").map((value) => dayNames[Number(value) % 7]).join(", ");
  }
  return `day-of-week ${field}`;
}

function describeCronSchedule(expression) {
  const normalized = String(expression || "").trim();
  const fieldHelp = "Fields run in this order: minute hour day-of-month month day-of-week.";
  if (!normalized) {
    return {
      summary: "No schedule set yet.",
      help: `${fieldHelp} Example: 0 2 * * 0 means every Sunday at 2:00 AM.`,
    };
  }

  const fields = normalized.split(/\s+/);
  if (fields.length !== 5) {
    return {
      summary: "Use five cron fields separated by spaces.",
      help: `${fieldHelp} Example: 0 2 * * 0 means every Sunday at 2:00 AM.`,
    };
  }

  const [minute, hour, dayOfMonth, month, dayOfWeek] = fields;
  const timeLabel = formatClock(hour, minute);
  let summary = `Runs on cron schedule ${normalized}.`;
  if (timeLabel) {
    if (dayOfMonth === "*" && month === "*" && dayOfWeek === "*") {
      summary = `Runs every day at ${timeLabel}.`;
    } else if (dayOfMonth === "*" && month === "*" && dayOfWeek !== "*") {
      summary = `Runs every ${describeDayOfWeek(dayOfWeek)} at ${timeLabel}.`;
    } else if (/^\d+$/.test(dayOfMonth) && month === "*" && dayOfWeek === "*") {
      summary = `Runs on day ${dayOfMonth} of every month at ${timeLabel}.`;
    } else if (dayOfMonth === "*" && /^\d+$/.test(month) && dayOfWeek === "*") {
      summary = `Runs during month ${month} at ${timeLabel}.`;
    } else if (dayOfMonth === "*" && month === "*" && dayOfWeek === "0") {
      summary = `Runs every Sunday at ${timeLabel}.`;
    }
  }

  return {
    summary,
    help: `${fieldHelp} Example: 0 2 * * 0 means every Sunday at 2:00 AM.`,
  };
}

function updateCronScheduleHint() {
  const input = document.getElementById("settings_cron_schedule");
  const hint = document.getElementById("settings-cron-help");
  if (!input || !hint) return;
  const description = describeCronSchedule(input.value);
  hint.textContent = description.summary;
  input.title = `${description.summary} ${description.help}`;
}

function describeSchedulerState(scheduler) {
  const state = String(scheduler?.state || "idle");
  if (state === "running") {
    return {
      label: "Running a backup cycle",
      help: "Edge is actively scanning, packing, or uploading right now.",
    };
  }
  if (state === "waiting") {
    return {
      label: "Waiting for the next scheduled run",
      help: "This is the normal idle state between backup cycles.",
    };
  }
  if (state === "stopped") {
    return {
      label: "Scheduler stopped",
      help: "The scheduler is not currently running.",
    };
  }
  return {
    label: "Ready",
    help: "Edge is ready for the next run request.",
  };
}

function describeUploadCircuit(uploadCircuit) {
  const failures = Number(uploadCircuit?.consecutive_failures || 0);
  const cooldown = Number(uploadCircuit?.cooldown_remaining_seconds || 0);
  if (uploadCircuit?.state === "open") {
    return {
      label: `Paused after upload failures (${cooldown}s left)`,
      help: "Edge temporarily pauses uploads after repeated failures, then retries automatically after the cooldown.",
    };
  }
  return {
    label: failures > 0 ? `Healthy, with ${failures} recent failure${failures === 1 ? "" : "s"}` : "Healthy",
    help: "Uploads are allowed. Edge only pauses this circuit after repeated failures talking to Central.",
  };
}

function fillMeta(data, encKey, encFingerprint) {
  const scheduler = data.scheduler || {};
  const uploadCircuit = data.upload_circuit || {};
  const settingsStatus = data.settings_status || {};
  const cronDetails = describeCronSchedule(data.cron_schedule);
  const schedulerDetails = describeSchedulerState(scheduler);
  const uploadCircuitDetails = describeUploadCircuit(uploadCircuit);
  const advertisedUrl = String(data.advertised_url || "").trim();
  const nextRunText = scheduler.next_run_at
    ? formatLocalDateTime(scheduler.next_run_at)
    : (scheduler.state === "running" ? "A backup cycle is running now." : "Waiting for the next scheduled time.");
  document.getElementById("meta").innerHTML = `
    <div><strong>Edge ID</strong><br>${renderClipValue("", data.edge_id, { className: "clip-code", clipLength: 28 })}</div>
    <div><strong>Instance ID</strong><br>${renderClipValue("", data.edge_instance_id || "—", { className: "clip-code", clipLength: 28 })}</div>
    <div><strong>Scan Root</strong><br>${renderClipValue("", data.scan_root, { className: "clip-code", clipLength: 34 })}</div>
    <div><strong>Central URL</strong><br>${renderClipValue("", data.central_url, { className: "clip-code", clipLength: 34 })}</div>
    <div><strong>Advertised URL</strong><br>${advertisedUrl ? renderClipValue("", advertisedUrl, { className: "clip-code", clipLength: 34 }) : '<span class="hint">Not set</span>'}</div>
    <div><strong>Cron Schedule</strong> ${renderHelpHint(cronDetails.help)}<br><code title="${escapeHtml(`${cronDetails.summary} ${cronDetails.help}`)}">${escapeHtml(data.cron_schedule)}</code><div class="hint">${escapeHtml(cronDetails.summary)}</div></div>
    <div><strong>Scheduler</strong> ${renderHelpHint(schedulerDetails.help)}<br>${escapeHtml(schedulerDetails.label)}</div>
    <div><strong>Next Run</strong><br>${escapeHtml(nextRunText)}</div>
    <div><strong>Upload Circuit</strong> ${renderHelpHint(uploadCircuitDetails.help)}<br>${escapeHtml(uploadCircuitDetails.label)}</div>
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
  updateCronScheduleHint();
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
    : "";
}

function directoryDisplayName(entry) {
  if (entry.relative_path === ".") {
    return "Scan Root";
  }
  return entry.relative_path.split("/").pop() || entry.relative_path;
}

function renderDirectoryHeader(entry, childCount, hasSelectedDescendant) {
  const relativePath = entry.relative_path;
  const progressLabel = formatDirectoryProgress(entry);
  const absolutePath = renderClipValue("", entry.absolute_path, { className: "clip-hint", clipLength: 52 });
  const pathValue = relativePath === "."
    ? renderClipValue("", latestData?.scan_root || entry.absolute_path, { className: "clip-code", clipLength: 52 })
    : renderClipValue("", entry.relative_path, { className: "clip-code", clipLength: 44 });
  const actionMarkup = entry.blocked_by_parent
    ? `<span class="dir-action-note" title="Nested folders under an already-selected parent are backed up through that parent job instead of getting their own .upload_dir settings.">Covered by parent job</span>`
    : `<button type="button" class="secondary" onclick="return openJobDialogFromEvent(event, decodeURIComponent('${encodedPath(relativePath)}'))">Edit</button>`;

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
        ${progressLabel ? `<div class="dir-state"><span class="hint">${escapeHtml(progressLabel)}</span></div>` : ""}
      </div>
      <div class="dir-actions">
        ${actionMarkup}
      </div>
    </div>
  `;
}

function isHiddenPath(relativePath) {
  const name = relativePath === "." ? "" : (relativePath.split("/").pop() || "");
  return name.startsWith(".");
}

function toggleHiddenDirs() {
  showHiddenDirs = !showHiddenDirs;
  const btn = document.getElementById("hidden-dirs-toggle");
  if (btn) {
    btn.setAttribute("aria-checked", String(showHiddenDirs));
    btn.classList.toggle("toggle-on", showHiddenDirs);
  }
  if (latestData) renderDirectories(latestData);
}

function renderDirectoryNode(relativePath, index) {
  const entry = index.entriesByPath.get(relativePath);
  if (!entry) {
    return { html: "", hasSelectedDescendant: false };
  }

  const allChildPaths = index.childrenByParent.get(relativePath) || [];
  const childPaths = allChildPaths.filter((p) => showHiddenDirs || !isHiddenPath(p));
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
  const selected = data.directories.filter((entry) => entry.selected && !entry.blocked_by_parent);
  document.getElementById("selected-jobs").innerHTML = selected.length
    ? selected.map((entry) => {
      const jobName = entry.config?.job_name || entry.relative_path;
      return `
      <div class="job-card">
        <div class="job-card-body">
          <div class="job-card-info">
            <div class="job-card-title">${renderStaticClipValue("", jobName, { className: "clip-title", clipLength: 34 })}</div>
            <div class="hint">${renderClipValue("", entry.relative_path, { className: "clip-code", clipLength: 42 })}</div>
            ${entry.state?.pending_archive_size ? `<div class="hint">Progress: ${escapeHtml(`${entry.state?.upload_offset || 0}/${entry.state.pending_archive_size} bytes`)}</div>` : ""}
            ${entry.state?.next_retry_at ? `<div class="hint">Next retry: ${escapeHtml(entry.state.next_retry_at)}</div>` : ""}
            ${entry.state?.last_error_detail ? `<div class="hint job-error">${renderClipValue("", entry.state.last_error_detail, { className: "clip-hint", clipLength: 68 })}</div>` : ""}
            ${entry.blocked_by_parent ? `<div class="hint">Covered by parent job ${renderClipValue("", entry.blocked_by_parent, { className: "clip-code", clipLength: 36 })}</div>` : ""}
            ${entry.config_error ? `<div class="hint job-error">${renderClipValue("", entry.config_error, { className: "clip-hint", clipLength: 68 })}</div>` : ""}
          </div>
          <div class="job-card-actions">
            <span class="hint-with-help">
              <button type="button" class="btn-force" onclick="return forceUploadFromEvent(event, decodeURIComponent('${encodeURIComponent(jobName)}'), this)">Force Upload</button>
              <span class="hover-hint" title="Upload even if unchanged. Central may reject as duplicate.">?</span>
            </span>
            <button type="button" class="btn-restore" onclick="return openRecoverDialogFromEvent(event, decodeURIComponent('${encodedPath(entry.relative_path)}'), decodeURIComponent('${encodeURIComponent(jobName)}'))">Restore</button>
            ${entry.blocked_by_parent ? "" : `<button type="button" class="btn-edit" onclick="return openJobDialogFromEvent(event, decodeURIComponent('${encodedPath(entry.relative_path)}'))">Edit</button>`}
          </div>
        </div>
      </div>
      `;
    }).join("")
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
  clearStatus("form-status");
  setStatus(
    "form-status",
    entry?.blocked_by_parent
      ? `This folder sits under ${entry.blocked_by_parent}. Edge follows the parent job, so nested folders here should not have their own active upload settings.`
      : entry?.selected
        ? "You are editing the upload settings Edge already uses for this folder."
        : "You are creating upload settings so Edge starts treating this folder as its own backup job.",
    entry?.blocked_by_parent ? "error" : "info",
  );
}

function resetForm() {
  document.getElementById("relative_path").value = ".";
  document.getElementById("job_name").value = "";
  document.getElementById("exclude").value = "";
  document.getElementById("include_hidden").checked = true;
  document.getElementById("follow_symlinks").checked = false;
  setStatus("form-status", "Choose a directory, then click Save Job to create or update its .upload_dir backup settings.", "info");
}

async function loadData({ silent = false } = {}) {
  if (isLoadingData) {
    return;
  }

  isLoadingData = true;
  if (!silent) {
    const spinner = '<div class="section-loading"><span class="section-spinner" aria-label="Loading…"></span></div>';
    document.getElementById("selected-jobs").innerHTML = spinner;
    document.getElementById("directory-tree").innerHTML = spinner;
  }
  try {
    const [dirRes, keyRes] = await Promise.all([
      fetch("/api/directories"),
      fetch("/api/encryption-key"),
    ]);
    if (!dirRes.ok || !keyRes.ok) {
      throw new Error("Refresh failed.");
    }

    latestData = await dirRes.json();
    const keyData = await keyRes.json();
    fillMeta(latestData, keyData.key || "", keyData.fingerprint || latestData.encryption_key_fingerprint || "");
    if (!document.getElementById("settings-dialog")?.open) {
      fillSettings(latestData.settings || {});
    }
    renderDirectories(latestData);
  } catch (error) {
    if (!silent) {
      setActionStatus(error.message || "Refresh failed.", "error");
    }
  } finally {
    isLoadingData = false;
  }
}

async function saveSettings() {
  setStatus("settings-status", "Saving...", "info");
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
  setStatus("settings-status", response.ok ? "Saved." : (body.detail || "Settings save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    if (latestData && body.settings) {
      latestData.settings = body.settings;
    }
    setActionStatus("Edge settings saved.", "success");
    await pause(350);
    closeDialog("settings-dialog");
    loadData({ silent: true });
  } else {
    setActionStatus(body.detail || "Settings save failed.", "error");
  }
}

async function saveJob() {
  setStatus("form-status", "Saving job settings...", "info");
  const relativePath = document.getElementById("relative_path").value || ".";
  const payload = {
    relative_path: relativePath,
    job_name: document.getElementById("job_name").value.trim() || null,
    exclude: document.getElementById("exclude").value.split("\n").map((value) => value.trim()).filter(Boolean),
    include_hidden: document.getElementById("include_hidden").checked,
    follow_symlinks: document.getElementById("follow_symlinks").checked,
  };

  const response = await fetch("/api/jobs", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await response.json();
  setStatus("form-status", response.ok ? "Saved. Closing..." : (body.detail || "Save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadData({ silent: true });
    setActionStatus(`Saved .upload_dir for ${relativePath}.`, "success");
    await pause(450);
    closeDialog("job-dialog");
  } else {
    setActionStatus(body.detail || "Save failed.", "error");
  }
}

async function deleteByPath(relativePath) {
  if (!await confirmApp({
    title: "Stop Backing Up Folder",
    message: `Stop backing up ${relativePath}? This only removes the .upload_dir settings file for this folder. It does not delete the folder itself or remove backups already stored in Central.`,
    confirmLabel: "Stop Backup",
    danger: true,
  })) {
    return;
  }
  setStatus("form-status", "Removing this folder's upload settings...", "info");
  const response = await fetch(`/api/jobs?relative_path=${encodeURIComponent(relativePath)}`, {
    method: "DELETE",
  });
  const body = await response.json();
  setStatus("form-status", response.ok ? "This folder is no longer treated as its own backup job." : (body.detail || "Delete failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadData({ silent: true });
    setActionStatus(`Edge will no longer back up ${relativePath} as its own job.`, "success");
    await pause(450);
    closeDialog("job-dialog");
  } else {
    if (response.status === 404 || body.detail === "directory not found") {
      await loadData({ silent: true });
      closeDialog("job-dialog");
      setActionStatus(`That folder was already gone, so Edge refreshed the directory list.`, "info");
      return;
    }
    setActionStatus(body.detail || "Delete failed.", "error");
  }
}

async function forceUpload(jobName, btn) {
  const label = jobName || "this job";
  if (!await confirmApp({
    title: "Force Upload",
    message: `Force an upload for ${label}? This bypasses the unchanged check. Central may still reject it as a duplicate if that snapshot already exists.`,
    confirmLabel: "Force Upload",
  })) {
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

function forceUploadFromEvent(event, jobName, btn) {
  stopActionEvent(event);
  forceUpload(jobName, btn);
  return false;
}

function openRecoverDialog(relativePath, jobName) {
  _recoverContext = { relativePath, jobName };
  document.getElementById("recover-dialog-job-name").textContent = jobName || relativePath;
  document.getElementById("recover-fingerprint").value = "";
  clearStatus("recover-status");
  openDialog("recover-dialog");
}

function openRecoverDialogFromEvent(event, relativePath, jobName) {
  stopActionEvent(event);
  openRecoverDialog(relativePath, jobName);
  return false;
}

async function confirmRecover() {
  if (!_recoverContext) return;
  const { relativePath, jobName } = _recoverContext;
  const label = jobName || relativePath;
  const fingerprint = document.getElementById("recover-fingerprint").value.trim();
  const btn = document.getElementById("recover-confirm-btn");

  setStatus("recover-status", "Restoring…", "info");
  if (btn) btn.disabled = true;
  try {
    const params = new URLSearchParams({ relative_path: relativePath });
    if (fingerprint) params.set("snapshot_fingerprint", fingerprint);
    const response = await fetch(`/api/jobs/recover-latest?${params}`, { method: "POST" });
    const body = await response.json();
    if (!response.ok) {
      setStatus("recover-status", body.detail || `Restore failed for ${label}.`, "error");
      setActionStatus(body.detail || `Restore failed for ${label}.`, "error");
      return;
    }
    if (body.status === "already_running") {
      setStatus("recover-status", "A backup or recovery operation is already running.", "error");
      setActionStatus("A backup or recovery operation is already running on this Edge.", "error");
      return;
    }
    const restoredFiles = Number(body.restored_files || 0);
    const snapshotName = body.snapshot_filename || "snapshot";
    closeDialog("recover-dialog");
    setActionStatus(
      `Restored ${label} from ${snapshotName} — ${restoredFiles} file${restoredFiles === 1 ? "" : "s"} replaced.`,
      "success",
    );
    await loadData({ silent: true });
  } catch (error) {
    setStatus("recover-status", error.message || `Restore failed for ${label}.`, "error");
    setActionStatus(error.message || `Restore failed for ${label}.`, "error");
  } finally {
    if (btn) btn.disabled = false;
  }
}

async function deleteJob() {
  const relativePath = document.getElementById("relative_path").value || ".";
  await deleteByPath(relativePath);
}

async function runNow() {
  try {
    const response = await fetch("/api/run-now", { method: "POST" });
    const body = await response.json();
    if (body.status === "queued" || body.status === "started") {
      const cleared = body.manual_retries_cleared || 0;
      setActionStatus(
        cleared > 0
          ? `Backup cycle requested. ${cleared} manual block(s) cleared for retry.`
          : "Backup cycle requested.",
        "success",
      );
      await loadData({ silent: true });
      return;
    }
    setActionStatus("A backup cycle is already running.", "info");
  } catch (error) {
    setActionStatus(error.message || "Failed to start a backup cycle.", "error");
  }
}

document.getElementById("hook_pre_command")?.addEventListener("input", () => {
  hookDraftDirty.pre = true;
});
document.getElementById("hook_post_command")?.addEventListener("input", () => {
  hookDraftDirty.post = true;
});

resetForm();
initializeFieldHelp(EDGE_SETTINGS_HELP);
document.getElementById("settings_cron_schedule")?.addEventListener("input", updateCronScheduleHint);
loadData();
