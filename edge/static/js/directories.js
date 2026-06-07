let directoryExpansionState = new Set(["."]);
let showHiddenDirs = false;

const JOB_EVENT_LINGER_MS = 10000;

function openJobDialog(relativePath = ".") {
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

function stopActionEvent(event) {
  event?.preventDefault();
  event?.stopPropagation();
}

function openJobDialogFromEvent(event, relativePath) {
  stopActionEvent(event);
  openJobDialog(relativePath);
  return false;
}

function rememberDirectoryExpansion() {
  const openPaths = Array.from(document.querySelectorAll("#directory-tree details[data-path][open]"))
    .map((element) => element.dataset.path)
    .filter(Boolean);
  directoryExpansionState = new Set(openPaths.length ? openPaths : ["."]);
}

function buildDirectoryIndex(directories) {
  const entriesByPath = new Map();
  const childrenByParent = new Map();

  directories.forEach((entry) => {
    entriesByPath.set(entry.relative_path, entry);
    childrenByParent.set(entry.relative_path, []);
  });

  directories.forEach((entry) => {
    if (entry.relative_path === ".") {
      return;
    }
    const segments = entry.relative_path.split("/");
    const parentPath = segments.length > 1 ? segments.slice(0, -1).join("/") : ".";
    if (!childrenByParent.has(parentPath)) {
      childrenByParent.set(parentPath, []);
    }
    childrenByParent.get(parentPath).push(entry.relative_path);
  });

  return { entriesByPath, childrenByParent };
}

function formatDirectoryProgress(entry) {
  return entry.state?.pending_archive_size
    ? `${entry.state?.upload_offset || 0}/${entry.state.pending_archive_size} bytes`
    : "";
}

function formatStatusLabel(status) {
  return String(status || "").replaceAll("_", " ");
}

function formatLastState(entry) {
  const status = String(entry.state?.last_status || "").trim();
  if (!status) return "";
  return `Last state: ${formatStatusLabel(status)}`;
}

function lastStateClass(entry) {
  const status = String(entry.state?.last_status || "").trim();
  if (["success", "recovered", "skipped_unchanged", "skipped_empty"].includes(status)) return "state-ok";
  if (["manual_intervention_required", "unexpected_exception", "recovery_failed"].includes(status)) return "state-error";
  if (["retry_scheduled", "waiting_retry", "circuit_open", "skipped_missing"].includes(status)) return "state-warn";
  return "";
}

function recentJobEvent(state, maxAgeMs = JOB_EVENT_LINGER_MS) {
  const stamp = state?.last_upload_updated_at || state?.last_upload_started_at || "";
  if (!stamp) return false;
  const parsed = Date.parse(stamp);
  if (Number.isNaN(parsed)) return false;
  return Date.now() - parsed <= maxAgeMs;
}

function jobActivityDetails(entry) {
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

function directoryDisplayName(entry) {
  if (entry.relative_path === ".") {
    return "Scan Root";
  }
  return entry.relative_path.split("/").pop() || entry.relative_path;
}

function renderDirectoryHeader(entry, childCount, hasSelectedDescendant) {
  const relativePath = entry.relative_path;
  const progressLabel = formatDirectoryProgress(entry);
  const absolutePath = renderClipValue("", entry.absolute_path, { className: "clip-hint", clipLength: 52 });
  const pathValue = relativePath === "."
    ? renderClipValue("", latestData?.scan_root || entry.absolute_path, { className: "clip-code", clipLength: 52 })
    : renderClipValue("", entry.relative_path, { className: "clip-code", clipLength: 44 });
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
        ${actionMarkup}
      </div>
    </div>
  `;
}

function isHiddenPath(relativePath) {
  const name = relativePath === "." ? "" : (relativePath.split("/").pop() || "");
  return name.startsWith(".");
}

function toggleHiddenDirs() {
  showHiddenDirs = !showHiddenDirs;
  const btn = document.getElementById("hidden-dirs-toggle");
  if (btn) {
    btn.setAttribute("aria-checked", String(showHiddenDirs));
    btn.classList.toggle("toggle-on", showHiddenDirs);
  }
  if (latestData) renderDirectories(latestData);
}

function renderDirectoryNode(relativePath, index) {
  const entry = index.entriesByPath.get(relativePath);
  if (!entry) {
    return { html: "", hasSelectedDescendant: false };
  }

  const allChildPaths = index.childrenByParent.get(relativePath) || [];
  const childPaths = allChildPaths.filter((p) => showHiddenDirs || !isHiddenPath(p));
  const renderedChildren = childPaths.map((childPath) => renderDirectoryNode(childPath, index));
  const hasSelectedDescendant = entry.selected || renderedChildren.some((child) => child.hasSelectedDescendant);
  const header = renderDirectoryHeader(entry, childPaths.length, renderedChildren.some((child) => child.hasSelectedDescendant));

  if (!childPaths.length) {
    return {
      hasSelectedDescendant,
      html: `<div class="dir-leaf" data-path="${escapeHtml(relativePath)}">${header}</div>`,
    };
  }

  const shouldOpen = directoryExpansionState.has(relativePath) || hasSelectedDescendant || relativePath === ".";
  return {
    hasSelectedDescendant,
    html: `
      <details class="dir-branch" data-path="${escapeHtml(relativePath)}"${shouldOpen ? " open" : ""}>
        <summary class="dir-summary">${header}</summary>
        <div class="dir-children">
          <div class="dir-children-inner">
            ${renderedChildren.map((child) => child.html).join("")}
          </div>
        </div>
      </details>
    `,
  };
}

function bindDirectoryTreeEvents() {
  document.querySelectorAll("#directory-tree details[data-path]").forEach((element) => {
    element.addEventListener("toggle", () => {
      const path = element.dataset.path;
      if (!path) return;
      if (element.open) {
        directoryExpansionState.add(path);
      } else {
        directoryExpansionState.delete(path);
      }
    });
  });
}

function renderSelectedJobs(directories) {
  const selected = (directories || []).filter((entry) => entry.selected && !entry.blocked_by_parent);
  const html = selected.length
    ? selected.map((entry) => {
      const jobName = entry.config?.job_name || entry.relative_path;
      const lastStateLabel = formatLastState(entry);
      const activity = jobActivityDetails(entry);
      return `
      <div class="job-card">
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
          <div class="job-card-actions">
            <span class="hint-with-help">
              <button type="button" class="btn-force" onclick="return forceUploadFromEvent(event, decodeURIComponent('${encodeURIComponent(jobName)}'), this)">Force Upload</button>
              <span class="hover-hint" title="Upload even if unchanged. Central may reject as duplicate.">?</span>
            </span>
            <button type="button" class="btn-restore" onclick="return openRecoverDialogFromEvent(event, decodeURIComponent('${encodedPath(entry.relative_path)}'), decodeURIComponent('${encodeURIComponent(jobName)}'))">Restore</button>
            ${entry.blocked_by_parent ? "" : `<button type="button" class="btn-edit" onclick="return openJobDialogFromEvent(event, decodeURIComponent('${encodedPath(entry.relative_path)}'))">Edit</button>`}
          </div>
        </div>
      </div>
      `;
    }).join("")
    : '<p class="hint">No directories are selected yet.</p>';
  setHtmlIfChanged("selected-jobs", html);
  setHtmlIfChanged("selected-jobs-count", String(selected.length));
}

function renderDirectoryTree(directories) {
  rememberDirectoryExpansion();
  const tree = renderDirectoryNode(".", buildDirectoryIndex(directories || []));
  if (setHtmlIfChanged("directory-tree", tree.html || '<p class="hint">No directories were found under the scan root.</p>')) {
    bindDirectoryTreeEvents();
  }
}

function renderDirectories(data, { refreshDirectoryTree = true } = {}) {
  renderSelectedJobs(data.directories);
  if (refreshDirectoryTree) {
    renderDirectoryTree(data.directories);
  }
}

function findEntry(relativePath) {
  return latestData?.directories.find((entry) => entry.relative_path === relativePath);
}

function editPath(relativePath) {
  const entry = findEntry(relativePath);
  document.getElementById("relative_path").value = relativePath;
  document.getElementById("job_name").value = entry?.config?.job_name || (relativePath === "." ? "" : relativePath.split("/").pop());
  document.getElementById("exclude").value = (entry?.config?.exclude || []).join("\n");
  document.getElementById("include_hidden").checked = entry?.config?.include_hidden ?? true;
  document.getElementById("follow_symlinks").checked = entry?.config?.follow_symlinks ?? false;
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

function resetForm() {
  document.getElementById("relative_path").value = ".";
  document.getElementById("job_name").value = "";
  document.getElementById("exclude").value = "";
  document.getElementById("include_hidden").checked = true;
  document.getElementById("follow_symlinks").checked = false;
  setStatus("form-status", "Choose a directory, then click Save Job to create or update its .upload_dir backup settings.", "info");
}
