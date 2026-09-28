let fileBrowserPath = ".";
let fileBrowserRequest = 0;

async function cancelOperation(btn) {
  btn.disabled = true;
  try {
    const response = await fetch("/api/cancel-operation", { method: "POST" });
    const body = await response.json();
    if (!response.ok) throw new Error(body.detail || "Cancellation failed.");
    setActionStatus(body.status === "cancelling"
      ? "Cancellation requested. Active backup work is stopping; completed staged archives are kept until you clear them."
      : "No active backup operation to cancel.", "info");
    requestEdgeActiveRefreshBurst();
    await loadData({ silent: true });
  } catch (error) {
    setActionStatus(error.message, "error");
  } finally {
    btn.disabled = false;
  }
}

function browseFilesFromEvent(event, path) {
  stopActionEvent(event);
  openDialog("files-dialog");
  browseFiles(path);
  return false;
}

async function browseFiles(path) {
  const request = ++fileBrowserRequest;
  fileBrowserPath = path;
  document.getElementById("files-path").textContent = path;
  const up = document.getElementById("files-up");
  up.disabled = path === ".";
  up.onclick = () => browseFiles(path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : ".");
  const size = document.getElementById("files-size");
  size.disabled = false;
  size.textContent = "Calculate folder size";
  size.onclick = () => calculateFolderSize(path, size);
  const list = document.getElementById("files-list");
  list.replaceChildren();
  setStatus("files-status", "Loading files…", "info");
  try {
    const response = await fetch(`/api/directories/browse?relative_path=${encodeURIComponent(path)}`);
    const body = await response.json();
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
      const button = (parent, label, action) => {
        const element = document.createElement("button");
        element.type = "button";
        element.className = "secondary";
        element.textContent = label;
        element.onclick = () => action(element);
        parent.appendChild(element);
        return element;
      };
      const name = cell();
      if (entry.kind === "directory" && entry.reason !== "Edge runtime data") {
        button(name, entry.name, () => browseFiles(entry.relative_path));
      } else {
        name.textContent = entry.name;
      }
      cell(entry.kind);
      const sizeCell = cell();
      if (entry.kind === "directory" && entry.reason !== "Edge runtime data") {
        button(sizeCell, "Calculate size", (btn) => calculateFolderSize(entry.relative_path, btn));
      } else if (entry.kind === "file") {
        sizeCell.textContent = formatBytes(entry.size);
        sizeCell.title = `${entry.size.toLocaleString()} bytes`;
      } else {
        sizeCell.textContent = "—";
      }
      cell(entry.reason || (entry.job_path ? `Included in ${entry.job_path}` : "No parent backup job"));
      const actions = cell();
      if (entry.job_path && !entry.excluded && currentUser?.is_admin) {
        button(actions, "Exclude", (btn) => excludeFilePath(entry.relative_path, btn));
      }
      list.appendChild(row);
    }
    setStatus("files-status", body.entries.length ? `${body.entries.length} items` : "This folder is empty.", "info");
  } catch (error) {
    if (request === fileBrowserRequest) setStatus("files-status", error.message, "error");
  }
}

async function calculateFolderSize(path, btn) {
  btn.disabled = true;
  btn.textContent = "Calculating…";
  try {
    const response = await fetch(`/api/directories/size?relative_path=${encodeURIComponent(path)}`);
    const body = await response.json();
    if (!response.ok) throw new Error(body.detail || "Could not calculate size.");
    btn.textContent = `${formatBytes(body.size)} (${body.files} files)`;
    btn.title = `${body.size.toLocaleString()} bytes. Click to recalculate.`;
  } catch (error) {
    btn.textContent = "Retry size";
    setStatus("files-status", error.message, "error");
  } finally {
    btn.disabled = false;
  }
}

async function excludeFilePath(path, btn) {
  btn.disabled = true;
  try {
    const response = await fetch("/api/directories/exclude", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ relative_path: path }),
    });
    const body = await response.json();
    if (!response.ok) throw new Error(body.detail || "Exclusion failed.");
    await loadData({ silent: true, refreshDirectoryTree: true });
    if (document.getElementById("files-dialog").open) await browseFiles(fileBrowserPath);
    setActionStatus(`Excluded ${path} from its parent backup job.`, "success");
  } catch (error) {
    setActionStatus(error.message, "error");
  } finally {
    btn.disabled = false;
  }
}

function excludeFileFromEvent(event, path, btn) {
  stopActionEvent(event);
  excludeFilePath(path, btn);
  return false;
}

function clearStagedFromEvent(event, path, btn) {
  stopActionEvent(event);
  clearStagedBackup(path, btn);
  return false;
}

async function clearStagedBackup(path, btn) {
  if (!await confirmApp({
    title: "Clear staged backup",
    message: `Discard the local staged backup for ${path} and reset its retry state? Source files and backups stored in Central are kept. The next backup can build a fresh archive.`,
    confirmLabel: "Clear staged backup", danger: true,
  })) return;
  btn.disabled = true;
  try {
    const response = await fetch("/api/directories/clear-staged", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ relative_path: path }),
    });
    const body = await response.json();
    if (!response.ok) throw new Error(body.detail || "Could not clear staged backup.");
    await loadData({ silent: true, refreshDirectoryTree: true });
    setActionStatus(`Cleared staged backup for ${path}.`, "success");
  } catch (error) {
    setActionStatus(error.message, "error");
  } finally {
    btn.disabled = false;
  }
}
