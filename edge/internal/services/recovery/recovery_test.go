package recovery

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"testing"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/services/state"
	"github.com/3to1go/edge/internal/services/upload"
)

// ---------------------------------------------------------------------------
// Mock jobStateStore
// ---------------------------------------------------------------------------

type mockStateStore struct {
	mu     sync.Mutex
	states map[string]state.JobState
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{states: make(map[string]state.JobState)}
}

func (m *mockStateStore) Get(key string) state.JobState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.states[key]
}

func (m *mockStateStore) Set(key string, s state.JobState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.states == nil {
		m.states = make(map[string]state.JobState)
	}
	m.states[key] = s
	return nil
}

func (m *mockStateStore) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.states, key)
	return nil
}

// ---------------------------------------------------------------------------
// Mock snapshotDownloader
// ---------------------------------------------------------------------------

type mockDownloader struct {
	err      error
	filename string
	content  []byte
}

func (m *mockDownloader) DownloadLatestSnapshot(_ context.Context, _, _, destPath string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if m.content != nil {
		os.WriteFile(destPath, m.content, 0o600)
	}
	return m.filename, nil
}

func (m *mockDownloader) DownloadSnapshotByFingerprint(_ context.Context, _, _, _, destPath string) (string, error) {
	return m.DownloadLatestSnapshot(context.Background(), "", "", destPath)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func discardSlogLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError + 100}))
}

func newTestRecoveryService(t *testing.T, ms *mockStateStore, dl *mockDownloader) *RecoveryService {
	t.Helper()
	spoolDir := t.TempDir()
	settings := &config.Settings{
		SpoolDir: spoolDir,
		EdgeID:   "edge-01",
	}
	key := make([]byte, 32)
	return NewRecoveryService(settings, discardSlogLogger(), ms, dl, key)
}

func testJob(root string) *backup.JobDefinition {
	return &backup.JobDefinition{JobName: "photos", RootPath: root}
}

// fakeEncryptedContent returns invalid encrypted content, causing DecryptFile to fail.
func fakeEncryptedContent() []byte {
	return []byte("not a valid DARE stream")
}

// ---------------------------------------------------------------------------
// wrapRecoveryError
// ---------------------------------------------------------------------------

func TestWrapRecoveryError_PassesThrough(t *testing.T) {
	orig := &RecoveryError{Message: "already wrapped", StatusCode: 409}
	got := wrapRecoveryError(orig, false)
	if got != orig {
		t.Error("expected the same RecoveryError to pass through")
	}
}

func TestWrapRecoveryError_UploadFailure404(t *testing.T) {
	uf := &upload.UploadFailure{StatusCode: 404, Message: "not found"}
	got := wrapRecoveryError(uf, false)
	if got.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", got.StatusCode)
	}
	if got.Message != "no snapshots found on Central" {
		t.Errorf("Message = %q", got.Message)
	}
}

func TestWrapRecoveryError_UploadFailureUnauthorized_NotPreview(t *testing.T) {
	uf := &upload.UploadFailure{Category: "unauthorized", Message: "401"}
	got := wrapRecoveryError(uf, false)
	if got.StatusCode != 502 {
		t.Errorf("StatusCode = %d, want 502", got.StatusCode)
	}
}

func TestWrapRecoveryError_UploadFailureUnauthorized_Preview(t *testing.T) {
	uf := &upload.UploadFailure{Category: "unauthorized", Message: "401"}
	got := wrapRecoveryError(uf, true)
	if got.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", got.StatusCode)
	}
}

func TestWrapRecoveryError_UploadFailureNetwork(t *testing.T) {
	uf := &upload.UploadFailure{Category: "network", Message: "timeout"}
	got := wrapRecoveryError(uf, false)
	if got.StatusCode != 502 {
		t.Errorf("StatusCode = %d, want 502", got.StatusCode)
	}
}

func TestWrapRecoveryError_GenericError(t *testing.T) {
	err := errors.New("something exploded")
	got := wrapRecoveryError(err, false)
	if got.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", got.StatusCode)
	}
	if got.Message != "something exploded" {
		t.Errorf("Message = %q", got.Message)
	}
}

// ---------------------------------------------------------------------------
// fingerprintFromFilename
// ---------------------------------------------------------------------------

func TestFingerprintFromFilename_Valid(t *testing.T) {
	name := "edge-01__photos__abc123def456.tar.zst"
	got := fingerprintFromFilename(name)
	if got != "abc123def456" {
		t.Errorf("got %q, want abc123def456", got)
	}
}

func TestFingerprintFromFilename_TooFewParts(t *testing.T) {
	got := fingerprintFromFilename("short.tar.zst")
	if got != "" {
		t.Errorf("expected empty string for short filename, got %q", got)
	}
}

func TestFingerprintFromFilename_Empty(t *testing.T) {
	got := fingerprintFromFilename("")
	if got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Recover — error paths
// ---------------------------------------------------------------------------

func TestRecover_DownloadError(t *testing.T) {
	ms := newMockStateStore()
	dl := &mockDownloader{err: &upload.UploadFailure{StatusCode: http.StatusNotFound, Message: "not found"}}
	rs := newTestRecoveryService(t, ms, dl)
	job := testJob(t.TempDir())

	_, err := rs.Recover(context.Background(), job, "")
	if err == nil {
		t.Fatal("expected error from Recover")
	}
	re, ok := err.(*RecoveryError)
	if !ok {
		t.Fatalf("expected *RecoveryError, got %T", err)
	}
	if re.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", re.StatusCode)
	}
	// State should be set to recovery_failed.
	st := ms.Get(job.RootPath)
	if st.LastStatus != "recovery_failed" {
		t.Errorf("LastStatus = %q, want recovery_failed", st.LastStatus)
	}
}

func TestRecover_DecryptError_WrongKey(t *testing.T) {
	ms := newMockStateStore()
	// Downloader returns malformed encrypted content so DecryptFile errors.
	dl := &mockDownloader{
		filename: "photos__photos__abc123.tar.zst",
		content:  fakeEncryptedContent(),
	}
	rs := newTestRecoveryService(t, ms, dl)
	job := testJob(t.TempDir())

	_, err := rs.Recover(context.Background(), job, "fp123")
	if err == nil {
		t.Fatal("expected error from Recover due to decrypt failure")
	}
	re, ok := err.(*RecoveryError)
	if !ok {
		t.Fatalf("expected *RecoveryError, got %T", err)
	}
	if re.StatusCode != 409 {
		t.Errorf("StatusCode = %d, want 409", re.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// Preview — error paths
// ---------------------------------------------------------------------------

func TestPreview_DownloadError(t *testing.T) {
	ms := newMockStateStore()
	dl := &mockDownloader{err: errors.New("connection refused")}
	rs := newTestRecoveryService(t, ms, dl)
	job := testJob(t.TempDir())

	_, err := rs.Preview(context.Background(), job, "")
	if err == nil {
		t.Fatal("expected error from Preview")
	}
	re, ok := err.(*RecoveryError)
	if !ok {
		t.Fatalf("expected *RecoveryError, got %T", err)
	}
	if re.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", re.StatusCode)
	}
}

func TestPreview_DecryptError_WrongKey(t *testing.T) {
	ms := newMockStateStore()
	dl := &mockDownloader{
		filename: "photos__photos__abc123.tar.zst",
		content:  fakeEncryptedContent(),
	}
	rs := newTestRecoveryService(t, ms, dl)
	job := testJob(t.TempDir())

	_, err := rs.Preview(context.Background(), job, "fp123")
	if err == nil {
		t.Fatal("expected error from Preview due to decrypt failure")
	}
	re, ok := err.(*RecoveryError)
	if !ok {
		t.Fatalf("expected *RecoveryError, got %T", err)
	}
	if re.StatusCode != 409 {
		t.Errorf("StatusCode = %d, want 409", re.StatusCode)
	}
}
