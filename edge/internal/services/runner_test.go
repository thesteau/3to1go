package services

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/config"
)

func testRunner(t *testing.T, settings *config.Settings, client *UploadClient) *EdgeRunner {
	t.Helper()
	stateStore, err := NewStateStore(settings.StateDir)
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &EdgeRunner{
		Settings:      settings,
		logger:        logger,
		encKey:        []byte("0123456789abcdef0123456789abcdef"),
		StateStore:    stateStore,
		UploadClient:  client,
		LockManager:   NewJobLockManager(),
		HookManager:   NewHookManager(t.TempDir(), logger),
		NtfyPublisher: NewNtfyPublisher(logger),
	}
}

func testRunnerSettings(t *testing.T) *config.Settings {
	t.Helper()
	root := t.TempDir()
	return &config.Settings{
		EdgeID:                            "edge-1",
		EdgeCredential:                    "credential",
		ScanRoot:                          filepath.Join(root, "scan"),
		MaxDepth:                          2,
		StateDir:                          filepath.Join(root, "state"),
		SpoolDir:                          filepath.Join(root, "spool"),
		CronSchedule:                      "*/30 * * * *",
		UploadRetryBaseDelaySeconds:       2,
		UploadRetryMaxDelaySeconds:        10,
		UploadChunkSizeMB:                 1,
		MinUploadChunkSizeMB:              1,
		MaxUploadChunkSizeMB:              1,
		UploadConnectTimeoutSeconds:       1,
		UploadReadTimeoutPaddingSeconds:   1,
		UploadMinThroughputBytesPerSecond: 1024,
		CircuitBreakerFailureThreshold:    3,
		CircuitBreakerCooldownSeconds:     1,
	}
}

func TestUploadPendingArchiveSuccessUpdatesState(t *testing.T) {
	settings := testRunnerSettings(t)
	job := &backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "job"), JobName: "job"}
	if err := os.MkdirAll(job.RootPath, 0o755); err != nil {
		t.Fatalf("mkdir job: %v", err)
	}
	archivePath := filepath.Join(settings.SpoolDir, "pending.tar.zst")
	if err := os.MkdirAll(settings.SpoolDir, 0o755); err != nil {
		t.Fatalf("mkdir spool: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("abcdef"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/backup/uploads/initiate":
			writeTestJSON(t, w, map[string]any{"upload_id": "u1", "status": "initiated", "next_offset": 0, "recommended_chunk_size_bytes": 3})
		case "/backup/uploads/u1/chunk":
			var received int64
			if r.URL.Query().Get("offset") == "3" {
				received = 6
			} else {
				received = 3
			}
			writeTestJSON(t, w, map[string]any{"next_offset": received})
		case "/backup/uploads/u1/finalize":
			writeTestJSON(t, w, map[string]any{"status": "ok", "stored_as": "stored.tar.zst", "pruned": 1, "duplicate": true})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := testUploadClient(server.URL)
	client.http = server.Client()
	runner := testRunner(t, settings, client)
	size := int64(6)
	state := &JobState{
		JobName:            "job",
		PendingArchive:     archivePath,
		PendingArchiveSize: &size,
		PendingFingerprint: "abcdef123456",
		PendingTimestamp:   "2024-01-01T00:00:00Z",
	}

	if !runner.uploadPendingArchive(job, state, settings) {
		t.Fatal("uploadPendingArchive returned false")
	}
	got := runner.StateStore.Get(job.RootPath)
	if got.LastStatus != "success" || got.LastStoredAs != "stored.tar.zst" || got.LastPruned != 1 || !got.LastDuplicate {
		t.Fatalf("state = %+v", got)
	}
	if got.PendingArchive != "" || got.UploadID != "" || got.UploadOffset != 0 {
		t.Fatalf("pending state not cleared: %+v", got)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("archive should be removed, stat err=%v", err)
	}
}

func TestUploadPendingArchiveFailureSchedulesRetryAndDiscard(t *testing.T) {
	settings := testRunnerSettings(t)
	settings.KeepLocalPending = false
	job := &backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "job"), JobName: "job"}
	if err := os.MkdirAll(job.RootPath, 0o755); err != nil {
		t.Fatalf("mkdir job: %v", err)
	}
	archivePath := filepath.Join(settings.SpoolDir, "pending.tar.zst")
	if err := os.MkdirAll(settings.SpoolDir, 0o755); err != nil {
		t.Fatalf("mkdir spool: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("abcdef"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]any{"detail": "service unavailable"})
	}))
	defer server.Close()
	client := testUploadClient(server.URL)
	client.http = server.Client()
	runner := testRunner(t, settings, client)
	size := int64(6)
	state := &JobState{
		JobName:            "job",
		PendingArchive:     archivePath,
		PendingArchiveSize: &size,
		PendingFingerprint: "abcdef123456",
		PendingTimestamp:   "2024-01-01T00:00:00Z",
	}

	if runner.uploadPendingArchive(job, state, settings) {
		t.Fatal("uploadPendingArchive returned true")
	}
	got := runner.StateStore.Get(job.RootPath)
	if got.LastStatus != "retry_scheduled" || got.LastErrorCategory != "server" || got.NextRetryAt == "" {
		t.Fatalf("state = %+v", got)
	}
	if got.PendingArchive != "" {
		t.Fatalf("pending archive should be discarded: %+v", got)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("archive should be removed, stat err=%v", err)
	}
}

func TestRunnerRetryAndPendingHelpers(t *testing.T) {
	settings := testRunnerSettings(t)
	runner := testRunner(t, settings, testUploadClient("http://example.invalid"))
	job := &backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "job"), JobName: "job"}
	if err := os.MkdirAll(job.RootPath, 0o755); err != nil {
		t.Fatalf("mkdir job: %v", err)
	}

	state := &JobState{PendingFingerprint: "fp", NextRetryAt: utcAfterSeconds(60)}
	if got := runner.checkRetry(job, state); got != "waiting" {
		t.Fatalf("checkRetry waiting = %q", got)
	}
	archivePath := filepath.Join(settings.SpoolDir, "pending.tar.zst")
	if err := os.MkdirAll(settings.SpoolDir, 0o755); err != nil {
		t.Fatalf("mkdir spool: %v", err)
	}
	if err := os.WriteFile(archivePath, []byte("x"), 0o644); err != nil {
		t.Fatalf("write pending: %v", err)
	}
	state = &JobState{PendingArchive: archivePath, PendingFingerprint: "fp", NextRetryAt: utcAfterSeconds(-1)}
	if got := runner.checkRetry(job, state); got != "upload_now" {
		t.Fatalf("checkRetry upload_now = %q", got)
	}
	os.Remove(archivePath)
	if got := runner.checkRetry(job, state); got != "none" {
		t.Fatalf("checkRetry missing = %q", got)
	}
	if state.PendingArchive != "" {
		t.Fatalf("missing pending should be cleared: %+v", state)
	}

	runner.setActivePhase(job, state, "compressing", 150)
	got := runner.StateStore.Get(job.RootPath)
	if got.ActivePhase != "compressing" || got.ActivePhasePercent != 100 {
		t.Fatalf("setActivePhase clamp = %+v", got)
	}
	runner.setActivePhase(job, state, "scanning", -10)
	got = runner.StateStore.Get(job.RootPath)
	if got.ActivePhasePercent != 0 {
		t.Fatalf("setActivePhase low clamp = %+v", got)
	}

	pending := filepath.Join(settings.SpoolDir, "clear.tar.zst")
	os.WriteFile(pending, []byte("x"), 0o644)
	state.PendingArchive = pending
	state.PendingArchiveSize = ptrInt64(1)
	state.PendingArchiveSHA256 = "sha"
	state.PendingFingerprint = "fp"
	state.PendingTimestamp = "ts"
	state.UploadID = "u"
	state.UploadOffset = 3
	state.CurrentChunkSizeBytes = ptrInt64(2)
	runner.clearPendingArchive(state)
	if state.PendingArchive != "" || state.PendingArchiveSize != nil || state.UploadID != "" {
		t.Fatalf("clearPendingArchive = %+v", state)
	}
	if _, err := os.Stat(pending); !os.IsNotExist(err) {
		t.Fatalf("cleared archive should be removed, err=%v", err)
	}

	state.PendingArchive = filepath.Join(settings.SpoolDir, "discard.tar.zst")
	state.PendingFingerprint = "fp"
	os.WriteFile(state.PendingArchive, []byte("x"), 0o644)
	runner.discardPendingArchiveFile(state)
	if state.PendingArchive != "" || state.PendingFingerprint == "" {
		t.Fatalf("discard should preserve fingerprint only: %+v", state)
	}
}

func TestPrepareArchiveLockedPaths(t *testing.T) {
	settings := testRunnerSettings(t)
	runner := testRunner(t, settings, testUploadClient("http://example.invalid"))
	job := &backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "job"), JobName: "job", IncludeHidden: true}
	if err := os.MkdirAll(job.RootPath, 0o755); err != nil {
		t.Fatalf("mkdir job: %v", err)
	}
	if err := os.MkdirAll(settings.SpoolDir, 0o755); err != nil {
		t.Fatalf("mkdir spool: %v", err)
	}

	emptyState := &JobState{}
	ready, err := runner.prepareArchiveLocked(job, emptyState, settings)
	if err != nil {
		t.Fatalf("prepare empty: %v", err)
	}
	if ready || emptyState.LastStatus != "skipped_empty" {
		t.Fatalf("empty ready=%v state=%+v", ready, emptyState)
	}

	if err := os.WriteFile(filepath.Join(job.RootPath, "data.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write data: %v", err)
	}
	state := &JobState{}
	ready, err = runner.prepareArchiveLocked(job, state, settings)
	if err != nil {
		t.Fatalf("prepare archive: %v", err)
	}
	if !ready || state.PendingArchive == "" || state.PendingFingerprint == "" || state.PendingArchiveSHA256 == "" {
		t.Fatalf("archive state = %+v", state)
	}
	if _, err := os.Stat(state.PendingArchive); err != nil {
		t.Fatalf("pending archive stat: %v", err)
	}

	pendingArchive := state.PendingArchive
	manual := &JobState{
		ManualInterventionRequired: true,
		PendingArchive:             pendingArchive,
		PendingFingerprint:         state.PendingFingerprint,
		LastErrorDetail:            "needs attention",
	}
	ready, err = runner.prepareArchiveLocked(job, manual, settings)
	if err != nil {
		t.Fatalf("prepare manual: %v", err)
	}
	if ready || manual.LastStatus != "manual_intervention_required" {
		t.Fatalf("manual ready=%v state=%+v", ready, manual)
	}

	unchanged := &JobState{LastSuccessfulFingerprint: state.PendingFingerprint, PendingArchive: pendingArchive}
	ready, err = runner.prepareArchiveLocked(job, unchanged, settings)
	if err != nil {
		t.Fatalf("prepare unchanged: %v", err)
	}
	if ready || unchanged.LastStatus != "skipped_unchanged" || unchanged.PendingArchive != "" {
		t.Fatalf("unchanged ready=%v state=%+v", ready, unchanged)
	}
}

func TestProcessJobLockedAndForceSendValidation(t *testing.T) {
	settings := testRunnerSettings(t)
	if err := os.MkdirAll(settings.ScanRoot, 0o755); err != nil {
		t.Fatalf("mkdir scan: %v", err)
	}
	jobRoot := filepath.Join(settings.ScanRoot, "job")
	if err := os.MkdirAll(jobRoot, 0o755); err != nil {
		t.Fatalf("mkdir job: %v", err)
	}
	if err := backup.WriteUploadDir(jobRoot, map[string]any{"job_name": "job"}); err != nil {
		t.Fatalf("WriteUploadDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jobRoot, "data.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatalf("write data: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/backup/uploads/initiate":
			writeTestJSON(t, w, map[string]any{"upload_id": "u1", "status": "initiated", "next_offset": 0, "recommended_chunk_size_bytes": 10})
		case r.URL.Path == "/backup/uploads/u1/chunk":
			writeTestJSON(t, w, map[string]any{"next_offset": 999999})
		case r.URL.Path == "/backup/uploads/u1/finalize":
			writeTestJSON(t, w, map[string]any{"status": "ok", "stored_as": "stored.tar.zst"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := testUploadClient(server.URL)
	client.http = server.Client()
	runner := testRunner(t, settings, client)

	if _, err := runner.ForceSendJob(context.Background(), " "); err == nil {
		t.Fatal("expected blank job error")
	}
	if _, err := runner.ForceSendJob(context.Background(), "missing"); err == nil {
		t.Fatal("expected missing job error")
	}

	resp, err := runner.ForceSendJob(context.Background(), "job")
	if err != nil {
		t.Fatalf("ForceSendJob: %v", err)
	}
	if resp["status"] != "started" || resp["job_name"] != "job" {
		t.Fatalf("force response = %+v", resp)
	}
	state := runner.StateStore.Get(jobRoot)
	if state.LastStatus != "success" {
		t.Fatalf("force state = %+v", state)
	}
}

func TestRunnerMiscHelpers(t *testing.T) {
	settings := testRunnerSettings(t)
	runner := testRunner(t, settings, testUploadClient("http://example.invalid"))
	job := &backup.JobDefinition{RootPath: filepath.Join(settings.ScanRoot, "job"), JobName: "job"}
	state := &JobState{
		LastStatus:         "retry_scheduled",
		LastErrorCategory:  "server",
		LastErrorDetail:    "down",
		LastStoredAs:       "stored",
		LastPruned:         2,
		LastDuplicate:      true,
		PendingArchive:     "pending",
		PendingFingerprint: "fp",
		PendingTimestamp:   "ts",
		UploadID:           "u",
		UploadOffset:       42,
		NextRetryAt:        "later",
	}
	ctx := runner.hookContext(job, state, settings)
	if ctx["edge_id"] != "edge-1" || ctx["job_name"] != "job" || ctx["upload_offset"] != int64(42) {
		t.Fatalf("hook context = %+v", ctx)
	}

	retryAfter := 7
	if got := runner.retryDelaySeconds(3, &UploadFailure{RetryAfterSeconds: &retryAfter}); got != retryAfter {
		t.Fatalf("retry delay retry-after = %d", got)
	}
	if got := runner.retryDelaySeconds(3, nil); got != 8 {
		t.Fatalf("retry delay exponential = %d", got)
	}
	settings.UploadRetryMaxDelaySeconds = 5
	if got := runner.retryDelaySeconds(5, nil); got != 5 {
		t.Fatalf("retry delay capped = %d", got)
	}

	if parseUTCTime("") != nil || parseUTCTime("not time") != nil || parseUTCTime("2024-01-01T00:00:00Z") == nil {
		t.Fatal("parseUTCTime failed")
	}
	if utcNow() == "" || utcAfterSeconds(1) == "" {
		t.Fatal("utc helpers returned empty")
	}
	if uploadPhasePercent(0, nil) != 50 || uploadPhasePercent(50, ptrInt64(100)) != 75 || uploadPhasePercent(200, ptrInt64(100)) != 100 || uploadPhasePercent(-1, ptrInt64(100)) != 50 {
		t.Fatal("uploadPhasePercent failed")
	}
}

func TestCleanupStaleArchives(t *testing.T) {
	settings := testRunnerSettings(t)
	runner := testRunner(t, settings, testUploadClient("http://example.invalid"))
	if err := os.MkdirAll(settings.SpoolDir, 0o755); err != nil {
		t.Fatalf("mkdir spool: %v", err)
	}
	keep := filepath.Join(settings.SpoolDir, "keep.tar.zst")
	stale := filepath.Join(settings.SpoolDir, "stale.tar.zst")
	other := filepath.Join(settings.SpoolDir, "note.txt")
	for _, path := range []string{keep, stale, other} {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(settings.SpoolDir, "dir.tar.zst"), 0o755); err != nil {
		t.Fatalf("mkdir dir: %v", err)
	}
	if err := runner.StateStore.Set("job", JobState{PendingArchive: keep}); err != nil {
		t.Fatalf("set state: %v", err)
	}
	runner.cleanupStaleArchives(settings)
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("keep stat: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("other stat: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale should be removed, err=%v", err)
	}
}

func TestDownloadLatestSnapshotPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backup/recovery/edge-1/instance-1/job/latest" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Write([]byte("snapshot"))
	}))
	defer server.Close()
	client := testUploadClient(server.URL)
	client.http = server.Client()
	dest := filepath.Join(t.TempDir(), "snapshot.tar.zst")
	filename, err := client.DownloadLatestSnapshot(context.Background(), "edge-1", "job", dest)
	if err != nil {
		t.Fatalf("DownloadLatestSnapshot: %v", err)
	}
	if filename != dest {
		t.Fatalf("fallback filename = %q", filename)
	}
}

//go:fix inline
func ptrInt64(v int64) *int64 {
	return new(v)
}
