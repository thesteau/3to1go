package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/relay/central/internal/store"
)

func newServiceWithIndex(t *testing.T, idx snapshotIndexer) *Service {
	t.Helper()
	svc := newTestService(t)
	svc.index = idx
	return svc
}

// computeSHA256 returns the hex-encoded SHA256 of data.
func computeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// --- StartUpload ---

func TestStartUpload_NewSession(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{})

	req := UploadInitRequest{
		EdgeID:           "edge1",
		EdgeInstanceID:   "", // empty -> registerEdge is a no-op
		JobName:          "backup",
		Fingerprint:      "abcdef12",
		Timestamp:        "2024-01-01T00:00:00Z",
		ArchiveFormat:    "tar.zst",
		ArchiveSizeBytes: 100,
		ArchiveSHA256:    "deadbeef",
		IdempotencyKey:   "key-new-1",
	}

	resp, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err != nil {
		t.Fatalf("StartUpload: %v", err)
	}
	if resp.Status != "initiated" {
		t.Errorf("Status = %q, want initiated", resp.Status)
	}
	if resp.Duplicate {
		t.Error("Duplicate should be false for new session")
	}
	if resp.UploadID == "" {
		t.Error("UploadID should be set")
	}
}

func TestStartUpload_IdempotencyKeyReturnsExisting(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{})

	req := UploadInitRequest{
		JobName:          "backup",
		Fingerprint:      "fp1",
		Timestamp:        "2024-01-01T00:00:00Z",
		ArchiveFormat:    "tar.zst",
		ArchiveSizeBytes: 100,
		ArchiveSHA256:    "sha256abc",
		IdempotencyKey:   "idem-key-1",
	}

	resp1, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err != nil {
		t.Fatalf("first StartUpload: %v", err)
	}
	resp2, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err != nil {
		t.Fatalf("second StartUpload: %v", err)
	}
	if resp1.UploadID != resp2.UploadID {
		t.Errorf("UploadID changed on idempotent retry: %q != %q", resp1.UploadID, resp2.UploadID)
	}
}

func TestStartUpload_IdempotencyKeyChecksumConflict(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{})

	req := UploadInitRequest{
		JobName:          "job",
		Fingerprint:      "fp",
		Timestamp:        "2024-01-01T00:00:00Z",
		ArchiveSizeBytes: 100,
		ArchiveSHA256:    "sha-a",
		IdempotencyKey:   "conflict-key",
	}
	svc.StartUpload(context.Background(), req, nil, nil)

	req.ArchiveSHA256 = "sha-b"
	_, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err == nil {
		t.Fatal("expected conflict error for mismatched checksum")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusConflict {
		t.Errorf("expected 409, got %v", err)
	}
}

func TestStartUpload_CommittedDuplicate(t *testing.T) {
	storedAs := "job__2024-01-01T00-00-00Z__abcdef12.tar.zst"
	svc := newServiceWithIndex(t, &mockIndex{
		findDuplicateResult: &store.SnapshotEntry{StoredAs: storedAs},
	})

	req := UploadInitRequest{
		JobName:          "job",
		Fingerprint:      "abcdef12",
		Timestamp:        "2024-01-01T00:00:00Z",
		ArchiveSizeBytes: 100,
		ArchiveSHA256:    "known-sha256",
		IdempotencyKey:   "dup-key",
	}

	resp, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err != nil {
		t.Fatalf("StartUpload: %v", err)
	}
	if !resp.Duplicate {
		t.Error("Duplicate should be true for committed duplicate")
	}
	if resp.Status != "completed" {
		t.Errorf("Status = %q, want completed", resp.Status)
	}
	if resp.StoredAs == nil || *resp.StoredAs != storedAs {
		t.Errorf("StoredAs = %v, want %q", resp.StoredAs, storedAs)
	}
}

func TestStartUpload_FindDuplicateError(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{findDuplicateErr: errors.New("db down")})

	req := UploadInitRequest{
		JobName: "job", Fingerprint: "fp", Timestamp: "2024-01-01T00:00:00Z",
		ArchiveSizeBytes: 100, ArchiveSHA256: "sha", IdempotencyKey: "key-err",
	}
	_, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err == nil {
		t.Fatal("expected error when FindDuplicate fails")
	}
}

func TestStartUpload_UploadTooLarge(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{})
	svc.settings.MaxUploadSizeMB = 1

	req := UploadInitRequest{
		JobName: "job", Fingerprint: "fp", Timestamp: "2024-01-01T00:00:00Z",
		ArchiveSizeBytes: 10 * 1024 * 1024, // 10MB > 1MB limit
		ArchiveSHA256:    "sha", IdempotencyKey: "key-large",
	}
	_, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err == nil {
		t.Fatal("expected error for too-large upload")
	}
	he, ok := err.(*HTTPError)
	if !ok || he.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("expected 413, got %v", err)
	}
}

func TestStartUpload_WithEdgeInstanceID_NewRegistration(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{getEdgeRegistrationResult: nil})

	req := UploadInitRequest{
		EdgeID: "edge1", EdgeInstanceID: "inst1",
		JobName: "job", Fingerprint: "fp", Timestamp: "2024-01-01T00:00:00Z",
		ArchiveSizeBytes: 50, ArchiveSHA256: "sha", IdempotencyKey: "key-new-inst",
	}
	resp, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err != nil {
		t.Fatalf("StartUpload with EdgeInstanceID: %v", err)
	}
	if resp.Status != "initiated" {
		t.Errorf("Status = %q", resp.Status)
	}
}

func TestStartUpload_WithEdgeInstanceID_ExistingRegistration(t *testing.T) {
	existing := &store.EdgeRegistration{
		EdgeID: "edge1", EdgeInstanceID: "inst1",
		FirstSeenAt: "2024-01-01T00:00:00Z", LastSeenAt: "2024-01-01T00:00:00Z",
	}
	svc := newServiceWithIndex(t, &mockIndex{getEdgeRegistrationResult: existing})

	req := UploadInitRequest{
		EdgeID: "edge1", EdgeInstanceID: "inst1",
		JobName: "job", Fingerprint: "fp", Timestamp: "2024-01-01T00:00:00Z",
		ArchiveSizeBytes: 50, ArchiveSHA256: "sha", IdempotencyKey: "key-exist-edge",
	}
	_, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err != nil {
		t.Fatalf("StartUpload with existing edge: %v", err)
	}
}

func TestStartUpload_RegisterEdgeGetError(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{getEdgeRegistrationErr: errors.New("db error")})

	req := UploadInitRequest{
		EdgeID: "edge1", EdgeInstanceID: "inst1",
		JobName: "job", Fingerprint: "fp", Timestamp: "2024-01-01T00:00:00Z",
		ArchiveSizeBytes: 50, ArchiveSHA256: "sha", IdempotencyKey: "key-err-edge",
	}
	_, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err == nil {
		t.Fatal("expected error when GetEdgeRegistration fails")
	}
}

func TestStartUpload_RegisterEdgeUpsertError(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{upsertEdgeRegistrationErr: errors.New("db error")})

	req := UploadInitRequest{
		EdgeID: "edge1", EdgeInstanceID: "inst1",
		JobName: "job", Fingerprint: "fp", Timestamp: "2024-01-01T00:00:00Z",
		ArchiveSizeBytes: 50, ArchiveSHA256: "sha", IdempotencyKey: "key-upsert-err",
	}
	_, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err == nil {
		t.Fatal("expected error when UpsertEdgeRegistration fails")
	}
}

func TestStartUpload_WithCredentialHashAndURL(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{})

	credHash := "cred-hash-abc"
	advURL := "https://edge.example.com"
	req := UploadInitRequest{
		EdgeID: "edge1", EdgeInstanceID: "inst1",
		JobName: "job", Fingerprint: "fp", Timestamp: "2024-01-01T00:00:00Z",
		ArchiveSizeBytes: 50, ArchiveSHA256: "sha", IdempotencyKey: "key-cred",
		AdvertisedURL: &advURL,
	}
	_, err := svc.StartUpload(context.Background(), req, nil, &credHash)
	if err != nil {
		t.Fatalf("StartUpload with cred hash: %v", err)
	}
}

func TestStartUpload_DiscardsMissingIdempotentSession(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{})

	// Create a completed session referencing a missing snapshot
	idemKey := "idem-missing-snap"
	uploadID := "upload-completed-123"
	storedAs := "job__2024-01-01T00-00-00Z__abcdef12.tar.zst"
	ns := "edge/inst/job"

	completedSess := &UploadSession{
		UploadID:         uploadID,
		IdempotencyKey:   idemKey,
		Status:           "completed",
		Namespace:        ns,
		Filename:         storedAs,
		StoredAs:         &storedAs,
		ArchiveSHA256:    "sha-old",
		ArchiveSizeBytes: 100,
		ExpiresAt:        utcAfter(svc.sessionTTL()),
	}
	svc.saveSession(completedSess)
	svc.writeKeyMapping(idemKey, uploadID)
	// Backend has no file for ns -> sessionReferencesMissingSnapshot returns true -> session discarded

	req := UploadInitRequest{
		EdgeID: "edge", EdgeInstanceID: "inst",
		JobName: "job", Fingerprint: "newfp", Timestamp: "2024-06-01T00:00:00Z",
		ArchiveSizeBytes: 50, ArchiveSHA256: "sha-new", IdempotencyKey: idemKey,
	}
	resp, err := svc.StartUpload(context.Background(), req, nil, nil)
	if err != nil {
		t.Fatalf("StartUpload after discarding missing session: %v", err)
	}
	if resp.UploadID == uploadID {
		t.Error("expected new session, got old completed session ID")
	}
}

// --- saveSession JSON marshal error ---

func TestSaveSession_MarshalError(t *testing.T) {
	svc := newTestService(t)
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("mock marshal failure") }
	defer func() { jsonMarshal = orig }()

	err := svc.saveSession(&UploadSession{UploadID: "fail-sess"})
	if err == nil {
		t.Error("expected error when json.Marshal fails")
	}
}

// --- writeKeyMapping JSON marshal error ---

func TestWriteKeyMapping_MarshalError(t *testing.T) {
	svc := newTestService(t)
	orig := jsonMarshal
	jsonMarshal = func(v any) ([]byte, error) { return nil, errors.New("mock marshal failure") }
	defer func() { jsonMarshal = orig }()

	if err := svc.writeKeyMapping("key", "upload-id"); err == nil {
		t.Error("expected error when json.Marshal fails")
	}
}

// --- FinalizeUpload happy path ---

func TestFinalizeUpload_Success(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{})

	content := []byte("archive content here")
	archiveSHA := computeSHA256(content)

	sess := makeUploadSession(t, svc, int64(len(content)))
	sess.ArchiveSHA256 = archiveSHA
	svc.saveSession(sess)

	svc.AppendChunk(context.Background(), sess.UploadID, 0, strings.NewReader(string(content)))

	resp, err := svc.FinalizeUpload(context.Background(), sess.UploadID)
	if err != nil {
		t.Fatalf("FinalizeUpload: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("Status = %q, want ok", resp.Status)
	}
	if resp.Duplicate {
		t.Error("Duplicate should be false")
	}
	if resp.StoredAs == "" {
		t.Error("StoredAs should be set")
	}
}

func TestFinalizeUpload_DuplicateUnderLock(t *testing.T) {
	storedAs := "existing__2024-01-01T00-00-00Z__abcdef12.tar.zst"
	svc := newServiceWithIndex(t, &mockIndex{
		findDuplicateResult: &store.SnapshotEntry{StoredAs: storedAs},
	})

	content := []byte("archive data")
	archiveSHA := computeSHA256(content)
	sess := makeUploadSession(t, svc, int64(len(content)))
	sess.ArchiveSHA256 = archiveSHA
	svc.saveSession(sess)
	svc.AppendChunk(context.Background(), sess.UploadID, 0, strings.NewReader(string(content)))

	resp, err := svc.FinalizeUpload(context.Background(), sess.UploadID)
	if err != nil {
		t.Fatalf("FinalizeUpload: %v", err)
	}
	if !resp.Duplicate {
		t.Error("Duplicate should be true when FindDuplicate returns a result under lock")
	}
	if resp.StoredAs != storedAs {
		t.Errorf("StoredAs = %q, want %q", resp.StoredAs, storedAs)
	}
}

func TestFinalizeUpload_FindDuplicateError(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{findDuplicateErr: errors.New("db down")})

	content := []byte("data")
	archiveSHA := computeSHA256(content)
	sess := makeUploadSession(t, svc, int64(len(content)))
	sess.ArchiveSHA256 = archiveSHA
	svc.saveSession(sess)
	svc.AppendChunk(context.Background(), sess.UploadID, 0, strings.NewReader(string(content)))

	_, err := svc.FinalizeUpload(context.Background(), sess.UploadID)
	if err == nil {
		t.Fatal("expected error when FindDuplicate fails")
	}
}

// --- ReconcileNamespace public method ---

func TestReconcileNamespace(t *testing.T) {
	svc := newServiceWithIndex(t, &mockIndex{})
	// Should not panic; backend has no files for this namespace
	svc.ReconcileNamespace(context.Background(), "edge/inst/job")
}
