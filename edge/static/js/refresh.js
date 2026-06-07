let latestData = null;
let isLoadingData = false;
let _edgeRefreshTimer = null;
let _edgeAutoRefreshStarted = false;
let _edgeRefreshBurstRemaining = 0;

const ACTIVE_JOB_STATUSES = new Set(["scanning", "compressing", "encrypting", "archive_created", "uploading", "force_send_requested", "manual_retry_requested"]);
const EDGE_ACTIVE_REFRESH_MS = 2500;
const EDGE_ACTIVE_REFRESH_BURST_COUNT = 6;
const EDGE_PAUSED_REFRESH_CHECK_MS = 2000;

function edgeHasActiveWork(data = latestData) {
  if (data?.scheduler?.state === "running") return true;
  return (data?.directories || []).some((entry) => ACTIVE_JOB_STATUSES.has(String(entry.state?.last_status || "").trim()));
}

function edgeAutoRefreshPaused() {
  return document.hidden || Boolean(document.querySelector("dialog[open]"));
}

function scheduleEdgeRefresh(delay = EDGE_ACTIVE_REFRESH_MS, { force = false } = {}) {
  if (!_edgeAutoRefreshStarted) return;
  const shouldRefresh = force || edgeHasActiveWork() || _edgeRefreshBurstRemaining > 0;
  if (!shouldRefresh) {
    if (_edgeRefreshTimer) {
      window.clearTimeout(_edgeRefreshTimer);
      _edgeRefreshTimer = null;
    }
    return;
  }
  if (_edgeRefreshTimer) {
    window.clearTimeout(_edgeRefreshTimer);
  }
  _edgeRefreshTimer = window.setTimeout(() => {
    _edgeRefreshTimer = null;
    if (edgeAutoRefreshPaused()) {
      scheduleEdgeRefresh(EDGE_PAUSED_REFRESH_CHECK_MS, { force: true });
      return;
    }
    if (_edgeRefreshBurstRemaining > 0) {
      _edgeRefreshBurstRemaining -= 1;
    }
    loadData({ silent: true, includeKey: false });
  }, delay);
}

function requestEdgeActiveRefreshBurst(count = EDGE_ACTIVE_REFRESH_BURST_COUNT) {
  _edgeRefreshBurstRemaining = Math.max(_edgeRefreshBurstRemaining, count);
  scheduleEdgeRefresh(EDGE_ACTIVE_REFRESH_MS, { force: true });
}

async function loadData({ silent = false, includeKey = true, refreshDirectoryTree = !silent } = {}) {
  if (isLoadingData) {
    scheduleEdgeRefresh(EDGE_ACTIVE_REFRESH_MS);
    return;
  }
  isLoadingData = true;

  if (!silent) {
    const spinner = '<div class="section-loading"><span class="section-spinner" aria-label="Loading…"></span></div>';
    setHtmlIfChanged("selected-jobs", spinner);
    setHtmlIfChanged("selected-jobs-count", "-");
    setHtmlIfChanged("directory-tree", spinner);
  }

  const statusFetch = (async () => {
    const res = await fetch("/api/status");
    if (!res.ok) return;
    const statusData = await res.json();
    latestData = { ...(latestData || {}), ...statusData };
      if (!document.getElementById("settings-dialog")?.open) {
        applyTheme(latestData.settings?.theme || "dark");
      }
    fillMetaFromDir(latestData);
      if (!document.getElementById("settings-dialog")?.open) {
        fillSettings(latestData.settings || {});
      }
  })().catch(() => {});

  const dirFetch = (async () => {
    const res = await fetch("/api/directories");
    if (!res.ok) {
      if (!silent) setActionStatus("Refresh failed.", "error");
      return;
    }
    const dirData = await res.json();
    latestData = { ...(latestData || {}), directories: dirData.directories };
    renderSelectedJobs(dirData.directories);
    if (refreshDirectoryTree) {
      requestAnimationFrame(() => renderDirectoryTree(dirData.directories));
    }
  })().catch((error) => {
    if (!silent) setActionStatus(error.message || "Refresh failed.", "error");
  });

  const keyFetch = includeKey
    ? (async () => {
      const keyRes = await fetch("/api/encryption-key");
      if (!keyRes.ok) return;
      const keyData = await keyRes.json();
      fillMetaEncKey(keyData.key_base64 || "", keyData.fingerprint || latestData?.encryption_key_fingerprint || "");
    })().catch(() => {})
    : null;

  try {
    await Promise.all([statusFetch, dirFetch, keyFetch].filter(Boolean));
  } finally {
    isLoadingData = false;
    scheduleEdgeRefresh();
  }
}
