function renderKeyManager(ns: {
  scout_id: string;
  scout_instance_id: string;
  encryption_key_fingerprint?: string;
}): string {
  const scoutId = ns.scout_id;
  const scoutInstanceId = ns.scout_instance_id;
  const scoutKeyId = buildScoutKeyId(scoutId, scoutInstanceId);
  const expectedFingerprint = ns.encryption_key_fingerprint || "";
  return `
    <div class="scout-key-panel" data-key-panel="${escapeHtml(scoutKeyId)}">
      <div class="scout-key-head">
        <strong>Scout Key</strong>
        ${renderStaticClipValue("Expected key fingerprint", expectedFingerprint || "unknown", { className: "scout-detail", clipLength: 24 })}
      </div>
      <div class="scout-key-controls">
        <input
          type="text" class="secret-value" autocomplete="off" spellcheck="false" autocapitalize="none"
          placeholder="Paste the Scout key"
          data-scout-key-input="${escapeHtml(scoutKeyId)}">
        <button class="btn btn-key" type="button" onclick="rememberEncKey(${inlineString(scoutId)},${inlineString(scoutInstanceId)})">Save Key</button>
        <button class="btn btn-clear" type="button" onclick="clearEncKey(${inlineString(scoutId)},${inlineString(scoutInstanceId)})">Clear</button>
      </div>
      <div class="key-status info" data-scout-key-status="${escapeHtml(scoutKeyId)}"></div>
    </div>
  `;
}

function renderInstanceMeta(instance: ScoutInstance): string {
  return `
    ${
      instance.advertised_url
        ? renderLinkValue("Scout URL", instance.advertised_url, { className: "scout-detail", clipLength: 28 })
        : '<span class="scout-detail scout-detail-muted">No URL set</span>'
    }
  `;
}

function renderInstanceCard(scoutId: string, instance: ScoutInstance): string {
  const instanceId = instance.scout_instance_id;
  const jobs = instance.jobs || [];
  const deleteBtn = instanceId
    ? `<button class="btn btn-del btn-del-instance" type="button" onclick="deleteInstance(${inlineString(scoutId)},${inlineString(instanceId)},this)">Delete Instance</button>`
    : "";
  const revokeBtn =
    instanceId && instance.credential_configured
      ? `<button class="btn btn-del btn-del-instance" type="button" onclick="revokeInstanceCredential(${inlineString(scoutId)},${inlineString(instanceId)},this)">Revoke Station Token</button>`
      : "";
  return `
    <section class="instance-card" data-instance-id="${escapeHtml(instanceId || "_legacy")}">
      <div class="instance-head">
        <div>
          <div class="instance-title">${escapeHtml(instanceId || "Legacy snapshots")}</div>
          <div class="scout-submeta">${renderInstanceMeta(instance)}</div>
        </div>
        <div class="instance-head-right">
          <span class="scout-count">${jobs.length} job${jobs.length !== 1 ? "s" : ""}</span>
          ${revokeBtn}
          ${deleteBtn}
        </div>
      </div>
      ${instance.last_upload_tls === false ? '<p class="instance-http-warning">Last upload from this Scout arrived over plain HTTP. Its Station token was not encrypted in transit.</p>' : ""}
      ${instance.scout_instance_id ? renderKeyManager({ scout_id: scoutId, scout_instance_id: instance.scout_instance_id, encryption_key_fingerprint: instance.encryption_key_fingerprint }) : ""}
      ${
        jobs
          .map(
            (job) => `
        <div class="job-block">
          <div class="job-header">
            <div class="job-header-main">
              <span class="job-name">${escapeHtml(job.job_name)}</span>
              <span class="job-count">${escapeHtml(String(job.snapshot_count))} snapshot${job.snapshot_count !== 1 ? "s" : ""}</span>
            </div>
          </div>
          <div class="snapshot-list">
            ${renderSnapshots(scoutId, instance.scout_instance_id, job.job_name, job.snapshots || [])}
          </div>
        </div>
      `,
          )
          .join("") || '<p class="no-snapshots">No jobs stored yet.</p>'
      }
    </section>
  `;
}

async function revokeInstanceCredential(
  scoutId: string,
  scoutInstanceId: string,
  btn: HTMLButtonElement,
): Promise<void> {
  const label = scoutInstanceId || "this instance";
  if (
    !(await confirmApp({
      title: "Revoke Station Token",
      message: `Revoke the Station token used by "${label}"? Other Scouts using it stop working too.`,
      confirmLabel: "Revoke Token",
      danger: true,
    }))
  ) {
    return;
  }
  const restore = setButtonBusy(btn, "Revoking…");
  try {
    const response = await fetch(
      `/api/credentials/instances/${encodeURIComponent(scoutId)}/${encodeURIComponent(scoutInstanceId)}`,
      {
        method: "DELETE",
      },
    );
    const body = await readJson<RevokeCredentialResponse>(response);
    if (!response.ok) {
      setActionStatus(body.detail || "Revoke failed.", "error");
      return;
    }
    const affected = body.affected_instances || [];
    setActionStatus(
      `Revoked Station token for ${affected.length || 1} instance${affected.length === 1 ? "" : "s"}.`,
      "success",
    );
    await loadOverview({ silent: true, force: true });
  } catch (error) {
    setActionStatus((error as Error).message || "Revoke failed.", "error");
  } finally {
    restore();
  }
}

async function deleteInstance(scoutId: string, scoutInstanceId: string, btn: HTMLButtonElement): Promise<void> {
  const label = scoutInstanceId || "this instance";
  if (
    !(await confirmApp({
      title: "Delete Instance",
      message: `Permanently delete all snapshots for "${label}" on Scout "${scoutId}"? This can't be undone.`,
      confirmLabel: "Delete Instance",
      danger: true,
    }))
  ) {
    return;
  }
  const restore = setButtonBusy(btn, "Deleting…");
  const baseUrl = `/api/instances/${encodeURIComponent(scoutId)}/${encodeURIComponent(scoutInstanceId)}`;
  try {
    const res = await fetch(baseUrl, { method: "DELETE" });
    if (!res.ok) {
      const body: InstanceDeleteResponse = await res.json().catch(() => ({}));
      const detail = body.detail || {};
      if (res.status === 409 && typeof detail === "object" && detail.cleanup_available) {
        if (
          !(await confirmApp({
            title: "Remove Stale Instance",
            message: `No backup files found for "${label}". Remove it from the list?`,
            confirmLabel: "Remove Entry",
            danger: true,
          }))
        ) {
          setActionStatus("Cleanup cancelled.", "info");
          return;
        }
        const cleanupRes = await fetch(`${baseUrl}?cleanup_missing=true`, { method: "DELETE" });
        if (!cleanupRes.ok) {
          const cleanupBody: InstanceDeleteResponse = await cleanupRes.json().catch(() => ({}));
          const cleanupDetail = cleanupBody.detail;
          setActionStatus(
            (typeof cleanupDetail === "string" ? cleanupDetail : cleanupDetail?.message) || "Cleanup failed.",
            "error",
          );
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
    setActionStatus((error as Error).message || "Delete failed.", "error");
  } finally {
    restore();
  }
}
