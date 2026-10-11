async function openHooksDialog(): Promise<void> {
  await openIntegrationsDialog("scripts");
}

async function uploadHookFile(): Promise<void> {
  if (!requirePanelReady("hooks")) return;
  const input = document.getElementById("hook_file_input") as HTMLInputElement | null;
  const file = input?.files?.[0];
  if (!input || !file) {
    setStatus("hooks-status", "Choose a file first.", "error");
    return;
  }
  const formData = new FormData();
  formData.append("hook_file", file);
  const response = await fetch("/api/hooks/files", { method: "POST", body: formData });
  const body: ApiBody = await response.json();
  setStatus(
    "hooks-status",
    response.ok ? "File uploaded." : body.detail || "Upload failed.",
    response.ok ? "success" : "error",
  );
  if (response.ok) {
    input.value = "";
    await loadHookConfig({ preserveDrafts: true });
    setActionStatus(`Uploaded ${file.name}.`, "success");
  } else {
    setActionStatus(body.detail || "Hook upload failed.", "error");
  }
}

async function viewHookFile(filename: string, viewable: boolean): Promise<void> {
  if (!viewable) {
    setActionStatus("This file cannot be viewed.", "error");
    return;
  }
  const response = await fetch(`/api/hooks/files/${encodeURIComponent(filename)}`);
  const body: HookFileResponse = await response.json();
  if (!response.ok) {
    setActionStatus(body.detail || "View failed.", "error");
    return;
  }
  document.getElementById("hook-view-filename")!.textContent = body.filename || filename;
  (document.getElementById("hook-view-content") as HTMLTextAreaElement).value = body.content || "";
  openDialog("hook-view-dialog");
}

async function deleteHookFile(filename: string): Promise<void> {
  if (!requirePanelReady("hooks")) return;
  if (
    !(await confirmApp({
      title: "Delete Hook File",
      message: `Delete ${filename}?`,
      confirmLabel: "Delete",
      danger: true,
    }))
  ) {
    return;
  }
  const response = await fetch(`/api/hooks/files/${encodeURIComponent(filename)}`, { method: "DELETE" });
  const body: ApiBody = await response.json();
  setStatus(
    "hooks-status",
    response.ok ? "File deleted." : body.detail || "Delete failed.",
    response.ok ? "success" : "error",
  );
  if (response.ok) {
    await loadHookConfig({ preserveDrafts: true });
    setActionStatus(`Deleted ${filename}.`, "success");
  } else {
    setActionStatus(body.detail || "Hook delete failed.", "error");
  }
}
