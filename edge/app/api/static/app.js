let latestData = null;

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

function fillMeta(data, encKey, encFingerprint) {
  const scheduler = data.scheduler || {};
  const uploadCircuit = data.upload_circuit || {};
  const settingsStatus = data.settings_status || {};
  document.getElementById("meta").innerHTML = `
    <div><strong>Edge ID</strong><br>${escapeHtml(data.edge_id)}</div>
    <div><strong>Instance ID</strong><br><code title="${escapeHtml(data.edge_instance_id || "")}">${escapeHtml((data.edge_instance_id || "—").slice(0, 12))}</code></div>
    <div><strong>Scan Root</strong><br>${escapeHtml(data.scan_root)}</div>
    <div><strong>Central URL</strong><br>${escapeHtml(data.central_url)}</div>
    <div><strong>Edge UI</strong><br>${escapeHtml(data.http_url)}</div>
    <div><strong>Settings File</strong><br>${escapeHtml(data.settings_path || "n/a")}</div>
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

function renderDirectories(data) {
  const rows = data.directories.map((entry) => {
    const state = entry.state?.last_status || "none";
    const progress = entry.state?.pending_archive_size
      ? `${entry.state?.upload_offset || 0}/${entry.state.pending_archive_size}`
      : "n/a";
    return `
      <tr>
        <td><code>${escapeHtml(entry.relative_path)}</code><br><span class="hint">${escapeHtml(entry.absolute_path)}</span></td>
        <td>${statusBadge(entry)}</td>
        <td>${escapeHtml(state)}<br><span class="hint">${escapeHtml(progress)}</span></td>
        <td><button type="button" class="secondary" onclick="editPath(decodeURIComponent('${encodedPath(entry.relative_path)}'))">Edit</button></td>
      </tr>
    `;
  }).join("");
  document.getElementById("directory-rows").innerHTML = rows;

  const selected = data.directories.filter((entry) => entry.selected);
  document.getElementById("selected-jobs").innerHTML = selected.length
    ? selected.map((entry) => `
      <div class="job-card">
        <strong>${escapeHtml(entry.config?.job_name || entry.relative_path)}</strong>
        <div class="hint"><code>${escapeHtml(entry.relative_path)}</code></div>
        <div class="hint">Last state: ${escapeHtml(entry.state?.last_status || "none")}</div>
        ${entry.state?.pending_archive_size ? `<div class="hint">Progress: ${escapeHtml(`${entry.state?.upload_offset || 0}/${entry.state.pending_archive_size} bytes`)}</div>` : ""}
        ${entry.state?.next_retry_at ? `<div class="hint">Next retry: ${escapeHtml(entry.state.next_retry_at)}</div>` : ""}
        ${entry.state?.last_error_detail ? `<div class="hint" style="color:#b42318;">${escapeHtml(entry.state.last_error_detail)}</div>` : ""}
        ${entry.blocked_by_parent ? `<div class="hint">Nested under existing job <code>${escapeHtml(entry.blocked_by_parent)}</code></div>` : ""}
        ${entry.config_error ? `<div class="hint" style="color:#b42318;">${escapeHtml(entry.config_error)}</div>` : ""}
        <div class="toolbar">
          <button type="button" class="secondary" onclick="editPath(decodeURIComponent('${encodedPath(entry.relative_path)}'))">Edit</button>
          <button type="button" class="danger" onclick="deleteByPath(decodeURIComponent('${encodedPath(entry.relative_path)}'))">Delete</button>
        </div>
      </div>
    `).join("")
    : '<p class="hint">No directories are selected yet.</p>';
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
  document.getElementById("form-status").textContent = "Pick a directory from the list to edit it.";
}

async function loadData() {
  const [dirRes, keyRes] = await Promise.all([
    fetch("/api/directories"),
    fetch("/api/encryption-key"),
  ]);
  latestData = await dirRes.json();
  const keyData = await keyRes.json();
  fillMeta(latestData, keyData.key || "", keyData.fingerprint || latestData.encryption_key_fingerprint || "");
  fillSettings(latestData.settings || {});
  renderDirectories(latestData);
  if (!document.getElementById("relative_path").value) {
    resetForm();
  }
}

async function saveSettings() {
  const payload = {
    edge_id: document.getElementById("settings_edge_id").value.trim(),
    scan_root: document.getElementById("settings_scan_root").value.trim(),
    central_url: document.getElementById("settings_central_url").value.trim(),
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
    return;
  }
  document.getElementById("run-status").textContent = "A cycle is already running.";
}

resetForm();
loadData();
