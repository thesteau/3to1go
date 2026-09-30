let _recoverContext = null;

function resetRecoverPreview() {
  if (_recoverContext) {
    _recoverContext.preview = null;
    _recoverContext.previewFingerprint = null;
  }
  const preview = document.getElementById("recover-preview");
  if (preview) {
    preview.hidden = true;
    preview.innerHTML = "";
  }
  const btn = document.getElementById("recover-confirm-btn");
  if (btn) {
    btn.textContent = "Preview Restore";
    btn.className = "";
  }
}

const RECOVER_PREVIEW_ROW_LIMIT = 400;
const RECOVER_PREVIEW_GROUP_LIMIT = 150;
const RECOVER_PREVIEW_OPEN_GROUPS = 8;
const RECOVER_PREVIEW_SEARCH_DELAY_MS = 150;

function renderRecoverPreview(body) {
  const preview = document.getElementById("recover-preview");
  if (!preview) return;
  const entries = body.entries || [];
  const replaceCount = Number(body.replace_count || 0);
  const addCount = Number(body.add_count || 0);
  const totalSize = entries.reduce((sum, entry) => sum + Number(entry.size || 0), 0);
  const snapshotName = body.snapshot_filename || "snapshot";
  const filterButton = (action, label, count) => `
    <button type="button" class="secondary" data-recover-filter="${action}" aria-pressed="${action === "all"}">
      ${escapeHtml(label)} <span class="recover-preview-count">${count}</span>
    </button>`;

  preview.innerHTML = `
    <div class="recover-preview-summary">
      <div class="recover-preview-title">
        <strong title="${escapeHtml(snapshotName)}">${escapeHtml(snapshotName)}</strong>
        <span class="hint">${entries.length} file${entries.length === 1 ? "" : "s"} · ${escapeHtml(formatBytes(totalSize))}</span>
      </div>
    </div>
    <p class="hint">Replace overwrites the local copy and Add creates a missing file. Local files not listed stay untouched.</p>
    <div class="recover-preview-tools">
      <input id="recover-preview-search" type="search" placeholder="Filter by file or folder" aria-label="Filter restore preview">
      <div class="recover-preview-filters" role="group" aria-label="Show files">
        ${filterButton("all", "All", entries.length)}
        ${filterButton("replace", "Replace", replaceCount)}
        ${filterButton("add", "Add", addCount)}
      </div>
    </div>
    <div id="recover-preview-list" class="recover-preview-list"></div>
  `;
  const filter = { query: "", action: "all" };
  const search = document.getElementById("recover-preview-search");
  let searchTimer = null;
  search.addEventListener("input", () => {
    window.clearTimeout(searchTimer);
    searchTimer = window.setTimeout(() => {
      filter.query = search.value.trim().toLowerCase();
      renderRecoverPreviewList(entries, filter);
    }, RECOVER_PREVIEW_SEARCH_DELAY_MS);
  });
  preview.querySelectorAll("[data-recover-filter]").forEach((button) => {
    button.addEventListener("click", () => {
      filter.action = button.dataset.recoverFilter;
      preview.querySelectorAll("[data-recover-filter]").forEach((other) => other.setAttribute("aria-pressed", String(other === button)));
      renderRecoverPreviewList(entries, filter);
    });
  });
  renderRecoverPreviewList(entries, filter);
  preview.hidden = false;
}

// Files are grouped by folder so a long restore reads as a few locations, not one flat wall of paths.
function renderRecoverPreviewList(entries, { query, action }) {
  const list = document.getElementById("recover-preview-list");
  if (!list) return;
  const matches = entries.filter((entry) => (action === "all" || (entry.action || "add") === action)
    && (!query || String(entry.path || "").toLowerCase().includes(query)));
  // Group every match so folder totals stay complete; rendered rows and folders are both capped.
  const groups = new Map();
  for (const entry of matches) {
    const path = String(entry.path || "");
    const slash = path.lastIndexOf("/");
    const folder = slash === -1 ? "" : path.slice(0, slash + 1);
    if (!groups.has(folder)) groups.set(folder, []);
    groups.get(folder).push({ ...entry, name: path.slice(slash + 1) });
  }
  const openGroups = Boolean(query) || groups.size <= RECOVER_PREVIEW_OPEN_GROUPS;
  const sortedGroups = Array.from(groups).sort(([a], [b]) => (a === "" ? -1 : b === "" ? 1 : a.localeCompare(b)));
  const shownGroups = sortedGroups.slice(0, RECOVER_PREVIEW_GROUP_LIMIT);
  const hiddenGroups = sortedGroups.slice(RECOVER_PREVIEW_GROUP_LIMIT);
  const hiddenFiles = hiddenGroups.flatMap(([, files]) => files);
  const hiddenSize = hiddenFiles.reduce((sum, file) => sum + Number(file.size || 0), 0);
  let rowBudget = RECOVER_PREVIEW_ROW_LIMIT;

  list.innerHTML = matches.length
    ? shownGroups.map(([folder, files]) => {
      const folderSize = files.reduce((sum, file) => sum + Number(file.size || 0), 0);
      const shownFiles = files.slice(0, Math.max(0, rowBudget));
      rowBudget -= shownFiles.length;
      const hidden = files.length - shownFiles.length;
      return `
        <details class="recover-preview-group"${openGroups && shownFiles.length ? " open" : ""}>
          <summary>
            <span class="recover-preview-folder" title="${escapeHtml(folder || "Job root")}">${escapeHtml(folder || "Job root")}</span>
            <span class="hint">${files.length} file${files.length === 1 ? "" : "s"} · ${escapeHtml(formatBytes(folderSize))}</span>
          </summary>
          ${shownFiles.map((file) => {
            const kind = file.action === "replace" ? "replace" : "add";
            return `
            <div class="recover-preview-row">
              <span class="recover-preview-action ${kind}">${kind === "replace" ? "Replace" : "Add"}</span>
              <span class="recover-preview-path" title="${escapeHtml(file.path || "")}">${escapeHtml(file.name)}</span>
              <span class="recover-preview-size">${escapeHtml(formatBytes(file.size || 0))}</span>
            </div>`;
          }).join("")}
          ${hidden ? `<p class="recover-preview-more hint">${hidden} file${hidden === 1 ? "" : "s"} in this folder not listed. Filter to narrow the list.</p>` : ""}
        </details>`;
    }).join("") + (hiddenGroups.length
      ? `<p class="recover-preview-more hint">${hiddenGroups.length} more folder${hiddenGroups.length === 1 ? "" : "s"} with ${hiddenFiles.length} file${hiddenFiles.length === 1 ? "" : "s"} · ${escapeHtml(formatBytes(hiddenSize))} not listed. Filter to narrow the list.</p>`
      : "")
    : '<p class="recover-preview-more hint">No files match this filter.</p>';
}

function openRecoverDialog(relativePath, jobName) {
  _recoverContext = { relativePath, jobName, preview: null, previewFingerprint: null };
  document.getElementById("recover-dialog-job-name").textContent = jobName || relativePath;
  document.getElementById("recover-fingerprint").value = "";
  resetRecoverPreview();
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
  const previewFingerprint = _recoverContext.preview?.snapshot_fingerprint || fingerprint;

  if (btn) btn.disabled = true;
  try {
    if (!_recoverContext.preview || _recoverContext.previewFingerprint !== fingerprint) {
      setStatus("recover-status", "Loading restore preview...", "info");
      const response = await fetch("/api/recovery/preview", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ relative_path: relativePath, fingerprint }),
      });
      const body = await response.json();
      if (!response.ok) {
        setStatus("recover-status", body.detail || `Preview failed for ${label}.`, "error");
        setActionStatus(body.detail || `Preview failed for ${label}.`, "error");
        return;
      }
      if (body.status === "already_running") {
        setStatus("recover-status", "A backup or recovery operation is already running.", "error");
        setActionStatus("A backup or recovery operation is already running on this Edge.", "error");
        return;
      }
      _recoverContext.preview = body;
      _recoverContext.previewFingerprint = fingerprint;
      renderRecoverPreview(body);
      if (btn) {
        btn.textContent = "Restore These Files";
        btn.className = "danger";
      }
      setStatus("recover-status", "Preview loaded. Click Restore These Files to continue.", "success");
      return;
    }

    setStatus("recover-status", "Restoring...", "info");
    const response = await fetch("/api/recovery/restore", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ relative_path: relativePath, fingerprint: previewFingerprint }),
    });
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
