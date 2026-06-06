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

function encodedPath(value) {
  return encodeURIComponent(value ?? ".");
}

function statusBadge(entry) {
  if (entry.config_error) {
    return '<span class="badge error">invalid config</span>';
  }
  if (entry.blocked_by_parent) {
    return `<span class="badge warn" title="Nested folders under an already-selected parent are backed up through that parent job instead of continuing as separate jobs.">managed by ${escapeHtml(entry.blocked_by_parent)}</span>`;
  }
  if (entry.selected) {
    return '<span class="badge">selected</span>';
  }
  return '<span class="badge warn">available</span>';
}

function shortFingerprint(value) {
  return value ? String(value).slice(0, 12) : "unknown";
}

function clipMiddle(value, maxLength = 32) {
  const text = String(value ?? "");
  if (text.length <= maxLength) return text;
  const head = Math.max(10, Math.floor((maxLength - 1) / 2));
  const tail = Math.max(8, maxLength - head - 1);
  return `${text.slice(0, head)}…${text.slice(-tail)}`;
}

function renderClipValue(label, value, { className = "", clipLength = 32 } = {}) {
  const full = String(value ?? "").trim();
  if (!full) return "—";
  return renderStaticClipValue(label, full, { className, clipLength });
}

function renderStaticClipValue(label, value, { className = "", clipLength = 32 } = {}) {
  const full = String(value ?? "").trim();
  if (!full) return "—";
  const short = clipMiddle(full, clipLength);
  const classes = className ? ` ${className}` : "";
  return `<span class="clip-static${classes}" title="${escapeHtml(full)}">${label ? `<span class="clip-label">${escapeHtml(label)}</span>` : ""}<span class="clip-value">${escapeHtml(short)}</span></span>`;
}

function setHtmlIfChanged(id, html) {
  const element = document.getElementById(id);
  if (!element || element.innerHTML === html) return false;
  element.innerHTML = html;
  return true;
}

async function readJson(response) {
  return response.json().catch(() => ({}));
}

function pause(ms) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function renderHelpHint(message) {
  return `<span class="hover-hint" tabindex="0" aria-label="${escapeHtml(message)}" title="${escapeHtml(message)}">?</span>`;
}

function formatBytes(bytes) {
  if (!bytes) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
  return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
}

function formatLocalDateTime(value) {
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

function formatClock(hourText, minuteText) {
  const hour = Number(hourText);
  const minute = Number(minuteText);
  if (!Number.isInteger(hour) || !Number.isInteger(minute)) {
    return null;
  }
  const parsed = new Date();
  parsed.setHours(hour, minute, 0, 0);
  return parsed.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
}

function describeDayOfWeek(field) {
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

function validateCronValue(value, spec) {
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

function cronValueNumber(value, spec) {
  const namedValue = spec.names?.[String(value || "").toUpperCase()];
  if (namedValue !== undefined) return namedValue;
  return Number(value);
}

function validateCronField(field, spec) {
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

function validateCronSchedule(expression) {
  const normalized = String(expression || "").trim();
  if (!normalized) return "";
  const fields = normalized.split(/\s+/);
  if (fields.length !== 5) {
    return "Use five cron fields separated by spaces.";
  }
  const specs = [
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

function describeCronSchedule(expression) {
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
