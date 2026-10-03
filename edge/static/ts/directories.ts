// Folders start collapsed; only the ones a user opens stay open across refreshes.
let directoryExpansionState = new Set<string>();
let showHiddenDirs = false;
// The tree loads one level at a time: "." holds the top level, other keys an opened folder.
const directoryChildren = new Map<string, DirectoryNode[]>();
// Each reload bumps this so responses requested before it cannot overwrite fresher data.
let directoryTreeGeneration = 0;
// The generation of each folder's pending request; an older one never blocks a newer one.
const directoryChildrenLoading = new Map<string, number>();

const JOB_EVENT_LINGER_MS = 10000;

function openJobDialog(relativePath = "."): void {
  const entry = findEntry(relativePath);
  if (entry?.blocked_by_parent) {
    setActionStatus(
      `That folder is nested under ${entry.blocked_by_parent}, so Edge follows the parent job instead of opening separate upload settings here.`,
      "error",
    );
    return;
  }
  editPath(relativePath);
  openDialog("job-dialog");
}

function stopActionEvent(event: Event | null | undefined): void {
  event?.preventDefault();
  event?.stopPropagation();
}

function openJobDialogFromEvent(event: Event, relativePath: string): false {
  stopActionEvent(event);
  openJobDialog(relativePath);
  return false;
}

async function fetchDirectoryChildren(relativePath: string): Promise<DirectoryNode[]> {
  const response = await fetch(`/api/directories/children?relative_path=${encodeURIComponent(relativePath)}`, {
    signal: globalThis.AbortSignal?.timeout?.(30000),
  });
  const body: DirectoryChildrenResponse = await response.json();
  if (!response.ok) throw new Error(body.detail || "Folders could not load.");
  return body.directories || [];
}

function directoryTreeLoaded(): boolean {
  return directoryChildren.has(".");
}

// Re-fetches the top level and every open folder; folders that vanished close.
async function reloadDirectoryTree(): Promise<void> {
  const generation = ++directoryTreeGeneration;
  const paths = [".", ...directoryExpansionState];
  const results = await Promise.all(paths.map(async (path) => {
    try {
      return [path, await fetchDirectoryChildren(path)] as const;
    } catch (error) {
      if (path === ".") throw error;
      return [path, null] as const;
    }
  }));
  if (generation !== directoryTreeGeneration) return;
  // Folders opened while this reload ran were fetched after it began; keep them.
  for (const path of [...directoryChildren.keys()]) {
    if (!paths.includes(path) && !directoryExpansionState.has(path)) directoryChildren.delete(path);
  }
  for (const [path, nodes] of results) {
    if (nodes) {
      directoryChildren.set(path, nodes);
    } else {
      // Drop the stale children too, so reopening the folder fetches it again.
      directoryChildren.delete(path);
      directoryExpansionState.delete(path);
    }
  }
  renderDirectoryTree();
}

async function loadDirectoryChildren(relativePath: string): Promise<void> {
  const generation = directoryTreeGeneration;
  if (directoryChildrenLoading.get(relativePath) === generation) return;
  directoryChildrenLoading.set(relativePath, generation);
  try {
    const nodes = await fetchDirectoryChildren(relativePath);
    // A reload started meanwhile, or a newer request for this folder, supplies fresher data.
    if (generation !== directoryTreeGeneration) return;
    directoryChildren.set(relativePath, nodes);
  } catch (error) {
    if (generation !== directoryTreeGeneration) return;
    directoryExpansionState.delete(relativePath);
    const element = Array.from(document.querySelectorAll<HTMLDetailsElement>("#directory-tree details[data-path]"))
      .find((item) => item.dataset.path === relativePath);
    if (element) element.open = false;
    setActionStatus((error as Error).message, "error");
  } finally {
    if (directoryChildrenLoading.get(relativePath) === generation) directoryChildrenLoading.delete(relativePath);
  }
  renderDirectoryTree();
}

function formatDirectoryProgress(entry: DirectoryEntry): string {
  return entry.state?.pending_archive_size
    ? `${entry.state?.upload_offset || 0}/${entry.state.pending_archive_size} bytes`
    : "";
}

function formatStatusLabel(status: unknown): string {
  return String(status || "").replaceAll("_", " ");
}

function formatLastState(entry: DirectoryEntry): string {
  const status = String(entry.state?.last_status || "").trim();
  if (!status) return "";
  return `Last state: ${formatStatusLabel(status)}`;
}

function lastStateClass(entry: DirectoryEntry): string {
  const status = String(entry.state?.last_status || "").trim();
  if (["success", "recovered", "skipped_unchanged", "skipped_empty"].includes(status)) return "state-ok";
  if (["manual_intervention_required", "unexpected_exception", "recovery_failed", "held_for_review"].includes(status)) return "state-error";
  if (["retry_scheduled", "waiting_retry", "circuit_open", "skipped_missing"].includes(status)) return "state-warn";
  return "";
}

function recentJobEvent(state: JobState | undefined, maxAgeMs = JOB_EVENT_LINGER_MS): boolean {
  const stamp = state?.last_upload_updated_at || state?.last_upload_started_at || "";
  if (!stamp) return false;
  const parsed = Date.parse(stamp);
  if (Number.isNaN(parsed)) return false;
  return Date.now() - parsed <= maxAgeMs;
}

function jobActivityDetails(entry: DirectoryEntry): string {
  const state = entry.state || {};
  const status = String(state.last_status || "").trim();
  const terminalStatuses = new Set(["success", "retry_scheduled", "manual_intervention_required", "circuit_open", "unexpected_exception", "skipped_missing"]);
  const isActive = ACTIVE_JOB_STATUSES.has(status);
  const isTerminal = terminalStatuses.has(status) && recentJobEvent(state);
  if (!isActive && !isTerminal) return "";

  const total = Number(state.pending_archive_size || 0);
  const uploaded = Math.max(0, Number(state.upload_offset || 0));
  const uploadPercent = total > 0 ? Math.round((uploaded / total) * 100) : 0;
  const phasePercent = Number(state.active_phase_percent || 0);
  const percent = status === "success"
    ? 100
    : status === "uploading"
      ? Math.min(99, Math.max(50, phasePercent || (50 + Math.round(uploadPercent / 2))))
      : phasePercent
        ? Math.min(100, Math.max(2, phasePercent))
        : status === "archive_created"
          ? 50
          : status === "scanning"
            ? 5
            : Math.max(8, Math.min(100, uploadPercent || 8));
  const kind = status === "success" ? "success" : isTerminal ? "warn" : "active";
  const label = status === "scanning"
    ? "Scanning files"
    : status === "compressing"
      ? "Compressing snapshot"
      : status === "encrypting"
        ? "Encrypting snapshot"
    : status === "archive_created"
      ? "Compression complete"
      : status === "uploading"
        ? "Uploading snapshot"
        : status === "success"
          ? "Snapshot sent"
          : formatStatusLabel(status);
  const detail = status === "success"
    ? (state.last_duplicate ? "Already stored" : "Completed")
    : status === "compressing" || status === "encrypting"
      ? (phasePercent > 0 ? `${phasePercent}%` : "In progress")
    : status === "archive_created"
      ? "Compressed, awaiting upload"
    : status === "retry_scheduled"
      ? (state.next_retry_at ? `Retry at ${formatLocalDateTime(state.next_retry_at)}` : "Retry scheduled")
      : status === "manual_intervention_required"
        ? "Needs manual retry"
        : status === "circuit_open"
          ? "Upload paused"
          : total > 0
            ? `${formatBytes(Math.min(uploaded, total))} / ${formatBytes(total)}`
            : "Preparing snapshot";

  return `
    <div class="job-activity ${kind}" aria-label="${escapeHtml(`${label}: ${detail}`)}">
      <div class="job-activity-head">
        <span>${escapeHtml(label)}</span>
        <span>${escapeHtml(detail)}</span>
      </div>
      <div class="job-energy-bar" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow="${escapeHtml(String(percent))}">
        <span style="width: ${escapeHtml(String(percent))}%"></span>
      </div>
    </div>
  `;
}

function directoryDisplayName(entry: DirectoryEntry): string {
  return entry.relative_path.split("/").pop() || entry.relative_path;
}

function renderDirectoryHeader(entry: DirectoryEntry, childCount: number, hasSelectedDescendant: boolean): string {
  const relativePath = entry.relative_path;
  const progressLabel = formatDirectoryProgress(entry);
  const absolutePath = renderClipValue("", entry.absolute_path, { className: "clip-hint", clipLength: 52 });
  const pathValue = renderClipValue("", entry.relative_path, { className: "clip-code", clipLength: 44 });
  const actionMarkup = entry.blocked_by_parent
    ? `<span class="dir-action-note" title="Nested folders under an already-selected parent are backed up through that parent job instead of getting their own .upload_dir settings.">Covered by parent job</span>`
    : `<button type="button" class="secondary" onclick="return openJobDialogFromEvent(event, decodeURIComponent('${encodedPath(relativePath)}'))">Edit</button>`;

  return `
    <div class="dir-row">
      <div class="dir-main">
        <div class="dir-title">
          ${childCount ? '<span class="dir-caret" aria-hidden="true"></span>' : '<span class="dir-caret dir-caret-placeholder" aria-hidden="true"></span>'}
          <span class="dir-name">${escapeHtml(directoryDisplayName(entry))}</span>
          ${statusBadge(entry)}
          ${childCount ? `<span class="dir-count">${escapeHtml(String(childCount))} nested</span>` : ""}
          ${hasSelectedDescendant && !entry.selected ? '<span class="dir-count">contains selected job</span>' : ""}
        </div>
        <div class="hint">${pathValue}</div>
        <div class="hint">${absolutePath}</div>
        ${progressLabel ? `<div class="dir-state"><span class="hint">${escapeHtml(progressLabel)}</span></div>` : ""}
      </div>
      <div class="dir-actions">
        <button type="button" class="secondary" onclick="return browseFilesFromEvent(event, decodeURIComponent('${encodedPath(relativePath)}'))">Browse files</button>
        ${entry.blocked_by_parent && !entry.excluded && currentUser?.is_admin ? `<button type="button" class="secondary" onclick="return excludeFileFromEvent(event, decodeURIComponent('${encodedPath(relativePath)}'), this)">Exclude folder</button>` : ""}
        ${actionMarkup}
      </div>
    </div>
  `;
}

function isHiddenPath(relativePath: string): boolean {
  const name = relativePath === "." ? "" : (relativePath.split("/").pop() || "");
  return name.startsWith(".");
}

function toggleHiddenDirs(): void {
  showHiddenDirs = !showHiddenDirs;
  const btn = document.getElementById("hidden-dirs-toggle");
  if (btn) {
    btn.setAttribute("aria-checked", String(showHiddenDirs));
    btn.classList.toggle("toggle-on", showHiddenDirs);
  }
  renderDirectoryTree();
}

function visibleChildren(nodes: DirectoryNode[]): DirectoryNode[] {
  return nodes.filter((node) => showHiddenDirs || !isHiddenPath(node.relative_path));
}

function visibleChildCount(node: DirectoryNode): number {
  const total = node.child_count || 0;
  return showHiddenDirs ? total : total - (node.hidden_child_count || 0);
}

// Job paths come from the job list, so unopened folders still show what they contain.
function renderDirectoryNode(node: DirectoryNode, jobPaths: string[]): string {
  const relativePath = node.relative_path;
  const childCount = visibleChildCount(node);
  const containsJob = jobPaths.some((path) => path.startsWith(`${relativePath}/`));
  const header = renderDirectoryHeader(node, childCount, containsJob);
  const excludedClass = node.excluded ? " dir-excluded" : "";

  if (!childCount) {
    return `<div class="dir-leaf${excludedClass}" data-path="${escapeHtml(relativePath)}">${header}</div>`;
  }

  const loaded = directoryChildren.get(relativePath);
  const shouldOpen = directoryExpansionState.has(relativePath);
  const children = loaded
    ? visibleChildren(loaded).map((child) => renderDirectoryNode(child, jobPaths)).join("")
    : '<div class="section-loading" role="status"><span class="section-spinner" aria-hidden="true"></span><span>Loading…</span></div>';
  return `
    <details class="dir-branch${excludedClass}" data-path="${escapeHtml(relativePath)}"${shouldOpen ? " open" : ""}>
      <summary class="dir-summary">${header}</summary>
      <div class="dir-children">
        <div class="dir-children-inner">
          ${children}
        </div>
      </div>
    </details>
  `;
}

function bindDirectoryTreeEvents(): void {
  document.querySelectorAll<HTMLDetailsElement>("#directory-tree details[data-path]").forEach((element) => {
    element.addEventListener("toggle", () => {
      const path = element.dataset.path;
      if (!path) return;
      if (element.open) {
        directoryExpansionState.add(path);
        if (!directoryChildren.has(path)) loadDirectoryChildren(path);
      } else {
        directoryExpansionState.delete(path);
      }
    });
  });
}

// While Edge is still searching the scan root, the list may be missing jobs.
function renderSelectedJobs(directories: DirectoryEntry[] | undefined, discovering = false): void {
  const selected = (directories || []).filter((entry) => entry.selected && !entry.blocked_by_parent);
  const searching = discovering
    ? '<div class="section-loading" role="status"><span class="section-spinner" aria-hidden="true"></span><span>Looking for backup jobs in the scan folder…</span></div>'
    : "";
  const html = searching + (selected.length
    ? selected.map((entry) => {
      const jobName = entry.config?.job_name || entry.relative_path;
      const lastStateLabel = formatLastState(entry);
      const activity = jobActivityDetails(entry);
      // A held backup waits for the operator, so Force Upload becomes the approval.
      const held = entry.state?.last_status === "held_for_review";
      const uploadLabel = held ? "Upload anyway" : "Force Upload";
      const uploadHint = held
        ? "This backup looks very different from earlier ones. Upload it if the change is expected, or clear it."
        : "Upload even if unchanged. Central may reject as duplicate.";
      return `
      <div class="job-card" data-path="${escapeHtml(entry.relative_path)}">
        <div class="job-card-body">
          <div class="job-card-info">
            <div class="job-card-header">
              <div class="job-card-title">${renderStaticClipValue("", jobName, { className: "clip-title", clipLength: 34 })}</div>
              <div class="hint">${renderClipValue("", entry.relative_path, { className: "clip-code", clipLength: 42 })}</div>
            </div>
            <div class="hint job-card-last-state ${lastStateClass(entry)}">${escapeHtml(lastStateLabel || "Last state: —")}</div>
            ${activity}
            ${entry.state?.next_retry_at ? `<div class="hint">Next retry: ${escapeHtml(entry.state.next_retry_at)}</div>` : ""}
            ${entry.state?.last_error_detail ? `<div class="hint job-error">${renderClipValue("", entry.state.last_error_detail, { className: "clip-hint", clipLength: 68 })}</div>` : ""}
            ${entry.blocked_by_parent ? `<div class="hint">Covered by parent job ${renderClipValue("", entry.blocked_by_parent, { className: "clip-code", clipLength: 36 })}</div>` : ""}
            ${entry.config_error ? `<div class="hint job-error">${renderClipValue("", entry.config_error, { className: "clip-hint", clipLength: 68 })}</div>` : ""}
          </div>
          <div class="job-card-side">
            <div class="job-card-actions">
              <button type="button" class="secondary" onclick="return browseFilesFromEvent(event, decodeURIComponent('${encodedPath(entry.relative_path)}'))">Files &amp; exclusions</button>
              ${entry.state?.pending_archive || entry.state?.pending_fingerprint ? `<button type="button" class="danger" onclick="return clearStagedFromEvent(event, decodeURIComponent('${encodedPath(entry.relative_path)}'), this)">Clear staged backup</button>` : ""}
              <span class="hint-with-help">
                <button type="button" class="btn-force" onclick="return forceUploadFromEvent(event, decodeURIComponent('${encodedPath(entry.relative_path)}'), decodeURIComponent('${encodedPath(jobName)}'), this)">${uploadLabel}</button>
                <span class="hover-hint" title="${escapeHtml(uploadHint)}">?</span>
              </span>
              <button type="button" class="btn-restore" onclick="return openRecoverDialogFromEvent(event, decodeURIComponent('${encodedPath(entry.relative_path)}'), decodeURIComponent('${encodedPath(jobName)}'))">Restore</button>
              ${entry.blocked_by_parent ? "" : `<button type="button" class="btn-edit" onclick="return openJobDialogFromEvent(event, decodeURIComponent('${encodedPath(entry.relative_path)}'))">Edit</button>`}
            </div>
            ${entry.state?.last_backup_size_bytes ? `<div class="hint job-card-size">Last backup: ${formatBytes(entry.state.last_backup_size_bytes)}</div>` : ""}
          </div>
        </div>
      </div>
      `;
    }).join("")
    : discovering ? "" : '<p class="hint">No directories are selected yet.</p>');
  setHtmlIfChanged("selected-jobs", html);
  setHtmlIfChanged("selected-jobs-count", discovering && !selected.length ? "-" : String(selected.length));
}

// The scan root itself is implied, so its folders form the top level of the tree.
function renderDirectoryTree(): void {
  const topLevel = directoryChildren.get(".");
  if (!topLevel) return;
  const jobPaths = (latestData?.directories || []).filter((entry) => entry.selected).map((entry) => entry.relative_path);
  const html = visibleChildren(topLevel).map((node) => renderDirectoryNode(node, jobPaths)).join("");
  if (setHtmlIfChanged("directory-tree", html || '<p class="hint">No directories were found under the scan root.</p>')) {
    bindDirectoryTreeEvents();
  }
}

function findEntry(relativePath: string): DirectoryEntry | undefined {
  const job = latestData?.directories?.find((entry) => entry.relative_path === relativePath);
  if (job) return job;
  for (const nodes of directoryChildren.values()) {
    const node = nodes.find((entry) => entry.relative_path === relativePath);
    if (node) return node;
  }
  return undefined;
}

function editPath(relativePath: string): void {
  const entry = findEntry(relativePath);
  (document.getElementById("relative_path") as HTMLInputElement).value = relativePath;
  (document.getElementById("job_name") as HTMLInputElement).value = entry?.config?.job_name || (relativePath === "." ? "" : relativePath.split("/").pop() || "");
  (document.getElementById("exclude") as HTMLTextAreaElement).value = (entry?.config?.exclude || []).join("\n");
  (document.getElementById("include_hidden") as HTMLInputElement).checked = entry?.config?.include_hidden ?? true;
  (document.getElementById("follow_symlinks") as HTMLInputElement).checked = entry?.config?.follow_symlinks ?? false;
  clearStatus("form-status");
  setStatus(
    "form-status",
    entry?.blocked_by_parent
      ? `This folder sits under ${entry.blocked_by_parent}. Edge follows the parent job, so nested folders here should not have their own active upload settings.`
      : entry?.selected
        ? "You are editing the upload settings Edge already uses for this folder."
        : "You are creating upload settings so Edge starts treating this folder as its own backup job.",
    entry?.blocked_by_parent ? "error" : "info",
  );
}

function resetForm(): void {
  (document.getElementById("relative_path") as HTMLInputElement).value = ".";
  (document.getElementById("job_name") as HTMLInputElement).value = "";
  (document.getElementById("exclude") as HTMLTextAreaElement).value = "";
  (document.getElementById("include_hidden") as HTMLInputElement).checked = true;
  (document.getElementById("follow_symlinks") as HTMLInputElement).checked = false;
  setStatus("form-status", "Choose a directory, then click Save Job to create or update its .upload_dir backup settings.", "info");
}
