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

const _encKeys = {};
let _edgeKeyFingerprints = {};

function getExpectedKeyFingerprint(edgeId) {
  return _edgeKeyFingerprints[edgeId] || null;
}

function getEncKey(edgeId) {
  if (_encKeys[edgeId]) return _encKeys[edgeId];
  const stored = sessionStorage.getItem(`relay_enc_${edgeId}`);
  if (stored) {
    _encKeys[edgeId] = stored;
    return stored;
  }
  return null;
}

function setEncKey(edgeId, key) {
  _encKeys[edgeId] = key;
  sessionStorage.setItem(`relay_enc_${edgeId}`, key);
}

function clearStoredEncKey(edgeId) {
  delete _encKeys[edgeId];
  sessionStorage.removeItem(`relay_enc_${edgeId}`);
}

function keyInputElement(edgeId) {
  const selector = `[data-edge-key-input="${escapeSelectorValue(edgeId)}"]`;
  return document.querySelector(selector);
}

function keyStatusElement(edgeId) {
  const selector = `[data-edge-key-status="${escapeSelectorValue(edgeId)}"]`;
  return document.querySelector(selector);
}

function setKeyStatus(edgeId, message, kind = "info") {
  const element = keyStatusElement(edgeId);
  if (!element) return;
  element.textContent = message;
  element.className = `key-status ${kind}`;
}

async function storeEncKey(edgeId, key, { alertOnError = false } = {}) {
  try {
    const actualFingerprint = await fingerprintKey(key);
    const expectedFingerprint = getExpectedKeyFingerprint(edgeId);
    if (expectedFingerprint && actualFingerprint !== expectedFingerprint) {
      clearStoredEncKey(edgeId);
      const message = `That key belongs to a different Edge. Expected ${shortFingerprint(expectedFingerprint)}, got ${shortFingerprint(actualFingerprint)}.`;
      setKeyStatus(edgeId, message, "error");
      if (alertOnError) alert(message);
      return null;
    }

    setEncKey(edgeId, key);
    setKeyStatus(
      edgeId,
      expectedFingerprint
        ? `Key saved and verified for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`
        : `Key saved for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`,
      "ok",
    );
    return key;
  } catch {
    clearStoredEncKey(edgeId);
    const message = "Encryption key was not valid base64url text.";
    setKeyStatus(edgeId, message, "error");
    if (alertOnError) alert(message);
    return null;
  }
}

async function rememberEncKey(edgeId) {
  const input = keyInputElement(edgeId);
  const key = input?.value.trim() || "";
  if (!key) {
    const expectedFingerprint = getExpectedKeyFingerprint(edgeId);
    setKeyStatus(
      edgeId,
      expectedFingerprint
        ? `Paste the Edge key first. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : "Paste the Edge key first.",
      "warn",
    );
    return;
  }
  const stored = await storeEncKey(edgeId, key);
  if (stored && input) input.value = "";
}

function clearEncKey(edgeId) {
  clearStoredEncKey(edgeId);
  const input = keyInputElement(edgeId);
  if (input) input.value = "";
  const expectedFingerprint = getExpectedKeyFingerprint(edgeId);
  setKeyStatus(
    edgeId,
    expectedFingerprint
      ? `Cleared. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
      : "Cleared saved key for this browser session.",
    "info",
  );
}

async function refreshKeyPanel(edgeId) {
  const expectedFingerprint = getExpectedKeyFingerprint(edgeId);
  const key = getEncKey(edgeId);

  if (!key) {
    setKeyStatus(
      edgeId,
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
      clearStoredEncKey(edgeId);
      setKeyStatus(
        edgeId,
        `Saved key fingerprint ${shortFingerprint(actualFingerprint)} did not match expected ${shortFingerprint(expectedFingerprint)} and was cleared.`,
        "error",
      );
      return;
    }

    setKeyStatus(
      edgeId,
      expectedFingerprint
        ? `Saved key verified for this browser session. Expected fingerprint ${shortFingerprint(expectedFingerprint)}.`
        : `Saved key present for this browser session. Fingerprint ${shortFingerprint(actualFingerprint)}.`,
      "ok",
    );
  } catch {
    clearStoredEncKey(edgeId);
    setKeyStatus(edgeId, "Saved key was invalid and has been cleared.", "error");
  }
}

async function resolveEncKey(edgeId) {
  const saved = getEncKey(edgeId);
  if (saved) return saved;

  const typed = keyInputElement(edgeId)?.value.trim() || "";
  if (typed) {
    return storeEncKey(edgeId, typed, { alertOnError: true });
  }

  const expectedFingerprint = getExpectedKeyFingerprint(edgeId);
  const promptMessage = expectedFingerprint
    ? `Snapshot is encrypted.\nEnter the encryption key for edge "${edgeId}".\nExpected fingerprint: ${shortFingerprint(expectedFingerprint)}`
    : `Snapshot is encrypted.\nEnter the encryption key for edge "${edgeId}".`;
  const prompted = (prompt(promptMessage) || "").trim();
  if (!prompted) return null;
  return storeEncKey(edgeId, prompted, { alertOnError: true });
}

async function downloadSnapshot(edgeId, jobName, filename, btn) {
  const url = `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`;
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

    const key = await resolveEncKey(edgeId);
    if (!key) return;

    try {
      const decrypted = await decryptBuffer(buffer, key);
      triggerBlobDownload(decrypted, filename);
    } catch {
      clearStoredEncKey(edgeId);
      await refreshKeyPanel(edgeId);
      const expectedFingerprint = getExpectedKeyFingerprint(edgeId);
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

async function deleteSnapshot(edgeId, jobName, filename, btn) {
  if (!confirm(`Delete ${filename}?\nThis cannot be undone.`)) return;

  btn.disabled = true;
  try {
    const res = await fetch(
      `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`,
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

function renderSnapshots(edgeId, jobName, snapshots) {
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
          <span class="snapshot-fp">${escapeHtml(fp)}</span>
        </div>
        <span class="snapshot-size">${escapeHtml(size)}</span>
        <div class="snapshot-actions">
          <button class="btn btn-dl"
            onclick="downloadSnapshot('${escapeHtml(edgeId)}','${escapeHtml(jobName)}','${escapeHtml(name)}',this)">Download</button>
          <button class="btn btn-del"
            onclick="deleteSnapshot('${escapeHtml(edgeId)}','${escapeHtml(jobName)}','${escapeHtml(name)}',this)">Delete</button>
        </div>
      </div>`;
  }).join("");
}

function renderKeyManager(ns) {
  const edgeId = ns.edge_id;
  const expectedFingerprint = ns.encryption_key_fingerprint || "";
  return `
    <div class="edge-key-panel">
      <div class="edge-key-head">
        <strong>Decryption Key</strong>
        <span class="edge-detail" title="${escapeHtml(expectedFingerprint || "unknown")}">
          Expected key fingerprint: ${escapeHtml(shortFingerprint(expectedFingerprint))}
        </span>
      </div>
      <div class="edge-key-controls">
        <input
          type="password"
          placeholder="Paste the Edge encryption key"
          data-edge-key-input="${escapeHtml(edgeId)}">
        <button class="btn btn-key" type="button" onclick="rememberEncKey('${escapeHtml(edgeId)}')">Save Key</button>
        <button class="btn btn-clear" type="button" onclick="clearEncKey('${escapeHtml(edgeId)}')">Clear</button>
      </div>
      <div class="key-status info" data-edge-key-status="${escapeHtml(edgeId)}"></div>
    </div>
  `;
}

async function loadOverview() {
  const res = await fetch("/api/overview");
  const data = await res.json();

  document.getElementById("meta").innerHTML = `
    <div><strong>Status</strong><br><span class="status-${escapeHtml(data.status)}">${escapeHtml(data.status)}</span></div>
    <div><strong>Backup Root</strong><br>${escapeHtml(data.backup_root)}</div>
    <div><strong>Staging Dir</strong><br>${escapeHtml(data.staging_dir)}</div>
    <div><strong>Retention</strong><br>keep last ${escapeHtml(String(data.retention_keep_last))}</div>
  `;

  const namespaces = data.namespaces || [];
  _edgeKeyFingerprints = Object.fromEntries(
    namespaces.map((ns) => [ns.edge_id, ns.encryption_key_fingerprint || ""]),
  );

  document.getElementById("namespaces").innerHTML = namespaces.length
    ? namespaces.map((ns) => `
        <div class="edge-card">
          <div class="edge-header">
            <div class="edge-header-main">
              <span class="edge-id">${escapeHtml(ns.edge_id)}</span>
              <div class="edge-submeta">
                ${ns.edge_instance_id ? `<span class="edge-detail" title="${escapeHtml(ns.edge_instance_id)}">Instance ${escapeHtml(ns.edge_instance_id.slice(0, 12))}</span>` : '<span class="edge-detail edge-detail-warn">Legacy Edge metadata</span>'}
                ${ns.last_seen_source ? `<span class="edge-detail" title="${escapeHtml(ns.last_seen_source)}">Source ${escapeHtml(ns.last_seen_source)}</span>` : '<span class="edge-detail edge-detail-warn">Source unknown</span>'}
                <span class="edge-detail" title="${escapeHtml(ns.encryption_key_fingerprint || "unknown")}">Key FP ${escapeHtml(shortFingerprint(ns.encryption_key_fingerprint))}</span>
              </div>
            </div>
            <span class="edge-count">${(ns.jobs || []).length} job${ns.jobs.length !== 1 ? "s" : ""}</span>
          </div>
          ${renderKeyManager(ns)}
          ${(ns.jobs || []).map((job) => `
            <div class="job-block">
              <div class="job-header">
                <span class="job-name">${escapeHtml(job.job_name)}</span>
                <span class="job-count">${escapeHtml(String(job.snapshot_count))} snapshot${job.snapshot_count !== 1 ? "s" : ""}</span>
              </div>
              <div class="snapshot-list">
                ${renderSnapshots(ns.edge_id, job.job_name, job.snapshots || [])}
              </div>
            </div>
          `).join("") || '<p class="no-snapshots">No jobs stored yet.</p>'}
        </div>
      `).join("")
    : '<p class="hint">No snapshots have been stored yet.</p>';

  await Promise.all(namespaces.map((ns) => refreshKeyPanel(ns.edge_id)));
}

loadOverview();
