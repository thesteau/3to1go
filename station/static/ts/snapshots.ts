function parseSnapshotDate(filename: string): Date | null {
  const parts = filename.split("__");
  if (parts.length < 3) return null;
  const iso = parts[1].replace(/T(\d{2})-(\d{2})-(\d{2})Z$/, "T$1:$2:$3Z");
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
}

function parseFingerprint(filename: string): string | null {
  const parts = filename.split("__");
  if (parts.length < 3) return null;
  return parts[2].replace(/\.tar\.zst$/, "");
}

function formatDate(d: Date | null): string {
  if (!d) return "—";
  return d.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

// Returns the snapshot response, or null after reporting why it could not be fetched.
async function fetchSnapshot(path: string): Promise<Response | null> {
  const res = await fetch(path);
  if (res.ok) return res;
  if (res.status === 404) {
    await loadOverview({ silent: true, force: true });
    setActionStatus(`That snapshot was already gone, so Station refreshed the snapshot list.`, "info");
    return null;
  }
  setActionStatus("Download failed.", "error");
  return null;
}

async function downloadSnapshot(
  scoutId: string,
  scoutInstanceId: string | null,
  jobName: string,
  filename: string,
  btn: HTMLButtonElement,
): Promise<void> {
  const basePath = scoutInstanceId
    ? `/api/snapshots/${encodeURIComponent(scoutId)}/${encodeURIComponent(scoutInstanceId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`
    : `/api/snapshots/${encodeURIComponent(scoutId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`;
  const restore = setButtonBusy(btn, "Downloading…");
  try {
    let res = await fetchSnapshot(basePath);
    if (!res) return;
    // The snapshot streams through decryption, so a large archive is never held whole in the tab.
    let reader = snapshotReaderFromResponse(res);
    let key: string | null = null;
    if (snapshotEncryption(await reader.peek(SNAPSHOT_HEAD_LEN))) {
      key = getEncKey(scoutId, scoutInstanceId);
      if (!key) {
        // Getting the key may mean waiting on the operator; don't hold the response open against
        // the server's write timeout meanwhile. Fetch the snapshot again once the key is known.
        reader.cancel();
        key = await resolveEncKey(scoutId, scoutInstanceId);
        if (!key) return;
        res = await fetchSnapshot(basePath);
        if (!res) return;
        reader = snapshotReaderFromResponse(res);
      }
    }

    try {
      triggerBlobDownload(await readSnapshot(reader, key), filename);
    } catch (error) {
      if (error instanceof SnapshotReadError) {
        setActionStatus("The download was interrupted. Check the connection and retry.", "error");
        return;
      }
      clearStoredEncKey(scoutId, scoutInstanceId);
      await refreshKeyPanel(scoutId, scoutInstanceId);
      const expectedFingerprint = getExpectedKeyFingerprint(scoutId, scoutInstanceId);
      setActionStatus(
        expectedFingerprint
          ? "Decryption failed. The archive may be damaged, or the key changed after this upload."
          : "Decryption failed - wrong key or corrupted archive.",
        "error",
      );
    }
  } finally {
    restore();
  }
}

async function deleteSnapshot(
  scoutId: string,
  scoutInstanceId: string | null,
  jobName: string,
  filename: string,
  btn: HTMLButtonElement,
): Promise<void> {
  if (
    !(await confirmApp({
      title: "Delete Snapshot",
      message: `Delete ${filename}? This cannot be undone.`,
      confirmLabel: "Delete",
      danger: true,
    }))
  )
    return;

  const restore = setButtonBusy(btn, "Deleting…");
  try {
    const url = scoutInstanceId
      ? `/api/snapshots/${encodeURIComponent(scoutId)}/${encodeURIComponent(scoutInstanceId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`
      : `/api/snapshots/${encodeURIComponent(scoutId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`;
    const res = await fetch(url, { method: "DELETE" });
    if (!res.ok) {
      if (res.status === 404) {
        await loadOverview({ silent: true, force: true });
        setActionStatus(`That snapshot was already gone, so Station refreshed the snapshot list.`, "info");
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

// Asks Scout to restore this exact snapshot. Scout's operator accepts or rejects it there,
// and supplies the source key there when restoring to another device.
async function requestSnapshotRestore(
  scoutId: string,
  scoutInstanceId: string,
  jobName: string,
  filename: string,
  btn: HTMLButtonElement,
): Promise<void> {
  const target = btn.parentElement?.querySelector<HTMLSelectElement>(".restore-target")?.value;
  const [targetScout, targetInstance] = target ? JSON.parse(target) : [scoutId, scoutInstanceId];
  if (targetScout === scoutId && targetInstance === scoutInstanceId && !getEncKey(scoutId, scoutInstanceId)) {
    setActionStatus("Save this Scout's encryption key first, then request the restore.", "warning");
    return;
  }
  const restore = setButtonBusy(btn, "Requesting…");
  try {
    const res = await fetch(
      `/api/snapshots/${encodeURIComponent(scoutId)}/${encodeURIComponent(scoutInstanceId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}/restore`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ target_scout_id: targetScout, target_instance_id: targetInstance }),
      },
    );
    const result = await res.json().catch(() => ({}));
    if (!res.ok) {
      setActionStatus(result.detail || "Restore request failed.", "error");
      return;
    }
    setActionStatus(
      targetScout !== scoutId || targetInstance !== scoutInstanceId
        ? "Restore requested on the target device. Accept it in Scout with the original snapshot’s encryption key."
        : result.notified
          ? "Restore requested. Accept it under Restore Requests in Scout."
          : "Restore requested. Scout shows it under Restore Requests on its next refresh.",
      "success",
    );
  } catch {
    setActionStatus("Restore request failed. Check the connection and retry.", "error");
  } finally {
    restore();
  }
}

function renderSnapshots(
  scoutId: string,
  scoutInstanceId: string | null | undefined,
  jobName: string,
  snapshots: Snapshot[],
): string {
  if (!snapshots.length) return '<p class="no-snapshots">No snapshots yet.</p>';
  return snapshots
    .map((snap, idx) => {
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
          ${snap.unusual ? `<span class="snapshot-unusual-tag" tabindex="0" title="${escapeHtml(snap.unusual)}" aria-label="Unusual size: ${escapeHtml(snap.unusual)}">unusual size</span>` : ""}
        </div>
        <span class="snapshot-size">${escapeHtml(size)}</span>
        <div class="snapshot-actions">
          <button class="btn btn-dl"
            onclick="downloadSnapshot(${inlineString(scoutId)},${scoutInstanceId ? inlineString(scoutInstanceId) : "null"},${inlineString(jobName)},${inlineString(name)},this)">Download</button>
          ${
            scoutInstanceId
              ? `<select class="restore-target" aria-label="Restore target device">${(typeof restoreTargets === "undefined" ? [] : restoreTargets).map((target) => `<option value="${escapeHtml(JSON.stringify([target.scoutId, target.instanceId]))}" ${target.scoutId === scoutId && target.instanceId === scoutInstanceId ? "selected" : ""}>${escapeHtml(target.scoutId)} / ${escapeHtml(target.instanceId)}</option>`).join("")}</select><button class="btn btn-restore"
            onclick="requestSnapshotRestore(${inlineString(scoutId)},${inlineString(scoutInstanceId)},${inlineString(jobName)},${inlineString(name)},this)">Restore</button>`
              : ""
          }
          <button class="btn btn-del"
            onclick="deleteSnapshot(${inlineString(scoutId)},${scoutInstanceId ? inlineString(scoutInstanceId) : "null"},${inlineString(jobName)},${inlineString(name)},this)">Delete</button>
        </div>
      </div>`;
    })
    .join("");
}
