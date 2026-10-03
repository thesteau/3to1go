interface FileBrowserRow {
  row: HTMLTableRowElement;
  reason: HTMLTableCellElement;
  actions: HTMLTableCellElement;
}

let fileBrowserPath = ".";
let fileBrowserRequest = 0;
// Rows of the folder on screen, so an exclusion can update its row in place.
let fileBrowserRows = new Map<string, FileBrowserRow>();

async function cancelOperation(btn: HTMLButtonElement): Promise<void> {
  btn.disabled = true;
  try {
    const response = await fetch("/api/cancel-operation", { method: "POST" });
    const body: OperationResponse = await response.json();
    if (!response.ok) throw new Error(body.detail || "Cancellation failed.");
    setActionStatus(
      body.status === "cancelling"
        ? "Cancellation requested. Active backup work is stopping; completed staged archives are kept until you clear them."
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

async function browseFiles(path: string): Promise<void> {
  const request = ++fileBrowserRequest;
  fileBrowserPath = path;
  document.getElementById("files-path")!.textContent = path;
  const up = document.getElementById("files-up") as HTMLButtonElement;
  up.disabled = path === ".";
  up.onclick = () => browseFiles(path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : ".");
  const size = document.getElementById("files-size") as HTMLButtonElement;
  size.disabled = false;
  size.textContent = "Calculate folder size";
  size.onclick = () => calculateFolderSize(path, size);
  const list = document.getElementById("files-list")!;
  list.replaceChildren();
  fileBrowserRows = new Map();
  setStatus("files-status", "Loading files…", "info");
  try {
    const response = await fetch(`/api/directories/browse?relative_path=${encodeURIComponent(path)}`);
    const body: BrowseResponse = await response.json();
    if (request !== fileBrowserRequest) return;
    if (!response.ok) throw new Error(body.detail || "Unable to browse this folder.");
    for (const entry of body.entries) {
      const row = document.createElement("tr");
      const cell = (text = "") => {
        const element = document.createElement("td");
        element.textContent = text;
        row.appendChild(element);
        return element;
      };
      const button = (parent: HTMLElement, label: string, action: (element: HTMLButtonElement) => unknown) => {
        const element = document.createElement("button");
        element.type = "button";
        element.className = "secondary";
        element.textContent = label;
        element.onclick = () => action(element);
        parent.appendChild(element);
        return element;
      };
      const name = cell();
      if (entry.kind === "directory" && entry.reason !== "Scout runtime data") {
        button(name, entry.name, () => browseFiles(entry.relative_path));
      } else {
        name.textContent = entry.name;
      }
      cell(entry.kind);
      const sizeCell = cell();
      if (entry.kind === "directory" && entry.reason !== "Scout runtime data") {
        button(sizeCell, "Calculate size", (btn) => calculateFolderSize(entry.relative_path, btn));
      } else if (entry.kind === "file") {
        sizeCell.textContent = formatBytes(entry.size);
        sizeCell.title = `${entry.size.toLocaleString()} bytes`;
      } else {
        sizeCell.textContent = "—";
      }
      const reason = cell(entry.reason || (entry.job_path ? `Included in ${entry.job_path}` : "No parent backup job"));
      const actions = cell();
      if (entry.job_path && !entry.excluded && currentUser?.is_admin) {
        button(actions, "Exclude", (btn) => excludeFilePath(entry.relative_path, btn));
      }
      fileBrowserRows.set(entry.relative_path, { row, reason, actions });
      list.appendChild(row);
    }
    setStatus("files-status", body.entries.length ? `${body.entries.length} items` : "This folder is empty.", "info");
  } catch (error) {
    if (request === fileBrowserRequest) setStatus("files-status", (error as Error).message, "error");
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
      message: `Discard the local staged backup for ${path} and reset its retry state? Source files and backups stored in Station are kept. The next backup can build a fresh archive.`,
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
