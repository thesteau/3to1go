const ENC_MAGIC = new Uint8Array([82, 67, 69, 78, 67, 49, 0, 0]); // "RCENC1\x00\x00"
const ENC_MAGIC_LEN = 8;
const ENC_IV_LEN = 12;

function isEncrypted(buffer) {
  if (buffer.byteLength < ENC_MAGIC_LEN + ENC_IV_LEN) return false;
  const view = new Uint8Array(buffer, 0, ENC_MAGIC_LEN);
  return ENC_MAGIC.every((b, i) => b === view[i]);
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function base64UrlToBytes(b64) {
  const normalized = String(b64 || "").trim();
  if (!normalized) throw new Error("missing key");
  const std = normalized.replace(/-/g, "+").replace(/_/g, "/");
  const padded = std.padEnd(std.length + ((4 - (std.length % 4)) % 4), "=");
  return Uint8Array.from(atob(padded), (c) => c.charCodeAt(0));
}

function bytesToHex(bytes) {
  return Array.from(bytes, (value) => value.toString(16).padStart(2, "0")).join("");
}

async function fingerprintKey(keyB64) {
  const keyBytes = base64UrlToBytes(keyB64);
  const digest = await crypto.subtle.digest("SHA-256", keyBytes);
  return bytesToHex(new Uint8Array(digest));
}

async function decryptBuffer(buffer, keyB64) {
  const keyBytes = base64UrlToBytes(keyB64);
  const iv = new Uint8Array(buffer, ENC_MAGIC_LEN, ENC_IV_LEN);
  const ciphertext = buffer.slice(ENC_MAGIC_LEN + ENC_IV_LEN);
  const key = await crypto.subtle.importKey("raw", keyBytes, { name: "AES-GCM" }, false, ["decrypt"]);
  return crypto.subtle.decrypt({ name: "AES-GCM", iv }, key, ciphertext);
}

function triggerBlobDownload(buffer, filename) {
  const url = URL.createObjectURL(new Blob([buffer]));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

function shortFingerprint(fingerprint) {
  return fingerprint ? fingerprint.slice(0, 12) : "unknown";
}

function escapeSelectorValue(value) {
  return String(value).replaceAll("\\", "\\\\").replaceAll('"', '\\"');
}

function clipMiddle(value, maxLength = 28) {
  const text = String(value ?? "");
  if (text.length <= maxLength) return text;
  const head = Math.max(8, Math.floor((maxLength - 1) / 2));
  const tail = Math.max(6, maxLength - head - 1);
  return `${text.slice(0, head)}…${text.slice(-tail)}`;
}

function renderClipValue(label, value, { className = "", clipLength = 28 } = {}) {
  const full = String(value ?? "").trim();
  if (!full) return "";
  return renderStaticClipValue(label, full, { className, clipLength });
}

function renderStaticClipValue(label, value, { className = "", clipLength = 28 } = {}) {
  const full = String(value ?? "").trim();
  if (!full) return "";
  const short = clipMiddle(full, clipLength);
  const classes = className ? ` ${className}` : "";
  return `<span class="clip-static${classes}" title="${escapeHtml(full)}">${label ? `<span class="clip-label">${escapeHtml(label)}</span>` : ""}<span class="clip-value">${escapeHtml(short)}</span></span>`;
}

function renderLinkValue(label, value, { className = "", clipLength = 28 } = {}) {
  const full = String(value ?? "").trim();
  if (!full) return "";
  const short = clipMiddle(full, clipLength);
  const classes = className ? ` ${className}` : "";
  return `<a class="clip-static clip-link${classes}" href="${escapeHtml(full)}" target="_blank" rel="noopener noreferrer" title="${escapeHtml(full)}">${label ? `<span class="clip-label">${escapeHtml(label)}</span>` : ""}<span class="clip-value">${escapeHtml(short)}</span></a>`;
}

const _encKeys = {};
let _edgeKeyFingerprints = {};
let _overviewRefreshTimer = null;
let _overviewLoading = false;
let _visibilityRefreshBound = false;
const OVERVIEW_REFRESH_INTERVAL_MS = 5000;

function setActionStatus(message, kind = "info") {
  const element = document.getElementById("action-status");
  if (!element) return;
  element.textContent = message || "";
  element.className = kind === "error" ? "hint key-status error" : "hint";
}

function clearStatus(id) {
  const element = document.getElementById(id);
  if (element) {
    element.textContent = "";
  }
}

function openDialog(id) {
  const dialog = document.getElementById(id);
  if (!dialog?.showModal || dialog.open) return;
  dialog.showModal();
}

function closeDialog(id) {
  const dialog = document.getElementById(id);
  if (dialog?.open) {
    dialog.close();
  }
}

function fillSettings(settings) {
  const data = settings || {};
  document.getElementById("settings_retention_keep_last").value = data.retention_keep_last ?? 3;
  document.getElementById("settings_log_level").value = data.log_level || "INFO";
  document.getElementById("settings_max_upload_size_mb").value = data.max_upload_size_mb ?? 2048;
  document.getElementById("settings_upload_chunk_size_mb").value = data.upload_chunk_size_mb ?? 8;
  document.getElementById("settings_upload_session_ttl_hours").value = data.upload_session_ttl_hours ?? 24;
  document.getElementById("settings_upload_cleanup_interval_seconds").value = data.upload_cleanup_interval_seconds ?? 300;
}

function openSettingsDialog() {
  fillSettings(window.__centralSettings || {});
  clearStatus("settings-status");
  openDialog("settings-dialog");
}

function buildEdgeKeyId(edgeId, edgeInstanceId) {
  return `${edgeId}::${edgeInstanceId || "_legacy"}`;
}

function getExpectedKeyFingerprint(edgeId, edgeInstanceId) {
  return _edgeKeyFingerprints[buildEdgeKeyId(edgeId, edgeInstanceId)] || null;
}

function getEncKey(edgeId, edgeInstanceId) {
  const keyId = buildEdgeKeyId(edgeId, edgeInstanceId);
  if (_encKeys[keyId]) return _encKeys[keyId];
  const stored = sessionStorage.getItem(`relay_enc_${keyId}`);
  if (stored) {
    _encKeys[keyId] = stored;
    return stored;
  }
  return null;
}

function setEncKey(edgeId, edgeInstanceId, key) {
  const keyId = buildEdgeKeyId(edgeId, edgeInstanceId);
  _encKeys[keyId] = key;
  sessionStorage.setItem(`relay_enc_${keyId}`, key);
}

function clearStoredEncKey(edgeId, edgeInstanceId) {
  const keyId = buildEdgeKeyId(edgeId, edgeInstanceId);
  delete _encKeys[keyId];
  sessionStorage.removeItem(`relay_enc_${keyId}`);
}

function keyInputElement(edgeId, edgeInstanceId) {
  const selector = `[data-edge-key-input="${escapeSelectorValue(buildEdgeKeyId(edgeId, edgeInstanceId))}"]`;
  return document.querySelector(selector);
}

function keyStatusElement(edgeId, edgeInstanceId) {
  const selector = `[data-edge-key-status="${escapeSelectorValue(buildEdgeKeyId(edgeId, edgeInstanceId))}"]`;
  return document.querySelector(selector);
}

function setKeyStatus(edgeId, edgeInstanceId, message, kind = "info") {
  const element = keyStatusElement(edgeId, edgeInstanceId);
  if (!element) return;
  element.textContent = message;
  element.className = `key-status ${kind}`;
}

async function storeEncKey(edgeId, edgeInstanceId, key, { alertOnError = false } = {}) {
  try {
    const actualFingerprint = await fingerprintKey(key);
    const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
    if (expectedFingerprint && actualFingerprint !== expectedFingerprint) {
      clearStoredEncKey(edgeId, edgeInstanceId);
      const message = `That key belongs to a different Edge. Expected ${shortFingerprint(expectedFingerprint)}, got ${shortFingerprint(actualFingerprint)}.`;
      setKeyStatus(edgeId, edgeInstanceId, message, "error");
      if (alertOnError) alert(message);
      return null;
    }

    setEncKey(edgeId, edgeInstanceId, key);
    setKeyStatus(
      edgeId,
      edgeInstanceId,
      expectedFingerprint
        ? `Key saved and verified for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`
        : `Key saved for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`,
      "ok",
    );
    return key;
  } catch {
    clearStoredEncKey(edgeId, edgeInstanceId);
    const message = "Encryption key was not valid base64url text.";
    setKeyStatus(edgeId, edgeInstanceId, message, "error");
    if (alertOnError) alert(message);
    return null;
  }
}

async function rememberEncKey(edgeId, edgeInstanceId) {
  const input = keyInputElement(edgeId, edgeInstanceId);
  const key = input?.value.trim() || "";
  if (!key) {
    const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
    setKeyStatus(
      edgeId,
      edgeInstanceId,
      expectedFingerprint
        ? `Paste the Edge key first. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : "Paste the Edge key first.",
      "warn",
    );
    return;
  }
  const stored = await storeEncKey(edgeId, edgeInstanceId, key);
  if (stored && input) input.value = "";
}

function clearEncKey(edgeId, edgeInstanceId) {
  clearStoredEncKey(edgeId, edgeInstanceId);
  const input = keyInputElement(edgeId, edgeInstanceId);
  if (input) input.value = "";
  const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
  setKeyStatus(
    edgeId,
    edgeInstanceId,
    expectedFingerprint
      ? `Cleared. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
      : "Cleared saved key for this browser session.",
    "info",
  );
}

async function refreshKeyPanel(edgeId, edgeInstanceId) {
  const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
  const key = getEncKey(edgeId, edgeInstanceId);

  if (!key) {
    setKeyStatus(
      edgeId,
      edgeInstanceId,
      expectedFingerprint
        ? `No key saved yet. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : "No key saved yet. Central has not seen a key fingerprint for this Edge yet.",
      expectedFingerprint ? "info" : "warn",
    );
    return;
  }

  try {
    const actualFingerprint = await fingerprintKey(key);
    if (expectedFingerprint && actualFingerprint !== expectedFingerprint) {
      clearStoredEncKey(edgeId, edgeInstanceId);
      setKeyStatus(
        edgeId,
        edgeInstanceId,
        `Saved key fingerprint ${shortFingerprint(actualFingerprint)} did not match expected ${shortFingerprint(expectedFingerprint)} and was cleared.`,
        "error",
      );
      return;
    }

    setKeyStatus(
      edgeId,
      edgeInstanceId,
      expectedFingerprint
        ? `Saved key verified for this browser session. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : `Saved key present for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`,
      "ok",
    );
  } catch {
    clearStoredEncKey(edgeId, edgeInstanceId);
    setKeyStatus(edgeId, edgeInstanceId, "Saved key was invalid and has been cleared.", "error");
  }
}

async function resolveEncKey(edgeId, edgeInstanceId) {
  const saved = getEncKey(edgeId, edgeInstanceId);
  if (saved) return saved;

  const typed = keyInputElement(edgeId, edgeInstanceId)?.value.trim() || "";
  if (typed) {
    return storeEncKey(edgeId, edgeInstanceId, typed, { alertOnError: true });
  }

  const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
  const instanceLabel = edgeInstanceId || "legacy";
  const promptMessage = expectedFingerprint
    ? `Snapshot is encrypted.\nEnter the encryption key for edge "${edgeId}" instance "${instanceLabel}".\nExpected fingerprint: ${shortFingerprint(expectedFingerprint)}`
    : `Snapshot is encrypted.\nEnter the encryption key for edge "${edgeId}" instance "${instanceLabel}".`;
  const prompted = (prompt(promptMessage) || "").trim();
  if (!prompted) return null;
  return storeEncKey(edgeId, edgeInstanceId, prompted, { alertOnError: true });
}

async function downloadSnapshot(edgeId, edgeInstanceId, jobName, filename, btn) {
  const basePath = edgeInstanceId
    ? `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(edgeInstanceId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`
    : `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`;
  const url = basePath;
  btn.disabled = true;
  try {
    const res = await fetch(url);
    if (!res.ok) {
      alert("Download failed.");
      return;
    }
    const buffer = await res.arrayBuffer();

    if (!isEncrypted(buffer)) {
      triggerBlobDownload(buffer, filename);
      return;
    }

    const key = await resolveEncKey(edgeId, edgeInstanceId);
    if (!key) return;

    try {
      const decrypted = await decryptBuffer(buffer, key);
      triggerBlobDownload(decrypted, filename);
    } catch {
      clearStoredEncKey(edgeId, edgeInstanceId);
      await refreshKeyPanel(edgeId, edgeInstanceId);
      const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
      alert(
        expectedFingerprint
          ? "Decryption failed after fingerprint verification. The archive may be corrupted, or the Edge key changed after this snapshot was uploaded."
          : "Decryption failed - wrong key or corrupted archive.",
      );
    }
  } finally {
    btn.disabled = false;
  }
}

function formatBytes(bytes) {
  if (!bytes) return "—";
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 ** 2) return (bytes / 1024).toFixed(1) + " KB";
  if (bytes < 1024 ** 3) return (bytes / 1024 ** 2).toFixed(1) + " MB";
  return (bytes / 1024 ** 3).toFixed(2) + " GB";
}

function parseSnapshotDate(filename) {
  const parts = filename.split("__");
  if (parts.length < 3) return null;
  const iso = parts[1].replace(/T(\d{2})-(\d{2})-(\d{2})Z$/, "T$1:$2:$3Z");
  const d = new Date(iso);
  return isNaN(d) ? null : d;
}

function parseFingerprint(filename) {
  const parts = filename.split("__");
  if (parts.length < 3) return null;
  return parts[2].replace(/\.tar\.zst$/, "");
}

function formatDate(d) {
  if (!d) return "—";
  return d.toLocaleString(undefined, {
    year: "numeric", month: "short", day: "numeric",
    hour: "2-digit", minute: "2-digit",
  });
}

let _token = sessionStorage.getItem("relay_token") || "";

function getToken() {
  if (!_token) {
    _token = prompt("Enter the relay auth token:") || "";
    if (_token) sessionStorage.setItem("relay_token", _token);
  }
  return _token;
}

async function deleteSnapshot(edgeId, edgeInstanceId, jobName, filename, btn) {
  if (!confirm(`Delete ${filename}?\nThis cannot be undone.`)) return;

  btn.disabled = true;
  try {
    const url = edgeInstanceId
      ? `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(edgeInstanceId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`
      : `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`;
    const res = await fetch(
      url,
      { method: "DELETE" },
    );
    if (!res.ok) {
      alert("Delete failed.");
      return;
    }
    btn.closest(".snapshot-row").remove();
  } finally {
    btn.disabled = false;
  }
}

function renderSnapshots(edgeId, edgeInstanceId, jobName, snapshots) {
  if (!snapshots.length) return '<p class="no-snapshots">No snapshots yet.</p>';
  return snapshots.map((snap) => {
    const name = snap.name;
    const size = formatBytes(snap.size_bytes);
    const date = formatDate(parseSnapshotDate(name));
    const fp = parseFingerprint(name) || "";
    return `
      <div class="snapshot-row">
        <div class="snapshot-meta">
          <span class="snapshot-date">${escapeHtml(date)}</span>
          ${fp ? renderClipValue("FP", fp, { className: "snapshot-fp", clipLength: 18 }) : ""}
        </div>
        <span class="snapshot-size">${escapeHtml(size)}</span>
        <div class="snapshot-actions">
          <button class="btn btn-dl"
            onclick="downloadSnapshot('${escapeHtml(edgeId)}',${edgeInstanceId ? `'${escapeHtml(edgeInstanceId)}'` : "null"},'${escapeHtml(jobName)}','${escapeHtml(name)}',this)">Download</button>
          <button class="btn btn-del"
            onclick="deleteSnapshot('${escapeHtml(edgeId)}',${edgeInstanceId ? `'${escapeHtml(edgeInstanceId)}'` : "null"},'${escapeHtml(jobName)}','${escapeHtml(name)}',this)">Delete</button>
        </div>
      </div>`;
  }).join("");
}

function renderKeyManager(ns) {
  const edgeId = ns.edge_id;
  const edgeInstanceId = ns.edge_instance_id;
  const edgeKeyId = buildEdgeKeyId(edgeId, edgeInstanceId);
  const expectedFingerprint = ns.encryption_key_fingerprint || "";
  return `
    <div class="edge-key-panel">
      <div class="edge-key-head">
        <strong>Decryption Key</strong>
        ${renderStaticClipValue("Expected key fingerprint", expectedFingerprint || "unknown", { className: "edge-detail", clipLength: 24 })}
      </div>
      <div class="edge-key-controls">
        <input
          type="password"
          placeholder="Paste the Edge encryption key"
          data-edge-key-input="${escapeHtml(edgeKeyId)}">
        <button class="btn btn-key" type="button" onclick="rememberEncKey('${escapeHtml(edgeId)}','${escapeHtml(edgeInstanceId)}')">Save Key</button>
        <button class="btn btn-clear" type="button" onclick="clearEncKey('${escapeHtml(edgeId)}','${escapeHtml(edgeInstanceId)}')">Clear</button>
      </div>
      <div class="key-status info" data-edge-key-status="${escapeHtml(edgeKeyId)}"></div>
    </div>
  `;
}

function renderInstanceMeta(instance) {
  return `
    ${instance.edge_instance_id ? renderClipValue("Instance", instance.edge_instance_id, { className: "edge-detail", clipLength: 24 }) : '<span class="edge-detail edge-detail-warn">Legacy snapshot path</span>'}
    ${instance.advertised_url ? renderLinkValue("Edge URL", instance.advertised_url, { className: "edge-detail", clipLength: 28 }) : ''}
    ${instance.last_seen_source ? renderClipValue("Source", instance.last_seen_source, { className: "edge-detail", clipLength: 24 }) : '<span class="edge-detail edge-detail-warn">Source unknown</span>'}
    ${renderClipValue("Key FP", instance.encryption_key_fingerprint || "unknown", { className: "edge-detail", clipLength: 24 })}
  `;
}

function renderInstanceCard(edgeId, instance) {
  return `
    <section class="instance-card">
      <div class="instance-head">
        <div>
          <div class="instance-title">${escapeHtml(instance.edge_instance_id || "Legacy snapshots")}</div>
          <div class="edge-submeta">${renderInstanceMeta(instance)}</div>
        </div>
        <span class="edge-count">${(instance.jobs || []).length} job${instance.jobs.length !== 1 ? "s" : ""}</span>
      </div>
      ${instance.edge_instance_id ? renderKeyManager({ edge_id: edgeId, edge_instance_id: instance.edge_instance_id, encryption_key_fingerprint: instance.encryption_key_fingerprint }) : ""}
      ${(instance.jobs || []).map((job) => `
        <div class="job-block">
          <div class="job-header">
            <span class="job-name">${escapeHtml(job.job_name)}</span>
            <span class="job-count">${escapeHtml(String(job.snapshot_count))} snapshot${job.snapshot_count !== 1 ? "s" : ""}</span>
          </div>
          <div class="snapshot-list">
            ${renderSnapshots(edgeId, instance.edge_instance_id, job.job_name, job.snapshots || [])}
          </div>
        </div>
      `).join("") || '<p class="no-snapshots">No jobs stored yet.</p>'}
    </section>
  `;
}

function captureOverviewUiState() {
  const expandedEdges = Array.from(document.querySelectorAll("#namespaces details[data-edge-id][open]"))
    .map((element) => element.dataset.edgeId)
    .filter(Boolean);
  const keyDrafts = Object.fromEntries(
    Array.from(document.querySelectorAll("[data-edge-key-input]"))
      .map((input) => [input.dataset.edgeKeyInput, input.value])
      .filter(([, value]) => value),
  );
  return {
    expandedEdges: new Set(expandedEdges),
    keyDrafts,
  };
}

function shouldDeferOverviewRefresh() {
  const activeElement = document.activeElement;
  return Boolean(activeElement?.matches?.("[data-edge-key-input]")) || Boolean(document.getElementById("settings-dialog")?.open);
}

function restoreKeyDrafts(keyDrafts) {
  Object.entries(keyDrafts || {}).forEach(([edgeKeyId, value]) => {
    const [edgeId, rawInstanceId] = String(edgeKeyId).split("::", 2);
    const input = keyInputElement(edgeId, rawInstanceId === "_legacy" ? null : rawInstanceId);
    if (input && !input.value) {
      input.value = value;
    }
  });
}

async function loadOverview({ silent = false } = {}) {
  if (_overviewLoading) {
    return;
  }
  if (silent && shouldDeferOverviewRefresh()) {
    return;
  }

  _overviewLoading = true;
  const uiState = captureOverviewUiState();

  try {
    const res = await fetch("/api/overview");
    if (!res.ok) {
      throw new Error("Refresh failed.");
    }
    const data = await res.json();
    window.__centralSettings = data.settings || {};

    document.getElementById("meta").innerHTML = `
      <div><strong>Status</strong><br><span class="status-${escapeHtml(data.status)}">${escapeHtml(data.status)}</span></div>
      <div><strong>Backup Root</strong><br>${escapeHtml(data.backup_root)}</div>
      <div><strong>Staging Dir</strong><br>${escapeHtml(data.staging_dir)}</div>
      <div><strong>Retention</strong><br>keep last ${escapeHtml(String(data.retention_keep_last))}</div>
      <div><strong>Settings File</strong><br>${escapeHtml(data.settings_path || "n/a")}</div>
    `;

    const edges = data.edges || [];
    const allInstances = edges.flatMap((edge) => (edge.instances || []).map((instance) => ({ edgeId: edge.edge_id, instance })));
    _edgeKeyFingerprints = Object.fromEntries(
      allInstances
        .filter(({ instance }) => instance.edge_instance_id)
        .map(({ edgeId, instance }) => [buildEdgeKeyId(edgeId, instance.edge_instance_id), instance.encryption_key_fingerprint || ""]),
    );

    document.getElementById("namespaces").innerHTML = edges.length
      ? edges.map((edge) => `
          <details class="edge-card edge-card-collapsible" data-edge-id="${escapeHtml(edge.edge_id)}"${uiState.expandedEdges.has(edge.edge_id) ? " open" : ""}>
            <summary class="edge-header edge-card-summary">
              <div class="edge-header-main">
                <span class="edge-id">${escapeHtml(edge.edge_id)}</span>
                <div class="edge-submeta">${escapeHtml(String((edge.instances || []).length))} instance${edge.instances.length !== 1 ? "s" : ""}</div>
                <span class="edge-expand-label">Expand</span>
              </div>
              <span class="edge-count">${(edge.instances || []).reduce((total, instance) => total + (instance.jobs || []).length, 0)} job${(edge.instances || []).reduce((total, instance) => total + (instance.jobs || []).length, 0) !== 1 ? "s" : ""}</span>
            </summary>
            <div class="edge-card-body">
              ${(edge.instances || []).map((instance) => renderInstanceCard(edge.edge_id, instance)).join("") || '<p class="no-snapshots">No instances registered yet.</p>'}
            </div>
          </details>
        `).join("")
      : '<p class="hint">No snapshots have been stored yet.</p>';

    restoreKeyDrafts(uiState.keyDrafts);
    if (!document.getElementById("settings-dialog")?.open) {
      fillSettings(data.settings || {});
    }
    await Promise.all(
      allInstances
        .filter(({ instance }) => instance.edge_instance_id)
        .map(({ edgeId, instance }) => refreshKeyPanel(edgeId, instance.edge_instance_id)),
    );
  } catch (error) {
    if (!silent) {
      alert(error.message || "Refresh failed.");
    }
  } finally {
    _overviewLoading = false;
  }
}

async function saveSettings() {
  const payload = {
    retention_keep_last: Number(document.getElementById("settings_retention_keep_last").value || 1),
    log_level: document.getElementById("settings_log_level").value,
    max_upload_size_mb: Number(document.getElementById("settings_max_upload_size_mb").value || 1),
    upload_chunk_size_mb: Number(document.getElementById("settings_upload_chunk_size_mb").value || 1),
    upload_session_ttl_hours: Number(document.getElementById("settings_upload_session_ttl_hours").value || 1),
    upload_cleanup_interval_seconds: Number(document.getElementById("settings_upload_cleanup_interval_seconds").value || 10),
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
    await loadOverview({ silent: true });
    setActionStatus("Central settings saved.");
    closeDialog("settings-dialog");
  } else {
    setActionStatus(body.detail || "Settings save failed.", "error");
  }
}

function startOverviewAutoRefresh() {
  if (_overviewRefreshTimer) {
    clearInterval(_overviewRefreshTimer);
  }

  _overviewRefreshTimer = setInterval(() => {
    if (document.hidden) {
      return;
    }
    loadOverview({ silent: true });
  }, OVERVIEW_REFRESH_INTERVAL_MS);

  if (!_visibilityRefreshBound) {
    document.addEventListener("visibilitychange", () => {
      if (!document.hidden) {
        loadOverview({ silent: true });
      }
    });
    _visibilityRefreshBound = true;
  }
}

loadOverview();
startOverviewAutoRefresh();
