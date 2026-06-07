let _overviewLoading = false;
let _knownSnapshotKeys = null;
const CENTRAL_REFRESH_MS = 15000;

function parseSnapshotDate(filename) {
  const parts = filename.split("__");
  if (parts.length < 3) return null;
  const iso = parts[1].replace(/T(\d{2})-(\d{2})-(\d{2})Z$/, "T$1:$2:$3Z");
  const d = new Date(iso);
  return isNaN(d) ? null : d;
}

function parseFingerprint(filename) {
  const parts = filename.split("__");
  if (parts.length < 3) return null;
  return parts[2].replace(/\.tar\.zst$/, "");
}

function formatDate(d) {
  if (!d) return "—";
  return d.toLocaleString(undefined, {
    year: "numeric", month: "short", day: "numeric",
    hour: "2-digit", minute: "2-digit",
  });
}

async function manualRefresh() {
  await loadOverview({ force: true, notifyNewSnapshots: true });
  setActionStatus("Refreshed.", "success");
}

async function downloadSnapshot(edgeId, edgeInstanceId, jobName, filename, btn) {
  const basePath = edgeInstanceId
    ? `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(edgeInstanceId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`
    : `/api/snapshots/${encodeURIComponent(edgeId)}/${encodeURIComponent(jobName)}/${encodeURIComponent(filename)}`;
  btn.disabled = true;
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
    btn.disabled = false;
  }
}

async function deleteSnapshot(edgeId, edgeInstanceId, jobName, filename, btn) {
  if (!await confirmApp({
    title: "Delete Snapshot",
    message: `Delete ${filename}? This cannot be undone.`,
    confirmLabel: "Delete",
    danger: true,
  })) return;

  btn.disabled = true;
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
    btn.closest(".snapshot-row")?.remove();
    setActionStatus(`Deleted snapshot ${filename}.`, "success");
  } finally {
    btn.disabled = false;
  }
}

function renderSnapshots(edgeId, edgeInstanceId, jobName, snapshots) {
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
            onclick="downloadSnapshot('${escapeHtml(edgeId)}',${edgeInstanceId ? `'${escapeHtml(edgeInstanceId)}'` : "null"},'${escapeHtml(jobName)}','${escapeHtml(name)}',this)">Download</button>
          <button class="btn btn-del"
            onclick="deleteSnapshot('${escapeHtml(edgeId)}',${edgeInstanceId ? `'${escapeHtml(edgeInstanceId)}'` : "null"},'${escapeHtml(jobName)}','${escapeHtml(name)}',this)">Delete</button>
        </div>
      </div>`;
  }).join("");
}

function renderKeyManager(ns) {
  const edgeId = ns.edge_id;
  const edgeInstanceId = ns.edge_instance_id;
  const edgeKeyId = buildEdgeKeyId(edgeId, edgeInstanceId);
  const expectedFingerprint = ns.encryption_key_fingerprint || "";
  return `
    <div class="edge-key-panel">
      <div class="edge-key-head">
        <strong>Decryption Key</strong>
        ${renderStaticClipValue("Expected key fingerprint", expectedFingerprint || "unknown", { className: "edge-detail", clipLength: 24 })}
      </div>
      <div class="edge-key-controls">
        <input
          type="password"
          placeholder="Paste the Edge encryption key"
          data-edge-key-input="${escapeHtml(edgeKeyId)}">
        <button class="btn btn-key" type="button" onclick="rememberEncKey('${escapeHtml(edgeId)}','${escapeHtml(edgeInstanceId)}')">Save Key</button>
        <button class="btn btn-clear" type="button" onclick="clearEncKey('${escapeHtml(edgeId)}','${escapeHtml(edgeInstanceId)}')">Clear</button>
      </div>
      <div class="key-status info" data-edge-key-status="${escapeHtml(edgeKeyId)}"></div>
    </div>
  `;
}

function renderInstanceMeta(instance) {
  return `
    ${instance.advertised_url
      ? renderLinkValue("Edge URL", instance.advertised_url, { className: "edge-detail", clipLength: 28 })
      : '<span class="edge-detail edge-detail-muted">No URL set</span>'}
  `;
}

function renderInstanceCard(edgeId, instance) {
  const instanceId = instance.edge_instance_id;
  const deleteBtn = instanceId
    ? `<button class="btn btn-del btn-del-instance" type="button" onclick="deleteInstance('${escapeHtml(edgeId)}','${escapeHtml(instanceId)}',this)">Delete Instance</button>`
    : "";
  const revokeBtn = instanceId && instance.credential_configured
    ? `<button class="btn btn-del btn-del-instance" type="button" onclick="revokeInstanceCredential('${escapeHtml(edgeId)}','${escapeHtml(instanceId)}',this)">Revoke Token</button>`
    : "";
  return `
    <section class="instance-card">
      <div class="instance-head">
        <div>
          <div class="instance-title">${escapeHtml(instanceId || "Legacy snapshots")}</div>
          <div class="edge-submeta">${renderInstanceMeta(instance)}</div>
        </div>
        <div class="instance-head-right">
          <span class="edge-count">${(instance.jobs || []).length} job${instance.jobs.length !== 1 ? "s" : ""}</span>
          ${revokeBtn}
          ${deleteBtn}
        </div>
      </div>
      ${instance.last_upload_tls === false ? '<p class="instance-http-warning">Last upload from this Edge arrived over plain HTTP. Credentials were not encrypted in transit.</p>' : ""}
      ${instance.edge_instance_id ? renderKeyManager({ edge_id: edgeId, edge_instance_id: instance.edge_instance_id, encryption_key_fingerprint: instance.encryption_key_fingerprint }) : ""}
      ${(instance.jobs || []).map((job) => `
        <div class="job-block">
          <div class="job-header">
            <div class="job-header-main">
              <span class="job-name">${escapeHtml(job.job_name)}</span>
              <span class="job-count">${escapeHtml(String(job.snapshot_count))} snapshot${job.snapshot_count !== 1 ? "s" : ""}</span>
            </div>
          </div>
          <div class="snapshot-list">
            ${renderSnapshots(edgeId, instance.edge_instance_id, job.job_name, job.snapshots || [])}
          </div>
        </div>
      `).join("") || '<p class="no-snapshots">No jobs stored yet.</p>'}
    </section>
  `;
}

async function revokeInstanceCredential(edgeId, edgeInstanceId, btn) {
  const label = edgeInstanceId || "this instance";
  if (!await confirmApp({
    title: "Revoke Token",
    message: `Revoke the Edge credential used by "${label}"? Any other instances using the same token will stop authenticating too.`,
    confirmLabel: "Revoke Token",
    danger: true,
  })) {
    return;
  }
  btn.disabled = true;
  try {
    const response = await fetch(`/api/credentials/instances/${encodeURIComponent(edgeId)}/${encodeURIComponent(edgeInstanceId)}`, {
      method: "DELETE",
    });
    const body = await readJson(response);
    if (!response.ok) {
      setActionStatus(body.detail || "Revoke failed.", "error");
      return;
    }
    const affected = body.affected_instances || [];
    setActionStatus(`Revoked token for ${affected.length || 1} instance${affected.length === 1 ? "" : "s"}.`, "success");
    await loadOverview({ silent: true, force: true });
  } catch (error) {
    setActionStatus(error.message || "Revoke failed.", "error");
  } finally {
    btn.disabled = false;
  }
}

async function deleteInstance(edgeId, edgeInstanceId, btn) {
  const label = edgeInstanceId || "this instance";
  if (!await confirmApp({
    title: "Delete Instance",
    message: `Delete all snapshots for instance "${label}" under edge "${edgeId}"? This permanently removes all backup files for this instance and cannot be undone.`,
    confirmLabel: "Delete Instance",
    danger: true,
  })) {
    return;
  }
  btn.disabled = true;
  const baseUrl = `/api/instances/${encodeURIComponent(edgeId)}/${encodeURIComponent(edgeInstanceId)}`;
  try {
    const res = await fetch(baseUrl, { method: "DELETE" });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      const detail = body.detail || {};
      if (res.status === 409 && detail.cleanup_available) {
        if (!await confirmApp({
          title: "Remove Stale Instance",
          message: `Central could not find backup files for instance "${label}". Remove this stale instance entry from the UI?`,
          confirmLabel: "Remove Entry",
          danger: true,
        })) {
          setActionStatus("Cleanup cancelled.", "info");
          return;
        }
        const cleanupRes = await fetch(`${baseUrl}?cleanup_missing=true`, { method: "DELETE" });
        if (!cleanupRes.ok) {
          const cleanupBody = await cleanupRes.json().catch(() => ({}));
          const cleanupDetail = cleanupBody.detail;
          setActionStatus((typeof cleanupDetail === "string" ? cleanupDetail : cleanupDetail?.message) || "Cleanup failed.", "error");
          return;
        }
        setActionStatus(`Removed stale instance entry ${label}.`, "success");
        await loadOverview({ silent: true, force: true });
        return;
      }
      setActionStatus((typeof detail === "string" ? detail : detail.message) || "Delete failed.", "error");
      return;
    }
    setActionStatus(`Deleted all snapshots for instance ${label}.`, "success");
    await loadOverview({ silent: true, force: true });
  } catch (error) {
    setActionStatus(error.message || "Delete failed.", "error");
  } finally {
    btn.disabled = false;
  }
}

function collectSnapshotEvents(data) {
  return (data.edges || []).flatMap((edge) => (
    (edge.instances || []).flatMap((instance) => (
      (instance.jobs || []).flatMap((job) => (
        (job.snapshots || []).map((snapshot) => {
          const edgeInstanceId = instance.edge_instance_id || "";
          const name = snapshot.name || snapshot.filename || "";
          return {
            key: `${edge.edge_id}::${edgeInstanceId}::${job.job_name}::${name}`,
            edgeId: edge.edge_id,
            edgeInstanceId,
            jobName: job.job_name,
            name,
          };
        })
      ))
    ))
  )).filter((event) => event.name);
}

function updateSnapshotArrivalToasts(data, { notify = false } = {}) {
  const events = collectSnapshotEvents(data);
  const nextKeys = new Set(events.map((event) => event.key));
  if (_knownSnapshotKeys === null) {
    _knownSnapshotKeys = nextKeys;
    return;
  }

  const arrivals = events.filter((event) => !_knownSnapshotKeys.has(event.key));
  _knownSnapshotKeys = nextKeys;
  if (!notify || !arrivals.length) return;

  arrivals.slice(0, 4).forEach((event) => {
    const instanceLabel = event.edgeInstanceId ? ` / ${event.edgeInstanceId}` : "";
    showToast(
      `Received ${event.jobName} from ${event.edgeId}${instanceLabel}.`,
      "success",
      { title: "Snapshot received" },
    );
  });
  if (arrivals.length > 4) {
    showToast(`${arrivals.length - 4} more snapshots received.`, "success", { title: "Snapshot received" });
  }
}

function captureOverviewUiState() {
  const expandedEdges = Array.from(document.querySelectorAll("#namespaces details[data-edge-id][open]"))
    .map((element) => element.dataset.edgeId)
    .filter(Boolean);
  const keyDrafts = Object.fromEntries(
    Array.from(document.querySelectorAll("[data-edge-key-input]"))
      .map((input) => [input.dataset.edgeKeyInput, input.value])
      .filter(([, value]) => value),
  );
  return {
    expandedEdges: new Set(expandedEdges),
    keyDrafts,
  };
}

function restoreKeyDrafts(keyDrafts) {
  Object.entries(keyDrafts || {}).forEach(([edgeKeyId, value]) => {
    const [edgeId, rawInstanceId] = String(edgeKeyId).split("::", 2);
    const input = keyInputElement(edgeId, rawInstanceId === "_legacy" ? null : rawInstanceId);
    if (input && !input.value) {
      input.value = value;
    }
  });
}

async function loadOverview({ silent = false, force = false, notifyNewSnapshots = false } = {}) {
  if (_overviewLoading) {
    return;
  }

  _overviewLoading = true;
  if (!silent) {
    document.getElementById("namespaces").innerHTML = '<div class="section-loading"><span class="section-spinner" aria-label="Loading…"></span></div>';
  }
  const uiState = captureOverviewUiState();

  try {
    const res = await fetch("/api/overview");
    if (!res.ok) {
      throw new Error("Refresh failed.");
    }
    const data = await res.json();
    window.__centralSettings = data.settings || {};
    if (!document.getElementById("settings-dialog")?.open) {
      applyTheme(window.__centralSettings.theme || "dark");
    }
    updateSnapshotArrivalToasts(data, { notify: notifyNewSnapshots });

    const diskFree = typeof data.disk_free_bytes === "number" ? formatBytes(data.disk_free_bytes) : null;
    const diskUsed = typeof data.disk_used_bytes === "number" ? formatBytes(data.disk_used_bytes) : null;
    const diskTotal = typeof data.disk_total_bytes === "number" ? formatBytes(data.disk_total_bytes) : null;
    const edges = data.edges || [];
    const allInstances = edges.flatMap((edge) => (edge.instances || []).map((instance) => ({ edgeId: edge.edge_id, instance })));
    _edgeKeyFingerprints = Object.fromEntries(
      allInstances
        .filter(({ instance }) => instance.edge_instance_id)
        .map(({ edgeId, instance }) => [buildEdgeKeyId(edgeId, instance.edge_instance_id), instance.encryption_key_fingerprint || ""]),
    );

    const totalEdges = edges.length;
    const totalInstances = edges.reduce((t, e) => t + (e.instances || []).length, 0);
    const totalJobs = edges.reduce((t, e) => t + (e.instances || []).reduce((tt, i) => tt + (i.jobs || []).length, 0), 0);
    const totalSnapshots = edges.reduce((t, e) => t + (e.instances || []).reduce((tt, i) => tt + (i.jobs || []).reduce((ttt, j) => ttt + (j.snapshot_count || 0), 0), 0), 0);

    document.getElementById("meta").innerHTML = `
      <div><strong>Status</strong><br><span class="status-${escapeHtml(data.status)}">${escapeHtml(data.status)}</span></div>
      <div><strong>Edges</strong> ${renderHelpHint("Unique Edge device IDs that have stored at least one snapshot on this Central.")}<br>${totalEdges}</div>
      <div><strong>Instances</strong> ${renderHelpHint("Each reinstall or unique Edge setup shows as a separate instance under the same Edge ID.")}<br>${totalInstances}</div>
      <div><strong>Jobs</strong> ${renderHelpHint("Named backup jobs across all instances. Each job backs up one source directory on an Edge device.")}<br>${totalJobs}</div>
      <div><strong>Snapshots</strong> ${renderHelpHint("Total backup snapshots stored on Central, across all edges, instances, and jobs.")}<br>${totalSnapshots}</div>
      <div><strong>Backup Root</strong><br>${escapeHtml(data.backup_dir)}</div>
      <div><strong>Retention</strong><br>keep last ${escapeHtml(String(data.retention_keep_last))} snapshots</div>
      ${diskUsed !== null ? `<div><strong>Backups Used</strong><br>${escapeHtml(diskUsed)}</div>` : ""}
      ${diskFree !== null ? `<div><strong>Disk Free</strong><br>${escapeHtml(diskFree)}</div>` : ""}
      ${diskTotal !== null ? `<div><strong>Disk Total</strong><br>${escapeHtml(diskTotal)}</div>` : ""}
    `;

    document.getElementById("namespaces").innerHTML = edges.length
      ? edges.map((edge) => {
          const edgeInstances = edge.instances || [];
          const edgeJobCount = edgeInstances.reduce((t, i) => t + (i.jobs || []).length, 0);
          const edgeSnapCount = edgeInstances.reduce((t, i) => t + (i.jobs || []).reduce((tt, j) => tt + (j.snapshot_count || 0), 0), 0);
          return `
          <details class="edge-card edge-card-collapsible" data-edge-id="${escapeHtml(edge.edge_id)}"${uiState.expandedEdges.has(edge.edge_id) ? " open" : ""}>
            <summary class="edge-header edge-card-summary">
              <div class="edge-header-main">
                <span class="edge-id">${escapeHtml(edge.edge_id)}</span>
                <div class="edge-submeta">
                  <span>${escapeHtml(String(edgeInstances.length))} instance${edgeInstances.length !== 1 ? "s" : ""}</span>
                  <span>${edgeJobCount} job${edgeJobCount !== 1 ? "s" : ""}</span>
                  <span>${edgeSnapCount} snapshot${edgeSnapCount !== 1 ? "s" : ""}</span>
                </div>
              </div>
              <span class="edge-expand-label"></span>
            </summary>
            <div class="edge-card-body">
              ${(edge.instances || []).map((instance) => renderInstanceCard(edge.edge_id, instance)).join("") || '<p class="no-snapshots">No instances registered yet.</p>'}
            </div>
          </details>
        `;
        }).join("")
      : '<p class="hint">No snapshots have been stored yet.</p>';

    restoreKeyDrafts(uiState.keyDrafts);
    if (!document.getElementById("settings-dialog")?.open) {
      fillSettings(data.settings || {});
    }
    await Promise.all(
      allInstances
        .filter(({ instance }) => instance.edge_instance_id)
        .map(({ edgeId, instance }) => refreshKeyPanel(edgeId, instance.edge_instance_id)),
    );
  } catch (error) {
    if (!silent) {
      setActionStatus(error.message || "Refresh failed.", "error");
    }
  } finally {
    _overviewLoading = false;
  }
  loadVerifyStatus();
}

function renderVerifyResult(data) {
  if (!data || data.status === "never_run") {
    const interval = window.__centralSettings?.snapshot_verify_interval_hours || 0;
    const hint = interval > 0
      ? `Automatic check every ${interval}h. No run yet since last start.`
      : "Automatic checks are disabled. Set an interval in Central Settings to enable.";
    return `
      <div class="verify-bar verify-bar-idle">
        <span class="verify-label">Integrity check: <strong>not yet run</strong></span>
        <span class="hint verify-hint">${escapeHtml(hint)}</span>
        <button type="button" class="secondary verify-run-btn" id="verify-run-btn" onclick="runVerifyNow()">Run Now</button>
      </div>`;
  }
  const checkedAt = data.checked_at ? formatDate(new Date(data.checked_at)) : "—";
  const hasFailures = data.failure_count > 0;
  const barClass = hasFailures ? "verify-bar-fail" : "verify-bar-ok";
  const summary = hasFailures
    ? `${data.failure_count} failure${data.failure_count !== 1 ? "s" : ""} out of ${data.total_checked} latest snapshot${data.total_checked !== 1 ? "s" : ""} checked`
    : `${data.total_checked} latest snapshot${data.total_checked !== 1 ? "s" : ""} verified OK`;
  const failureDetail = hasFailures && data.last_failure
    ? `<span class="verify-failure-detail" title="${escapeHtml(data.last_failure_msg || "")}">Last failure: ${escapeHtml(data.last_failure)}</span>`
    : "";
  return `
    <div class="verify-bar ${escapeHtml(barClass)}">
      <span class="verify-label">Integrity check: <strong>${escapeHtml(summary)}</strong></span>
      <span class="hint verify-hint">Checked ${escapeHtml(checkedAt)}</span>
      ${failureDetail}
      <button type="button" class="secondary verify-run-btn" id="verify-run-btn" onclick="runVerifyNow()">Run Now</button>
    </div>`;
}

async function loadVerifyStatus() {
  const el = document.getElementById("verify-status");
  if (!el) return;
  try {
    const res = await fetch("/api/admin/verify");
    if (!res.ok) return;
    const data = await res.json();
    el.innerHTML = renderVerifyResult(data);
  } catch {
    // non-critical, don't surface errors
  }
}

async function runVerifyNow() {
  const btn = document.getElementById("verify-run-btn");
  if (btn) btn.disabled = true;
  const el = document.getElementById("verify-status");
  if (el) {
    el.innerHTML = `<div class="verify-bar verify-bar-idle"><span class="section-spinner verify-spinner" aria-hidden="true"></span><span class="verify-label">Running integrity check…</span></div>`;
  }
  try {
    const res = await fetch("/api/admin/verify", { method: "POST" });
    if (!res.ok) {
      setActionStatus("Integrity check failed to run.", "error");
      await loadVerifyStatus();
      return;
    }
    const data = await res.json();
    if (el) el.innerHTML = renderVerifyResult(data);
    if (data.failure_count > 0) {
      setActionStatus(`Integrity check found ${data.failure_count} failure${data.failure_count !== 1 ? "s" : ""}.`, "error");
    } else {
      setActionStatus(`Integrity check passed — ${data.total_checked} snapshot${data.total_checked !== 1 ? "s" : ""} verified.`, "success");
    }
  } catch {
    setActionStatus("Integrity check failed to run.", "error");
    await loadVerifyStatus();
  }
}
