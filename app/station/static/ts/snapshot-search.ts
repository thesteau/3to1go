interface SnapshotSizeSearch {
  operator: "=" | ">" | ">=" | "<" | "<=" | "range";
  bytes: number;
  upper?: number;
}

interface SnapshotSearch {
  text: string[];
  dates: string[];
  sizes: SnapshotSizeSearch[];
  error: string | null;
}

interface SnapshotSearchMetadata {
  scoutId: string;
  instanceId: string;
  jobName: string;
  name: string;
  sizeBytes: number;
  date: Date | null;
}

let _snapshotSearchExpandedScouts: Set<string> | null = null;
const SNAPSHOT_SEARCH_QUANTITY = "\\d+(?:\\.\\d+)?\\s*(?:[KMGT]i?B|B)";

function snapshotSearchBytes(quantity: string): number {
  const parts = quantity.replace(/\s/g, "").match(/^(\d+(?:\.\d+)?)([kmgt]?i?b)$/i)!;
  const units: Record<string, number> = {
    b: 1,
    kb: 1024,
    kib: 1024,
    mb: 1024 ** 2,
    mib: 1024 ** 2,
    gb: 1024 ** 3,
    gib: 1024 ** 3,
    tb: 1024 ** 4,
    tib: 1024 ** 4,
  };
  return Number(parts[1]) * units[parts[2].toLowerCase()];
}

function parseSnapshotSearch(value: string): SnapshotSearch {
  const search: SnapshotSearch = { text: [], dates: [], sizes: [], error: null };
  const range = new RegExp(
    `(^|\\s)(?:size:\\s*)?(${SNAPSHOT_SEARCH_QUANTITY})\\s*(?:-|\\.\\.)\\s*(${SNAPSHOT_SEARCH_QUANTITY})(?=\\s|$)`,
    "gi",
  );
  const comparison = new RegExp(`(^|\\s)(?:size:\\s*)?(>=|<=|>|<|=)?\\s*(${SNAPSHOT_SEARCH_QUANTITY})(?=\\s|$)`, "gi");
  const remaining = value
    .replace(range, (_match, leading: string, lower: string, upper: string) => {
      const bytes = snapshotSearchBytes(lower);
      const end = snapshotSearchBytes(upper);
      if (bytes > end)
        search.error = "Size ranges must start with the smaller size. See the Search guide for examples.";
      search.sizes.push({ operator: "range", bytes, upper: end });
      return leading;
    })
    .replace(
      comparison,
      (_match, leading: string, operator: SnapshotSizeSearch["operator"] | undefined, quantity: string) => {
        search.sizes.push({ operator: operator || "=", bytes: snapshotSearchBytes(quantity) });
        return leading;
      },
    );
  if (
    search.sizes.some(
      (size) =>
        !Number.isFinite(size.bytes) ||
        size.bytes > Number.MAX_SAFE_INTEGER ||
        (size.upper !== undefined && (!Number.isFinite(size.upper) || size.upper > Number.MAX_SAFE_INTEGER)),
    )
  ) {
    search.error = "That size is too large to search. See the Search guide for supported units.";
  }
  for (const token of remaining.trim().split(/\s+/).filter(Boolean)) {
    if (token.toLowerCase() === "and") continue;
    const date = token.match(/^(\d{4})(?:-(\d{1,2})(?:-(\d{1,2}))?)?$/);
    if (date) {
      const year = Number(date[1]);
      const month = date[2] === undefined ? 1 : Number(date[2]);
      const day = date[3] === undefined ? 1 : Number(date[3]);
      const check = new Date(Date.UTC(year, month - 1, day));
      if (
        year < 1000 ||
        check.getUTCFullYear() !== year ||
        check.getUTCMonth() + 1 !== month ||
        check.getUTCDate() !== day
      ) {
        search.error = "Use a valid date: YYYY, YYYY-MM, or YYYY-MM-DD. See the Search guide for details.";
      }
      search.dates.push(
        `${date[1]}${date[2] === undefined ? "" : `-${String(month).padStart(2, "0")}`}${date[3] === undefined ? "" : `-${String(day).padStart(2, "0")}`}`,
      );
    } else search.text.push(token);
  }
  return search;
}

function matchesSnapshotSearch(search: SnapshotSearch, metadata: SnapshotSearchMetadata): boolean {
  if (search.error) return false;
  const names = [metadata.scoutId, metadata.instanceId, metadata.jobName, metadata.name];
  if (!search.text.every((term) => names.some((name) => fileBrowserMatchesPath(name, term)))) return false;
  const date = metadata.date;
  const localDate = date
    ? `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`
    : "";
  if (!search.dates.every((prefix) => localDate.startsWith(prefix))) return false;
  return search.sizes.every((filter) => {
    const bytes = metadata.sizeBytes;
    if (!Number.isFinite(bytes)) return false;
    switch (filter.operator) {
      case "range":
        return bytes >= filter.bytes && bytes <= filter.upper!;
      case ">":
        return bytes > filter.bytes;
      case ">=":
        return bytes >= filter.bytes;
      case "<":
        return bytes < filter.bytes;
      case "<=":
        return bytes <= filter.bytes;
      case "=":
        return bytes === filter.bytes;
    }
    return false;
  });
}

function applySnapshotSearch(): void {
  const input = document.getElementById("snapshot-search") as HTMLInputElement | null;
  if (!input || !_overviewHasData) return;
  const value = input.value.trim();
  const search = parseSnapshotSearch(value);
  const cards = Array.from(document.querySelectorAll<HTMLDetailsElement>("#namespaces details[data-scout-id]"));
  if (value && _snapshotSearchExpandedScouts === null) {
    _snapshotSearchExpandedScouts = new Set(cards.filter((card) => card.open).map((card) => card.dataset.scoutId!));
  }
  let snapshots = 0;
  let jobs = 0;
  let scouts = 0;
  for (const card of cards) {
    let scoutMatches = 0;
    for (const instance of card.querySelectorAll<HTMLElement>(".instance-card")) {
      let instanceMatches = 0;
      for (const job of instance.querySelectorAll<HTMLElement>("[data-job-name]")) {
        let jobMatches = 0;
        for (const row of job.querySelectorAll<HTMLElement>("[data-snapshot-name]")) {
          const name = row.dataset.snapshotName || "";
          const matches = matchesSnapshotSearch(search, {
            scoutId: card.dataset.scoutId || "",
            instanceId: instance.dataset.instanceId || "",
            jobName: job.dataset.jobName || "",
            name,
            sizeBytes: row.dataset.sizeBytes ? Number(row.dataset.sizeBytes) : Number.NaN,
            date: parseSnapshotDate(name),
          });
          row.hidden = Boolean(value) && !matches;
          if (matches) jobMatches++;
        }
        job.hidden = Boolean(value) && jobMatches === 0;
        snapshots += jobMatches;
        if (jobMatches) {
          jobs++;
          instanceMatches++;
        }
      }
      instance.hidden = Boolean(value) && instanceMatches === 0;
      scoutMatches += instanceMatches;
    }
    card.hidden = Boolean(value) && scoutMatches === 0;
    if (scoutMatches) scouts++;
    if (value && scoutMatches) card.open = true;
    else if (!value && _snapshotSearchExpandedScouts !== null)
      card.open = _snapshotSearchExpandedScouts.has(card.dataset.scoutId!);
  }
  if (!value) _snapshotSearchExpandedScouts = null;
  const status = document.getElementById("snapshot-search-status")!;
  status.textContent =
    search.error ||
    (value
      ? `${snapshots} matching snapshot${snapshots === 1 ? "" : "s"} in ${jobs} job${jobs === 1 ? "" : "s"} across ${scouts} Scout${scouts === 1 ? "" : "s"}.`
      : "");
  status.className = search.error ? "hint error" : "hint";
  document.getElementById("snapshot-search-empty")!.hidden = !value || Boolean(search.error) || snapshots > 0;
  (document.getElementById("snapshot-search-clear") as HTMLButtonElement).disabled = !input.value;
}

function clearSnapshotSearch(): void {
  const input = document.getElementById("snapshot-search") as HTMLInputElement;
  input.value = "";
  applySnapshotSearch();
  input.focus();
}

function openSnapshotSearchGuide(): void {
  window.open("https://3to1go.docs.thesteau.com/station/snapshots#search-backups", "_blank", "noopener,noreferrer");
}
