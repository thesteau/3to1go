function renderVerifyResult(data: VerifyResult | null): string {
  if (!data || data.status === "never_run") {
    const interval = window.__stationSettings?.snapshot_verify_interval_hours || 0;
    const hint =
      interval > 0
        ? `Automatic check every ${interval}h. No run yet since last start.`
        : "Automatic checks are disabled. Set an interval in Station Settings to enable.";
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
  const failureDetail =
    hasFailures && data.last_failure
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

async function loadVerifyStatus(): Promise<void> {
  const el = document.getElementById("verify-status");
  if (!el) return;
  try {
    const res = await fetch("/api/admin/verify");
    if (!res.ok) return;
    const data: VerifyResult = await res.json();
    el.innerHTML = renderVerifyResult(data);
  } catch {
    // non-critical, don't surface errors
  }
}

async function runVerifyNow(): Promise<void> {
  const btn = document.getElementById("verify-run-btn") as HTMLButtonElement | null;
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
    const data: VerifyResult = await res.json();
    if (el) el.innerHTML = renderVerifyResult(data);
    if (data.failure_count > 0) {
      setActionStatus(
        `Integrity check found ${data.failure_count} failure${data.failure_count !== 1 ? "s" : ""}.`,
        "error",
      );
    } else {
      setActionStatus(
        `Integrity check passed — ${data.total_checked} snapshot${data.total_checked !== 1 ? "s" : ""} verified.`,
        "success",
      );
    }
  } catch {
    setActionStatus("Integrity check failed to run.", "error");
    await loadVerifyStatus();
  }
}
