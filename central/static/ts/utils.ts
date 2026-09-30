function shortFingerprint(fingerprint: string | null | undefined): string {
  return fingerprint ? fingerprint.slice(0, 12) : "unknown";
}

function escapeSelectorValue(value: unknown): string {
  return String(value).replaceAll("\\", "\\\\").replaceAll('"', '\\"');
}

function clipMiddle(value: unknown, maxLength = 28): string {
  const text = String(value ?? "");
  if (text.length <= maxLength) return text;
  const head = Math.max(8, Math.floor((maxLength - 1) / 2));
  const tail = Math.max(6, maxLength - head - 1);
  return `${text.slice(0, head)}…${text.slice(-tail)}`;
}

function renderClipValue(label: string, value: unknown, { className = "", clipLength = 28 }: ClipOptions = {}): string {
  const full = String(value ?? "").trim();
  if (!full) return "";
  return renderStaticClipValue(label, full, { className, clipLength });
}

function renderStaticClipValue(label: string, value: unknown, { className = "", clipLength = 28 }: ClipOptions = {}): string {
  const full = String(value ?? "").trim();
  if (!full) return "";
  const short = clipMiddle(full, clipLength);
  const classes = className ? ` ${className}` : "";
  return `<span class="clip-static${classes}" title="${escapeHtml(full)}">${label ? `<span class="clip-label">${escapeHtml(label)}</span>` : ""}<span class="clip-value">${escapeHtml(short)}</span></span>`;
}

function renderLinkValue(label: string, value: unknown, { className = "", clipLength = 28 }: ClipOptions = {}): string {
  const full = String(value ?? "").trim();
  if (!full) return "";
  const short = clipMiddle(full, clipLength);
  const classes = className ? ` ${className}` : "";
  return `<a class="clip-static clip-link${classes}" href="${escapeHtml(full)}" target="_blank" rel="noopener noreferrer" title="${escapeHtml(full)}">${label ? `<span class="clip-label">${escapeHtml(label)}</span>` : ""}<span class="clip-value">${escapeHtml(short)}</span></a>`;
}

function formatBytes(bytes: number | null | undefined): string {
  if (!bytes) return "—";
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 ** 2) return (bytes / 1024).toFixed(1) + " KB";
  if (bytes < 1024 ** 3) return (bytes / 1024 ** 2).toFixed(1) + " MB";
  return (bytes / 1024 ** 3).toFixed(2) + " GB";
}

// Removed items fade out rather than vanishing, so it is clear which one went away.
function fadeOutAndRemove(element: Element | null | undefined): void {
  if (!element) return;
  if (!element.animate || globalThis.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
    element.remove();
    return;
  }
  element.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 220, easing: "ease-in", fill: "forwards" })
    .finished.then(() => element.remove(), () => element.remove());
}
