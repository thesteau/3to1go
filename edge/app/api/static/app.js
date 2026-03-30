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

function fillMeta(data) {
  const scheduler = data.scheduler || {};
  document.getElementById("meta").innerHTML = `
    <div><strong>Edge ID</strong><br>${escapeHtml(data.edge_id)}</div>
    <div><strong>Scan Root</strong><br>${escapeHtml(data.scan_root)}</div>
    <div><strong>Central URL</strong><br>${escapeHtml(data.central_url)}</div>
    <div><strong>Edge UI</strong><br>${escapeHtml(data.http_url)}</div>
    <div><strong>Cron Schedule</strong><br><code>${escapeHtml(data.cron_schedule)}</code></div>
    <div><strong>Minimum Gap</strong><br>${escapeHtml(`${data.minimum_cycle_gap_minutes} minutes`)}</div>
    <div><strong>Scheduler</strong><br>${escapeHtml(scheduler.state || "idle")}</div>
    <div><strong>Next Run</strong><br>${escapeHtml(scheduler.next_run_at || "waiting for first cycle")}</div>
  `;
}

function renderDirectories(data) {
  const rows = data.directories.map((entry) => {
    const state = entry.state?.last_status || "none";
    return `
      <tr>
        <td><code>${escapeHtml(entry.relative_path)}</code><br><span class="hint">${escapeHtml(entry.absolute_path)}</span></td>
        <td>${statusBadge(entry)}</td>
        <td>${escapeHtml(state)}</td>
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
  const response = await fetch("/api/directories");
  latestData = await response.json();
  fillMeta(latestData);
  renderDirectories(latestData);
  if (!document.getElementById("relative_path").value) {
    resetForm();
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
  document.getElementById("run-status").textContent = body.status === "queued" || body.status === "started"
    ? "Backup cycle requested."
    : "A cycle is already running.";
}

resetForm();
loadData();
