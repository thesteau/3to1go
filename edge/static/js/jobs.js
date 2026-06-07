async function saveJob() {
  setStatus("form-status", "Saving job settings...", "info");
  const relativePath = document.getElementById("relative_path").value || ".";
  const payload = {
    relative_path: relativePath,
    config: {
      job_name: document.getElementById("job_name").value.trim() || null,
      exclude: document.getElementById("exclude").value.split("\n").map((value) => value.trim()).filter(Boolean),
      include_hidden: document.getElementById("include_hidden").checked,
      follow_symlinks: document.getElementById("follow_symlinks").checked,
    },
  };

  const response = await fetch("/api/directories/save-job", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body = await response.json();
  setStatus("form-status", response.ok ? "Saved. Closing..." : (body.detail || "Save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadData({ silent: true, refreshDirectoryTree: true });
    setActionStatus(`Saved .upload_dir for ${relativePath}.`, "success");
    await pause(450);
    closeDialog("job-dialog");
  } else {
    setActionStatus(body.detail || "Save failed.", "error");
  }
}

async function deleteByPath(relativePath) {
  if (!await confirmApp({
    title: "Stop Backing Up Folder",
    message: `Stop backing up ${relativePath}? This only removes the .upload_dir settings file for this folder. It does not delete the folder itself or remove backups already stored in Central.`,
    confirmLabel: "Stop Backup",
    danger: true,
  })) {
    return;
  }
  setStatus("form-status", "Removing this folder's upload settings...", "info");
  const response = await fetch("/api/directories/delete-job", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ relative_path: relativePath }),
  });
  const body = await response.json();
  setStatus("form-status", response.ok ? "This folder is no longer treated as its own backup job." : (body.detail || "Delete failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadData({ silent: true, refreshDirectoryTree: true });
    setActionStatus(`Edge will no longer back up ${relativePath} as its own job.`, "success");
    await pause(450);
    closeDialog("job-dialog");
  } else {
    if (response.status === 404 || body.detail === "directory not found") {
      await loadData({ silent: true, refreshDirectoryTree: true });
      closeDialog("job-dialog");
      setActionStatus(`That folder was already gone, so Edge refreshed the directory list.`, "info");
      return;
    }
    setActionStatus(body.detail || "Delete failed.", "error");
  }
}

async function forceUpload(jobName, btn) {
  const label = jobName || "this job";
  if (!await confirmApp({
    title: "Force Upload",
    message: `Force an upload for ${label}? This bypasses the unchanged check. Central may still reject it as a duplicate if that snapshot already exists.`,
    confirmLabel: "Force Upload",
  })) {
    return;
  }

  btn.disabled = true;
  try {
    requestEdgeActiveRefreshBurst();
    const responsePromise = fetch("/api/directories/force-send", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ job_name: jobName }),
    });
    window.setTimeout(() => loadData({ silent: true, includeKey: false }), 250);
    const response = await responsePromise;
    const body = await response.json();
    if (!response.ok) {
      setActionStatus(body.detail || `Force upload failed for ${label}.`, "error");
      return;
    }
    if (body.status === "already_running") {
      setActionStatus("A backup or recovery operation is already running on this Edge.", "error");
      return;
    }

    setActionStatus(
      body.manual_retry_cleared
        ? `Forced upload for ${label}. A manual block was cleared first.`
        : `Forced upload for ${label}. Central may still reject it as a duplicate.`,
      "success",
    );
    await loadData({ silent: true });
  } catch (error) {
    setActionStatus(error.message || `Force upload failed for ${label}.`, "error");
  } finally {
    btn.disabled = false;
  }
}

function forceUploadFromEvent(event, jobName, btn) {
  stopActionEvent(event);
  forceUpload(jobName, btn);
  return false;
}

async function deleteJob() {
  const relativePath = document.getElementById("relative_path").value || ".";
  await deleteByPath(relativePath);
}

async function runNow() {
  try {
    requestEdgeActiveRefreshBurst();
    const responsePromise = fetch("/api/run-now", { method: "POST" });
    window.setTimeout(() => loadData({ silent: true, includeKey: false }), 250);
    const response = await responsePromise;
    const body = await response.json();
    if (body.status === "queued" || body.status === "started") {
      const cleared = body.manual_retries_cleared || 0;
      setActionStatus(
        cleared > 0
          ? `Backup cycle requested. ${cleared} manual block(s) cleared for retry.`
          : "Backup cycle requested.",
        "success",
      );
      await loadData({ silent: true });
      return;
    }
    setActionStatus("A backup cycle is already running.", "info");
  } catch (error) {
    setActionStatus(error.message || "Failed to start a backup cycle.", "error");
  }
}
