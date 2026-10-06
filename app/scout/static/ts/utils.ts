interface CronFieldSpec {
  label: string;
  min: number;
  max: number;
  names?: Record<string, number>;
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
    return `<span class="badge warn" title="Covered by its parent job.">managed by ${escapeHtml(entry.blocked_by_parent === "." ? "scan root job" : entry.blocked_by_parent)}</span>`;
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

function renderStaticClipValue(
  label: string,
  value: unknown,
  { className = "", clipLength = 32 }: ClipOptions = {},
): string {
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
    return field
      .split(",")
      .map((value) => dayNames[Number(value) % 7])
      .join(", ");
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
    {
      label: "Month",
      min: 1,
      max: 12,
      names: { JAN: 1, FEB: 2, MAR: 3, APR: 4, MAY: 5, JUN: 6, JUL: 7, AUG: 8, SEP: 9, OCT: 10, NOV: 11, DEC: 12 },
    },
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
