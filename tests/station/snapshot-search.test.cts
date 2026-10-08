const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const { loadFeature } = require("../helpers/scripts.cts");

function searchContext() {
  const ctx = vm.createContext({});
  loadFeature(ctx, "station", "files");
  loadFeature(ctx, "station", "snapshot-search");
  return ctx;
}

const MB = 1024 ** 2;
const GB = 1024 ** 3;

function metadata(overrides = {}) {
  return {
    scoutId: "scout-home",
    instanceId: "device-a",
    jobName: "immich",
    name: "immich__2026-10-07T12-00-00Z__abcd1234.tar.zst",
    sizeBytes: 500 * MB,
    date: new Date(2026, 9, 7, 12),
    ...overrides,
  };
}

test("Station searches metadata across Scouts with implicit or explicit AND", () => {
  const ctx = searchContext();
  const matches = (query, fields = {}) => ctx.matchesSnapshotSearch(ctx.parseSnapshotSearch(query), metadata(fields));
  assert.equal(matches("IMMICH and 2026"), true);
  assert.equal(matches("scout-home immich AND 2026"), true);
  assert.equal(matches("device-a abcd1234"), true);
  assert.equal(matches("immich 2025"), false);
  assert.equal(matches("scout-office immich"), false);
  assert.equal(matches("immich", { jobName: "documents", name: "documents.tar.zst", scoutId: "immich-host" }), true);
});

test("Station searches years, months, and dates using the displayed local date", () => {
  const ctx = searchContext();
  const matches = (query, date = new Date(2026, 9, 7, 0, 15)) =>
    ctx.matchesSnapshotSearch(ctx.parseSnapshotSearch(query), metadata({ date }));
  for (const query of ["2026", "2026-10", "2026-10-07", "2026-10-7"]) assert.equal(matches(query), true, query);
  for (const query of ["2025", "2026-09", "2026-10-08"]) assert.equal(matches(query), false, query);
  assert.equal(matches("2026", null), false);
  // The archive timestamp may cross a year boundary in the browser's timezone.
  const localYearEnd = {
    getFullYear: () => 2025,
    getMonth: () => 11,
    getDate: () => 31,
    getUTCFullYear: () => 2026,
    getUTCMonth: () => 0,
    getUTCDate: () => 1,
  };
  assert.equal(matches("2025-12-31", localYearEnd), true);
  assert.equal(matches("2026", localYearEnd), false);
});

test("Station size ranges are inclusive and combine with names and dates", () => {
  const ctx = searchContext();
  for (const query of ["immich and 2026 100MB-2GB", "immich 100 mb - 2 gib", "size:100MiB..2GiB"]) {
    const search = ctx.parseSnapshotSearch(query);
    for (const sizeBytes of [100 * MB, 500 * MB, 2 * GB])
      assert.equal(ctx.matchesSnapshotSearch(search, metadata({ sizeBytes })), true, query);
    for (const sizeBytes of [100 * MB - 1, 2 * GB + 1])
      assert.equal(ctx.matchesSnapshotSearch(search, metadata({ sizeBytes })), false, query);
  }
});

test("Station supports size comparisons, decimal quantities, and exact byte sizes", () => {
  const ctx = searchContext();
  for (const [query, sizeBytes, expected] of [
    [">=500MB <2GB", 500 * MB, true],
    [">=500MB <2GB", 2 * GB, false],
    ["> 500 MB <= 2 GB", 500 * MB, false],
    ["> 500 MB <= 2 GB", 2 * GB, true],
    ["size:>=1.5GiB", 1.5 * GB, true],
    ["512000B", 512000, true],
    ["=500MB", 500 * MB + 1, false],
    ["0B", 0, true],
    ["1TB", 1024 ** 4, true],
  ]) {
    const search = ctx.parseSnapshotSearch(query);
    assert.equal(ctx.matchesSnapshotSearch(search, metadata({ sizeBytes })), expected, query);
  }
});

test("Station reports invalid dates and ranges with a pointer to the guide", () => {
  const ctx = searchContext();
  for (const query of ["2026-02-29", "2026-13", "2026-10-32", "2GB-100MB", "999999999TB"]) {
    const search = ctx.parseSnapshotSearch(query);
    assert.match(search.error, /Search guide/, query);
    assert.equal(ctx.matchesSnapshotSearch(search, metadata()), false, query);
  }
  assert.equal(ctx.parseSnapshotSearch("2024-02-29").error, null);
});

test("Station handles missing metadata without losing text-only matches", () => {
  const ctx = searchContext();
  const missing = metadata({ sizeBytes: Number.NaN, date: null });
  assert.equal(ctx.matchesSnapshotSearch(ctx.parseSnapshotSearch("immich"), missing), true);
  assert.equal(ctx.matchesSnapshotSearch(ctx.parseSnapshotSearch("2026"), missing), false);
  assert.equal(ctx.matchesSnapshotSearch(ctx.parseSnapshotSearch(">0B"), missing), false);
  assert.equal(ctx.matchesSnapshotSearch(ctx.parseSnapshotSearch("   "), missing), true);
});
