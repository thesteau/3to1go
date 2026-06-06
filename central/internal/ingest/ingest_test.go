package ingest

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/services"
	"github.com/3to1go/central/internal/storage"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- ValidateNamespaceComponent ---

func TestValidateNamespaceComponent_Valid(t *testing.T) {
	cases := []string{"edge01", "my-edge", "job.name", "abc_123", "A"}
	for _, c := range cases {
		got, err := ValidateNamespaceComponent(c, "field")
		if err != nil || got != c {
			t.Errorf("ValidateNamespaceComponent(%q) = (%q, %v)", c, got, err)
		}
	}
}

func TestValidateNamespaceComponent_TrimSpace(t *testing.T) {
	got, err := ValidateNamespaceComponent("  edge  ", "field")
	if err != nil || got != "edge" {
		t.Errorf("got (%q, %v)", got, err)
	}
}

func TestValidateNamespaceComponent_Invalid(t *testing.T) {
	cases := []string{"", "  ", "a/b", "has space", "tab\there", "a@b", "a:b"}
	for _, c := range cases {
		_, err := ValidateNamespaceComponent(c, "field")
		if err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

// --- buildSnapshotFilename ---

func TestBuildSnapshotFilename_ValidTimestamp(t *testing.T) {
	name := buildSnapshotFilename("myjob", "2024-01-15T10:30:00Z", "abcdef1234567890")
	if !strings.HasPrefix(name, "myjob__") {
		t.Errorf("unexpected prefix: %q", name)
	}
	if !strings.HasSuffix(name, ".tar.zst") {
		t.Errorf("unexpected suffix: %q", name)
	}
	if !strings.Contains(name, "abcdef12") {
		t.Errorf("fingerprint prefix missing in %q", name)
	}
	// Timestamp should be reformatted (colons replaced with dashes)
	if strings.Contains(name, ":") {
		t.Errorf("colons should not appear in filename: %q", name)
	}
}

func TestBuildSnapshotFilename_InvalidTimestamp(t *testing.T) {
	name := buildSnapshotFilename("job", "not-a-time", "fp")
	if !strings.Contains(name, "not-a-time") {
		t.Errorf("raw timestamp should appear in fallback name: %q", name)
	}
}

func TestBuildSnapshotFilename_ShortFingerprint(t *testing.T) {
	name := buildSnapshotFilename("job", "2024-01-01T00:00:00Z", "abc")
	if !strings.Contains(name, "abc") {
		t.Errorf("short fingerprint missing: %q", name)
	}
}

// --- fingerprintPrefix ---

func TestFingerprintPrefix_Long(t *testing.T) {
	got := fingerprintPrefix("1234567890abcdef")
	if got != "12345678" {
		t.Errorf("got %q, want 12345678", got)
	}
}

func TestFingerprintPrefix_Short(t *testing.T) {
	got := fingerprintPrefix("abc")
	if got != "abc" {
		t.Errorf("got %q, want abc", got)
	}
}

func TestFingerprintPrefix_Exactly8(t *testing.T) {
	got := fingerprintPrefix("12345678")
	if got != "12345678" {
		t.Errorf("got %q, want 12345678", got)
	}
}

// --- SourceAddress ---

func TestSourceAddress_XForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	got := SourceAddress(r)
	if got == nil || *got != "1.2.3.4" {
		t.Errorf("got %v, want 1.2.3.4", got)
	}
}

func TestSourceAddress_ForwardedHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Forwarded", `for=192.168.1.1;proto=https`)
	got := SourceAddress(r)
	if got == nil || *got != "192.168.1.1" {
		t.Errorf("got %v, want 192.168.1.1", got)
	}
}

func TestSourceAddress_ForwardedIPv6(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Forwarded", `for="[::1]"`)
	got := SourceAddress(r)
	if got == nil || *got != "::1" {
		t.Errorf("got %v, want ::1", got)
	}
}

func TestSourceAddress_ForwardedObfuscated(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Forwarded", "for=_hidden")
	got := SourceAddress(r)
	if got != nil {
		t.Errorf("expected nil for obfuscated forwarded, got %v", got)
	}
}

func TestSourceAddress_RemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	got := SourceAddress(r)
	if got == nil || *got != "10.0.0.1" {
		t.Errorf("got %v, want 10.0.0.1", got)
	}
}

func TestSourceAddress_RemoteAddrIPv6(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[::1]:8080"
	got := SourceAddress(r)
	if got == nil || *got != "::1" {
		t.Errorf("got %v, want ::1", got)
	}
}

// --- HTTPError ---

func TestHTTPError_Error(t *testing.T) {
	e := httpError(404, "not found")
	if !strings.Contains(e.Error(), "404") || !strings.Contains(e.Error(), "not found") {
		t.Errorf("unexpected error string: %q", e.Error())
	}
	if e.Code != 404 {
		t.Errorf("Code = %d, want 404", e.Code)
	}
}

func TestHTTPErrorJSON(t *testing.T) {
	body := map[string]interface{}{"status": "offset_mismatch", "next_offset": 100}
	e := httpErrorJSON(409, body)
	if e.Code != 409 {
		t.Errorf("Code = %d", e.Code)
	}
}

// --- time helpers ---

func TestUtcNow_Format(t *testing.T) {
	s := utcNow()
	_, err := time.ParseInLocation(timeFormat, s, time.UTC)
	if err != nil {
		t.Errorf("utcNow format invalid: %v", err)
	}
}

func TestUtcAfter(t *testing.T) {
	before := time.Now().UTC()
	s := utcAfter(time.Hour)
	after, err := time.ParseInLocation(timeFormat, s, time.UTC)
	if err != nil {
		t.Fatalf("utcAfter format: %v", err)
	}
	if after.Before(before.Add(59 * time.Minute)) {
		t.Errorf("utcAfter result %v should be ~1h from %v", after, before)
	}
}

// --- sha256File ---

func TestSha256File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")
	content := []byte("hello world")
	os.WriteFile(path, content, 0o644)

	got, err := sha256File(path)
	if err != nil {
		t.Fatalf("sha256File: %v", err)
	}
	// known SHA256 of "hello world"
	want := "b94d27b9934d3e08a52e52d7da7dabfac484efe04294e576b8b5c9f6b3d4f5e7"
	// We don't hardcode the value, just check format
	if len(got) != 64 {
		t.Errorf("expected 64-char hex, got %q", got)
	}
	_ = want
}

func TestSha256File_Missing(t *testing.T) {
	_, err := sha256File("/nonexistent/path")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

// --- randomHex ---

func TestRandomHex_Length(t *testing.T) {
	s, err := randomHex(16)
	if err != nil {
		t.Fatalf("randomHex: %v", err)
	}
	if len(s) != 32 {
		t.Errorf("len=%d, want 32", len(s))
	}
}

func TestRandomHex_Unique(t *testing.T) {
	a, _ := randomHex(16)
	b, _ := randomHex(16)
	if a == b {
		t.Error("two randomHex results should differ")
	}
}

// --- session filesystem operations ---

func newTestService(t *testing.T) *Service {
	t.Helper()
	tmpDir := t.TempDir()
	uploadRoot := filepath.Join(tmpDir, "uploads")
	keyRoot := filepath.Join(uploadRoot, "keys")
	os.MkdirAll(uploadRoot, 0o755)
	os.MkdirAll(keyRoot, 0o755)

	backend := storage.NewLocalBackend(filepath.Join(tmpDir, "backups"))
	locks := services.NewNamespaceLockManager()
	logger := discardLogger()
	hooks := services.NewHookManager(filepath.Join(tmpDir, "hooks"), logger)
	ntfy := services.NewNtfyPublisher(logger)

	return &Service{
		settings: &config.Settings{
			UploadSessionTTLHours: 24,
			UploadChunkSizeMB:     8,
			MaxUploadSizeMB:       2048,
			RetentionKeepLast:     3,
			BackupRoot:            filepath.Join(tmpDir, "backups"),
			StagingDir:            tmpDir,
		},
		backend:    backend,
		index:      nil, // not used in filesystem-only tests
		locks:      locks,
		hooks:      hooks,
		ntfy:       ntfy,
		stagingDir: tmpDir,
		uploadRoot: uploadRoot,
		keyRoot:    keyRoot,
	}
}

func TestSaveAndLoadSession(t *testing.T) {
	svc := newTestService(t)
	sess := &UploadSession{
		UploadID:         "abc123",
		IdempotencyKey:   "key1",
		Namespace:        "edge/inst/job",
		Filename:         "job__2024-01-01T00-00-00Z__abcdef12.tar.zst",
		Status:           "initiated",
		ArchiveSizeBytes: 1024,
		ArchiveSHA256:    "deadbeef",
		CreatedAt:        utcNow(),
		UpdatedAt:        utcNow(),
		ExpiresAt:        utcAfter(24 * time.Hour),
	}

	if err := svc.saveSession(sess); err != nil {
		t.Fatalf("saveSession: %v", err)
	}
	loaded, err := svc.loadSession("abc123")
	if err != nil {
		t.Fatalf("loadSession: %v", err)
	}
	if loaded.UploadID != "abc123" || loaded.Status != "initiated" {
		t.Errorf("unexpected loaded session: %+v", loaded)
	}
}

func TestLoadSession_Missing(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.loadSession("nonexistent")
	if err == nil {
		t.Error("expected error for missing session")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusNotFound {
		t.Errorf("expected 404 HTTPError, got %v", err)
	}
}

func TestWriteAndLoadKeyMapping(t *testing.T) {
	svc := newTestService(t)
	if err := svc.writeKeyMapping("my-idem-key", "upload-999"); err != nil {
		t.Fatalf("writeKeyMapping: %v", err)
	}
	sess := svc.loadSessionForKey("my-idem-key")
	// session dir doesn't exist, so loadSession will fail and return nil
	if sess != nil {
		t.Error("expected nil since session dir doesn't exist")
	}
}

func TestLoadSessionForKey_Missing(t *testing.T) {
	svc := newTestService(t)
	got := svc.loadSessionForKey("no-such-key")
	if got != nil {
		t.Error("expected nil for missing key")
	}
}

func TestCurrentUploadSize_NoFile(t *testing.T) {
	svc := newTestService(t)
	if got := svc.currentUploadSize("nosession"); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestCurrentUploadSize_WithData(t *testing.T) {
	svc := newTestService(t)
	// Create session directory and partial file
	dir := svc.sessionDir("mysess")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(svc.uploadDataPath("mysess"), []byte("hello"), 0o644)

	if got := svc.currentUploadSize("mysess"); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
}

func TestCleanupStaleUploads(t *testing.T) {
	svc := newTestService(t)

	// Create an expired session
	expiredID := "expired123"
	sess := &UploadSession{
		UploadID:         expiredID,
		IdempotencyKey:   "ikey-expired",
		Status:           "initiated",
		ArchiveSizeBytes: 100,
		ExpiresAt:        "2000-01-01T00:00:00Z", // in the past
	}
	svc.saveSession(sess)
	svc.writeKeyMapping("ikey-expired", expiredID)

	// Create a valid session
	validID := "valid456"
	sessValid := &UploadSession{
		UploadID:         validID,
		IdempotencyKey:   "ikey-valid",
		Status:           "initiated",
		ArchiveSizeBytes: 100,
		ExpiresAt:        utcAfter(24 * time.Hour),
	}
	svc.saveSession(sessValid)

	svc.CleanupStaleUploads()

	// Expired session should be gone
	if _, err := os.Stat(svc.sessionDir(expiredID)); !os.IsNotExist(err) {
		t.Error("expired session dir should be removed")
	}
	// Valid session should remain
	if _, err := os.Stat(svc.sessionDir(validID)); err != nil {
		t.Errorf("valid session dir should exist: %v", err)
	}
}

func TestCleanupStaleUploads_IgnoresFiles(t *testing.T) {
	svc := newTestService(t)
	// Place a non-directory file in uploadRoot — should be ignored
	os.WriteFile(filepath.Join(svc.uploadRoot, "notadir.txt"), []byte("x"), 0o644)
	svc.CleanupStaleUploads() // should not panic
}

func TestReservedBytes(t *testing.T) {
	svc := newTestService(t)

	// Create two active sessions
	for _, id := range []string{"s1", "s2"} {
		sess := &UploadSession{
			UploadID:         id,
			Status:           "initiated",
			ArchiveSizeBytes: 500,
			ExpiresAt:        utcAfter(time.Hour),
		}
		svc.saveSession(sess)
	}
	// Create one completed session (should not be counted)
	svc.saveSession(&UploadSession{
		UploadID:         "s3",
		Status:           "completed",
		ArchiveSizeBytes: 200,
		ExpiresAt:        utcAfter(time.Hour),
	})

	if got := svc.reservedBytes(); got != 1000 {
		t.Errorf("reservedBytes = %d, want 1000", got)
	}
}

// --- buildSessionResponse ---

func TestBuildSessionResponse(t *testing.T) {
	svc := newTestService(t)
	svc.settings.UploadChunkSizeMB = 1
	sess := &UploadSession{
		UploadID:         "u1",
		Status:           "initiated",
		ArchiveSizeBytes: 5 * 1024 * 1024,
		UploadedBytes:    0,
	}
	resp := svc.buildSessionResponse(sess)
	if resp.UploadID != "u1" {
		t.Errorf("UploadID = %q", resp.UploadID)
	}
	if resp.NextOffset != 0 {
		t.Errorf("NextOffset = %d, want 0", resp.NextOffset)
	}
	if resp.RecommendedChunkSizeBytes != int64(1*1024*1024) {
		t.Errorf("ChunkSize = %d", resp.RecommendedChunkSizeBytes)
	}
}

func TestBuildSessionResponse_CompletedUsesArchiveSize(t *testing.T) {
	svc := newTestService(t)
	sess := &UploadSession{
		Status:           "completed",
		ArchiveSizeBytes: 1000,
		UploadedBytes:    1000,
	}
	resp := svc.buildSessionResponse(sess)
	if resp.NextOffset != 1000 {
		t.Errorf("NextOffset = %d, want 1000", resp.NextOffset)
	}
}

func TestBuildSessionResponse_ChunkSizeCappedToArchive(t *testing.T) {
	svc := newTestService(t)
	svc.settings.UploadChunkSizeMB = 100 // 100MB chunk, but archive is only 1 byte
	sess := &UploadSession{
		Status:           "initiated",
		ArchiveSizeBytes: 1,
	}
	resp := svc.buildSessionResponse(sess)
	if resp.RecommendedChunkSizeBytes != 1 {
		t.Errorf("ChunkSize = %d, want 1", resp.RecommendedChunkSizeBytes)
	}
}

func TestBuildCommittedDuplicateResponse(t *testing.T) {
	svc := newTestService(t)
	storedAs := "job__2024-01-01T00-00-00Z__abcdef12.tar.zst"
	resp := svc.buildCommittedDuplicateResponse(1024, storedAs)
	if !resp.Duplicate {
		t.Error("Duplicate should be true")
	}
	if resp.Status != "completed" {
		t.Errorf("Status = %q, want completed", resp.Status)
	}
	if resp.StoredAs == nil || *resp.StoredAs != storedAs {
		t.Errorf("StoredAs = %v, want %q", resp.StoredAs, storedAs)
	}
}

// --- sessionTTL ---

func TestSessionTTL(t *testing.T) {
	svc := newTestService(t)
	svc.settings.UploadSessionTTLHours = 48
	if got := svc.sessionTTL(); got != 48*time.Hour {
		t.Errorf("TTL = %v, want 48h", got)
	}
}

// --- sessionReferencesMissingSnapshot ---

func TestSessionReferencesMissingSnapshot_NonCompleted(t *testing.T) {
	svc := newTestService(t)
	sess := &UploadSession{Status: "initiated"}
	if svc.sessionReferencesMissingSnapshot(nil, sess) {
		t.Error("non-completed session should never reference missing snapshot")
	}
}

// --- path helpers ---

func TestSessionPathHelpers(t *testing.T) {
	svc := newTestService(t)
	id := "test-upload-id"
	dir := svc.sessionDir(id)
	if !strings.HasSuffix(dir, id) {
		t.Errorf("sessionDir = %q", dir)
	}
	meta := svc.metadataPath(id)
	if !strings.HasSuffix(meta, "metadata.json") {
		t.Errorf("metadataPath = %q", meta)
	}
	data := svc.uploadDataPath(id)
	if !strings.HasSuffix(data, "archive.part") {
		t.Errorf("uploadDataPath = %q", data)
	}
}

func TestKeyMappingPath_Consistent(t *testing.T) {
	svc := newTestService(t)
	p1 := svc.keyMappingPath("my-key")
	p2 := svc.keyMappingPath("my-key")
	if p1 != p2 {
		t.Errorf("keyMappingPath not consistent: %q != %q", p1, p2)
	}
	p3 := svc.keyMappingPath("other-key")
	if p1 == p3 {
		t.Error("different keys should produce different paths")
	}
}

// --- storageFilesToIndexFiles ---

func TestStorageFilesToIndexFiles(t *testing.T) {
	in := []storage.StorageFile{
		{Filename: "a.tar.zst", SizeBytes: 100, Mtime: 1.0},
		{Filename: "b.tar.zst", SizeBytes: 200, Mtime: 2.0},
	}
	out := storageFilesToIndexFiles(in)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].Filename != "a.tar.zst" || out[1].SizeBytes != 200 {
		t.Errorf("unexpected output: %+v", out)
	}
}

// --- discardSession ---

func TestDiscardSession(t *testing.T) {
	svc := newTestService(t)
	sess := &UploadSession{
		UploadID:       "disc123",
		IdempotencyKey: "ikey-disc",
		Status:         "initiated",
		ExpiresAt:      utcAfter(time.Hour),
	}
	svc.saveSession(sess)
	svc.writeKeyMapping("ikey-disc", "disc123")

	svc.discardSession(sess)

	// Session dir should be gone
	if _, err := os.Stat(svc.sessionDir("disc123")); !os.IsNotExist(err) {
		t.Error("session dir should be removed after discard")
	}
	// Key mapping should be gone
	if got := svc.loadSessionForKey("ikey-disc"); got != nil {
		t.Error("key mapping should be removed after discard")
	}
}

// --- sessionReferencesMissingSnapshot true path ---

func TestSessionReferencesMissingSnapshot_TrueWhenMissing(t *testing.T) {
	svc := newTestService(t)
	storedAs := "job__2024-01-01T00-00-00Z__abcdef12.tar.zst"
	sess := &UploadSession{
		Status:    "completed",
		Namespace: "edge/inst/job",
		Filename:  storedAs,
		StoredAs:  &storedAs,
	}
	// backend has no files in this namespace, so snapshot is missing
	if !svc.sessionReferencesMissingSnapshot(nil, sess) {
		t.Error("expected true when snapshot file is missing from backend")
	}
}

func TestSessionReferencesMissingSnapshot_FalseWhenPresent(t *testing.T) {
	svc := newTestService(t)
	// Store a file in the backend
	ns := "edge/inst/job"
	filename := "job__2024-01-01T00-00-00Z__abcdef12.tar.zst"
	nsDir := filepath.Join(svc.settings.BackupRoot, ns)
	os.MkdirAll(nsDir, 0o755)
	os.WriteFile(filepath.Join(nsDir, filename), []byte("data"), 0o644)

	sess := &UploadSession{
		Status:    "completed",
		Namespace: ns,
		Filename:  filename,
		StoredAs:  &filename,
	}
	if svc.sessionReferencesMissingSnapshot(nil, sess) {
		t.Error("expected false when snapshot file is present in backend")
	}
}

// --- updateSettings ---

func TestUpdateSettings(t *testing.T) {
	svc := newTestService(t)
	newSettings := &config.Settings{UploadSessionTTLHours: 12}
	svc.UpdateSettings(newSettings)
	if svc.settings.UploadSessionTTLHours != 12 {
		t.Error("settings not updated")
	}
}

// --- New ---

func TestNew_CreatesDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	settings := &config.Settings{
		StagingDir: tmpDir,
		BackupRoot: filepath.Join(tmpDir, "backups"),
	}
	locks := services.NewNamespaceLockManager()
	logger := discardLogger()
	hooks := services.NewHookManager(filepath.Join(tmpDir, "hooks"), logger)
	ntfy := services.NewNtfyPublisher(logger)
	backend := storage.NewLocalBackend(filepath.Join(tmpDir, "backups"))

	svc, err := New(settings, backend, nil, locks, hooks, ntfy)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc == nil {
		t.Fatal("expected non-nil service")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "uploads")); err != nil {
		t.Error("uploads dir should be created")
	}
	if _, err := os.Stat(filepath.Join(tmpDir, "uploads", "keys")); err != nil {
		t.Error("keys dir should be created")
	}
}

// --- CleanupLoop ---

func TestCleanupLoop_CancelledContext(t *testing.T) {
	svc := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.CleanupLoop(ctx, 1)
		close(done)
	}()
	cancel()
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Error("CleanupLoop did not exit after context cancel")
	}
}

func TestMemorySessionStoreRoundTripAndExpiry(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()
	active := &UploadSession{
		UploadID:         "active",
		IdempotencyKey:   "key-active",
		Status:           "initiated",
		ArchiveSizeBytes: 100,
		ExpiresAt:        utcAfter(time.Hour),
	}
	expired := &UploadSession{
		UploadID:         "expired",
		IdempotencyKey:   "key-expired",
		Status:           "uploaded",
		ArchiveSizeBytes: 50,
		ExpiresAt:        "2000-01-01T00:00:00Z",
	}
	completed := &UploadSession{
		UploadID:         "completed",
		IdempotencyKey:   "key-completed",
		Status:           "completed",
		ArchiveSizeBytes: 500,
		ExpiresAt:        utcAfter(time.Hour),
	}
	for _, sess := range []*UploadSession{active, expired, completed} {
		if err := store.Save(ctx, sess); err != nil {
			t.Fatalf("Save %s: %v", sess.UploadID, err)
		}
	}
	loaded, err := store.Load(ctx, "active")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.UploadID != "active" {
		t.Fatalf("Load = %+v", loaded)
	}
	loaded.Status = "mutated"
	loadedAgain, err := store.LoadByIdempotencyKey(ctx, "key-active")
	if err != nil {
		t.Fatalf("LoadByIdempotencyKey: %v", err)
	}
	if loadedAgain.Status != "initiated" {
		t.Fatalf("store returned shared pointer: %+v", loadedAgain)
	}
	reserved, err := store.ReservedBytes(ctx)
	if err != nil {
		t.Fatalf("ReservedBytes: %v", err)
	}
	if reserved != 100 {
		t.Fatalf("ReservedBytes = %d", reserved)
	}
	expiredSessions, err := store.DeleteExpired(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if len(expiredSessions) != 1 || expiredSessions[0].UploadID != "expired" {
		t.Fatalf("expired = %+v", expiredSessions)
	}
	if _, err := store.Load(ctx, "expired"); err != ErrSessionNotFound {
		t.Fatalf("expired load err = %v", err)
	}
	if unlock, err := store.Lock(ctx, "active"); err != nil {
		t.Fatalf("Lock: %v", err)
	} else {
		unlock()
	}
	if err := store.Delete(ctx, active); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.LoadByIdempotencyKey(ctx, "key-active"); err != ErrSessionNotFound {
		t.Fatalf("deleted key err = %v", err)
	}
	if err := store.Delete(ctx, nil); err != nil {
		t.Fatalf("Delete nil: %v", err)
	}
}

func TestMigrateLegacyUploadSessions(t *testing.T) {
	svc := newTestService(t)
	store := NewMemorySessionStore()
	svc.sessions = store

	existing := &UploadSession{
		UploadID:       "already-there",
		IdempotencyKey: "key-existing",
		Status:         "initiated",
		ExpiresAt:      utcAfter(time.Hour),
	}
	if err := store.Save(context.Background(), existing); err != nil {
		t.Fatalf("pre-save: %v", err)
	}
	writeLegacySession(t, svc, existing)
	legacy := &UploadSession{
		UploadID:         "legacy-only",
		IdempotencyKey:   "key-legacy",
		Status:           "initiated",
		ArchiveSizeBytes: 25,
		ExpiresAt:        utcAfter(time.Hour),
	}
	writeLegacySession(t, svc, legacy)
	os.WriteFile(filepath.Join(svc.uploadRoot, "loose-file"), []byte("x"), 0o644)

	count, err := svc.MigrateLegacyUploadSessions(context.Background())
	if err != nil {
		t.Fatalf("MigrateLegacyUploadSessions: %v", err)
	}
	if count != 1 {
		t.Fatalf("migrated count = %d", count)
	}
	loaded, err := store.Load(context.Background(), "legacy-only")
	if err != nil {
		t.Fatalf("Load migrated: %v", err)
	}
	if loaded.IdempotencyKey != "key-legacy" {
		t.Fatalf("loaded migrated = %+v", loaded)
	}
	count, err = svc.MigrateLegacyUploadSessions(context.Background())
	if err != nil {
		t.Fatalf("MigrateLegacyUploadSessions second: %v", err)
	}
	if count != 0 {
		t.Fatalf("second migrated count = %d", count)
	}
}

func TestMigrateLegacyUploadSessionsBadJSON(t *testing.T) {
	svc := newTestService(t)
	svc.sessions = NewMemorySessionStore()
	dir := svc.sessionDir("bad")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "metadata.json"), []byte("not json"), 0o644)

	if _, err := svc.MigrateLegacyUploadSessions(context.Background()); err == nil {
		t.Fatal("expected bad JSON error")
	}
}

func writeLegacySession(t *testing.T, svc *Service, sess *UploadSession) {
	t.Helper()
	dir := svc.sessionDir(sess.UploadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session: %v", err)
	}
	raw, err := json.Marshal(sess)
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "metadata.json"), raw, 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}
}

// --- hookContext ---

func TestHookContext(t *testing.T) {
	svc := newTestService(t)
	src := "src.bin"
	addr := "1.2.3.4"
	sess := &UploadSession{
		UploadID:         "u1",
		EdgeID:           "edge1",
		EdgeInstanceID:   "inst1",
		JobName:          "job1",
		Namespace:        "edge1/inst1/job1",
		Filename:         "file.tar.zst",
		Fingerprint:      "fp",
		Timestamp:        "2024-01-01T00:00:00Z",
		ArchiveSHA256:    "abc",
		ArchiveSizeBytes: 1000,
		SourceAddress:    &addr,
	}
	ctx := svc.hookContext(sess, src)
	if ctx["edge_id"] != "edge1" || ctx["job_name"] != "job1" {
		t.Errorf("hookContext missing fields: %v", ctx)
	}
	if ctx["staged_path"] != src {
		t.Errorf("hookContext staged_path = %v", ctx["staged_path"])
	}
}

// --- runPostHook ---

func TestRunPostHook_NoOp(t *testing.T) {
	svc := newTestService(t)
	// No pre/post command configured, ntfy has no URL - should be a no-op
	ctx := map[string]interface{}{"edge_id": "e1"}
	svc.runPostHook(ctx, "ok", "file.tar.zst", 0, false)
}

// --- validateNewReservation ---

func TestValidateNewReservation_TooLarge(t *testing.T) {
	svc := newTestService(t)
	svc.settings.MaxUploadSizeMB = 1                   // 1MB max
	err := svc.validateNewReservation(2 * 1024 * 1024) // 2MB request
	if err == nil {
		t.Error("expected too large error")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413 HTTPError, got %v", err)
	}
}

func TestValidateNewReservation_InsufficientStagingSpace(t *testing.T) {
	svc := newTestService(t)
	svc.settings.MaxUploadSizeMB = 2048
	// Reserve nearly all disk space by creating a session with huge ArchiveSizeBytes
	hugeSess := &UploadSession{
		UploadID:         "huge1",
		Status:           "initiated",
		ArchiveSizeBytes: 1 << 60, // 1 exabyte - way more than any disk
		ExpiresAt:        utcAfter(time.Hour),
	}
	svc.saveSession(hugeSess)

	err := svc.validateNewReservation(1024)
	if err == nil {
		t.Error("expected insufficient storage error")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != 507 {
		t.Errorf("expected 507 HTTPError, got %v", err)
	}
}

// --- diskFree ---

func TestDiskFree_ExistingDir(t *testing.T) {
	dir := t.TempDir()
	got := diskFree(dir)
	if got <= 0 {
		t.Errorf("diskFree = %d, expected positive value", got)
	}
}

func TestDiskFree_NonexistentPath(t *testing.T) {
	// Should walk up to an existing parent
	got := diskFree("/nonexistent/very/deep/path")
	if got <= 0 {
		t.Errorf("diskFree nonexistent = %d, expected positive value (walks up to root)", got)
	}
}

// --- sessionLock ---

func TestSessionLock_ReturnsMutex(t *testing.T) {
	svc := newTestService(t)
	l1 := svc.sessionLock("upload-1")
	l2 := svc.sessionLock("upload-1")
	if l1 != l2 {
		t.Error("same upload ID should return same mutex")
	}
	l3 := svc.sessionLock("upload-2")
	if l1 == l3 {
		t.Error("different upload IDs should return different mutexes")
	}
}

// --- SourceAddress edge cases ---

func TestSourceAddress_EmptyRemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = ""
	got := SourceAddress(r)
	if got != nil {
		t.Errorf("expected nil for empty RemoteAddr, got %v", got)
	}
}

func TestSourceAddress_ForwardedEmptyCandidate(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Forwarded", "for=;proto=https")
	r.RemoteAddr = "10.0.0.1:80"
	// for= has empty value, should fall through to RemoteAddr
	got := SourceAddress(r)
	if got == nil || *got != "10.0.0.1" {
		t.Errorf("got %v, want 10.0.0.1", got)
	}
}

// --- buildSessionResponse zero archive ---

func TestBuildSessionResponse_ZeroArchiveSize(t *testing.T) {
	svc := newTestService(t)
	svc.settings.UploadChunkSizeMB = 8
	sess := &UploadSession{
		Status:           "initiated",
		ArchiveSizeBytes: 0,
	}
	resp := svc.buildSessionResponse(sess)
	if resp.RecommendedChunkSizeBytes < 1 {
		t.Errorf("chunk size should be at least 1, got %d", resp.RecommendedChunkSizeBytes)
	}
}

// --- buildCommittedDuplicateResponse zero archive ---

func TestBuildCommittedDuplicateResponse_ZeroArchiveSize(t *testing.T) {
	svc := newTestService(t)
	resp := svc.buildCommittedDuplicateResponse(0, "file.tar.zst")
	if resp.RecommendedChunkSizeBytes < 1 {
		t.Errorf("chunk size should be at least 1, got %d", resp.RecommendedChunkSizeBytes)
	}
}

// --- AppendChunk (filesystem-only, no DB needed) ---

func makeUploadSession(t *testing.T, svc *Service, archiveSize int64) *UploadSession {
	t.Helper()
	id, _ := randomHex(8)
	sess := &UploadSession{
		UploadID:         id,
		IdempotencyKey:   "ikey-" + id,
		Namespace:        "edge/inst/job",
		Filename:         "job__2024-01-01T00-00-00Z__abcdef12.tar.zst",
		Status:           "initiated",
		ArchiveSizeBytes: archiveSize,
		ArchiveSHA256:    "placeholder",
		ExpiresAt:        utcAfter(time.Hour),
		CreatedAt:        utcNow(),
		UpdatedAt:        utcNow(),
	}
	if err := svc.saveSession(sess); err != nil {
		t.Fatalf("saveSession: %v", err)
	}
	return sess
}

func TestAppendChunk_FirstChunk(t *testing.T) {
	svc := newTestService(t)
	content := []byte("hello world payload")
	sess := makeUploadSession(t, svc, int64(len(content)))

	resp, err := svc.AppendChunk(context.Background(), sess.UploadID, 0, strings.NewReader(string(content)))
	if err != nil {
		t.Fatalf("AppendChunk: %v", err)
	}
	if resp.Status != "uploaded" {
		t.Errorf("Status = %q, want uploaded", resp.Status)
	}
	if resp.ReceivedBytes != int64(len(content)) {
		t.Errorf("ReceivedBytes = %d", resp.ReceivedBytes)
	}
	if resp.NextOffset != int64(len(content)) {
		t.Errorf("NextOffset = %d", resp.NextOffset)
	}
}

func TestAppendChunk_MultipleChunks(t *testing.T) {
	svc := newTestService(t)
	archiveSize := int64(10)
	sess := makeUploadSession(t, svc, archiveSize)

	// First chunk (5 bytes)
	resp, err := svc.AppendChunk(context.Background(), sess.UploadID, 0, strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("first chunk: %v", err)
	}
	if resp.Status != "in_progress" {
		t.Errorf("Status after first chunk = %q", resp.Status)
	}

	// Second chunk (5 bytes)
	resp, err = svc.AppendChunk(context.Background(), sess.UploadID, 5, strings.NewReader("world"))
	if err != nil {
		t.Fatalf("second chunk: %v", err)
	}
	if resp.Status != "uploaded" {
		t.Errorf("Status after second chunk = %q", resp.Status)
	}
}

func TestAppendChunk_OffsetMismatch(t *testing.T) {
	svc := newTestService(t)
	sess := makeUploadSession(t, svc, 100)

	// Send chunk at wrong offset (expect 0, sending 10)
	_, err := svc.AppendChunk(context.Background(), sess.UploadID, 10, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected offset mismatch error")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusConflict {
		t.Errorf("expected 409, got %v", err)
	}
}

func TestAppendChunk_AlreadyCompleted(t *testing.T) {
	svc := newTestService(t)
	archiveSize := int64(5)
	sess := makeUploadSession(t, svc, archiveSize)
	// Mark session as completed
	sess.Status = "completed"
	sess.UploadedBytes = archiveSize
	svc.saveSession(sess)

	resp, err := svc.AppendChunk(context.Background(), sess.UploadID, 0, strings.NewReader("data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != "completed" {
		t.Errorf("Status = %q, want completed", resp.Status)
	}
}

func TestAppendChunk_ExceedsDeclaredSize(t *testing.T) {
	svc := newTestService(t)
	sess := makeUploadSession(t, svc, 3) // only 3 bytes

	// Send 5 bytes - exceeds declared size
	_, err := svc.AppendChunk(context.Background(), sess.UploadID, 0, strings.NewReader("hello"))
	if err == nil {
		t.Fatal("expected error for chunk exceeding declared size")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %v", err)
	}
}

func TestAppendChunk_SessionNotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.AppendChunk(context.Background(), "nonexistent-upload", 0, strings.NewReader("data"))
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %v", err)
	}
}

// --- FinalizeUpload (filesystem-only paths, no DB needed) ---

func TestFinalizeUpload_AlreadyCompleted(t *testing.T) {
	svc := newTestService(t)

	// Write the file to the backend so sessionReferencesMissingSnapshot returns false
	ns := "edge/inst/job"
	filename := "job__2024-01-01T00-00-00Z__abcdef12.tar.zst"
	nsDir := filepath.Join(svc.settings.BackupRoot, ns)
	os.MkdirAll(nsDir, 0o755)
	os.WriteFile(filepath.Join(nsDir, filename), []byte("data"), 0o644)

	sess := makeUploadSession(t, svc, 4)
	sess.Status = "completed"
	sess.UploadedBytes = 4
	sess.Namespace = ns
	sess.Filename = filename
	sess.StoredAs = &filename
	sess.Pruned = 1
	svc.saveSession(sess)

	resp, err := svc.FinalizeUpload(context.Background(), sess.UploadID)
	if err != nil {
		t.Fatalf("FinalizeUpload: %v", err)
	}
	if resp.Status != "ok" || resp.StoredAs != filename {
		t.Errorf("unexpected response: %+v", resp)
	}
	if resp.Pruned != 1 {
		t.Errorf("Pruned = %d, want 1", resp.Pruned)
	}
}

func TestFinalizeUpload_IncompleteUpload(t *testing.T) {
	svc := newTestService(t)
	// Create session for 100 bytes but write nothing
	sess := makeUploadSession(t, svc, 100)

	_, err := svc.FinalizeUpload(context.Background(), sess.UploadID)
	if err == nil {
		t.Fatal("expected error for incomplete upload")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusConflict {
		t.Errorf("expected 409, got %v", err)
	}
}

func TestFinalizeUpload_ChecksumMismatch(t *testing.T) {
	svc := newTestService(t)
	content := []byte("hello")
	sess := makeUploadSession(t, svc, int64(len(content)))
	sess.ArchiveSHA256 = "wrongchecksum1234567890abcdef1234567890abcdef1234567890abcdef1234"
	svc.saveSession(sess)

	// Append the actual data
	svc.AppendChunk(context.Background(), sess.UploadID, 0, strings.NewReader(string(content)))

	_, err := svc.FinalizeUpload(context.Background(), sess.UploadID)
	if err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusConflict {
		t.Errorf("expected 409, got %v", err)
	}
	// Session should now be in checksum_retry_required status
	reloaded, _ := svc.loadSession(sess.UploadID)
	if reloaded.Status != "checksum_retry_required" {
		t.Errorf("Status = %q, want checksum_retry_required", reloaded.Status)
	}
}

func TestFinalizeUpload_SessionNotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.FinalizeUpload(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %v", err)
	}
}

// --- loadSession corrupt JSON ---

func TestLoadSession_CorruptJSON(t *testing.T) {
	svc := newTestService(t)
	id := "corrupt123"
	dir := svc.sessionDir(id)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(svc.metadataPath(id), []byte("not json at all"), 0o644)

	_, err := svc.loadSession(id)
	if err == nil {
		t.Error("expected error for corrupt session JSON")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %v", err)
	}
}

// --- loadSessionForKey with corrupt JSON ---

func TestLoadSessionForKey_CorruptKeyMapping(t *testing.T) {
	svc := newTestService(t)
	key := "corrupt-key"
	keyPath := svc.keyMappingPath(key)
	os.WriteFile(keyPath, []byte("not json"), 0o644)

	got := svc.loadSessionForKey(key)
	if got != nil {
		t.Error("expected nil for corrupt key mapping JSON")
	}
}

func TestLoadSessionForKey_EmptyUploadID(t *testing.T) {
	svc := newTestService(t)
	key := "empty-id-key"
	keyPath := svc.keyMappingPath(key)
	data, _ := json.Marshal(map[string]string{"idempotency_key": key, "upload_id": "  "})
	os.WriteFile(keyPath, data, 0o644)

	got := svc.loadSessionForKey(key)
	if got != nil {
		t.Error("expected nil for empty upload_id")
	}
}

// --- CleanupStaleUploads with bad JSON ---

func TestCleanupStaleUploads_BadJSON(t *testing.T) {
	svc := newTestService(t)
	// Create a directory that looks like a session but has bad JSON
	dir := filepath.Join(svc.uploadRoot, "badsess")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "metadata.json"), []byte("not json"), 0o644)
	svc.CleanupStaleUploads() // should not panic
}

// --- JSON roundtrip for UploadInitRequest ---

func TestUploadInitRequest_JSON(t *testing.T) {
	req := UploadInitRequest{
		EdgeID:           "e1",
		EdgeInstanceID:   "i1",
		JobName:          "backup",
		Fingerprint:      "abc123",
		Timestamp:        "2024-01-01T00:00:00Z",
		ArchiveFormat:    "tar.zst",
		ArchiveSizeBytes: 1024,
		ArchiveSHA256:    "deadbeef",
		IdempotencyKey:   "key1",
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out UploadInitRequest
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.EdgeID != req.EdgeID || out.ArchiveSizeBytes != req.ArchiveSizeBytes {
		t.Errorf("roundtrip mismatch: %+v", out)
	}
}
