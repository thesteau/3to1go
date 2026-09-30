// --- Certificates ---

function renderCertificateFiles(files: StoredFile[] | undefined): string {
  const items = files || [];
  if (!items.length) {
    return '<p class="hint">No certificates saved yet.</p>';
  }
  return items.map((file) => `
    <div class="hook-file-row">
      <div class="hook-file-main">
        <strong>${escapeHtml(file.name)}</strong>
        <span class="hint">${escapeHtml(formatBytes(file.size_bytes))}</span>
      </div>
      <div class="hook-file-actions">
        <button type="button" class="btn btn-del" onclick="deleteCertificateFile(${inlineString(file.name)})">Delete</button>
      </div>
    </div>
  `).join("");
}

function fillCertificateForm(config: CertificateConfig | null | undefined): void {
  const data = config || {};
  document.getElementById("certificate-dir")!.textContent = data.cert_dir || "n/a";
  document.getElementById("certificate-files")!.innerHTML = renderCertificateFiles(data.files || []);
}

async function loadCertificateConfig(): Promise<CertificateConfig> {
  return loadEditorPanel("certificates", async () => {
  const response = await fetch("/api/certificates", { signal: globalThis.AbortSignal?.timeout?.(30000) });
  const body: CertificateConfig = await response.json();
  if (!response.ok) {
    throw new Error(body.detail || "Failed to load certificates.");
  }
  fillCertificateForm(body);
  return body;
  });
}

async function uploadCertificateFile(): Promise<void> {
  if (!requirePanelReady("certificates")) return;
  const input = document.getElementById("certificate_file_input") as HTMLInputElement | null;
  const file = input?.files?.[0];
  if (!input || !file) {
    setStatus("certificates-status", "Choose a certificate first.", "error");
    return;
  }
  const formData = new FormData();
  formData.append("certificate_file", file);
  const response = await fetch("/api/certificates/files", { method: "POST", body: formData });
  const body: ApiBody = await response.json();
  setStatus("certificates-status", response.ok ? "Certificate uploaded." : (body.detail || "Upload failed."), response.ok ? "success" : "error");
  if (response.ok) {
    input.value = "";
    await loadCertificateConfig();
    setActionStatus(`Uploaded ${file.name}.`, "success");
  } else {
    setActionStatus(body.detail || "Certificate upload failed.", "error");
  }
}

async function deleteCertificateFile(filename: string): Promise<void> {
  if (!requirePanelReady("certificates")) return;
  if (!await confirmApp({
    title: "Delete Certificate",
    message: `Delete ${filename}?`,
    confirmLabel: "Delete",
    danger: true,
  })) {
    return;
  }
  const response = await fetch(`/api/certificates/files/${encodeURIComponent(filename)}`, { method: "DELETE" });
  const body: ApiBody = await response.json();
  setStatus("certificates-status", response.ok ? "Certificate deleted." : (body.detail || "Delete failed."), response.ok ? "success" : "error");
  if (response.ok) {
    await loadCertificateConfig();
    setActionStatus(`Deleted ${filename}.`, "success");
  } else {
    setActionStatus(body.detail || "Certificate delete failed.", "error");
  }
}
