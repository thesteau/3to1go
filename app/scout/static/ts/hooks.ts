let scoutHookConfig: HookConfig | null = null;
let hookDraftDirty = { pre: false, post: false };

function clearHookState(): void {
  scoutHookConfig = null;
  hookDraftDirty = { pre: false, post: false };
}

function renderHookFiles(files: StoredFile[] | undefined): string {
  const items = files || [];
  if (!items.length) {
    return '<p class="hint">No files saved yet.</p>';
  }
  return items
    .map(
      (file) => `
    <div class="hook-file-row">
      <div class="hook-file-main">
        <strong>${escapeHtml(file.name)}</strong>
        <span class="hint">${escapeHtml(formatBytes(file.size_bytes))}</span>
      </div>
      <div class="hook-file-actions">
        <button type="button" class="secondary" onclick="viewHookFile(${inlineString(file.name)}, ${file.viewable ? "true" : "false"})">View</button>
        <button type="button" class="danger" onclick="deleteHookFile(${inlineString(file.name)})">Delete</button>
      </div>
    </div>
  `,
    )
    .join("");
}

function fillHookForm(config: HookConfig | null | undefined, { preserveDrafts = true } = {}): void {
  const data = config || {};
  document.getElementById("hook-script-dir")!.textContent = data.script_dir || "n/a";
  if (!preserveDrafts || !hookDraftDirty.pre) {
    (document.getElementById("hook_pre_command") as HTMLTextAreaElement).value = data.pre_command || "";
    hookDraftDirty.pre = false;
  }
  if (!preserveDrafts || !hookDraftDirty.post) {
    (document.getElementById("hook_post_command") as HTMLTextAreaElement).value = data.post_command || "";
    hookDraftDirty.post = false;
  }
  document.getElementById("hook-files")!.innerHTML = renderHookFiles(data.files || []);
}

async function loadHookConfig({ preserveDrafts = true } = {}): Promise<HookConfig> {
  const user = currentUser;
  return loadEditorPanel("hooks", async () => {
    const response = await fetch("/api/hooks", { signal: globalThis.AbortSignal?.timeout?.(30000) });
    const body: HookConfig = await response.json();
    if (!response.ok) {
      throw new Error(body.detail || "Failed to load hook settings.");
    }
    if (currentUser !== user || !canManageIntegrations()) throw new Error("Admin access required.");
    scoutHookConfig = body;
    fillHookForm(body, { preserveDrafts });
    return body;
  });
}

function clearHookCommand(kind: "pre" | "post"): void {
  const input = document.getElementById(
    kind === "pre" ? "hook_pre_command" : "hook_post_command",
  ) as HTMLTextAreaElement | null;
  if (!input) return;
  input.value = "";
  hookDraftDirty[kind] = true;
}

async function saveHookCommands(): Promise<void> {
  if (!requirePanelReady("hooks")) return;
  setStatus("hooks-status", "Saving...", "info");
  const payload = {
    hook_pre_command: (document.getElementById("hook_pre_command") as HTMLTextAreaElement).value.trim(),
    hook_post_command: (document.getElementById("hook_post_command") as HTMLTextAreaElement).value.trim(),
  };
  const response = await fetch("/api/hooks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body: ApiBody = await response.json();
  setStatus(
    "hooks-status",
    response.ok ? "Commands saved." : body.detail || "Save failed.",
    response.ok ? "success" : "error",
  );
  if (response.ok) {
    hookDraftDirty = { pre: false, post: false };
    await loadData({ silent: true });
    await loadHookConfig({ preserveDrafts: false });
    setActionStatus("Scout hook commands saved.", "success");
  } else {
    setActionStatus(body.detail || "Hook save failed.", "error");
  }
}
