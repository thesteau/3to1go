const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

function renderSnapshots(snapshots) {
  const ctx = vm.createContext({ document: { querySelectorAll: () => [] } });
  for (const feature of ["utils", "keys", "crypto", "snapshots"]) {
    loadFeature(ctx, "station", feature);
  }
  return ctx.renderSnapshots("scout-a", "inst-1", "docs", snapshots);
}

test("snapshots with an unusual size show a badge explaining why", () => {
  const reason = "This archive is 39.1 KB, but this job's archives are usually about 979.0 KB. <b>";
  const html = renderSnapshots([
    { name: "docs__2026-10-02T02-00-00Z__abcdef12.tar.zst", size_bytes: 40000, unusual: reason },
    { name: "docs__2026-09-25T02-00-00Z__12345678.tar.zst", size_bytes: 1000000 },
  ]);
  const badges = html.match(/class="snapshot-unusual-tag"/g) || [];
  assert.equal(badges.length, 1, "only the unusual snapshot is flagged");
  assert.match(html, /unusual size<\/span>/);
  assert.match(
    html,
    /title="This archive is 39\.1 KB, but this job&#39;s archives are usually about 979\.0 KB\. &lt;b&gt;"/,
  );
  assert.doesNotMatch(html, /<b>/, "the reason is escaped");
});

test("snapshot actions keep Download, View, Restore, Delete in that order", () => {
  const html = renderSnapshots([{ name: "docs.tar.zst", size_bytes: 500 }]);
  const labels = [...html.matchAll(/>(Download|View|Restore|Delete)<\/button>/g)].map((match) => match[1]);
  assert.deepEqual(labels, ["Download", "View", "Restore", "Delete"]);
  assert.match(html, /openSnapshotView\(/);
});
