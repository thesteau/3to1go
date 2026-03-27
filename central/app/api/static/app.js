function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

async function loadOverview() {
  const response = await fetch("/api/overview");
  const data = await response.json();

  document.getElementById("meta").innerHTML = `
    <div><strong>Status</strong><br>${escapeHtml(data.status)}</div>
    <div><strong>Backup Root</strong><br>${escapeHtml(data.backup_root)}</div>
    <div><strong>Staging Dir</strong><br>${escapeHtml(data.staging_dir)}</div>
    <div><strong>Retention</strong><br>keep last ${escapeHtml(data.retention_keep_last)}</div>
    <div><strong>Central UI</strong><br>${escapeHtml(data.http_url)}</div>
  `;

  const namespaces = data.namespaces || [];
  document.getElementById("namespaces").innerHTML = namespaces.length
    ? namespaces.map((namespaceEntry) => `
      <div class="job-card">
        <strong>${escapeHtml(namespaceEntry.edge_id)}</strong>
        ${(namespaceEntry.jobs || []).length
          ? namespaceEntry.jobs.map((job) => `
            <div class="job-detail" style="margin-top:10px;padding-top:10px;border-top:1px solid #eadfce;">
              <div><strong>${escapeHtml(job.job_name)}</strong> (${escapeHtml(job.snapshot_count)} snapshots)</div>
              <div>${job.snapshots.map((name) => `<code>${escapeHtml(name)}</code>`).join(" ") || "No snapshots yet."}</div>
            </div>
          `).join("")
          : '<div class="job-detail" style="margin-top:10px;">No jobs stored yet.</div>'}
      </div>
    `).join("")
    : '<p class="hint">No snapshots have been stored yet.</p>';
}

loadOverview();
