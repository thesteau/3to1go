interface FileBrowserRow {
  row: HTMLTableRowElement;
  reason: HTMLTableCellElement;
  actions: HTMLTableCellElement;
}

let fileBrowserPath = ".";
let fileBrowserRequest = 0;
// Rows on screen, including those of expanded folders, so an exclusion can update its row in place.
let fileBrowserRows = new Map<string, FileBrowserRow>();
// Folders currently expanded, so a second click collapses them.
let fileBrowserExpanded = new Set<string>();

async function cancelOperation(btn: HTMLButtonElement): Promise<void> {
  btn.disabled = true;
  try {
    const response = await fetch("/api/cancel-operation", { method: "POST" });
    const body: OperationResponse = await response.json();
    if (!response.ok) throw new Error(body.detail || "Cancellation failed.");
    setActionStatus(
      body.status === "cancelling"
        ? "Stopping. Finished archives stay staged until cleared."
        : "No active backup operation to cancel.",
      "info",
    );
    requestScoutActiveRefreshBurst();
    await loadData({ silent: true });
  } catch (error) {
    setActionStatus((error as Error).message, "error");
  } finally {
    btn.disabled = false;
  }
}

function browseFilesFromEvent(event: Event, path: string): false {
  stopActionEvent(event);
  openDialog("files-dialog");
  browseFiles(path);
  return false;
}

async function fetchFolderEntries(path: string): Promise<BrowseEntry[]> {
  const response = await fetch(`/api/directories/browse?relative_path=${encodeURIComponent(path)}`);
  const body: BrowseResponse = await response.json();
  if (!response.ok) throw new Error(body.detail || "Unable to browse this folder.");
  return body.entries;
}

// Shows the job's folder. Folders inside it expand in place instead of replacing the list.
async function browseFiles(path: string): Promise<void> {
  const request = ++fileBrowserRequest;
  fileBrowserPath = path;
  document.getElementById("files-path")!.textContent = path;
  const size = document.getElementById("files-size") as HTMLButtonElement;
  size.disabled = false;
  size.textContent = "Calculate folder size";
  size.onclick = () => calculateFolderSize(path, size);
  const list = document.getElementById("files-list")!;
  list.replaceChildren();
  fileBrowserRows = new Map();
  fileBrowserExpanded = new Set();
  setStatus("files-status", "Loading files…", "info");
  try {
    const entries = await fetchFolderEntries(path);
    if (request !== fileBrowserRequest) return;
    for (const entry of entries) list.appendChild(fileBrowserRow(entry, 0));
    setStatus("files-status", entries.length ? `${entries.length} items` : "This folder is empty.", "info");
  } catch (error) {
    if (request === fileBrowserRequest) setStatus("files-status", (error as Error).message, "error");
  }
}

function fileBrowserRow(entry: BrowseEntry, depth: number): HTMLTableRowElement {
  const expandable = entry.kind === "directory" && entry.reason !== "Scout runtime data";
  const { row, size, details, actions } = browserFileRow({
    name: entry.name,
    kind: entry.kind,
    size: entry.kind === "file" ? entry.size : undefined,
    depth,
    details: entry.reason || (entry.job_path ? `Included in ${entry.job_path}` : "No parent backup job"),
    folder: expandable ? { expanded: false, toggle: (toggle) => toggleFolder(entry, depth, row, toggle) } : undefined,
  });
  if (expandable) {
    size.textContent = "";
    fileBrowserButton(size, "Calculate size", (btn) => calculateFolderSize(entry.relative_path, btn));
  }
  if (entry.job_path && !entry.excluded && currentUser?.is_admin) {
    fileBrowserButton(actions, "Exclude", (btn) => excludeFilePath(entry.relative_path, btn));
  }
  fileBrowserRows.set(entry.relative_path, { row, reason: details!, actions });
  return row;
}

// Expands a folder's contents as indented rows below it, or collapses them again.
async function toggleFolder(
  entry: BrowseEntry,
  depth: number,
  row: HTMLTableRowElement,
  toggle: HTMLButtonElement,
): Promise<void> {
  const path = entry.relative_path;
  if (fileBrowserExpanded.has(path)) {
    collapseFileBrowserFolders(fileBrowserExpanded, path);
    for (const [rowPath, child] of fileBrowserRows) {
      if (!rowPath.startsWith(`${path}/`)) continue;
      child.row.remove();
      fileBrowserRows.delete(rowPath);
    }
    setFileBrowserFolderLabel(toggle, entry.name, false);
    return;
  }
  const request = fileBrowserRequest;
  toggle.disabled = true;
  try {
    const entries = await fetchFolderEntries(path);
    // The dialog moved on to another job, or the row was collapsed with its parent.
    if (request !== fileBrowserRequest || !fileBrowserRows.has(path)) return;
    fileBrowserExpanded.add(path);
    row.after(...entries.map((child) => fileBrowserRow(child, depth + 1)));
    setFileBrowserFolderLabel(toggle, entry.name, true);
    if (!entries.length) setStatus("files-status", `${entry.name} is empty.`, "info");
  } catch (error) {
    if (request === fileBrowserRequest) setStatus("files-status", (error as Error).message, "error");
  } finally {
    toggle.disabled = false;
  }
}

async function calculateFolderSize(path: string, btn: HTMLButtonElement): Promise<void> {
  btn.disabled = true;
  btn.textContent = "Calculating…";
  try {
    const response = await fetch(`/api/directories/size?relative_path=${encodeURIComponent(path)}`);
    const body: FolderSizeResponse = await response.json();
    if (!response.ok) throw new Error(body.detail || "Could not calculate size.");
    btn.textContent = `${formatBytes(body.size)} (${body.files} files)`;
    btn.title = `${body.size.toLocaleString()} bytes. Click to recalculate.`;
  } catch (error) {
    btn.textContent = "Retry size";
    setStatus("files-status", (error as Error).message, "error");
  } finally {
    btn.disabled = false;
  }
}

function directoryTreeItem(path: string): HTMLElement | undefined {
  return Array.from(document.querySelectorAll?.<HTMLElement>("#directory-tree [data-path]") || []).find(
    (element) => element.dataset.path === path,
  );
}

async function excludeFilePath(path: string, btn: HTMLButtonElement): Promise<void> {
  const restore = setButtonBusy(btn, "Excluding…");
  try {
    const response = await fetch("/api/directories/exclude", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ relative_path: path }),
    });
    const body: ApiBody = await response.json();
    if (!response.ok) throw new Error(body.detail || "Exclusion failed.");
    setActionStatus(`Excluded ${path}. The next backup of its parent job skips it.`, "success");
    const fileRow = fileBrowserRows.get(path);
    if (fileRow) {
      fileRow.reason.textContent = "Excluded by job settings";
      fileRow.actions.replaceChildren();
      flashElement(fileRow.row);
    }
    await loadData({ silent: true, refreshDirectoryTree: true });
    // The tree re-renders on the next frame; highlight the fresh element, not the replaced one.
    globalThis.requestAnimationFrame?.(() => flashElement(directoryTreeItem(path)));
  } catch (error) {
    setActionStatus((error as Error).message, "error");
  } finally {
    restore();
  }
}

function excludeFileFromEvent(event: Event, path: string, btn: HTMLButtonElement): false {
  stopActionEvent(event);
  excludeFilePath(path, btn);
  return false;
}

function clearStagedFromEvent(event: Event, path: string, btn: HTMLButtonElement): false {
  stopActionEvent(event);
  clearStagedBackup(path, btn);
  return false;
}

async function clearStagedBackup(path: string, btn: HTMLButtonElement): Promise<void> {
  if (
    !(await confirmApp({
      title: "Clear staged backup",
      message: `Discard the staged backup for ${path}? Your files and Station's backups are kept.`,
      confirmLabel: "Clear staged backup",
      danger: true,
    }))
  )
    return;
  const restore = setButtonBusy(btn, "Clearing…");
  try {
    const response = await fetch("/api/directories/clear-staged", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ relative_path: path }),
    });
    const body: ApiBody = await response.json();
    if (!response.ok) throw new Error(body.detail || "Could not clear staged backup.");
    setActionStatus(`Cleared the staged backup for ${path}. The next backup builds a fresh archive.`, "success");
    await loadData({ silent: true, refreshDirectoryTree: true });
    flashElement(
      Array.from(document.querySelectorAll?.<HTMLElement>("#selected-jobs .job-card") || []).find(
        (card) => card.dataset.path === path,
      ),
    );
  } catch (error) {
    setActionStatus((error as Error).message, "error");
  } finally {
    restore();
  }
}
