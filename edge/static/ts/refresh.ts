interface LoadDataOptions {
  silent?: boolean;
  includeKey?: boolean;
  refreshDirectoryTree?: boolean;
}

let latestData: EdgeData | null = null;
let isLoadingData = false;
let _loadDataInFlight: Promise<void> | null = null;
let _edgeRefreshTimer: number | null = null;
let _edgeAutoRefreshStarted = false;
let _edgeRefreshBurstRemaining = 0;

const ACTIVE_JOB_STATUSES = new Set(["scanning", "compressing", "encrypting", "archive_created", "uploading", "force_send_requested", "manual_retry_requested"]);
const EDGE_ACTIVE_REFRESH_MS = 2500;
const EDGE_IDLE_REFRESH_MS = 15000;
const EDGE_ACTIVE_REFRESH_BURST_COUNT = 6;
const EDGE_PAUSED_REFRESH_CHECK_MS = 2000;

// The last job list and top-level folders, kept for this tab only, so a reload
// shows them at once while fresh data loads. Settings are never kept here,
// because they include the Central credential.
const EDGE_VIEW_CACHE = "3to1go-edge-view";

interface EdgeViewCache {
  directories?: DirectoryEntry[];
  discovering?: boolean;
  topLevel?: DirectoryNode[];
}

function saveEdgeView(): void {
  try {
    const view: EdgeViewCache = {
      directories: latestData?.directories,
      discovering: latestData?.jobs_discovering,
      topLevel: directoryChildren.get("."),
    };
    sessionStorage.setItem(EDGE_VIEW_CACHE, JSON.stringify(view));
  } catch {
    // Storage can be full or blocked; the page works without it.
  }
}

function restoreEdgeView(): void {
  try {
    const view: EdgeViewCache = JSON.parse(sessionStorage.getItem(EDGE_VIEW_CACHE) || "null");
    if (!view) return;
    if (Array.isArray(view.directories)) {
      latestData = { ...(latestData || {}), directories: view.directories, jobs_discovering: Boolean(view.discovering) };
      renderSelectedJobs(view.directories, Boolean(view.discovering));
    }
    if (Array.isArray(view.topLevel) && !directoryTreeLoaded()) {
      directoryChildren.set(".", view.topLevel);
      renderDirectoryTree();
    }
  } catch {
    // A missing or unreadable copy just means a normal load.
  }
}

function clearEdgeView(): void {
  try {
    sessionStorage.removeItem(EDGE_VIEW_CACHE);
  } catch {
    // Nothing to clear.
  }
}

function edgeHasActiveWork(data: EdgeData | null = latestData): boolean {
  // Check back soon while the first search for jobs is still running.
  if (data?.scheduler?.state === "running" || data?.jobs_discovering) return true;
  return (data?.directories || []).some((entry) => ACTIVE_JOB_STATUSES.has(String(entry.state?.last_status || "").trim()));
}

function edgeAutoRefreshPaused(): boolean {
  return document.hidden || Boolean(document.querySelector("dialog[open]"));
}

function scheduleEdgeRefresh(delay = EDGE_ACTIVE_REFRESH_MS, { force = false } = {}): void {
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

function requestEdgeActiveRefreshBurst(count = EDGE_ACTIVE_REFRESH_BURST_COUNT): void {
  _edgeRefreshBurstRemaining = Math.max(_edgeRefreshBurstRemaining, count);
  scheduleEdgeRefresh(EDGE_ACTIVE_REFRESH_MS, { force: true });
}

async function loadData(options: LoadDataOptions = {}): Promise<void> {
  if (isLoadingData) {
    // An action refreshing after a change needs data fetched after that change, not the poll in flight.
    await _loadDataInFlight;
    return loadData(options);
  }
  isLoadingData = true;
  _loadDataInFlight = fetchEdgeData(options);
  return _loadDataInFlight;
}

async function fetchEdgeData({ silent = false, includeKey = true, refreshDirectoryTree = !silent }: LoadDataOptions = {}): Promise<void> {
  const spinner = '<div class="section-loading" role="status"><span class="section-spinner" aria-hidden="true"></span><span>Loading…</span></div>';
  if (!latestData?.directories) {
    setHtmlIfChanged("selected-jobs", spinner);
    setHtmlIfChanged("selected-jobs-count", "-");
  }
  if (!directoryTreeLoaded()) setHtmlIfChanged("directory-tree", spinner);

  const statusFetch = (async () => {
    const res = await fetch("/api/status", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    if (!res.ok) throw new Error("Status unavailable");
    const statusData: StatusResponse = await res.json();
    latestData = { ...(latestData || {}), ...statusData };
      if (!(document.getElementById("settings-dialog") as HTMLDialogElement | null)?.open) {
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
      throw new Error("Jobs could not load.");
    }
    const dirData: DirectoriesResponse = await res.json();
    latestData = { ...(latestData || {}), directories: dirData.directories, jobs_discovering: Boolean(dirData.discovering) };
    renderSelectedJobs(dirData.directories, Boolean(dirData.discovering));
    // Keeps "contains selected job" current; unchanged markup leaves the DOM alone.
    renderDirectoryTree();
  })().catch((error) => {
    if (!latestData?.directories) {
      setHtmlIfChanged("selected-jobs", '<p role="status">Jobs could not load. <button type="button" onclick="loadData()">Retry</button></p>');
    }
    if (!silent) setActionStatus(error.message || "Refresh failed.", "error");
  });

  // Polls fetch only the job list; the folder tree reloads on first load and after changes.
  const treeFetch = refreshDirectoryTree || !directoryTreeLoaded()
    ? reloadDirectoryTree().catch((error) => {
      if (!directoryTreeLoaded()) {
        setHtmlIfChanged("directory-tree", '<p role="status">Folders could not load. <button type="button" onclick="loadData()">Retry</button></p>');
      }
      if (!silent) setActionStatus(error.message || "Refresh failed.", "error");
    })
    : null;

  const keyFetch = includeKey
    ? (async () => {
      setPanelReady("encryption-key", false);
      const keyRes = await fetch("/api/encryption-key", { signal: globalThis.AbortSignal?.timeout?.(30000) });
      if (!keyRes.ok) throw new Error("Key unavailable");
      const keyData: EncryptionKeyResponse = await keyRes.json();
      fillMetaEncKey(keyData.key_base64 || "", keyData.fingerprint || latestData?.encryption_key_fingerprint || "");
    })().catch(() => {
      if (!document.getElementById("enc-key-value")?.dataset?.key) {
        setHtmlIfChanged("enc-key-value", 'Unavailable <button type="button" class="secondary" onclick="loadData()">Retry</button>');
      }
    })
    : null;

  try {
    await Promise.all([statusFetch, dirFetch, treeFetch, keyFetch].filter(Boolean));
    saveEdgeView();
  } finally {
    isLoadingData = false;
    scheduleEdgeRefresh();
  }
}
