// Node 24 executes this TypeScript directly. It turns test results into one
// self-contained HTML report and, on GitHub Actions, a job summary.
//
// As a Node test reporter (writes reports/frontend-tests.html, or $TEST_REPORT_HTML):
//   node --test --test-reporter=./scripts/test-report.cts --test-reporter-destination=stdout ...
// From `go test -json` output:
//   node scripts/test-report.cts go "Scout Go tests" go-tests.json go-tests.html
const { appendFileSync, mkdirSync, readFileSync, writeFileSync } = require("node:fs");
const { dirname, relative, resolve } = require("node:path");
const { inspect } = require("node:util");

type Status = "pass" | "fail" | "skip";
type TestCase = { suite: string; name: string; status: Status; durationMs: number; output: string };
type Report = { title: string; cases: TestCase[]; durationMs: number };

const MAX_SUMMARY_FAILURES = 50;

function formatError(error: any): string {
  // The runner wraps a test's own error (often an assertion) in `cause`.
  const cause = error?.cause ?? error;
  if (cause === undefined || cause === null) return "";
  return typeof cause === "string" ? cause : inspect(cause, { depth: 4 });
}

// Node imports a reporter's default export; for CommonJS that is module.exports itself.
async function* nodeReporter(source: AsyncIterable<{ type: string; data: any }>): AsyncGenerator<string> {
  const started = Date.now();
  const cases: TestCase[] = [];
  const stderr = new Map<string, string[]>();
  for await (const { type, data } of source) {
    if (type === "test:stderr" && data.file) {
      if (!stderr.has(data.file)) stderr.set(data.file, []);
      stderr.get(data.file)!.push(data.message);
    }
    if (type !== "test:pass" && type !== "test:fail") continue;
    if (data.details?.type === "suite") continue;
    // A parent failing only because a subtest failed would count that failure twice.
    if (data.details?.error?.failureType === "subtestsFailed") continue;
    const suite = data.file ? relative(process.cwd(), data.file).replaceAll("\\", "/") : "(unknown file)";
    // Each file also reports itself; that entry matters only when the file itself failed.
    const fileEntry = Boolean(data.file) && resolve(data.name) === data.file;
    if (fileEntry && type === "test:pass") continue;
    const status: Status = data.skip || data.todo ? "skip" : type === "test:pass" ? "pass" : "fail";
    // A file that fails to load reports only "test failed"; its stderr holds the cause.
    const fileOutput = fileEntry ? (stderr.get(data.file) || []).join("") : "";
    cases.push({
      suite,
      name: fileEntry ? "(file)" : data.name,
      status,
      durationMs: data.details?.duration_ms ?? 0,
      output: status === "fail" ? fileOutput || formatError(data.details?.error) : "",
    });
  }
  const report: Report = {
    title: process.env.TEST_REPORT_TITLE || "Frontend tests",
    cases,
    durationMs: Date.now() - started,
  };
  const htmlPath = process.env.TEST_REPORT_HTML || "reports/frontend-tests.html";
  writeReport(report, htmlPath);
  yield `\nHTML test report: ${htmlPath}\n`;
}

// Events are described at https://pkg.go.dev/cmd/test2json.
function parseGoEvents(text: string): { cases: TestCase[]; durationMs: number } {
  const cases: TestCase[] = [];
  const output = new Map<string, string[]>();
  const packagesWithFailedTests = new Set<string>();
  let first = Infinity;
  let last = -Infinity;
  const append = (key: string, line: string) => {
    if (!output.has(key)) output.set(key, []);
    output.get(key)!.push(line);
  };
  for (const line of text.split(/\r?\n/)) {
    if (!line.startsWith("{")) continue;
    let event: any;
    try {
      event = JSON.parse(line);
    } catch {
      continue;
    }
    const time = Date.parse(event.Time);
    if (!Number.isNaN(time)) {
      first = Math.min(first, time);
      last = Math.max(last, time);
    }
    if (event.Action === "build-output") {
      append(`build\0${event.ImportPath}`, event.Output);
      continue;
    }
    const pkg = event.Package ?? "";
    const key = `${pkg}\0${event.Test ?? ""}`;
    if (event.Action === "output") {
      append(key, event.Output);
      continue;
    }
    if (event.Action !== "pass" && event.Action !== "fail" && event.Action !== "skip") continue;
    const durationMs = Math.round((event.Elapsed ?? 0) * 1000);
    if (event.Test) {
      if (event.Action === "fail") packagesWithFailedTests.add(pkg);
      cases.push({
        suite: pkg,
        name: event.Test,
        status: event.Action,
        durationMs,
        output: event.Action === "fail" ? (output.get(key) || []).join("") : "",
      });
    } else if (event.Action === "fail" && !packagesWithFailedTests.has(pkg)) {
      // A package can fail without a failing test: build errors, panics, or TestMain exits.
      const build = event.FailedBuild ? output.get(`build\0${event.FailedBuild}`) || [] : [];
      cases.push({
        suite: pkg,
        name: "(package)",
        status: "fail",
        durationMs,
        output: [...build, ...(output.get(key) || [])].join(""),
      });
    }
  }
  return { cases, durationMs: last >= first ? last - first : 0 };
}

function counts(cases: TestCase[]): Record<Status, number> {
  const result: Record<Status, number> = { pass: 0, fail: 0, skip: 0 };
  for (const testCase of cases) result[testCase.status]++;
  return result;
}

function seconds(ms: number): string {
  return `${(ms / 1000).toFixed(1)}s`;
}

function escapeHtml(value: string): string {
  return value.replace(
    /[&<>"']/g,
    (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]!,
  );
}

function renderSummary(report: Report): string {
  const total = counts(report.cases);
  const failures = report.cases.filter((testCase) => testCase.status === "fail");
  const lines = [
    `### ${failures.length ? "❌" : "✅"} ${report.title}`,
    "",
    `${total.pass} passed, ${total.fail} failed, ${total.skip} skipped in ${seconds(report.durationMs)}.`,
  ];
  if (failures.length) {
    lines.push("", "| Suite | Test |", "|---|---|");
    for (const testCase of failures.slice(0, MAX_SUMMARY_FAILURES)) {
      const cell = (value: string) => escapeHtml(value).replaceAll("|", "&#124;");
      lines.push(`| ${cell(testCase.suite)} | ${cell(testCase.name)} |`);
    }
    if (failures.length > MAX_SUMMARY_FAILURES) lines.push("", `…and ${failures.length - MAX_SUMMARY_FAILURES} more.`);
  }
  lines.push("", "The full HTML report is in this run's artifacts.", "", "");
  return lines.join("\n");
}

function renderHtml(report: Report): string {
  const total = counts(report.cases);
  const suites = new Map<string, TestCase[]>();
  for (const testCase of report.cases) {
    if (!suites.has(testCase.suite)) suites.set(testCase.suite, []);
    suites.get(testCase.suite)!.push(testCase);
  }
  // Failing suites first, so problems are the first thing on the page.
  const ordered = [...suites].sort(
    ([a, aCases], [b, bCases]) =>
      Number(bCases.some((c) => c.status === "fail")) - Number(aCases.some((c) => c.status === "fail")) ||
      a.localeCompare(b),
  );
  const label: Record<Status, string> = { pass: "Passed", fail: "Failed", skip: "Skipped" };
  const sections = ordered
    .map(([suite, cases]) => {
      const suiteCounts = counts(cases);
      const rows = cases
        .map(
          (testCase) => `
        <li class="case ${testCase.status}">
          <div class="case-head">
            <span class="badge ${testCase.status}">${label[testCase.status]}</span>
            <span class="case-name">${escapeHtml(testCase.name)}</span>
            <span class="duration">${testCase.durationMs < 1000 ? `${Math.round(testCase.durationMs)} ms` : seconds(testCase.durationMs)}</span>
          </div>
          ${testCase.output ? `<pre>${escapeHtml(testCase.output.trimEnd())}</pre>` : ""}
        </li>`,
        )
        .join("");
      return `
    <details class="suite${suiteCounts.fail ? " has-failures" : ""}"${suiteCounts.fail ? " open" : ""}>
      <summary>
        <span class="suite-name">${escapeHtml(suite)}</span>
        <span class="suite-counts">${suiteCounts.pass} passed${suiteCounts.fail ? `, <strong>${suiteCounts.fail} failed</strong>` : ""}${suiteCounts.skip ? `, ${suiteCounts.skip} skipped` : ""}</span>
      </summary>
      <ul>${rows}
      </ul>
    </details>`;
    })
    .join("");
  const generated = new Date()
    .toISOString()
    .replace("T", " ")
    .replace(/\.\d+Z$/, " UTC");

  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>${escapeHtml(report.title)}</title>
<style>
  :root { color-scheme: light dark; --bg: #f7f7f8; --panel: #fff; --text: #1d1d20; --muted: #5f6068; --line: #dddde3;
    --pass: #1a7f37; --fail: #c62828; --skip: #8a6d00; --pre: #f1f1f4; }
  @media (prefers-color-scheme: dark) { :root { --bg: #141416; --panel: #1d1d21; --text: #ececf0; --muted: #a0a0aa; --line: #34343b;
    --pass: #4cc06d; --fail: #ff6b6b; --skip: #e0bb3a; --pre: #26262c; } }
  * { box-sizing: border-box; }
  body { margin: 0; background: var(--bg); color: var(--text); font: 15px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif; }
  main { max-width: 1100px; margin: 0 auto; padding: 24px 16px 48px; }
  h1 { margin: 0 0 4px; font-size: 1.5rem; }
  .meta { color: var(--muted); margin: 0 0 20px; }
  .totals { display: flex; flex-wrap: wrap; gap: 12px; margin-bottom: 16px; }
  .total { background: var(--panel); border: 1px solid var(--line); border-radius: 8px; padding: 10px 16px; min-width: 120px; }
  .total b { display: block; font-size: 1.5rem; }
  .total.pass b { color: var(--pass); } .total.fail b { color: var(--fail); } .total.skip b { color: var(--skip); }
  .toolbar { margin: 0 0 16px; color: var(--muted); }
  .suite { background: var(--panel); border: 1px solid var(--line); border-radius: 8px; margin-bottom: 10px; }
  .suite.has-failures { border-color: var(--fail); }
  summary { cursor: pointer; padding: 10px 14px; display: flex; flex-wrap: wrap; gap: 4px 12px; justify-content: space-between; }
  .suite-name { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; overflow-wrap: anywhere; }
  .suite-counts { color: var(--muted); } .suite-counts strong { color: var(--fail); }
  ul { list-style: none; margin: 0; padding: 0 14px 10px; }
  .case { border-top: 1px solid var(--line); padding: 8px 0; }
  .case-head { display: flex; gap: 10px; align-items: baseline; }
  .case-name { flex: 1; overflow-wrap: anywhere; }
  .duration { color: var(--muted); font-size: 0.85rem; white-space: nowrap; }
  .badge { font-size: 0.75rem; font-weight: 600; border-radius: 4px; padding: 1px 6px; color: #fff; white-space: nowrap; }
  .badge.pass { background: var(--pass); } .badge.fail { background: var(--fail); } .badge.skip { background: var(--skip); }
  pre { background: var(--pre); border-radius: 6px; padding: 10px; margin: 8px 0 0; overflow-x: auto; font-size: 0.82rem; white-space: pre-wrap; overflow-wrap: anywhere; }
  body.failures-only .case.pass, body.failures-only .case.skip, body.failures-only .suite:not(.has-failures) { display: none; }
</style>
</head>
<body${total.fail ? ' class="failures-only"' : ""}>
<main>
  <h1>${total.fail ? "❌" : "✅"} ${escapeHtml(report.title)}</h1>
  <p class="meta">Generated ${escapeHtml(generated)} · ${report.cases.length} tests in ${suites.size} suites · ${seconds(report.durationMs)}</p>
  <div class="totals">
    <div class="total pass"><b>${total.pass}</b>passed</div>
    <div class="total fail"><b>${total.fail}</b>failed</div>
    <div class="total skip"><b>${total.skip}</b>skipped</div>
  </div>
  <label class="toolbar"><input type="checkbox" id="failures-only"${total.fail ? " checked" : ""}> Show failures only</label>
  ${sections || "<p>No test results were recorded.</p>"}
</main>
<script>
  document.getElementById('failures-only').addEventListener('change', (event) => {
    document.body.classList.toggle('failures-only', event.target.checked);
  });
</script>
</body>
</html>
`;
}

function writeReport(report: Report, htmlPath: string): void {
  mkdirSync(dirname(resolve(htmlPath)), { recursive: true });
  writeFileSync(htmlPath, renderHtml(report));
  if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, renderSummary(report));
}

function main(args: string[]): void {
  const [mode, title, input, htmlPath] = args;
  if (mode !== "go" || !title || !input || !htmlPath) {
    throw new Error("Usage: node scripts/test-report.cts go <title> <go-test.json> <report.html>");
  }
  const report: Report = { title, ...parseGoEvents(readFileSync(input, "utf8")) };
  writeReport(report, htmlPath);
  // `go test -json` hides the usual console output, so repeat what CI logs need.
  for (const testCase of report.cases.filter((c) => c.status === "fail")) {
    console.log(`--- FAIL: ${testCase.suite} ${testCase.name}\n${testCase.output.trimEnd()}\n`);
  }
  const total = counts(report.cases);
  console.log(`${title}: ${total.pass} passed, ${total.fail} failed, ${total.skip} skipped. HTML report: ${htmlPath}`);
}

module.exports = Object.assign(nodeReporter, { parseGoEvents, renderHtml, renderSummary });
if (require.main === module) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    console.error((error as Error).message);
    process.exitCode = 1;
  }
}
