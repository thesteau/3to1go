interface SnapshotView {
  keyId: string;
  filename: string;
  controller: AbortController;
  archive: SnapshotArchive | null;
  folders: Map<string, number[]>;
  expanded: Set<string>;
  selected: Set<number>;
  page: number;
  downloading: boolean;
  prompting: boolean;
  previewGeneration: number;
  previewURL: string | null;
}

let snapshotView: SnapshotView | null = null;
const SNAPSHOT_VIEW_PAGE_SIZE = 100;
const SNAPSHOT_TEXT_PREVIEW_BYTES = 2 * 1024 * 1024;
const SNAPSHOT_IMAGE_PREVIEW_BYTES = 10 * 1024 * 1024;

function clearSnapshotPreview(): void {
  const view = snapshotView;
  if (view) {
    view.previewGeneration++;
    if (view.previewURL) URL.revokeObjectURL(view.previewURL);
    view.previewURL = null;
  }
  document.getElementById("snapshot-view-preview-body")?.replaceChildren();
  const preview = document.getElementById("snapshot-view-preview");
  if (preview) preview.hidden = true;
}

function closeSnapshotView(): void {
  const view = snapshotView;
  if (!view) return;
  clearSnapshotPreview();
  snapshotView = null;
  view.controller.abort();
  view.archive = null;
  view.folders.clear();
  view.expanded.clear();
  view.selected.clear();
  // Do not leave a Scout key prompt for a viewer that has already been closed.
  if (view.prompting) resolveAppDialog(false);
  closeDialog("snapshot-view-dialog");
  const dialog = document.getElementById("snapshot-view-dialog") as HTMLDialogElement | null;
  if (dialog) {
    dialog.onclose = null;
    dialog.oncancel = null;
  }
  for (const id of [
    "snapshot-view-files",
    "snapshot-view-status",
    "snapshot-view-source",
    "snapshot-view-count",
    "snapshot-view-page",
    "snapshot-view-preview-title",
  ]) {
    document.getElementById(id)?.replaceChildren();
  }
  const search = document.getElementById("snapshot-view-search") as HTMLInputElement | null;
  if (search) search.value = "";
  const content = document.getElementById("snapshot-view-content");
  if (content) content.hidden = true;
}

function closeSnapshotViewForKey(keyId: string): void {
  if (snapshotView?.keyId === keyId) closeSnapshotView();
}

async function openSnapshotView(
  scoutId: string,
  scoutInstanceId: string | null,
  jobName: string,
  filename: string,
  btn: HTMLButtonElement,
): Promise<void> {
  if (snapshotView) return;
  const view: SnapshotView = {
    keyId: buildScoutKeyId(scoutId, scoutInstanceId),
    filename,
    controller: new AbortController(),
    archive: null,
    folders: new Map(),
    expanded: new Set(),
    selected: new Set(),
    page: 0,
    downloading: false,
    prompting: false,
    previewGeneration: 0,
    previewURL: null,
  };
  snapshotView = view;
  const dialog = document.getElementById("snapshot-view-dialog") as HTMLDialogElement;
  dialog.onclose = () => {
    if (snapshotView === view) closeSnapshotView();
  };
  dialog.oncancel = () => closeSnapshotView();
  document.getElementById("snapshot-view-source")!.textContent = `${jobName}: ${filename}`;
  const status = document.getElementById("snapshot-view-status")!;
  const content = document.getElementById("snapshot-view-content")!;
  content.hidden = true;
  status.textContent = "Checking Scout key…";
  openDialog("snapshot-view-dialog");
  const restore = setButtonBusy(btn, "Opening…");
  try {
    // Resolve before requesting data. Canceling a key prompt never reads the snapshot.
    view.prompting = true;
    const key = await resolveEncKey(scoutId, scoutInstanceId);
    view.prompting = false;
    if (snapshotView !== view) return;
    if (!key) {
      closeSnapshotView();
      return;
    }
    status.textContent = "Downloading and decrypting snapshot…";
    const blob = await loadSnapshotBlob(scoutId, scoutInstanceId, jobName, filename, view.controller.signal);
    if (snapshotView !== view) return;
    if (!blob) {
      status.textContent = "Could not open this snapshot. Close the popup and retry.";
      return;
    }
    status.textContent = "Reading snapshot files…";
    const tar = await decompressSnapshotArchive(blob, view.controller.signal);
    const archive = await indexSnapshotTar(tar, view.controller.signal);
    if (snapshotView !== view) return;
    view.archive = archive;
    archive.entries.forEach((entry, index) => {
      const parent = entry.path.slice(0, Math.max(0, entry.path.lastIndexOf("/")));
      const children = view.folders.get(parent) || [];
      children.push(index);
      view.folders.set(parent, children);
    });
    for (const children of view.folders.values()) {
      children.sort(
        (a, b) =>
          Number(archive.entries[b].directory) - Number(archive.entries[a].directory) ||
          archive.entries[a].path.localeCompare(archive.entries[b].path),
      );
    }
    const files = archive.entries.filter((entry) => !entry.directory).length;
    status.textContent = `${files.toLocaleString()} file${files === 1 ? "" : "s"} in this snapshot.`;
    content.hidden = false;
    renderSnapshotView();
  } catch (error) {
    if (snapshotView !== view) return;
    // Archive parsing failures do not mean that the Scout key was wrong.
    status.textContent = `Could not read this snapshot. ${(error as Error).message || "Download the full archive or retry."}`;
  } finally {
    restore();
  }
}

function visibleSnapshotEntries(view: SnapshotView): { entry: SnapshotArchiveEntry; index: number; depth: number }[] {
  const search = (document.getElementById("snapshot-view-search") as HTMLInputElement).value.trim().toLocaleLowerCase();
  const entries = view.archive?.entries || [];
  if (search)
    return entries
      .map((entry, index) => ({ entry, index, depth: 0 }))
      .filter(({ entry }) => entry.path.toLocaleLowerCase().includes(search))
      .sort((a, b) => a.entry.path.localeCompare(b.entry.path));
  const visible: { entry: SnapshotArchiveEntry; index: number; depth: number }[] = [];
  const stack = (view.folders.get("") || []).map((index) => ({ index, depth: 0 })).reverse();
  while (stack.length) {
    const { index, depth } = stack.pop()!;
    const entry = entries[index];
    visible.push({ entry, index, depth });
    if (entry.directory && view.expanded.has(entry.path)) {
      for (const child of [...(view.folders.get(entry.path) || [])].reverse())
        stack.push({ index: child, depth: depth + 1 });
    }
  }
  return visible;
}

function renderSnapshotView(resetPage = true): void {
  const view = snapshotView;
  if (!view?.archive) return;
  if (resetPage) view.page = 0;
  const visible = visibleSnapshotEntries(view);
  const pages = Math.max(1, Math.ceil(visible.length / SNAPSHOT_VIEW_PAGE_SIZE));
  view.page = Math.min(view.page, pages - 1);
  const search = (document.getElementById("snapshot-view-search") as HTMLInputElement).value.trim();
  const list = document.getElementById("snapshot-view-files")!;
  list.replaceChildren();
  const shown = visible.slice(view.page * SNAPSHOT_VIEW_PAGE_SIZE, (view.page + 1) * SNAPSHOT_VIEW_PAGE_SIZE);
  for (const { entry, index, depth } of shown) {
    const name = search ? entry.path : entry.path.split("/").at(-1)!;
    const { row, actions } = browserFileRow({
      name,
      kind: entry.directory ? "directory" : "file",
      size: entry.directory ? undefined : entry.size,
      depth,
      selection: {
        checked: view.selected.has(index),
        disabled: view.downloading,
        label: `Select ${entry.path}`,
        change: (checked) => toggleSnapshotFile(index, checked),
      },
      folder: entry.directory
        ? { expanded: view.expanded.has(entry.path), toggle: () => toggleSnapshotViewFolder(entry.path) }
        : undefined,
      preview: entry.directory ? undefined : () => previewSnapshotFile(index),
    });
    if (!entry.directory) {
      const download = fileBrowserButton(actions, "Download", (btn) => downloadSnapshotViewFiles([index], btn));
      download.disabled = view.downloading;
    }
    list.appendChild(row);
  }
  if (!shown.length) {
    const row = document.createElement("tr");
    const cell = document.createElement("td");
    cell.colSpan = 5;
    cell.textContent = "No files or folders found.";
    row.appendChild(cell);
    list.appendChild(row);
  }
  (document.getElementById("snapshot-view-prev") as HTMLButtonElement).disabled = view.page === 0;
  (document.getElementById("snapshot-view-next") as HTMLButtonElement).disabled = view.page + 1 >= pages;
  document.getElementById("snapshot-view-page")!.textContent = `Page ${view.page + 1} of ${pages}`;
  (document.getElementById("snapshot-view-select") as HTMLButtonElement).disabled =
    view.downloading || !shown.some(({ entry }) => !entry.directory);
  updateSnapshotSelection();
}

function updateSnapshotSelection(): void {
  const view = snapshotView;
  if (!view?.archive) return;
  const count = view.selected.size;
  document.getElementById("snapshot-view-count")!.textContent = `${count} selected`;
  const download = document.getElementById("snapshot-view-download") as HTMLButtonElement;
  download.disabled = view.downloading || count === 0;
  if (!view.downloading) download.textContent = count > 2 ? "Download ZIP" : "Download selected";
  (document.getElementById("snapshot-view-clear") as HTMLButtonElement).disabled = view.downloading || count === 0;
}

function toggleSnapshotViewFolder(folder: string): void {
  const view = snapshotView;
  if (!view?.archive?.entries.some((entry) => entry.directory && entry.path === folder)) return;
  if (view.expanded.has(folder)) collapseFileBrowserFolders(view.expanded, folder);
  else {
    view.expanded.add(folder);
    if (!view.folders.get(folder)?.length)
      document.getElementById("snapshot-view-status")!.textContent = `${folder} is empty.`;
  }
  // Opening a folder from search results reveals its tree and opens all its ancestors.
  const search = document.getElementById("snapshot-view-search") as HTMLInputElement;
  if (search.value.trim()) {
    search.value = "";
    const parts = folder.split("/");
    for (let count = 1; count < parts.length; count++) view.expanded.add(parts.slice(0, count).join("/"));
    renderSnapshotView();
  } else renderSnapshotView(false);
}

function changeSnapshotViewPage(direction: number): void {
  if (!snapshotView) return;
  snapshotView.page = Math.max(0, snapshotView.page + direction);
  renderSnapshotView(false);
}

function toggleSnapshotFile(index: number, selected: boolean): void {
  const view = snapshotView;
  const entry = view?.archive?.entries[index];
  if (!view || !entry || entry.directory || view.downloading) return;
  if (selected) view.selected.add(index);
  else view.selected.delete(index);
  updateSnapshotSelection();
}

function selectVisibleSnapshotFiles(): void {
  const view = snapshotView;
  if (!view?.archive || view.downloading) return;
  const start = view.page * SNAPSHOT_VIEW_PAGE_SIZE;
  visibleSnapshotEntries(view)
    .slice(start, start + SNAPSHOT_VIEW_PAGE_SIZE)
    .forEach(({ entry, index }) => {
      if (!entry.directory) view.selected.add(index);
    });
  renderSnapshotView(false);
}

function clearSnapshotSelection(): void {
  if (!snapshotView || snapshotView.downloading) return;
  snapshotView.selected.clear();
  renderSnapshotView(false);
}

async function previewSnapshotFile(index: number): Promise<void> {
  const view = snapshotView;
  const archive = view?.archive;
  const entry = archive?.entries[index];
  if (!view || !archive || !entry || entry.directory) return;
  clearSnapshotPreview();
  const generation = view.previewGeneration;
  const preview = document.getElementById("snapshot-view-preview")!;
  const body = document.getElementById("snapshot-view-preview-body")!;
  document.getElementById("snapshot-view-preview-title")!.textContent = entry.path;
  preview.hidden = false;
  body.textContent = "Loading preview…";
  const blob = snapshotEntryBlob(archive, entry);
  const images: Record<string, string> = {
    png: "image/png",
    jpg: "image/jpeg",
    jpeg: "image/jpeg",
    gif: "image/gif",
    webp: "image/webp",
    bmp: "image/bmp",
    avif: "image/avif",
    ico: "image/x-icon",
  };
  const mime = images[entry.path.split(".").at(-1)!.toLowerCase()];
  if (mime) {
    if (entry.size > SNAPSHOT_IMAGE_PREVIEW_BYTES) {
      body.textContent = "This image is too large to preview. Use Download to save it.";
    } else {
      view.previewURL = URL.createObjectURL(blob.slice(0, blob.size, mime));
      const image = document.createElement("img");
      image.alt = entry.path;
      image.src = view.previewURL;
      image.onerror = () => {
        if (snapshotView === view && generation === view.previewGeneration)
          body.textContent = "This image could not be previewed. Use Download to save it.";
      };
      body.replaceChildren(image);
    }
  } else {
    try {
      const bytes = await blob.slice(0, SNAPSHOT_TEXT_PREVIEW_BYTES).arrayBuffer();
      if (snapshotView !== view || generation !== view.previewGeneration) return;
      if (new Uint8Array(bytes).some((byte) => byte < 32 && byte !== 9 && byte !== 10 && byte !== 13)) {
        throw new Error("Binary file");
      }
      // A streaming decode allows a multi-byte character cut off at the preview boundary.
      const text = new TextDecoder("utf-8", { fatal: true }).decode(bytes, {
        stream: blob.size > SNAPSHOT_TEXT_PREVIEW_BYTES,
      });
      const pre = document.createElement("pre");
      pre.textContent = text || "(Empty file)";
      body.replaceChildren(pre);
      if (blob.size > SNAPSHOT_TEXT_PREVIEW_BYTES) {
        const note = document.createElement("p");
        note.className = "hint";
        note.textContent = "Showing the first 2 MB. Download the file to read it all.";
        body.appendChild(note);
      }
    } catch {
      if (snapshotView === view && generation === view.previewGeneration)
        body.textContent = "Preview is unavailable for this file. Use Download to save it.";
    }
  }
  if (snapshotView === view && generation === view.previewGeneration)
    preview.scrollIntoView({ behavior: "smooth", block: "nearest" });
}

async function downloadSelectedSnapshotFiles(btn: HTMLButtonElement): Promise<void> {
  await downloadSnapshotViewFiles([...(snapshotView?.selected || [])], btn);
}

async function downloadSnapshotViewFiles(indices: number[], btn: HTMLButtonElement): Promise<void> {
  const view = snapshotView;
  const archive = view?.archive;
  if (!view || !archive || view.downloading) return;
  const entries = [...new Set(indices)]
    .map((index) => archive.entries[index])
    .filter((entry) => entry && !entry.directory);
  if (!entries.length) return;
  view.downloading = true;
  const restore = setButtonBusy(btn, entries.length > 2 ? "Preparing ZIP…" : "Downloading…");
  renderSnapshotView(false);
  try {
    if (entries.length > 2) {
      const blob = await zipSnapshotFiles(archive, entries, view.controller.signal);
      if (snapshotView === view) triggerBlobDownload(blob, `${view.filename.replace(/\.tar\.zst$/, "")}-files.zip`);
    } else {
      for (const entry of entries)
        triggerBlobDownload(snapshotEntryBlob(archive, entry), entry.path.split("/").at(-1)!);
    }
  } catch (error) {
    if (snapshotView === view)
      setActionStatus(`Could not download selected files. ${(error as Error).message}`, "error");
  } finally {
    restore();
    if (snapshotView === view) {
      view.downloading = false;
      renderSnapshotView(false);
    }
  }
}
