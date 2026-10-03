interface OverviewOptions {
  silent?: boolean;
  force?: boolean;
  notifyNewSnapshots?: boolean;
}

interface SnapshotEvent {
  key: string;
  edgeId: string;
  edgeInstanceId: string;
  jobName: string;
  name: string;
}

let _overviewHasData = false;
let _storageLoading = false;

async function loadStorageOverview(): Promise<void> {
  const panel = document.getElementById("storage-meta");
  if (!panel || _storageLoading) return;
  _storageLoading = true;
  try {
    const response = await fetch("/api/overview?section=storage", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    if (!response.ok) throw new Error("Storage status unavailable");
    const data: StorageOverview = await response.json();
    panel.innerHTML = `<div><strong>Storage status</strong><br>${escapeHtml(data.status)}</div>` +
      ([["Backups Used", data.disk_used_bytes], ["Disk Free", data.disk_free_bytes], ["Disk Total", data.disk_total_bytes]] as const)
        .map(([label, value]) => `<div><strong>${label}</strong><br>${typeof value === "number" ? formatBytes(value) : "—"}</div>`).join("");
  } catch {
    panel.innerHTML = '<p role="status">Storage status could not load. <button type="button" onclick="loadStorageOverview()">Retry</button></p>';
  } finally {
    _storageLoading = false;
  }
}

let _settingsInFlight: Promise<void> | null = null;

// Loads settings on their own, so the settings editor opens without waiting
// for the snapshot list. A call during a load waits for it, then loads again,
// so a caller that just saved gets the saved values.
async function loadCentralSettings(): Promise<void> {
  while (_settingsInFlight) await _settingsInFlight;
  _settingsInFlight = fetchCentralSettings();
  try {
    await _settingsInFlight;
  } finally {
    _settingsInFlight = null;
  }
}

async function fetchCentralSettings(): Promise<void> {
  try {
    const response = await fetch("/api/overview?section=settings", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    if (!response.ok) throw new Error("Settings unavailable");
    const data: OverviewResponse = await response.json();
    window.__centralSettings = data.settings || {};
    setPanelReady("settings", Boolean(data.settings && Object.keys(data.settings).length));
    if (!(document.getElementById("settings-dialog") as HTMLDialogElement | null)?.open) {
      applyTheme(window.__centralSettings.theme || "dark");
      fillSettings(window.__centralSettings);
    }
  } catch {
    // Settings loaded earlier stay editable; one failed poll must not lock an open editor.
    if (!window.__centralSettings) setPanelReady("settings", false);
  }
}

let _overviewLoading = false;
let _overviewInFlight: Promise<boolean> | null = null;
let _knownSnapshotKeys: Set<string> | null = null;
const CENTRAL_REFRESH_MS = 15000;

async function manualRefresh(): Promise<void> {
  if (await loadOverview({ notifyNewSnapshots: true })) {
    setActionStatus("Refreshed.", "success");
  }
}

function collectSnapshotEvents(data: OverviewResponse): SnapshotEvent[] {
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

function updateSnapshotArrivalToasts(data: OverviewResponse, { notify = false } = {}): void {
  const events = collectSnapshotEvents(data);
  const nextKeys = new Set(events.map((event) => event.key));
  if (_knownSnapshotKeys === null) {
    _knownSnapshotKeys = nextKeys;
    return;
  }

  const knownKeys = _knownSnapshotKeys;
  const arrivals = events.filter((event) => !knownKeys.has(event.key));
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

function captureOverviewUiState(): { expandedEdges: Set<string> } {
  const expandedEdges = Array.from(document.querySelectorAll<HTMLElement>("#namespaces details[data-edge-id][open]"))
    .map((element) => element.dataset.edgeId)
    .filter((edgeId): edgeId is string => Boolean(edgeId));
  return {
    expandedEdges: new Set(expandedEdges),
  };
}

// Reconcile existing cards in place so refresh never replaces a surviving key input.
// Its value, focus and selection belong to the user, not the overview response.
function updateOverviewDom(container: HTMLElement, html: string): void {
  const focusedInput = container.contains(document.activeElement) && document.activeElement!.matches("[data-edge-key-input]")
    ? document.activeElement as HTMLInputElement : null;
  const selection = focusedInput ? [focusedInput.selectionStart, focusedInput.selectionEnd, focusedInput.selectionDirection] : null;
  const template = document.createElement("template");
  template.innerHTML = html;
  const nodeKey = (node: Node) => node.nodeType === 1
    ? (node as Element).getAttribute("data-edge-id") ?? (node as Element).getAttribute("data-instance-id") ?? (node as Element).getAttribute("data-key-panel") ?? (node as Element).getAttribute("data-edge-key-input")
    : null;
  function syncChildren(target: Node, source: Node): void {
    let cursor: ChildNode | null = target.firstChild;
    for (const next of Array.from(source.childNodes)) {
      const key = nodeKey(next);
      let existing: ChildNode | null | undefined = cursor;
      if (key !== null) {
        existing = Array.from(target.childNodes).find((node) => nodeKey(node) === key && node.nodeName === next.nodeName);
      }
      if (!existing || existing.nodeName !== next.nodeName || nodeKey(existing) !== key) {
        target.insertBefore(next.cloneNode(true), cursor);
        continue;
      }
      if (existing !== cursor) target.insertBefore(existing, cursor);
      if (next.nodeType === 1) {
        const existingElement = existing as Element;
        const nextElement = next as Element;
        // Key inputs are deliberately left untouched, including their live value.
        if (!existingElement.matches("[data-edge-key-input]")) {
          for (const attr of Array.from(existingElement.attributes)) {
            if (!nextElement.hasAttribute(attr.name)) existingElement.removeAttribute(attr.name);
          }
          for (const attr of Array.from(nextElement.attributes)) {
            if (existingElement.getAttribute(attr.name) !== attr.value) existingElement.setAttribute(attr.name, attr.value);
          }
          syncChildren(existingElement, nextElement);
        }
      } else if (existing.nodeValue !== next.nodeValue) {
        existing.nodeValue = next.nodeValue;
      }
      cursor = existing.nextSibling;
    }
    while (cursor) {
      const obsolete: ChildNode = cursor;
      cursor = cursor.nextSibling;
      obsolete.remove();
    }
  }
  syncChildren(container, template.content);
  // Moving a card after server-side reordering can blur a retained input.
  if (focusedInput?.isConnected && document.activeElement !== focusedInput) {
    focusedInput.focus({ preventScroll: true });
    focusedInput.setSelectionRange(...(selection as [number | null, number | null, "forward" | "backward" | "none" | undefined]));
  }
}

async function loadOverview(options: OverviewOptions = {}): Promise<boolean> {
  if (_overviewLoading) {
    if (!options.force) return false;
    // A refresh after a change needs data fetched after that change, not the poll in flight.
    await _overviewInFlight;
    return loadOverview(options);
  }
  _overviewInFlight = fetchOverview(options);
  return _overviewInFlight;
}

async function fetchOverview({ silent = false, notifyNewSnapshots = false, force = false }: OverviewOptions = {}): Promise<boolean> {
  _overviewLoading = true;
  // A forced refresh follows a change, so it waits for any older settings
  // request and loads again; otherwise that older response could restore old values.
  if (force || !_settingsInFlight) loadCentralSettings();
  loadStorageOverview();
  if (!silent && !document.getElementById("namespaces")!.children.length) {
    document.getElementById("namespaces")!.innerHTML = '<div class="section-loading"><span class="section-spinner" aria-label="Loading…"></span></div>';
  }

  try {
    const res = await fetch("/api/overview?section=snapshots", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    if (!res.ok) {
      throw new Error("Refresh failed.");
    }
    const data: OverviewResponse = await res.json();
    updateSnapshotArrivalToasts(data, { notify: notifyNewSnapshots });

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

    document.getElementById("meta")!.innerHTML = `
      <div><strong>Edges</strong> ${renderHelpHint("Unique Edge device IDs that have stored at least one snapshot on this Central.")}<br>${totalEdges}</div>
      <div><strong>Instances</strong> ${renderHelpHint("Each reinstall or unique Edge setup shows as a separate instance under the same Edge ID.")}<br>${totalInstances}</div>
      <div><strong>Jobs</strong> ${renderHelpHint("Named backup jobs across all instances. Each job backs up one source directory on an Edge device.")}<br>${totalJobs}</div>
      <div><strong>Snapshots</strong> ${renderHelpHint("Total backup snapshots stored on Central, across all edges, instances, and jobs.")}<br>${totalSnapshots}</div>
      <div><strong>Backup Root</strong><br>${escapeHtml(data.backup_dir)}</div>
      <div><strong>Retention</strong><br>keep last ${escapeHtml(String(data.retention_keep_last))} snapshots</div>
    `;

    // Capture after the request: edits and expanded cards may change while it is in flight.
    const uiState = captureOverviewUiState();
    const overviewHtml = edges.length
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

    updateOverviewDom(document.getElementById("namespaces")!, overviewHtml);
    _overviewHasData = true;
    Promise.allSettled(
      allInstances
        .filter(({ instance }) => instance.edge_instance_id)
        .map(({ edgeId, instance }) => refreshKeyPanel(edgeId, instance.edge_instance_id)),
    );
  } catch (error) {
    if (!_overviewHasData) {
      document.getElementById("namespaces")!.innerHTML = '<p role="status">Snapshots could not load. <button type="button" onclick="loadOverview()">Retry</button></p>';
      document.getElementById("meta")!.innerHTML = '<p class="hint">Snapshot summary unavailable.</p>';
    }
    if (!silent) {
      setActionStatus((error as Error).message || "Refresh failed.", "error");
    }
    return false;
  } finally {
    _overviewLoading = false;
  }
  loadVerifyStatus();
  return true;
}
