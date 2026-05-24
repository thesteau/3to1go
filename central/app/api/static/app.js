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
          <a class="btn btn-dl" href="${escapeHtml(dlUrl)}" download="${escapeHtml(name)}">Download</a>
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
