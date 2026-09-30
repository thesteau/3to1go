interface ClipOptions {
  className?: string;
  clipLength?: number;
}

function escapeHtml(value: unknown): string {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function formatMessage(value: unknown, fallback = ""): string {
  if (value === undefined || value === null || value === "") {
    return fallback;
  }
  if (typeof value === "string") {
    return value;
  }
  if (Array.isArray(value)) {
    return value.map((entry) => formatMessage(entry)).filter(Boolean).join("; ") || fallback;
  }
  if (typeof value === "object") {
    const record = value as { message?: unknown; msg?: unknown; loc?: unknown; detail?: unknown };
    if (typeof record.message === "string") return record.message;
    if (typeof record.msg === "string") {
      const location = Array.isArray(record.loc)
        ? record.loc.filter((part) => !["body", "query", "path"].includes(String(part))).join(".")
        : "";
      return location ? `${location}: ${record.msg}` : record.msg;
    }
    if (record.detail) return formatMessage(record.detail, fallback);
  }
  return String(value || fallback);
}

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

function pause(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

// Shows an action is in flight on the button that started it; the returned function restores it.
function setButtonBusy(button: HTMLButtonElement | null | undefined, busyLabel: string): () => void {
  if (!button) return () => {};
  const label = button.textContent;
  button.disabled = true;
  button.textContent = busyLabel;
  return () => {
    button.disabled = false;
    button.textContent = label;
  };
}

// Briefly highlights the element an action changed so the result is visible where it happened.
function flashElement(element: HTMLElement | null | undefined): void {
  if (!element?.classList) return;
  element.classList.remove("action-flash");
  void element.offsetWidth;
  element.classList.add("action-flash");
  element.addEventListener("animationend", () => element.classList.remove("action-flash"), { once: true });
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

async function readJson<T = ApiBody>(response: Response): Promise<T> {
  return response.json().catch(() => ({}));
}

function renderHelpHint(message: string): string {
  return `<span class="hover-hint" tabindex="0" aria-label="${escapeHtml(message)}" title="${escapeHtml(message)}">?</span>`;
}
