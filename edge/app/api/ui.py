def render_edge_ui() -> str:
    return """<!doctype html>
<html lang='en'>
<head>
  <meta charset='utf-8'>
  <title>RelayCentralizer Edge</title>
  <style>
    :root { color-scheme: light; --bg:#f5f1e8; --panel:#fffdf8; --ink:#1f2933; --muted:#6b7280; --accent:#0f766e; --accent-soft:#d9f3ee; --danger:#b42318; --border:#d7d0c2; }
    body { margin:0; font-family: Georgia, 'Times New Roman', serif; background:linear-gradient(135deg,#efe7d7 0%,#f8f5ee 55%,#e2efe8 100%); color:var(--ink); }
    main { max-width:1200px; margin:0 auto; padding:24px; }
    .hero, .panel { background:var(--panel); border:1px solid var(--border); border-radius:18px; box-shadow:0 12px 30px rgba(31,41,51,.08); }
    .hero { padding:24px; margin-bottom:20px; }
    .hero h1 { margin:0 0 8px; font-size:2.2rem; }
    .meta { display:grid; grid-template-columns:repeat(auto-fit,minmax(220px,1fr)); gap:12px; margin-top:16px; }
    .meta div { background:#f8f4ea; border-radius:12px; padding:12px; }
    .layout { display:grid; grid-template-columns:1.3fr .9fr; gap:20px; }
    .panel { padding:18px; }
    table { width:100%; border-collapse:collapse; }
    th, td { padding:10px 8px; border-bottom:1px solid #ebe4d7; text-align:left; vertical-align:top; }
    th { font-size:.85rem; color:var(--muted); text-transform:uppercase; letter-spacing:.04em; }
    .badge { display:inline-block; padding:3px 9px; border-radius:999px; font-size:.8rem; background:var(--accent-soft); color:var(--accent); }
    .badge.warn { background:#fff1d6; color:#9a6700; }
    .badge.error { background:#fde7e4; color:var(--danger); }
    button { border:0; border-radius:10px; background:var(--accent); color:white; padding:10px 14px; cursor:pointer; font:inherit; }
    button.secondary { background:#d8e9e6; color:#10403b; }
    button.danger { background:var(--danger); }
    input, textarea, select { width:100%; box-sizing:border-box; border:1px solid var(--border); border-radius:10px; padding:10px 12px; font:inherit; background:#fff; }
    label { display:block; font-size:.92rem; margin:10px 0 6px; }
    textarea { min-height:90px; resize:vertical; }
    .row { display:grid; grid-template-columns:1fr 1fr; gap:12px; }
    .toolbar { display:flex; gap:10px; flex-wrap:wrap; margin:14px 0 0; }
    .small { color:var(--muted); font-size:.92rem; }
    .jobs { display:grid; gap:12px; margin-bottom:18px; }
    .job-card { border:1px solid #ebe4d7; border-radius:12px; padding:12px; background:#fffaf2; }
    @media (max-width: 900px) { .layout { grid-template-columns:1fr; } }
  </style>
</head>
<body>
<main>
  <section class='hero'>
    <h1>RelayCentralizer Edge</h1>
    <p class='small'>Browse the scan root, see which directories already have <code>.upload_dir</code>, and create, update, or delete job markers directly from the UI.</p>
    <div class='meta' id='meta'></div>
    <div class='toolbar'>
      <button onclick='loadData()'>Refresh</button>
      <button class='secondary' onclick='runNow()'>Run Backup Cycle Now</button>
      <span class='small' id='run-status'></span>
    </div>
  </section>

  <div class='layout'>
    <section class='panel'>
      <h2>Selected Jobs</h2>
      <div id='selected-jobs' class='jobs'></div>
      <h2>Directories Under Scan Root</h2>
      <table>
        <thead>
          <tr><th>Path</th><th>Status</th><th>Last State</th><th>Action</th></tr>
        </thead>
        <tbody id='directory-rows'></tbody>
      </table>
    </section>

    <section class='panel'>
      <h2>Job Editor</h2>
      <p class='small'>Saving this form writes or updates <code>.upload_dir</code> inside the selected directory.</p>
      <label>Directory</label>
      <input id='relative_path' readonly>
      <label>Job Name</label>
      <input id='job_name' placeholder='Defaults to the folder name'>
      <label>Exclude Patterns</label>
      <textarea id='exclude' placeholder='One pattern per line'></textarea>
      <div class='row'>
        <label><input type='checkbox' id='include_hidden' checked> Include hidden files</label>
        <label><input type='checkbox' id='follow_symlinks'> Follow symlinks</label>
      </div>
      <h3>Docker Compose Controls</h3>
      <label>Project Dir</label>
      <input id='dc_project_dir' placeholder='/srv/stacks/app'>
      <div class='row'>
        <div>
          <label>Compose File</label>
          <input id='dc_compose_file' placeholder='compose.yml'>
        </div>
        <div>
          <label>Env File</label>
          <input id='dc_env_file' placeholder='.env'>
        </div>
      </div>
      <div class='row'>
        <div>
          <label>Project Name</label>
          <input id='dc_project_name' placeholder='Optional'>
        </div>
        <div>
          <label>Services</label>
          <input id='dc_services' placeholder='comma,separated,services'>
        </div>
      </div>
      <div class='row'>
        <div>
          <label>Shutdown Action</label>
          <select id='dc_shutdown_action'>
            <option value='stop'>stop</option>
            <option value='down'>down</option>
          </select>
        </div>
        <div>
          <label>Startup Action</label>
          <select id='dc_startup_action'>
            <option value=''>default</option>
            <option value='start'>start</option>
            <option value='up'>up</option>
            <option value='none'>none</option>
          </select>
        </div>
      </div>
      <label>Compose Timeout Seconds</label>
      <input id='dc_timeout' type='number' min='1' value='300'>
      <div class='toolbar'>
        <button onclick='saveJob()'>Save Job</button>
        <button class='secondary' onclick='resetForm()'>New Selection</button>
        <button class='danger' onclick='deleteJob()'>Delete .upload_dir</button>
      </div>
      <p class='small' id='form-status'></p>
    </section>
  </div>
</main>
<script>
let latestData = null;

function escapeHtml(value) {
  return String(value ?? '').replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;').replaceAll("'", '&#39;');
}

function encodedPath(value) {
  return encodeURIComponent(value ?? '.');
}

function statusBadge(entry) {
  if (entry.config_error) return `<span class="badge error">invalid config</span>`;
  if (entry.selected) return `<span class="badge">selected</span>`;
  if (entry.blocked_by_parent) return `<span class="badge warn">nested under ${escapeHtml(entry.blocked_by_parent)}</span>`;
  return `<span class="badge warn">available</span>`;
}

function fillMeta(data) {
  document.getElementById('meta').innerHTML = `
    <div><strong>Edge ID</strong><br>${escapeHtml(data.edge_id)}</div>
    <div><strong>Scan Root</strong><br>${escapeHtml(data.scan_root)}</div>
    <div><strong>Central URL</strong><br>${escapeHtml(data.central_url)}</div>
    <div><strong>Edge UI</strong><br>${escapeHtml(data.http_url)}</div>
  `;
}

function renderDirectories(data) {
  const rows = data.directories.map((entry) => {
    const state = entry.state?.last_status || 'none';
    return `
      <tr>
        <td><code>${escapeHtml(entry.relative_path)}</code><br><span class="small">${escapeHtml(entry.absolute_path)}</span></td>
        <td>${statusBadge(entry)}</td>
        <td>${escapeHtml(state)}</td>
        <td><button class="secondary" onclick="editPath(decodeURIComponent('${encodedPath(entry.relative_path)}'))">Edit</button></td>
      </tr>
    `;
  }).join('');
  document.getElementById('directory-rows').innerHTML = rows;

  const selected = data.directories.filter((entry) => entry.selected);
  document.getElementById('selected-jobs').innerHTML = selected.length ? selected.map((entry) => `
    <div class="job-card">
      <strong>${escapeHtml(entry.config?.job_name || entry.relative_path)}</strong>
      <div class="small"><code>${escapeHtml(entry.relative_path)}</code></div>
      <div class="small">Last state: ${escapeHtml(entry.state?.last_status || 'none')}</div>
      ${entry.blocked_by_parent ? `<div class="small">Nested under existing job <code>${escapeHtml(entry.blocked_by_parent)}</code></div>` : ''}
      ${entry.config_error ? `<div class="small" style="color:#b42318;">${escapeHtml(entry.config_error)}</div>` : ''}
      <div class="toolbar">
        <button class="secondary" onclick="editPath(decodeURIComponent('${encodedPath(entry.relative_path)}'))">Edit</button>
        <button class="danger" onclick="deleteByPath(decodeURIComponent('${encodedPath(entry.relative_path)}'))">Delete</button>
      </div>
    </div>
  `).join('') : '<p class="small">No directories are selected yet.</p>';
}

function findEntry(relativePath) {
  return latestData?.directories.find((entry) => entry.relative_path === relativePath);
}

function editPath(relativePath) {
  const entry = findEntry(relativePath);
  document.getElementById('relative_path').value = relativePath;
  document.getElementById('job_name').value = entry?.config?.job_name || (relativePath === '.' ? '' : relativePath.split('/').pop());
  document.getElementById('exclude').value = (entry?.config?.exclude || []).join('\n');
  document.getElementById('include_hidden').checked = entry?.config?.include_hidden ?? true;
  document.getElementById('follow_symlinks').checked = entry?.config?.follow_symlinks ?? false;
  document.getElementById('dc_project_dir').value = entry?.config?.docker_compose?.project_dir || '';
  document.getElementById('dc_compose_file').value = entry?.config?.docker_compose?.compose_file || '';
  document.getElementById('dc_env_file').value = entry?.config?.docker_compose?.env_file || '';
  document.getElementById('dc_project_name').value = entry?.config?.docker_compose?.project_name || '';
  document.getElementById('dc_services').value = (entry?.config?.docker_compose?.services || []).join(',');
  document.getElementById('dc_shutdown_action').value = entry?.config?.docker_compose?.shutdown_action || 'stop';
  document.getElementById('dc_startup_action').value = entry?.config?.docker_compose?.startup_action || '';
  document.getElementById('dc_timeout').value = entry?.config?.docker_compose?.command_timeout_seconds || 300;
  document.getElementById('form-status').textContent = entry?.selected ? 'Editing existing .upload_dir' : 'Creating a new .upload_dir';
}

function resetForm() {
  document.getElementById('relative_path').value = '.';
  document.getElementById('job_name').value = '';
  document.getElementById('exclude').value = '';
  document.getElementById('include_hidden').checked = true;
  document.getElementById('follow_symlinks').checked = false;
  document.getElementById('dc_project_dir').value = '';
  document.getElementById('dc_compose_file').value = '';
  document.getElementById('dc_env_file').value = '';
  document.getElementById('dc_project_name').value = '';
  document.getElementById('dc_services').value = '';
  document.getElementById('dc_shutdown_action').value = 'stop';
  document.getElementById('dc_startup_action').value = '';
  document.getElementById('dc_timeout').value = 300;
  document.getElementById('form-status').textContent = 'Pick a directory from the list to edit it.';
}

async function loadData() {
  const response = await fetch('/api/directories');
  latestData = await response.json();
  fillMeta(latestData);
  renderDirectories(latestData);
  if (!document.getElementById('relative_path').value) resetForm();
}

async function saveJob() {
  const relativePath = document.getElementById('relative_path').value || '.';
  const dockerProjectDir = document.getElementById('dc_project_dir').value.trim();
  const payload = {
    relative_path: relativePath,
    job_name: document.getElementById('job_name').value.trim() || null,
    exclude: document.getElementById('exclude').value.split('\n').map((v) => v.trim()).filter(Boolean),
    include_hidden: document.getElementById('include_hidden').checked,
    follow_symlinks: document.getElementById('follow_symlinks').checked,
  };
  if (dockerProjectDir) {
    payload.docker_compose = {
      project_dir: dockerProjectDir,
      compose_file: document.getElementById('dc_compose_file').value.trim() || null,
      env_file: document.getElementById('dc_env_file').value.trim() || null,
      project_name: document.getElementById('dc_project_name').value.trim() || null,
      services: document.getElementById('dc_services').value.split(',').map((v) => v.trim()).filter(Boolean),
      shutdown_action: document.getElementById('dc_shutdown_action').value,
      startup_action: document.getElementById('dc_startup_action').value || null,
      command_timeout_seconds: Number(document.getElementById('dc_timeout').value || 300),
    };
  }
  const response = await fetch('/api/jobs', {
    method: 'POST',
    headers: {'Content-Type': 'application/json'},
    body: JSON.stringify(payload),
  });
  const body = await response.json();
  document.getElementById('form-status').textContent = response.ok ? 'Saved successfully.' : (body.detail || 'Save failed.');
  if (response.ok) await loadData();
}

async function deleteByPath(relativePath) {
  if (!confirm(`Delete .upload_dir from ${relativePath}?`)) return;
  const response = await fetch(`/api/jobs?relative_path=${encodeURIComponent(relativePath)}`, {method: 'DELETE'});
  const body = await response.json();
  document.getElementById('form-status').textContent = response.ok ? 'Deleted successfully.' : (body.detail || 'Delete failed.');
  if (response.ok) await loadData();
}

async function deleteJob() {
  const relativePath = document.getElementById('relative_path').value || '.';
  await deleteByPath(relativePath);
}

async function runNow() {
  const response = await fetch('/api/run-now', {method: 'POST'});
  const body = await response.json();
  document.getElementById('run-status').textContent = body.status === 'started' ? 'Backup cycle started.' : 'A cycle is already running.';
}

resetForm();
loadData();
</script>
</body>
</html>"""
