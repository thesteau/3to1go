interface ClipOptions {
  className?: string;
  clipLength?: number;
}

interface CronFieldSpec {
  label: string;
  min: number;
  max: number;
  names?: Record<string, number>;
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

function encodedPath(value: string | null | undefined): string {
  // encodeURIComponent leaves apostrophes intact; our inline handlers use single quotes.
  return encodeURIComponent(value ?? ".").replaceAll("'", "%27");
}

function statusBadge(entry: DirectoryEntry): string {
  if (entry.config_error) {
    return '<span class="badge error">invalid config</span>';
  }
  if (entry.excluded) {
    return `<span class="badge muted" title="The parent job's exclusions skip this folder, so it is not backed up.">excluded from ${escapeHtml(entry.blocked_by_parent === "." ? "scan root job" : entry.blocked_by_parent)}</span>`;
  }
  if (entry.blocked_by_parent) {
    return `<span class="badge warn" title="Nested folders under an already-selected parent are backed up through that parent job instead of continuing as separate jobs.">managed by ${escapeHtml(entry.blocked_by_parent === "." ? "scan root job" : entry.blocked_by_parent)}</span>`;
  }
  if (entry.selected) {
    return '<span class="badge">selected</span>';
  }
  return '<span class="badge warn">available</span>';
}

function shortFingerprint(value: unknown): string {
  return value ? String(value).slice(0, 12) : "unknown";
}

function clipMiddle(value: unknown, maxLength = 32): string {
  const text = String(value ?? "");
  if (text.length <= maxLength) return text;
  const head = Math.max(10, Math.floor((maxLength - 1) / 2));
  const tail = Math.max(8, maxLength - head - 1);
  return `${text.slice(0, head)}…${text.slice(-tail)}`;
}

function renderClipValue(label: string, value: unknown, { className = "", clipLength = 32 }: ClipOptions = {}): string {
  const full = String(value ?? "").trim();
  if (!full) return "—";
  return renderStaticClipValue(label, full, { className, clipLength });
}

function renderStaticClipValue(label: string, value: unknown, { className = "", clipLength = 32 }: ClipOptions = {}): string {
  const full = String(value ?? "").trim();
  if (!full) return "—";
  const short = clipMiddle(full, clipLength);
  const classes = className ? ` ${className}` : "";
  return `<span class="clip-static${classes}" title="${escapeHtml(full)}">${label ? `<span class="clip-label">${escapeHtml(label)}</span>` : ""}<span class="clip-value">${escapeHtml(short)}</span></span>`;
}

function setHtmlIfChanged(id: string, html: string): boolean {
  const element = document.getElementById(id);
  if (!element || element.innerHTML === html) return false;
  element.innerHTML = html;
  return true;
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

function pause(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function renderHelpHint(message: string): string {
  return `<span class="hover-hint" tabindex="0" aria-label="${escapeHtml(message)}" title="${escapeHtml(message)}">?</span>`;
}

function formatBytes(bytes: number | null | undefined): string {
  if (!bytes) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
  return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
}

function formatLocalDateTime(value: unknown): string {
  const text = String(value || "").trim();
  if (!text) return "—";
  const parsed = new Date(text);
  if (Number.isNaN(parsed.getTime())) {
    return text;
  }
  return parsed.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

function formatClock(hourText: string, minuteText: string): string | null {
  const hour = Number(hourText);
  const minute = Number(minuteText);
  if (!Number.isInteger(hour) || !Number.isInteger(minute)) {
    return null;
  }
  const parsed = new Date();
  parsed.setHours(hour, minute, 0, 0);
  return parsed.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

function describeDayOfWeek(field: string): string {
  const dayNames = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
  if (field === "1-5") return "weekdays";
  if (/^\d$/.test(field)) return dayNames[Number(field) % 7];
  if (/^\d-\d$/.test(field)) {
    const [start, end] = field.split("-").map(Number);
    return `${dayNames[start % 7]} through ${dayNames[end % 7]}`;
  }
  if (/^\d(?:,\d)+$/.test(field)) {
    return field.split(",").map((value) => dayNames[Number(value) % 7]).join(", ");
  }
  return `day-of-week ${field}`;
}

function validateCronValue(value: string, spec: CronFieldSpec): string {
  const namedValue = spec.names?.[String(value || "").toUpperCase()];
  if (namedValue !== undefined) return "";
  if (!/^\d+$/.test(value)) {
    return `${spec.label} must be numeric, a range, a list, or *.`;
  }
  const n = Number(value);
  if (n < spec.min || n > spec.max) {
    return `${spec.label} must be between ${spec.min} and ${spec.max}.`;
  }
  return "";
}

function cronValueNumber(value: string, spec: CronFieldSpec): number {
  const namedValue = spec.names?.[String(value || "").toUpperCase()];
  if (namedValue !== undefined) return namedValue;
  return Number(value);
}

function validateCronField(field: string, spec: CronFieldSpec): string {
  if (!field) return `${spec.label} is required.`;
  for (const rawPart of field.split(",")) {
    if (!rawPart) return `${spec.label} contains an empty list item.`;
    const stepParts = rawPart.split("/");
    if (stepParts.length > 2) return `${spec.label} has an invalid step.`;
    const base = stepParts[0];
    if (stepParts.length === 2) {
      if (!/^\d+$/.test(stepParts[1]) || Number(stepParts[1]) < 1) {
        return `${spec.label} step must be a positive number.`;
      }
    }
    if (base === "*") continue;
    const range = base.split("-");
    if (range.length > 2) return `${spec.label} has an invalid range.`;
    if (range.length === 2) {
      const startError = validateCronValue(range[0], spec);
      if (startError) return startError;
      const endError = validateCronValue(range[1], spec);
      if (endError) return endError;
      if (cronValueNumber(range[0], spec) > cronValueNumber(range[1], spec)) {
        return `${spec.label} range must start before it ends.`;
      }
      continue;
    }
    const valueError = validateCronValue(base, spec);
    if (valueError) return valueError;
  }
  return "";
}

function validateCronSchedule(expression: unknown): string {
  const normalized = String(expression || "").trim();
  if (!normalized) return "";
  const fields = normalized.split(/\s+/);
  if (fields.length !== 5) {
    return "Use five cron fields separated by spaces.";
  }
  const specs: CronFieldSpec[] = [
    { label: "Minute", min: 0, max: 59 },
    { label: "Hour", min: 0, max: 23 },
    { label: "Day of month", min: 1, max: 31 },
    { label: "Month", min: 1, max: 12, names: { JAN: 1, FEB: 2, MAR: 3, APR: 4, MAY: 5, JUN: 6, JUL: 7, AUG: 8, SEP: 9, OCT: 10, NOV: 11, DEC: 12 } },
    { label: "Day of week", min: 0, max: 6, names: { SUN: 0, MON: 1, TUE: 2, WED: 3, THU: 4, FRI: 5, SAT: 6 } },
  ];
  for (let i = 0; i < fields.length; i += 1) {
    const error = validateCronField(fields[i], specs[i]);
    if (error) return error;
  }
  return "";
}

function describeCronSchedule(expression: unknown): { summary: string; help: string } {
  const normalized = String(expression || "").trim();
  const fieldHelp = "Fields run in this order: minute hour day-of-month month day-of-week.";
  if (!normalized) {
    return {
      summary: "No schedule set yet.",
      help: `${fieldHelp} Example: 0 2 * * 0 means every Sunday at 2:00 AM.`,
    };
  }

  const fields = normalized.split(/\s+/);
  if (fields.length !== 5) {
    return {
      summary: "Use five cron fields separated by spaces.",
      help: `${fieldHelp} Example: 0 2 * * 0 means every Sunday at 2:00 AM.`,
    };
  }

  const [minute, hour, dayOfMonth, month, dayOfWeek] = fields;
  const validationError = validateCronSchedule(normalized);
  if (validationError) {
    return {
      summary: validationError,
      help: fieldHelp,
    };
  }
  const timeLabel = formatClock(hour, minute);
  let summary = `Runs on cron schedule ${normalized}.`;
  if (timeLabel) {
    if (dayOfMonth === "*" && month === "*" && dayOfWeek === "*") {
      summary = `Runs every day at ${timeLabel}.`;
    } else if (dayOfMonth === "*" && month === "*" && dayOfWeek !== "*") {
      summary = `Runs every ${describeDayOfWeek(dayOfWeek)} at ${timeLabel}.`;
    } else if (/^\d+$/.test(dayOfMonth) && month === "*" && dayOfWeek === "*") {
      summary = `Runs on day ${dayOfMonth} of every month at ${timeLabel}.`;
    } else if (dayOfMonth === "*" && /^\d+$/.test(month) && dayOfWeek === "*") {
      summary = `Runs during month ${month} at ${timeLabel}.`;
    } else if (dayOfMonth === "*" && month === "*" && dayOfWeek === "0") {
      summary = `Runs every Sunday at ${timeLabel}.`;
    }
  }

  return {
    summary,
    help: `${fieldHelp} Example: 0 2 * * 0 means every Sunday at 2:00 AM.`,
  };
}
