let latestData = null;
let isLoadingData = false;
let _edgeRefreshTimer = null;
let _edgeAutoRefreshStarted = false;
let _edgeRefreshBurstRemaining = 0;

const ACTIVE_JOB_STATUSES = new Set(["scanning", "compressing", "encrypting", "archive_created", "uploading", "force_send_requested", "manual_retry_requested"]);
const EDGE_ACTIVE_REFRESH_MS = 2500;
const EDGE_IDLE_REFRESH_MS = 15000;
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
    delay = EDGE_IDLE_REFRESH_MS;
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

  if (!latestData?.directories) {
    const spinner = '<div class="section-loading" role="status"><span class="section-spinner" aria-hidden="true"></span><span>Loading…</span></div>';
    setHtmlIfChanged("selected-jobs", spinner);
    setHtmlIfChanged("selected-jobs-count", "-");
    setHtmlIfChanged("directory-tree", spinner);
  }

  const statusFetch = (async () => {
    const res = await fetch("/api/status", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    if (!res.ok) throw new Error("Status unavailable");
    const statusData = await res.json();
    latestData = { ...(latestData || {}), ...statusData };
      if (!document.getElementById("settings-dialog")?.open) {
        applyTheme(latestData.settings?.theme || "dark");
      }
    fillMetaFromDir(latestData);
    setPanelReady("settings", Boolean(statusData.settings && Object.keys(statusData.settings).length));
    setHtmlIfChanged("meta-load-status", "");
  })().catch(() => {
    // Settings loaded earlier stay editable; one failed poll must not lock an open editor.
    if (!latestData?.settings) setPanelReady("settings", false);
    setHtmlIfChanged("meta-load-status", '<p role="status">Status could not load. <button type="button" onclick="loadData()">Retry</button></p>');
  });

  const dirFetch = (async () => {
    const res = await fetch("/api/directories", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    if (!res.ok) {
      throw new Error("Directories could not load.");
    }
    const dirData = await res.json();
    const firstLoad = !latestData?.directories;
    latestData = { ...(latestData || {}), directories: dirData.directories };
    renderSelectedJobs(dirData.directories);
    if (refreshDirectoryTree || firstLoad) {
      requestAnimationFrame(() => renderDirectoryTree(dirData.directories));
    }
  })().catch((error) => {
    if (!latestData?.directories) {
      const failure = '<p role="status">Folders and jobs could not load. <button type="button" onclick="loadData()">Retry</button></p>';
      setHtmlIfChanged("selected-jobs", failure);
      setHtmlIfChanged("directory-tree", failure);
    }
    if (!silent) setActionStatus(error.message || "Refresh failed.", "error");
  });

  const keyFetch = includeKey
    ? (async () => {
      setPanelReady("encryption-key", false);
      const keyRes = await fetch("/api/encryption-key", { signal: globalThis.AbortSignal?.timeout?.(30000) });
      if (!keyRes.ok) throw new Error("Key unavailable");
      const keyData = await keyRes.json();
      fillMetaEncKey(keyData.key_base64 || "", keyData.fingerprint || latestData?.encryption_key_fingerprint || "");
    })().catch(() => {
      if (!document.getElementById("enc-key-value")?.dataset?.key) {
        setHtmlIfChanged("enc-key-value", 'Unavailable <button type="button" class="secondary" onclick="loadData()">Retry</button>');
      }
    })
    : null;

  try {
    await Promise.all([statusFetch, dirFetch, keyFetch].filter(Boolean));
  } finally {
    isLoadingData = false;
    scheduleEdgeRefresh();
  }
}
