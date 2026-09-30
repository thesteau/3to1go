function parseSnapshotDate(filename: string): Date | null {
  const parts = filename.split("__");
  if (parts.length < 3) return null;
  const iso = parts[1].replace(/T(\d{2})-(\d{2})-(\d{2})Z$/, "T$1:$2:$3Z");
  const d = new Date(iso);
  return isNaN(d.getTime()) ? null : d;
}

function parseFingerprint(filename: string): string | null {
  const parts = filename.split("__");
  if (parts.length < 3) return null;
  return parts[2].replace(/\.tar\.zst$/, "");
}

function formatDate(d: Date | null): string {
  if (!d) return "—";
  return d.toLocaleString(undefined, {
    year: "numeric", month: "short", day: "numeric",
    hour: "2-digit", minute: "2-digit",
  });
}

async function downloadSnapshot(edgeId: string, edgeInstanceId: string | null, jobName: string, filename: string, btn: HTMLButtonElement): Promise<void> {
  const basePath = edgeInstanceId
    ? `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(edgeInstanceId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`
    : `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`;
  const restore = setButtonBusy(btn, "Downloading…");
  try {
    const res = await fetch(basePath);
    if (!res.ok) {
      if (res.status === 404) {
        await loadOverview({ silent: true, force: true });
        setActionStatus(`That snapshot was already gone, so Central refreshed the snapshot list.`, "info");
        return;
      }
      setActionStatus("Download failed.", "error");
      return;
    }
    const buffer = await res.arrayBuffer();

    if (!isEncrypted(buffer)) {
      triggerBlobDownload(buffer, filename);
      return;
    }

    const key = await resolveEncKey(edgeId, edgeInstanceId);
    if (!key) return;

    try {
      const decrypted = await decryptBuffer(buffer, key);
      triggerBlobDownload(decrypted, filename);
    } catch {
      clearStoredEncKey(edgeId, edgeInstanceId);
      await refreshKeyPanel(edgeId, edgeInstanceId);
      const expectedFingerprint = getExpectedKeyFingerprint(edgeId, edgeInstanceId);
      setActionStatus(
        expectedFingerprint
          ? "Decryption failed after fingerprint verification. The archive may be corrupted, or the Edge key changed after this snapshot was uploaded."
          : "Decryption failed - wrong key or corrupted archive.",
        "error",
      );
    }
  } finally {
    restore();
  }
}

async function deleteSnapshot(edgeId: string, edgeInstanceId: string | null, jobName: string, filename: string, btn: HTMLButtonElement): Promise<void> {
  if (!await confirmApp({
    title: "Delete Snapshot",
    message: `Delete ${filename}? This cannot be undone.`,
    confirmLabel: "Delete",
    danger: true,
  })) return;

  const restore = setButtonBusy(btn, "Deleting…");
  try {
    const url = edgeInstanceId
      ? `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(edgeInstanceId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`
      : `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`;
    const res = await fetch(url, { method: "DELETE" });
    if (!res.ok) {
      if (res.status === 404) {
        await loadOverview({ silent: true, force: true });
        setActionStatus(`That snapshot was already gone, so Central refreshed the snapshot list.`, "info");
        return;
      }
      setActionStatus("Delete failed.", "error");
      return;
    }
    setActionStatus(`Deleted snapshot ${filename}.`, "success");
    fadeOutAndRemove(btn.closest(".snapshot-row"));
  } finally {
    restore();
  }
}

function renderSnapshots(edgeId: string, edgeInstanceId: string | null | undefined, jobName: string, snapshots: Snapshot[]): string {
  if (!snapshots.length) return '<p class="no-snapshots">No snapshots yet.</p>';
  return snapshots.map((snap, idx) => {
    const name = snap.name;
    const size = formatBytes(snap.size_bytes);
    const date = formatDate(parseSnapshotDate(name));
    const fp = parseFingerprint(name) || "";
    const isLatest = idx === 0;
    return `
      <div class="snapshot-row">
        <div class="snapshot-meta">
          <span class="snapshot-date">${escapeHtml(date)}</span>
          ${fp ? renderClipValue("FP", fp, { className: "snapshot-fp", clipLength: 18 }) : ""}
          ${isLatest ? '<span class="snapshot-latest-tag">latest</span>' : ""}
        </div>
        <span class="snapshot-size">${escapeHtml(size)}</span>
        <div class="snapshot-actions">
          <button class="btn btn-dl"
            onclick="downloadSnapshot(${inlineString(edgeId)},${edgeInstanceId ? inlineString(edgeInstanceId) : "null"},${inlineString(jobName)},${inlineString(name)},this)">Download</button>
          <button class="btn btn-del"
            onclick="deleteSnapshot(${inlineString(edgeId)},${edgeInstanceId ? inlineString(edgeInstanceId) : "null"},${inlineString(jobName)},${inlineString(name)},this)">Delete</button>
        </div>
      </div>`;
  }).join("");
}
