interface StationRestoreRequest {
  id: string;
  job_name: string;
  filename: string;
  status: string;
  created_at: string;
}

let restoreDecisionBusy = false;
let restoreRequestsMarkup = "";

function showScoutWorkspace(name: "jobs" | "restore-requests"): void {
  for (const tab of ["jobs", "restore-requests"]) {
    const button = document.getElementById(`${tab}-tab`);
    const panel = document.getElementById(`${tab}-workspace`);
    button?.setAttribute("aria-selected", String(tab === name));
    button?.setAttribute("tabindex", tab === name ? "0" : "-1");
    if (panel) panel.hidden = tab !== name;
  }
}

function handleScoutWorkspaceKey(event: KeyboardEvent): void {
  if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) return;
  event.preventDefault();
  const restoreTab = document.getElementById("restore-requests-tab");
  const current =
    document.getElementById("jobs-tab")?.getAttribute("aria-selected") === "true" ? "jobs" : "restore-requests";
  const next =
    event.key === "Home" || restoreTab?.hidden
      ? "jobs"
      : event.key === "End"
        ? "restore-requests"
        : current === "jobs"
          ? "restore-requests"
          : "jobs";
  showScoutWorkspace(next);
  document.getElementById(`${next}-tab`)?.focus();
}

async function loadRestoreRequests({ silent = false } = {}): Promise<void> {
  if (restoreDecisionBusy) return;
  try {
    const response = await fetch("/api/restore-requests", { signal: globalThis.AbortSignal?.timeout?.(15000) });
    if (!response.ok) throw new Error("Restore requests could not load from Station.");
    const requests: StationRestoreRequest[] = await response.json();
    if (restoreDecisionBusy) return;
    renderRestoreRequests(requests);
  } catch {
    // Background polls stay quiet so an unreachable Station doesn't repeat the message.
    if (!silent) {
      setStatus("restore-requests-status", "Restore requests could not load from Station. Refresh to retry.", "error");
    }
  }
}

function renderRestoreRequests(requests: StationRestoreRequest[]): void {
  const tab = document.getElementById("restore-requests-tab");
  if (!tab) return;
  // Once opened, retain the tab so an empty list and the final result remain visible.
  if (requests.length) tab.hidden = false;
  tab.textContent = `Restore Requests${requests.length ? ` (${requests.length})` : ""}`;
  const markup =
    requests
      .map(
        (request) => `
    <article class="job-card">
      <h3>${escapeHtml(request.job_name)}</h3>
      <p>${escapeHtml(request.filename)}</p>
      <p class="hint">Requested ${escapeHtml(new Date(request.created_at).toLocaleString())}${request.status === "accepted" ? " · Accepted; retry restoration or reject to dismiss" : ""}</p>
      <label>Destination folder under scan root
        <input type="text" id="restore-destination-${escapeHtml(request.id)}" placeholder="e.g. projects/my-folder" autocomplete="off">
      </label>
      <div class="restore-request-actions">
        <button type="button" onclick="decideStationRestore(${inlineString(request.id)},'accept',this)">${request.status === "accepted" ? "Retry Restore" : "Accept"}</button>
        <button type="button" class="secondary" onclick="decideStationRestore(${inlineString(request.id)},'reject',this)">Reject</button>
      </div>
    </article>`,
      )
      .join("") || '<p class="hint">No pending restore requests.</p>';
  if (markup === restoreRequestsMarkup) return;
  const list = document.getElementById("restore-requests-list");
  if (!list) return;
  const destinations = new Map(Array.from(list.querySelectorAll("input"), (input) => [input.id, input.value]));
  list.innerHTML = markup;
  restoreRequestsMarkup = markup;
  for (const request of requests) {
    const input = document.getElementById(`restore-destination-${request.id}`) as HTMLInputElement | null;
    if (!input) continue;
    const matches = (latestData?.directories || []).filter((entry) => entry.config?.job_name === request.job_name);
    input.value = destinations.get(input.id) ?? (matches.length === 1 ? matches[0].relative_path : "");
  }
}

async function decideStationRestore(
  id: string,
  decision: "accept" | "reject",
  button: HTMLButtonElement,
): Promise<void> {
  if (restoreDecisionBusy) return;
  const input = document.getElementById(`restore-destination-${id}`) as HTMLInputElement | null;
  const destination = input?.value.trim() || "";
  if (decision === "accept" && !destination) {
    setStatus("restore-requests-status", "Enter a destination folder relative to the scan root.", "error");
    input?.focus();
    return;
  }
  if (
    decision === "accept" &&
    !(await confirmApp({
      title: "Accept Restore",
      message: `Restore this archive into ${destination}? Existing files will be replaced.`,
      confirmLabel: "Accept and Restore",
      danger: true,
    }))
  )
    return;
  restoreDecisionBusy = true;
  const done = setButtonBusy(button, decision === "accept" ? "Restoring…" : "Rejecting…");
  try {
    const response = await fetch("/api/restore-requests/decision", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ id, decision, relative_path: destination }),
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.detail || "Restore decision failed.");
    setStatus(
      "restore-requests-status",
      decision === "reject"
        ? "Restore request rejected."
        : `Restored ${result.restored_files || 0} files from ${result.snapshot_filename}.`,
      "success",
    );
  } catch (error) {
    setStatus("restore-requests-status", error instanceof Error ? error.message : "Restore decision failed.", "error");
  } finally {
    restoreDecisionBusy = false;
    done();
    await loadRestoreRequests();
  }
}
