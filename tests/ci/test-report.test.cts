const { test } = require("node:test");
const assert = require("node:assert/strict");
const { parseGoEvents, renderHtml, renderSummary } = require("../../scripts/test-report.cts");

function goEvents(...events) {
  return events
    .map((event, index) =>
      JSON.stringify({ Time: new Date(Date.UTC(2026, 0, 1, 0, 0, index)).toISOString(), ...event }),
    )
    .join("\n");
}

test("Go results keep each test outcome, its failure output, and the wall-clock duration", () => {
  const pkg = "example.com/app";
  const { cases, durationMs } = parseGoEvents(
    `${goEvents(
      { Action: "run", Package: pkg, Test: "TestOK" },
      { Action: "pass", Package: pkg, Test: "TestOK", Elapsed: 0.25 },
      { Action: "output", Package: pkg, Test: "TestBad", Output: "    app_test.go:9: want 2\n" },
      { Action: "fail", Package: pkg, Test: "TestBad", Elapsed: 0.5 },
      { Action: "skip", Package: pkg, Test: "TestLater", Elapsed: 0 },
      { Action: "fail", Package: pkg, Elapsed: 1 },
      { Action: "skip", Package: "example.com/empty", Elapsed: 0 },
    )}\nnot json\n`,
  );
  assert.deepEqual(
    cases.map((c) => [c.name, c.status, c.durationMs]),
    [
      ["TestOK", "pass", 250],
      ["TestBad", "fail", 500],
      ["TestLater", "skip", 0],
    ],
    "a package failing because of its tests is not reported again; packages without tests are left out",
  );
  assert.equal(cases[1].output, "    app_test.go:9: want 2\n");
  assert.equal(cases[0].output, "", "passing output is not kept");
  assert.equal(durationMs, 6000);
});

test("Go packages that fail without a failing test are reported with their build output", () => {
  const { cases } = parseGoEvents(
    goEvents(
      {
        Action: "build-output",
        ImportPath: "example.com/broken [example.com/broken.test]",
        Output: "./broken.go:3: cannot use string\n",
      },
      { Action: "build-fail", ImportPath: "example.com/broken [example.com/broken.test]" },
      { Action: "output", Package: "example.com/broken", Output: "FAIL\texample.com/broken [build failed]\n" },
      {
        Action: "fail",
        Package: "example.com/broken",
        Elapsed: 0,
        FailedBuild: "example.com/broken [example.com/broken.test]",
      },
      { Action: "output", Package: "example.com/panics", Output: "panic: boom\n" },
      { Action: "fail", Package: "example.com/panics", Elapsed: 0.1 },
    ),
  );
  assert.deepEqual(
    cases.map((c) => [c.suite, c.name, c.status]),
    [
      ["example.com/broken", "(package)", "fail"],
      ["example.com/panics", "(package)", "fail"],
    ],
  );
  assert.match(cases[0].output, /cannot use string[\s\S]*build failed/);
  assert.equal(cases[1].output, "panic: boom\n");
});

test("the HTML report escapes names and output and lists failing suites first, opened", () => {
  const html = renderHtml({
    title: "Edge <tests>",
    durationMs: 1500,
    cases: [
      { suite: "a/passing", name: "works", status: "pass", durationMs: 3, output: "" },
      {
        suite: "z/failing",
        name: "<script>alert(1)</script>",
        status: "fail",
        durationMs: 2000,
        output: 'expected "a" & got <b>',
      },
    ],
  });
  assert.doesNotMatch(html, /<script>alert/);
  assert.match(html, /&lt;script&gt;alert\(1\)&lt;\/script&gt;/);
  assert.match(html, /expected &quot;a&quot; &amp; got &lt;b&gt;/);
  assert.match(html, /<title>Edge &lt;tests&gt;<\/title>/);
  assert.ok(html.indexOf("z/failing") < html.indexOf("a/passing"), "failing suites come first");
  assert.match(html, /<details class="suite has-failures" open>/);
  assert.match(html, /<body class="failures-only">/);
});

test("the job summary lists failures and caps a long list", () => {
  const failures = Array.from({ length: 55 }, (_, i) => ({
    suite: "pkg|one",
    name: `Test${i}`,
    status: "fail",
    durationMs: 0,
    output: "",
  }));
  const summary = renderSummary({
    title: "Central Go tests",
    durationMs: 2000,
    cases: [{ suite: "pkg", name: "TestOK", status: "pass", durationMs: 0, output: "" }, ...failures],
  });
  assert.match(summary, /^### ❌ Central Go tests/);
  assert.match(summary, /1 passed, 55 failed, 0 skipped in 2\.0s/);
  assert.match(summary, /\| pkg&#124;one \| Test0 \|/, "pipes cannot break the table");
  assert.doesNotMatch(summary, /Test50/);
  assert.match(summary, /…and 5 more\./);
  assert.match(renderSummary({ title: "Frontend tests", durationMs: 0, cases: [] }), /^### ✅ Frontend tests/);
});
