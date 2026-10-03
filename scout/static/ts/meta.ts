function updateCronScheduleHint(): void {
  const input = document.getElementById("settings_cron_schedule") as HTMLInputElement | null;
  const hint = document.getElementById("settings-cron-help");
  if (!input || !hint) return;
  input.setCustomValidity(validateCronSchedule(input.value));
  const description = describeCronSchedule(input.value);
  hint.textContent = description.summary;
  input.title = `${description.summary} ${description.help}`;
}

function describeSchedulerState(scheduler: SchedulerStatus | null | undefined): { label: string; help: string } {
  const state = String(scheduler?.state || "idle");
  if (state === "running") {
    return {
      label: "Running a backup cycle",
      help: "Scout is actively scanning, packing, or uploading right now.",
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
    help: "Scout is ready for the next run request.",
  };
}

function describeUploadCircuit(uploadCircuit: UploadCircuit | null | undefined): { label: string; help: string } {
  const failures = Number(uploadCircuit?.consecutive_failures || 0);
  const cooldown = Number(uploadCircuit?.cooldown_remaining_seconds || 0);
  if (uploadCircuit?.state === "open") {
    return {
      label: `Paused after upload failures (${cooldown}s left)`,
      help: "Uploads pause after repeated failures, then retry.",
    };
  }
  return {
    label: failures > 0 ? `Healthy, with ${failures} recent failure${failures === 1 ? "" : "s"}` : "Healthy",
    help: "Uploads are running normally.",
  };
}

function initMeta(): void {
  const pending = '<span class="hint loading-placeholder" role="status">Loading...</span>';
  document.getElementById("meta")!.innerHTML = `
    <div><strong>Scout ID</strong><br><span id="meta-val-scout-id">${pending}</span></div>
    <div><strong>Instance ID</strong><br><span id="meta-val-instance-id">${pending}</span></div>
    <div><strong>Scan Root</strong><br><span id="meta-val-scan-dir">${pending}</span></div>
    <div><strong>Station URL</strong><br><span id="meta-val-station-url">${pending}</span></div>
    <div><strong>Advertised URL</strong><br><span id="meta-val-advertised-url">${pending}</span></div>
    <div><strong>Cron Schedule</strong> <span id="meta-hint-cron"></span><br><span id="meta-val-cron">${pending}</span></div>
    <div><strong>Upload Circuit</strong> <span id="meta-hint-upload-circuit"></span><br><span id="meta-val-upload-circuit">${pending}</span></div>
    <div><strong>Scout Credential</strong><br><span id="meta-val-scout-credential">${pending}</span></div>
    <div class="enc-key-cell">
      <strong>Encryption Key</strong>
      <div class="enc-key-row">
        <code id="enc-key-value">…</code>
        <button type="button" class="secondary enc-key-copy" data-requires="encryption-key" disabled onclick="copyEncKey()">Copy</button>
        <button type="button" class="danger enc-key-rotate" data-requires="encryption-key" disabled onclick="rotateEncKey()">Rotate</button>
      </div>
      <span class="hint" id="meta-val-enc-fingerprint">…</span>
    </div>
  `;
}

function fillMetaFromDir(data: ScoutData): void {
  const uploadCircuit = data.upload_circuit || {};
  const settingsStatus = data.settings_status || {};
  const cronDetails = describeCronSchedule(data.cron_schedule);
  const uploadCircuitDetails = describeUploadCircuit(uploadCircuit);
  const advertisedUrl = String(data.advertised_url || "").trim();

  const set = setHtmlIfChanged;

  set("meta-val-scout-id", renderClipValue("", data.scout_id, { className: "clip-code", clipLength: 28 }));
  set(
    "meta-val-instance-id",
    renderClipValue("", data.scout_instance_id || "—", { className: "clip-code", clipLength: 28 }),
  );
  set("meta-val-scan-dir", renderClipValue("", data.scan_root, { className: "clip-code", clipLength: 34 }));
  set("meta-val-station-url", renderClipValue("", data.station_url, { className: "clip-code", clipLength: 34 }));
  set(
    "meta-val-advertised-url",
    advertisedUrl
      ? renderClipValue("", advertisedUrl, { className: "clip-code", clipLength: 34 })
      : '<span class="hint">Not set</span>',
  );
  set("meta-hint-cron", renderHelpHint(cronDetails.help));
  set(
    "meta-val-cron",
    `<code title="${escapeHtml(`${cronDetails.summary} ${cronDetails.help}`)}">${escapeHtml(data.cron_schedule)}</code><div class="hint">${escapeHtml(cronDetails.summary)}</div>`,
  );
  set("meta-hint-upload-circuit", renderHelpHint(uploadCircuitDetails.help));
  set("meta-val-upload-circuit", escapeHtml(uploadCircuitDetails.label));
  set("meta-val-scout-credential", escapeHtml(settingsStatus.scout_credential_configured ? "configured" : "missing"));
  if (data.encryption_key_fingerprint) {
    set("meta-val-enc-fingerprint", `Fingerprint ${escapeHtml(shortFingerprint(data.encryption_key_fingerprint))}.`);
  }
}

function fillMetaEncKey(key: string, fingerprint: string): void {
  setPanelReady("encryption-key", Boolean(key && fingerprint));
  const keyEl = document.getElementById("enc-key-value");
  if (keyEl) {
    keyEl.dataset.key = key || "";
    keyEl.textContent = key ? "••••••••••••••••••••••••••••••••" : "—";
  }
  if (fingerprint) {
    const fpEl = document.getElementById("meta-val-enc-fingerprint");
    if (fpEl) fpEl.textContent = `Fingerprint ${shortFingerprint(fingerprint)}.`;
  }
}

async function copyEncKey(): Promise<void> {
  const keyEl = document.getElementById("enc-key-value");
  const key = keyEl?.dataset?.key;
  if (!key) return;
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
    setTimeout(() => {
      btn.textContent = "Copy";
    }, 2000);
  }
}

async function rotateEncKey(): Promise<void> {
  if (!requirePanelReady("encryption-key")) return;
  const confirmed = await confirmApp({
    title: "Rotate Encryption Key",
    message: "Future backups use a new key. Keep the old key: older snapshots still need it.\n\nRotate the key?",
    confirmLabel: "Rotate Key",
    danger: true,
  });
  if (!confirmed) return;

  const rotateBtn = document.querySelector<HTMLButtonElement>(".enc-key-rotate");
  if (rotateBtn) rotateBtn.disabled = true;
  try {
    const res = await fetch("/api/encryption-key/rotate", { method: "POST" });
    const body: EncryptionKeyResponse = await res.json();
    if (!res.ok) {
      setActionStatus(body.detail || "Key rotation failed.", "error");
      return;
    }
    fillMetaEncKey(body.key_base64 || "", body.new_fingerprint || "");
    setActionStatus("Key rotated. Copy and save the new key.", "success");
  } catch {
    setActionStatus("Key rotation failed.", "error");
  } finally {
    if (rotateBtn) rotateBtn.disabled = false;
  }
}
