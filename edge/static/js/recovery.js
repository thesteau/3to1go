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

function renderRecoverPreview(body) {
  const preview = document.getElementById("recover-preview");
  if (!preview) return;
  const entries = body.entries || [];
  const visibleEntries = entries.slice(0, 80);
  const remaining = Math.max(0, entries.length - visibleEntries.length);
  const replaceCount = Number(body.replace_count || 0);
  const addCount = Number(body.add_count || 0);
  const snapshotName = body.snapshot_filename || "snapshot";

  preview.innerHTML = `
    <div class="recover-preview-summary">
      <strong>${escapeHtml(snapshotName)}</strong>
      <span>${entries.length} file${entries.length === 1 ? "" : "s"} total</span>
      <span>${replaceCount} replace</span>
      <span>${addCount} add</span>
    </div>
    <p class="hint">Restore will replace only the listed local files marked replace. Local files not listed here stay untouched.</p>
    <div class="recover-preview-list">
      ${visibleEntries.map((entry) => `
        <div class="recover-preview-row">
          <span class="recover-preview-action ${entry.action === "replace" ? "replace" : "add"}">${escapeHtml(entry.action || "add")}</span>
          <span class="recover-preview-path">${escapeHtml(entry.path || "")}</span>
          <span class="hint">${escapeHtml(formatBytes(entry.size || 0))}</span>
        </div>
      `).join("")}
      ${remaining ? `<div class="recover-preview-more hint">${remaining} more file${remaining === 1 ? "" : "s"}</div>` : ""}
    </div>
  `;
  preview.hidden = false;
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
