const ENC_MAGIC = new Uint8Array([82, 67, 69, 78, 67, 49, 0, 0]); // "RCENC1\x00\x00"
const ENC_MAGIC_LEN = 8;
const ENC_IV_LEN = 12;

function isEncrypted(buffer) {
  if (buffer.byteLength < ENC_MAGIC_LEN + ENC_IV_LEN) return false;
  const view = new Uint8Array(buffer, 0, ENC_MAGIC_LEN);
  return ENC_MAGIC.every((b, i) => b === view[i]);
}

function base64UrlToBytes(b64) {
  const std = b64.replace(/-/g, "+").replace(/_/g, "/");
  return Uint8Array.from(atob(std), (c) => c.charCodeAt(0));
}

async function decryptBuffer(buffer, keyB64) {
  const keyBytes = base64UrlToBytes(keyB64.trim());
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

const _encKeys = {};

function getEncKey(edgeId) {
  if (_encKeys[edgeId]) return _encKeys[edgeId];
  const stored = sessionStorage.getItem(`relay_enc_${edgeId}`);
  if (stored) { _encKeys[edgeId] = stored; return stored; }
  return null;
}

function setEncKey(edgeId, key) {
  _encKeys[edgeId] = key;
  sessionStorage.setItem(`relay_enc_${edgeId}`, key);
}

async function downloadSnapshot(edgeId, jobName, filename, btn) {
  const url = `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`;
  btn.disabled = true;
  try {
    const res = await fetch(url);
    if (!res.ok) { alert("Download failed."); return; }
    const buffer = await res.arrayBuffer();

    if (!isEncrypted(buffer)) {
      triggerBlobDownload(buffer, filename);
      return;
    }

    let key = getEncKey(edgeId);
    if (!key) {
      key = prompt(`Snapshot is encrypted.\nEnter the encryption key for edge "${escapeHtml(edgeId)}":`);
      if (!key) return;
    }

    try {
      const decrypted = await decryptBuffer(buffer, key);
      setEncKey(edgeId, key);
      triggerBlobDownload(decrypted, filename);
    } catch {
      delete _encKeys[edgeId];
      sessionStorage.removeItem(`relay_enc_${edgeId}`);
      alert("Decryption failed — wrong key or corrupted archive.");
    }
  } finally {
    btn.disabled = false;
  }
}

function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
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
  const token = getToken();
  if (!token) return;
  if (!confirm(`Delete ${filename}?\nThis cannot be undone.`)) return;

  btn.disabled = true;
  try {
    const res = await fetch(
      `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`,
      { method: "DELETE", headers: { Authorization: `Bearer ${token}` } },
    );
    if (res.status === 401) {
      _token = "";
      sessionStorage.removeItem("relay_token");
      alert("Invalid token — cleared. Try again.");
      return;
    }
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
    const dlUrl = `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(name)}`;
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
  document.getElementById("namespaces").innerHTML = namespaces.length
    ? namespaces.map((ns) => `
        <div class="edge-card">
          <div class="edge-header">
            <span class="edge-id">${escapeHtml(ns.edge_id)}</span>
            <span class="edge-count">${(ns.jobs || []).length} job${ns.jobs.length !== 1 ? "s" : ""}</span>
          </div>
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
}

loadOverview();
