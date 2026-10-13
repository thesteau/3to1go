const { loadFeature } = require("../helpers/scripts.cts");
const { test } = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

test("clear staged backup and cancellation use separate endpoints and report errors", async () => {
  const calls = [];
  const messages = [];
  const ctx = vm.createContext({
    fetch: async (url, options) => {
      calls.push([url, options]);
      if (url.includes("clear-staged")) return { ok: false, json: async () => ({ detail: "Backup is still running" }) };
      return { ok: true, json: async () => ({ status: "cancelling" }) };
    },
    document: { getElementById: () => null },
    setActionStatus: (...args) => messages.push(args),
    loadData: async () => {},
    requestScoutActiveRefreshBurst() {},
    confirmApp: async () => true,
  });
  loadFeature(ctx, "scout", "utils");
  loadFeature(ctx, "scout", "files");
  const btn = { textContent: "", disabled: false };
  await ctx.clearStagedBackup("job", btn);
  assert.equal(messages.at(-1)[1], "error");
  assert.equal(btn.disabled, false);
  await ctx.cancelOperation(btn);
  assert.deepEqual(
    calls.map(([url]) => url),
    ["/api/directories/clear-staged", "/api/cancel-operation"],
  );
  assert.match(messages.at(-1)[0], /^Stopping\./);
});

test("size holds offer Approve & Upload, unusual backups offer Upload anyway, other jobs keep Force Upload", () => {
  const jobs = { innerHTML: "" };
  const count = { innerHTML: "" };
  const ctx = vm.createContext({
    document: {
      getElementById: (id) => ({ "selected-jobs": jobs, "selected-jobs-count": count })[id] || null,
      querySelectorAll: () => [],
    },
    currentUser: { is_admin: true },
    latestData: null,
    ACTIVE_JOB_STATUSES: new Set(["uploading"]),
  });
  loadFeature(ctx, "scout", "utils");
  loadFeature(ctx, "scout", "directories");
  ctx.renderSelectedJobs([
    {
      relative_path: "docs",
      selected: true,
      config: { job_name: "docs" },
      state: {
        last_status: "held_for_review",
        last_error_detail:
          "The archive barely compresses (100% of the original size, usually 41%), which is typical of encrypted files.",
      },
    },
    {
      relative_path: "large",
      selected: true,
      config: { job_name: "large" },
      state: {
        last_status: "held_for_review",
        last_error_category: "upload_approval_required",
        last_error_detail: "Backup is 3072 MB; approval required above 2048 MB.",
      },
    },
    { relative_path: "photos", selected: true, config: { job_name: "photos" }, state: { last_status: "success" } },
  ]);
  const [held, oversized, normal] = jobs.innerHTML.split('class="job-card"').slice(1);
  assert.match(held, />Upload anyway<\/button>/);
  assert.match(held, /Last state: held for review/);
  assert.match(held, /state-error/);
  assert.match(held, /typical of encrypted files/);
  assert.match(oversized, />Approve &amp; Upload<\/button>/);
  assert.match(oversized, /3072 MB/);
  assert.match(oversized, /data-held-for-review="true"/);
  assert.match(oversized, /Approve once for this job/);
  assert.match(normal, />Force Upload<\/button>/);
  assert.doesNotMatch(normal, /Upload anyway/);
});

test("size approval uploads in one click while unusual backups still need confirmation", async () => {
  const confirmations = [];
  const calls = [];
  const messages = [];
  let approve = false;
  const ctx = vm.createContext({
    latestData: { settings: { auto_approve_large_uploads: false } },
    confirmApp: async (options) => {
      confirmations.push(options);
      return approve;
    },
    fetch: async (url, options) => {
      calls.push([url, JSON.parse(options.body)]);
      return { ok: true, json: async () => ({ status: "queued", manual_retry_cleared: true }) };
    },
    requestScoutActiveRefreshBurst() {},
    window: { setTimeout() {} },
    setActionStatus: (...args) => messages.push(args),
  });
  loadFeature(ctx, "scout", "jobs");
  const btn = {
    disabled: false,
    dataset: {
      heldForReview: "true",
      sizeApproval: "true",
      reviewReason: "Backup is 3072 MB; approval required above 2048 MB.",
    },
  };
  await ctx.forceUpload("large", "large", btn);
  assert.equal(calls.length, 1);
  assert.equal(confirmations.length, 0);
  ctx.latestData.settings.auto_approve_large_uploads = true;
  await ctx.forceUpload("large", "large", btn);
  assert.equal(calls.length, 2);
  assert.equal(confirmations.length, 0);
  assert.equal(messages.at(-1)[0], "Upload queued for large.");
  calls.length = 0;
  btn.dataset.sizeApproval = "false";
  btn.dataset.reviewReason = "Unexpected change in file types.";
  await ctx.forceUpload("large", "large", btn);
  assert.equal(calls.length, 0);
  assert.equal(confirmations[0].title, "Upload anyway");
  assert.match(confirmations[0].message, /Unexpected change in file types/);
  approve = true;
  await ctx.forceUpload("large", "large", btn);
  assert.deepEqual(calls, [["/api/directories/force-send", { relative_path: "large" }]]);
  assert.equal(messages.at(-1)[0], "Upload queued for large.");
  assert.equal(btn.disabled, false);
});

test("the job list says Scout is still looking until the first search finishes", () => {
  const jobs = { innerHTML: "" };
  const count = { innerHTML: "" };
  const ctx = vm.createContext({
    document: {
      getElementById: (id) => ({ "selected-jobs": jobs, "selected-jobs-count": count })[id] || null,
      querySelectorAll: () => [],
    },
    currentUser: { is_admin: true },
    latestData: null,
    ACTIVE_JOB_STATUSES: new Set(["uploading"]),
  });
  loadFeature(ctx, "scout", "utils");
  loadFeature(ctx, "scout", "directories");
  ctx.renderSelectedJobs([], true);
  assert.match(jobs.innerHTML, /Looking for backup jobs/);
  assert.doesNotMatch(jobs.innerHTML, /No directories are selected/);
  assert.equal(count.innerHTML, "-");

  ctx.renderSelectedJobs([{ relative_path: "docs", selected: true, config: { job_name: "docs" }, state: {} }], true);
  assert.match(jobs.innerHTML, /Looking for backup jobs/);
  assert.match(jobs.innerHTML, /class="job-card"/, "jobs found so far still show");
  assert.equal(count.innerHTML, "1");

  ctx.renderSelectedJobs([], false);
  assert.doesNotMatch(jobs.innerHTML, /Looking for backup jobs/);
  assert.match(jobs.innerHTML, /No directories are selected/);
});

test("folder paths round-trip through executable inline action handlers", () => {
  const ctx = vm.createContext({});
  loadFeature(ctx, "scout", "utils");
  for (const folder of [
    "Family's photos",
    'quotes" & spaces',
    "photos/日本語",
    "x');throw new Error('unexpected');//",
  ]) {
    const encoded = ctx.encodedPath(folder);
    for (const action of ["openJobDialogFromEvent", "forceUploadFromEvent", "openRecoverDialogFromEvent"]) {
      const handler = new Function(action, "event", `return ${action}(event, decodeURIComponent('${encoded}'))`);
      assert.equal(
        handler((_event, actualPath) => actualPath, {}),
        folder,
      );
    }
  }
});
