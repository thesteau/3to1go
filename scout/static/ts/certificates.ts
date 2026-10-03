function renderCertificateFiles(files: StoredFile[] | undefined): string {
  const items = files || [];
  if (!items.length) {
    return '<p class="hint">No certificates saved yet.</p>';
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
        <button type="button" class="danger" onclick="deleteCertificateFile(${inlineString(file.name)})">Delete</button>
      </div>
    </div>
  `,
    )
    .join("");
}
