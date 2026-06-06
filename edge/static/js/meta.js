function updateCronScheduleHint() {
  const input = document.getElementById("settings_cron_schedule");
  const hint = document.getElementById("settings-cron-help");
  if (!input || !hint) return;
  input.setCustomValidity(validateCronSchedule(input.value));
  const description = describeCronSchedule(input.value);
  hint.textContent = description.summary;
  input.title = `${description.summary} ${description.help}`;
}

function describeSchedulerState(scheduler) {
  const state = String(scheduler?.state || "idle");
  if (state === "running") {
    return {
      label: "Running a backup cycle",
      help: "Edge is actively scanning, packing, or uploading right now.",
    };
  }
  if (state === "waiting") {
    return {
      label: "Waiting for the next scheduled run",
      help: "This is the normal idle state between backup cycles.",
    };
  }
  if (state === "stopped") {
    return {
      label: "Scheduler stopped",
      help: "The scheduler is not currently running.",
    };
  }
  return {
    label: "Ready",
    help: "Edge is ready for the next run request.",
  };
}

function describeUploadCircuit(uploadCircuit) {
  const failures = Number(uploadCircuit?.consecutive_failures || 0);
  const cooldown = Number(uploadCircuit?.cooldown_remaining_seconds || 0);
  if (uploadCircuit?.state === "open") {
    return {
      label: `Paused after upload failures (${cooldown}s left)`,
      help: "Edge temporarily pauses uploads after repeated failures, then retries automatically after the cooldown.",
    };
  }
  return {
    label: failures > 0 ? `Healthy, with ${failures} recent failure${failures === 1 ? "" : "s"}` : "Healthy",
    help: "Uploads are allowed. Edge only pauses this circuit after repeated failures talking to Central.",
  };
}

function initMeta() {
  const pending = '<span class="hint">…</span>';
  document.getElementById("meta").innerHTML = `
    <div><strong>Edge ID</strong><br><span id="meta-val-edge-id">${pending}</span></div>
    <div><strong>Instance ID</strong><br><span id="meta-val-instance-id">${pending}</span></div>
    <div><strong>Scan Root</strong><br><span id="meta-val-scan-dir">${pending}</span></div>
    <div><strong>Central URL</strong><br><span id="meta-val-central-url">${pending}</span></div>
    <div><strong>Advertised URL</strong><br><span id="meta-val-advertised-url">${pending}</span></div>
    <div><strong>Cron Schedule</strong> <span id="meta-hint-cron"></span><br><span id="meta-val-cron">${pending}</span></div>
    <div><strong>Upload Circuit</strong> <span id="meta-hint-upload-circuit"></span><br><span id="meta-val-upload-circuit">${pending}</span></div>
    <div><strong>Edge Credential</strong><br><span id="meta-val-edge-credential">${pending}</span></div>
    <div class="enc-key-cell">
      <strong>Encryption Key</strong>
      <div class="enc-key-row">
        <code id="enc-key-value">…</code>
        <button type="button" class="secondary enc-key-copy" onclick="copyEncKey()">Copy</button>
      </div>
      <span class="hint" id="meta-val-enc-fingerprint">…</span>
    </div>
  `;
}

function fillMetaFromDir(data) {
  const uploadCircuit = data.upload_circuit || {};
  const settingsStatus = data.settings_status || {};
  const cronDetails = describeCronSchedule(data.cron_schedule);
  const uploadCircuitDetails = describeUploadCircuit(uploadCircuit);
  const advertisedUrl = String(data.advertised_url || "").trim();

  const set = setHtmlIfChanged;

  set("meta-val-edge-id", renderClipValue("", data.edge_id, { className: "clip-code", clipLength: 28 }));
  set("meta-val-instance-id", renderClipValue("", data.edge_instance_id || "—", { className: "clip-code", clipLength: 28 }));
  set("meta-val-scan-dir", renderClipValue("", data.scan_root, { className: "clip-code", clipLength: 34 }));
  set("meta-val-central-url", renderClipValue("", data.central_url, { className: "clip-code", clipLength: 34 }));
  set("meta-val-advertised-url", advertisedUrl ? renderClipValue("", advertisedUrl, { className: "clip-code", clipLength: 34 }) : '<span class="hint">Not set</span>');
  set("meta-hint-cron", renderHelpHint(cronDetails.help));
  set("meta-val-cron", `<code title="${escapeHtml(`${cronDetails.summary} ${cronDetails.help}`)}">${escapeHtml(data.cron_schedule)}</code><div class="hint">${escapeHtml(cronDetails.summary)}</div>`);
  set("meta-hint-upload-circuit", renderHelpHint(uploadCircuitDetails.help));
  set("meta-val-upload-circuit", escapeHtml(uploadCircuitDetails.label));
  set("meta-val-edge-credential", escapeHtml(settingsStatus.edge_credential_configured ? "configured" : "missing"));
  if (data.encryption_key_fingerprint) {
    set("meta-val-enc-fingerprint", `Fingerprint ${escapeHtml(shortFingerprint(data.encryption_key_fingerprint))}. Central uses this to confirm you pasted the right key for this Edge before decrypting.`);
  }
}

function fillMetaEncKey(key, fingerprint) {
  const keyEl = document.getElementById("enc-key-value");
  if (keyEl) keyEl.textContent = key || "—";
  if (fingerprint) {
    const fpEl = document.getElementById("meta-val-enc-fingerprint");
    if (fpEl) fpEl.textContent = `Fingerprint ${shortFingerprint(fingerprint)}. Central uses this to confirm you pasted the right key for this Edge before decrypting.`;
  }
}

async function copyEncKey() {
  const key = document.getElementById("enc-key-value")?.textContent;
  if (!key || key === "—" || key === "…") return;
  try {
    await navigator.clipboard.writeText(key);
  } catch {
    const ta = document.createElement("textarea");
    ta.value = key;
    ta.style.cssText = "position:fixed;opacity:0;pointer-events:none";
    document.body.appendChild(ta);
    ta.select();
    document.execCommand("copy");
    document.body.removeChild(ta);
  }
  const btn = document.querySelector(".enc-key-copy");
  if (btn) {
    btn.textContent = "Copied!";
    setTimeout(() => { btn.textContent = "Copy"; }, 2000);
  }
}
