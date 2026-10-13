interface RestoreProgress {
  operation_id?: string;
  status: "idle" | "running" | "completed" | "failed";
  phase?: string;
  percent: number | null;
  bytes_done?: number;
  bytes_total?: number;
  restored_files?: number;
}

function renderRestoreProgress(id: string, progress: RestoreProgress): void {
  const container = document.getElementById(id);
  if (!container) return;
  const label =
    progress.status === "completed"
      ? "Restore complete"
      : progress.status === "failed"
        ? "Restore failed"
        : progress.phase === "downloading"
          ? "Downloading snapshot"
          : progress.phase === "decrypting"
            ? "Decrypting snapshot"
            : progress.phase === "extracting"
              ? "Extracting files"
              : "Starting restore";
  const percent =
    progress.percent !== null && Number.isFinite(progress.percent)
      ? Math.min(100, Math.max(0, Math.floor(progress.percent)))
      : null;
  const detail =
    progress.phase === "extracting" || progress.status === "completed"
      ? `${progress.restored_files || 0} files restored`
      : progress.bytes_total
        ? `${formatBytes(progress.bytes_done || 0)} / ${formatBytes(progress.bytes_total)}${percent !== null ? ` · ${percent}%` : ""}`
        : progress.bytes_done
          ? formatBytes(progress.bytes_done)
          : "";
  const kind = progress.status === "completed" ? "success" : progress.status === "failed" ? "warn" : "active";
  container.hidden = false;
  container.innerHTML = `<div class="job-activity ${kind}">
    <div class="job-activity-head"><span>${label}</span><span>${escapeHtml(detail)}</span></div>
    <div class="job-energy-bar${percent === null && kind === "active" ? " restore-indeterminate" : ""}" role="progressbar" aria-label="${label}" aria-valuemin="0" aria-valuemax="100"${percent === null ? "" : ` aria-valuenow="${percent}"`}>
      <span style="width: ${percent ?? 35}%"></span>
    </div>
  </div>`;
}

// Progress polling is separate from page refreshes, which pause for dialogs and
// restore decisions. A unique ID prevents a previous restore from appearing here.
function startRestoreProgress(id: string): {
  operationID: string;
  finish: (success: boolean, restoredFiles?: number) => void;
} {
  const operationID = globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  const user = currentUser;
  let stopped = false;
  let timer: number | null = null;
  let latest: RestoreProgress = { status: "running", percent: null };
  renderRestoreProgress(id, latest);

  const poll = async () => {
    try {
      const response = await fetch(`/api/recovery/progress?operation_id=${encodeURIComponent(operationID)}`, {
        cache: "no-store",
        signal: globalThis.AbortSignal?.timeout?.(5000),
      });
      if (!response.ok) return;
      const progress: RestoreProgress = await response.json();
      if (stopped || currentUser !== user || !currentUser?.is_admin) return;
      if (progress.operation_id === operationID && progress.status !== "idle") {
        latest = progress;
        renderRestoreProgress(id, latest);
      }
    } catch {
      // A missed progress poll does not cancel a restore or repeat error toasts.
    } finally {
      if (!stopped && currentUser === user && currentUser?.is_admin) timer = window.setTimeout(poll, 1000);
    }
  };
  if (document.getElementById(id)) void poll();

  return {
    operationID,
    finish(success, restoredFiles) {
      stopped = true;
      if (timer !== null) window.clearTimeout(timer);
      if (currentUser !== user) return;
      renderRestoreProgress(id, {
        ...latest,
        status: success ? "completed" : "failed",
        percent: success ? 100 : latest.percent,
        restored_files: restoredFiles ?? latest.restored_files,
      });
    },
  };
}
