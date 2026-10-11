interface LoadDataOptions {
  silent?: boolean;
  includeKey?: boolean;
  refreshDirectoryTree?: boolean;
}

let latestData: ScoutData | null = null;
let isLoadingData = false;
let _loadDataInFlight: Promise<void> | null = null;
let _scoutRefreshTimer: number | null = null;
let _scoutAutoRefreshStarted = false;
let _scoutRefreshBurstRemaining = 0;

const ACTIVE_JOB_STATUSES = new Set([
  "scanning",
  "compressing",
  "encrypting",
  "archive_created",
  "uploading",
  "force_send_requested",
  "manual_retry_requested",
]);
const SCOUT_ACTIVE_REFRESH_MS = 2500;
const SCOUT_IDLE_REFRESH_MS = 15000;
const SCOUT_ACTIVE_REFRESH_BURST_COUNT = 6;
const SCOUT_PAUSED_REFRESH_CHECK_MS = 2000;

// The last job list and top-level folders, kept for this tab only, so a reload
// shows them at once while fresh data loads. Settings are never kept here,
// because they include the Station credential.
const SCOUT_VIEW_CACHE = "3to1go-scout-view";

interface ScoutViewCache {
  directories?: DirectoryEntry[];
  discovering?: boolean;
  topLevel?: DirectoryNode[];
}

function saveScoutView(): void {
  try {
    const view: ScoutViewCache = {
      directories: latestData?.directories,
      discovering: latestData?.jobs_discovering,
      topLevel: directoryChildren.get("."),
    };
    sessionStorage.setItem(SCOUT_VIEW_CACHE, JSON.stringify(view));
  } catch {
    // Storage can be full or blocked; the page works without it.
  }
}

function restoreScoutView(): void {
  try {
    const view: ScoutViewCache = JSON.parse(sessionStorage.getItem(SCOUT_VIEW_CACHE) || "null");
    if (!view) return;
    if (Array.isArray(view.directories)) {
      latestData = {
        ...(latestData || {}),
        directories: view.directories,
        jobs_discovering: Boolean(view.discovering),
      };
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

function clearScoutView(): void {
  try {
    sessionStorage.removeItem(SCOUT_VIEW_CACHE);
  } catch {
    // Nothing to clear.
  }
}

function scoutHasActiveWork(data: ScoutData | null = latestData): boolean {
  // Check back soon while the first search for jobs is still running.
  if (data?.scheduler?.state === "running" || data?.jobs_discovering) return true;
  return (data?.directories || []).some((entry) =>
    ACTIVE_JOB_STATUSES.has(String(entry.state?.last_status || "").trim()),
  );
}

function scoutAutoRefreshPaused(): boolean {
  return document.hidden || Boolean(document.querySelector("dialog[open]"));
}

function scheduleScoutRefresh(delay = SCOUT_ACTIVE_REFRESH_MS, { force = false } = {}): void {
  if (!_scoutAutoRefreshStarted) return;
  const shouldRefresh = force || scoutHasActiveWork() || _scoutRefreshBurstRemaining > 0;
  if (!shouldRefresh) {
    delay = SCOUT_IDLE_REFRESH_MS;
  }
  if (_scoutRefreshTimer) {
    window.clearTimeout(_scoutRefreshTimer);
  }
  _scoutRefreshTimer = window.setTimeout(() => {
    _scoutRefreshTimer = null;
    if (scoutAutoRefreshPaused()) {
      scheduleScoutRefresh(SCOUT_PAUSED_REFRESH_CHECK_MS, { force: true });
      return;
    }
    if (_scoutRefreshBurstRemaining > 0) {
      _scoutRefreshBurstRemaining -= 1;
    }
    loadData({ silent: true, includeKey: false });
  }, delay);
}

function requestScoutActiveRefreshBurst(count = SCOUT_ACTIVE_REFRESH_BURST_COUNT): void {
  _scoutRefreshBurstRemaining = Math.max(_scoutRefreshBurstRemaining, count);
  scheduleScoutRefresh(SCOUT_ACTIVE_REFRESH_MS, { force: true });
}

async function loadData(options: LoadDataOptions = {}): Promise<void> {
  if (isLoadingData) {
    // An action refreshing after a change needs data fetched after that change, not the poll in flight.
    await _loadDataInFlight;
    return loadData(options);
  }
  isLoadingData = true;
  _loadDataInFlight = fetchScoutData(options);
  return _loadDataInFlight;
}

async function fetchScoutData({
  silent = false,
  includeKey = true,
  refreshDirectoryTree = !silent,
}: LoadDataOptions = {}): Promise<void> {
  const spinner =
    '<div class="section-loading" role="status"><span class="section-spinner" aria-hidden="true"></span><span>Loading…</span></div>';
  if (!latestData?.directories) {
    setHtmlIfChanged("selected-jobs", spinner);
    setHtmlIfChanged("selected-jobs-count", "-");
  }
  if (!directoryTreeLoaded()) setHtmlIfChanged("directory-tree", spinner);

  const statusFetch = (async () => {
    const user = currentUser;
    const res = await fetch("/api/status", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    if (!res.ok) throw new Error("Status unavailable");
    const statusData: StatusResponse = await res.json();
    if (currentUser !== user || !currentUser?.is_admin) delete statusData.settings;
    latestData = { ...(latestData || {}), ...statusData, settings: statusData.settings };
    if (!(document.getElementById("settings-dialog") as HTMLDialogElement | null)?.open) {
      applyTheme(latestData.settings?.theme || "dark");
    }
    fillMetaFromDir(latestData);
    setPanelReady("settings", Boolean(statusData.settings && Object.keys(statusData.settings).length));
    setHtmlIfChanged("meta-load-status", "");
  })().catch(() => {
    // Settings loaded earlier stay editable; one failed poll must not lock an open editor.
    if (!latestData?.settings) setPanelReady("settings", false);
    setHtmlIfChanged(
      "meta-load-status",
      '<p role="status">Status could not load. <button type="button" onclick="loadData()">Retry</button></p>',
    );
  });

  const dirFetch = (async () => {
    const res = await fetch("/api/directories", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    if (!res.ok) {
      throw new Error("Jobs could not load.");
    }
    const dirData: DirectoriesResponse = await res.json();
    latestData = {
      ...(latestData || {}),
      directories: dirData.directories,
      jobs_discovering: Boolean(dirData.discovering),
    };
    renderSelectedJobs(dirData.directories, Boolean(dirData.discovering));
    // Keeps "contains selected job" current; unchanged markup leaves the DOM alone.
    renderDirectoryTree();
  })().catch((error) => {
    if (!latestData?.directories) {
      setHtmlIfChanged(
        "selected-jobs",
        '<p role="status">Jobs could not load. <button type="button" onclick="loadData()">Retry</button></p>',
      );
    }
    if (!silent) setActionStatus(error.message || "Refresh failed.", "error");
  });

  // Polls fetch only the job list; the folder tree reloads on first load and after changes.
  const treeFetch =
    refreshDirectoryTree || !directoryTreeLoaded()
      ? reloadDirectoryTree().catch((error) => {
          if (!directoryTreeLoaded()) {
            setHtmlIfChanged(
              "directory-tree",
              '<p role="status">Folders could not load. <button type="button" onclick="loadData()">Retry</button></p>',
            );
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
          setHtmlIfChanged(
            "enc-key-value",
            'Unavailable <button type="button" class="secondary" onclick="loadData()">Retry</button>',
          );
        }
      })
    : null;

  // Station holds restore requests; check for new ones with each refresh. Wait for
  // the job list so each request's destination can default to its job's folder.
  const restoreFetch = dirFetch.then(() => loadRestoreRequests({ silent }));

  try {
    await Promise.all([statusFetch, dirFetch, treeFetch, keyFetch, restoreFetch].filter(Boolean));
    saveScoutView();
  } finally {
    isLoadingData = false;
    scheduleScoutRefresh();
  }
}
