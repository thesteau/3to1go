def render_central_ui() -> str:
    return """<!doctype html>
<html lang='en'>
<head>
  <meta charset='utf-8'>
  <title>RelayCentralizer Central</title>
  <style>
    :root { --bg:#f3efe7; --panel:#fffdf9; --ink:#24303b; --muted:#667085; --accent:#8a3b12; --border:#e3d8ca; }
    body { margin:0; font-family: Georgia, 'Times New Roman', serif; background:linear-gradient(160deg,#ede4d7 0%,#f9f7f2 58%,#edf3f0 100%); color:var(--ink); }
    main { max-width:1100px; margin:0 auto; padding:24px; }
    .hero, .panel { background:var(--panel); border:1px solid var(--border); border-radius:18px; box-shadow:0 12px 30px rgba(36,48,59,.08); }
    .hero { padding:24px; margin-bottom:20px; }
    .grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(220px,1fr)); gap:12px; }
    .grid div, .job { background:#faf4ec; border-radius:12px; padding:12px; }
    .job-list { display:grid; gap:12px; }
    .panel { padding:18px; }
    .small { color:var(--muted); font-size:.92rem; }
    h1 { margin:0 0 8px; font-size:2.2rem; }
    button { border:0; border-radius:10px; background:var(--accent); color:white; padding:10px 14px; cursor:pointer; font:inherit; }
    code { background:#f3ece3; padding:2px 6px; border-radius:6px; }
  </style>
</head>
<body>
<main>
  <section class='hero'>
    <h1>RelayCentralizer Central</h1>
    <p class='small'>This view shows the active storage pathing and the snapshots currently stored per Edge and job namespace.</p>
    <div class='grid' id='meta'></div>
    <p><button onclick='loadOverview()'>Refresh</button></p>
  </section>
  <section class='panel'>
    <h2>Stored Snapshots</h2>
    <div id='namespaces' class='job-list'></div>
  </section>
</main>
<script>
function escapeHtml(value) {
  return String(value ?? '').replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&#39;');
}

async function loadOverview() {
  const response = await fetch('/api/overview');
  const data = await response.json();
  document.getElementById('meta').innerHTML = `
    <div><strong>Status</strong><br>${escapeHtml(data.status)}</div>
    <div><strong>Backup Root</strong><br>${escapeHtml(data.backup_root)}</div>
    <div><strong>Staging Dir</strong><br>${escapeHtml(data.staging_dir)}</div>
    <div><strong>Retention</strong><br>keep last ${escapeHtml(data.retention_keep_last)}</div>
    <div><strong>Central UI</strong><br>${escapeHtml(data.http_url)}</div>
  `;

  const namespaces = data.namespaces || [];
  document.getElementById('namespaces').innerHTML = namespaces.length ? namespaces.map((ns) => `
    <div class="job">
      <strong>${escapeHtml(ns.edge_id)}</strong>
      ${(ns.jobs || []).length ? ns.jobs.map((job) => `
        <div style="margin-top:10px; padding-top:10px; border-top:1px solid #eadfce;">
          <div><strong>${escapeHtml(job.job_name)}</strong> <span class="small">(${escapeHtml(job.snapshot_count)} snapshots)</span></div>
          <div class="small">${job.snapshots.map((name) => `<code>${escapeHtml(name)}</code>`).join(' ') || 'No snapshots yet.'}</div>
        </div>
      `).join('') : '<div class="small" style="margin-top:10px;">No jobs stored yet.</div>'}
    </div>
  `).join('') : '<p class="small">No snapshots have been stored yet.</p>';
}

loadOverview();
</script>
</body>
</html>"""
