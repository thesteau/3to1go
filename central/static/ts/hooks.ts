let _centralHookConfig: HookConfig | null = null;
let _hookDraftDirty = { pre: false, post: false };

// --- Hooks ---

function renderHookFiles(files: StoredFile[] | undefined): string {
  const items = files || [];
  if (!items.length) {
    return '<p class="hint">No files saved yet.</p>';
  }
  return items.map((file) => `
    <div class="hook-file-row">
      <div class="hook-file-main">
        <strong>${escapeHtml(file.name)}</strong>
        <span class="hint">${escapeHtml(formatBytes(file.size_bytes))}</span>
      </div>
      <div class="hook-file-actions">
        <button type="button" class="secondary" onclick="viewHookFile(${inlineString(file.name)}, ${file.viewable ? "true" : "false"})">View</button>
        <button type="button" class="btn btn-del" onclick="deleteHookFile(${inlineString(file.name)})">Delete</button>
      </div>
    </div>
  `).join("");
}

function fillHookForm(config: HookConfig | null | undefined, { preserveDrafts = true } = {}): void {
  const data = config || {};
  document.getElementById("hook-script-dir")!.textContent = data.script_dir || "n/a";
  if (!preserveDrafts || !_hookDraftDirty.pre) {
    (document.getElementById("hook_pre_command") as HTMLTextAreaElement).value = data.pre_command || "";
    _hookDraftDirty.pre = false;
  }
  if (!preserveDrafts || !_hookDraftDirty.post) {
    (document.getElementById("hook_post_command") as HTMLTextAreaElement).value = data.post_command || "";
    _hookDraftDirty.post = false;
  }
  document.getElementById("hook-files")!.innerHTML = renderHookFiles(data.files || []);
}

async function loadHookConfig({ preserveDrafts = true } = {}): Promise<HookConfig> {
  return loadEditorPanel("hooks", async () => {
  const response = await fetch("/api/hooks", { signal: globalThis.AbortSignal?.timeout?.(30000) });
  const body: HookConfig = await response.json();
  if (!response.ok) {
    throw new Error(body.detail || "Failed to load hook settings.");
  }
  _centralHookConfig = body;
  fillHookForm(body, { preserveDrafts });
  return body;
  });
}

function clearHookCommand(kind: "pre" | "post"): void {
  const input = document.getElementById(kind === "pre" ? "hook_pre_command" : "hook_post_command") as HTMLTextAreaElement | null;
  if (!input) return;
  input.value = "";
  _hookDraftDirty[kind] = true;
}

async function saveHookCommands(): Promise<void> {
  if (!requirePanelReady("hooks")) return;
  setStatus("hooks-status", "Saving...", "info");
  const payload = {
    pre_command: (document.getElementById("hook_pre_command") as HTMLTextAreaElement).value.trim(),
    post_command: (document.getElementById("hook_post_command") as HTMLTextAreaElement).value.trim(),
  };
  const response = await fetch("/api/hooks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const body: ApiBody = await response.json();
  setStatus("hooks-status", response.ok ? "Commands saved." : (body.detail || "Save failed."), response.ok ? "success" : "error");
  if (response.ok) {
    _hookDraftDirty = { pre: false, post: false };
    await loadCentralSettings();
    await loadHookConfig({ preserveDrafts: false });
    setActionStatus("Central hook commands saved.", "success");
  } else {
    setActionStatus(body.detail || "Hook save failed.", "error");
  }
}
