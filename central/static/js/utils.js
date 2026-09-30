function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function formatMessage(value, fallback = "") {
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
    if (typeof value.message === "string") return value.message;
    if (typeof value.msg === "string") {
      const location = Array.isArray(value.loc)
        ? value.loc.filter((part) => !["body", "query", "path"].includes(String(part))).join(".")
        : "";
      return location ? `${location}: ${value.msg}` : value.msg;
    }
    if (value.detail) return formatMessage(value.detail, fallback);
  }
  return String(value || fallback);
}

function shortFingerprint(fingerprint) {
  return fingerprint ? fingerprint.slice(0, 12) : "unknown";
}

function escapeSelectorValue(value) {
  return String(value).replaceAll("\\", "\\\\").replaceAll('"', '\\"');
}

function clipMiddle(value, maxLength = 28) {
  const text = String(value ?? "");
  if (text.length <= maxLength) return text;
  const head = Math.max(8, Math.floor((maxLength - 1) / 2));
  const tail = Math.max(6, maxLength - head - 1);
  return `${text.slice(0, head)}…${text.slice(-tail)}`;
}

function renderClipValue(label, value, { className = "", clipLength = 28 } = {}) {
  const full = String(value ?? "").trim();
  if (!full) return "";
  return renderStaticClipValue(label, full, { className, clipLength });
}

function renderStaticClipValue(label, value, { className = "", clipLength = 28 } = {}) {
  const full = String(value ?? "").trim();
  if (!full) return "";
  const short = clipMiddle(full, clipLength);
  const classes = className ? ` ${className}` : "";
  return `<span class="clip-static${classes}" title="${escapeHtml(full)}">${label ? `<span class="clip-label">${escapeHtml(label)}</span>` : ""}<span class="clip-value">${escapeHtml(short)}</span></span>`;
}

function renderLinkValue(label, value, { className = "", clipLength = 28 } = {}) {
  const full = String(value ?? "").trim();
  if (!full) return "";
  const short = clipMiddle(full, clipLength);
  const classes = className ? ` ${className}` : "";
  return `<a class="clip-static clip-link${classes}" href="${escapeHtml(full)}" target="_blank" rel="noopener noreferrer" title="${escapeHtml(full)}">${label ? `<span class="clip-label">${escapeHtml(label)}</span>` : ""}<span class="clip-value">${escapeHtml(short)}</span></a>`;
}

function formatBytes(bytes) {
  if (!bytes) return "—";
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 ** 2) return (bytes / 1024).toFixed(1) + " KB";
  if (bytes < 1024 ** 3) return (bytes / 1024 ** 2).toFixed(1) + " MB";
  return (bytes / 1024 ** 3).toFixed(2) + " GB";
}

function pause(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

// Shows an action is in flight on the button that started it; the returned function restores it.
function setButtonBusy(button, busyLabel) {
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
function flashElement(element) {
  if (!element?.classList) return;
  element.classList.remove("action-flash");
  void element.offsetWidth;
  element.classList.add("action-flash");
  element.addEventListener("animationend", () => element.classList.remove("action-flash"), { once: true });
}

// Removed items fade out rather than vanishing, so it is clear which one went away.
function fadeOutAndRemove(element) {
  if (!element) return;
  if (!element.animate || globalThis.matchMedia?.("(prefers-reduced-motion: reduce)").matches) {
    element.remove();
    return;
  }
  element.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 220, easing: "ease-in", fill: "forwards" })
    .finished.then(() => element.remove(), () => element.remove());
}

async function readJson(response) {
  return response.json().catch(() => ({}));
}

function renderHelpHint(message) {
  return `<span class="hover-hint" tabindex="0" aria-label="${escapeHtml(message)}" title="${escapeHtml(message)}">?</span>`;
}
