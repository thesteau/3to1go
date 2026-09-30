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

// Encode a string for JavaScript inside an HTML event attribute.
function inlineString(value: string): string {
  return escapeHtml(JSON.stringify(value));
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

async function readJson<T = ApiBody>(response: Response): Promise<T> {
  return response.json().catch(() => ({}));
}

function renderHelpHint(message: string): string {
  return `<span class="hover-hint" tabindex="0" aria-label="${escapeHtml(message)}" title="${escapeHtml(message)}">?</span>`;
}
