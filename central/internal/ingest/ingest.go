package ingest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/services"
	"github.com/3to1go/central/internal/storage"
	"github.com/3to1go/central/internal/store"
)

const timeFormat = "2006-01-02T15:04:05Z"

var activeSessionStatuses = map[string]bool{
	"initiated": true, "in_progress": true, "uploaded": true, "checksum_retry_required": true,
}

// jsonMarshal is a package-level var so tests can inject a failing implementation.
var jsonMarshal = json.Marshal

// snapshotIndexer abstracts the database operations needed by Service.
type snapshotIndexer interface {
	FindDuplicate(ctx context.Context, namespace, archiveSHA string) (*store.SnapshotEntry, error)
	UpsertSnapshot(ctx context.Context, namespace string, e store.SnapshotEntry) error
	ReconcileNamespace(ctx context.Context, namespace string, files []store.StorageFile) error
	GetEdgeRegistration(ctx context.Context, edgeID, instID string) (*store.EdgeRegistration, error)
	UpsertEdgeRegistration(ctx context.Context, r *store.EdgeRegistration) error
}

// UploadSession holds state for a resumable upload.
type UploadSession struct {
	UploadID         string  `json:"upload_id"`
	IdempotencyKey   string  `json:"idempotency_key"`
	Namespace        string  `json:"namespace"`
	Filename         string  `json:"filename"`
	EdgeID           string  `json:"edge_id"`
	EdgeInstanceID   string  `json:"edge_instance_id"`
	JobName          string  `json:"job_name"`
	Fingerprint      string  `json:"fingerprint"`
	Timestamp        string  `json:"timestamp"`
	ArchiveFormat    string  `json:"archive_format"`
	ArchiveSizeBytes int64   `json:"archive_size_bytes"`
	ArchiveSHA256    string  `json:"archive_sha256"`
	SourceAddress    *string `json:"source_address"`
	UploadedBytes    int64   `json:"uploaded_bytes"`
	Status           string  `json:"status"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
	ExpiresAt        string  `json:"expires_at"`
	StoredAs         *string `json:"stored_as"`
	Pruned           int     `json:"pruned"`
	EncryptionKeyFP  *string `json:"encryption_key_fingerprint,omitempty"`
	AdvertisedURL    *string `json:"advertised_url,omitempty"`
	CredentialHash   *string `json:"credential_hash,omitempty"`
}

// UploadMetadata from the Edge client.
type UploadMetadata struct {
	EdgeID                   string
	EdgeInstanceID           string
	JobName                  string
	Fingerprint              string
	Timestamp                string
	ArchiveFormat            string
	EncryptionKeyFingerprint *string
	AdvertisedURL            *string
}

type UploadInitRequest struct {
	EdgeID                   string  `json:"edge_id"`
	EdgeInstanceID           string  `json:"edge_instance_id,omitempty"`
	JobName                  string  `json:"job_name"`
	Fingerprint              string  `json:"fingerprint"`
	Timestamp                string  `json:"timestamp"`
	ArchiveFormat            string  `json:"archive_format"`
	ArchiveSizeBytes         int64   `json:"archive_size_bytes"`
	ArchiveSHA256            string  `json:"archive_sha256"`
	IdempotencyKey           string  `json:"idempotency_key"`
	EncryptionKeyFingerprint *string `json:"encryption_key_fingerprint,omitempty"`
	AdvertisedURL            *string `json:"advertised_url,omitempty"`
}

type SessionResponse struct {
	UploadID                  string  `json:"upload_id"`
	Status                    string  `json:"status"`
	NextOffset                int64   `json:"next_offset"`
	ArchiveSizeBytes          int64   `json:"archive_size_bytes"`
	RecommendedChunkSizeBytes int64   `json:"recommended_chunk_size_bytes"`
	StoredAs                  *string `json:"stored_as,omitempty"`
	Pruned                    int     `json:"pruned"`
	Duplicate                 bool    `json:"duplicate"`
}

type ChunkResponse struct {
	UploadID      string `json:"upload_id"`
	Status        string `json:"status"`
	NextOffset    int64  `json:"next_offset"`
	ReceivedBytes int64  `json:"received_bytes"`
}

type FinalizeResponse struct {
	Status    string `json:"status"`
	StoredAs  string `json:"stored_as"`
	Pruned    int    `json:"pruned"`
	Duplicate bool   `json:"duplicate"`
}

// Service manages resumable uploads.
type Service struct {
	settings     *config.Settings
	backend      *storage.LocalBackend
	index        snapshotIndexer
	locks        *services.NamespaceLockManager
	hooks        *services.HookManager
	ntfy         *services.NtfyPublisher
	stagingDir   string
	uploadRoot   string
	keyRoot      string
	sessionLocks sync.Map
	mu           sync.Mutex
}

func New(
	settings *config.Settings,
	backend *storage.LocalBackend,
	index snapshotIndexer,
	locks *services.NamespaceLockManager,
	hooks *services.HookManager,
	ntfy *services.NtfyPublisher,
) (*Service, error) {
	stagingDir := settings.StagingDir
	uploadRoot := filepath.Join(stagingDir, "uploads")
	keyRoot := filepath.Join(uploadRoot, "keys")
	if err := os.MkdirAll(uploadRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create upload staging directory: %w", err)
	}
	if err := os.MkdirAll(keyRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create idempotency key directory: %w", err)
	}
	return &Service{
		settings:   settings,
		backend:    backend,
		index:      index,
		locks:      locks,
		hooks:      hooks,
		ntfy:       ntfy,
		stagingDir: stagingDir,
		uploadRoot: uploadRoot,
		keyRoot:    keyRoot,
	}, nil
}

func (s *Service) UpdateSettings(settings *config.Settings) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = settings
}

func (s *Service) StartUpload(ctx context.Context, req UploadInitRequest, sourceAddr, credHash *string) (*SessionResponse, error) {
	s.CleanupStaleUploads()

	meta := UploadMetadata{
		EdgeID:                   req.EdgeID,
		EdgeInstanceID:           req.EdgeInstanceID,
		JobName:                  req.JobName,
		Fingerprint:              req.Fingerprint,
		Timestamp:                req.Timestamp,
		ArchiveFormat:            req.ArchiveFormat,
		EncryptionKeyFingerprint: req.EncryptionKeyFingerprint,
		AdvertisedURL:            req.AdvertisedURL,
	}

	// Register edge under a per-edge lock
	if err := s.registerEdge(ctx, meta, credHash); err != nil {
		return nil, err
	}

	// Check idempotency
	existing := s.loadSessionForKey(req.IdempotencyKey)
	if existing != nil {
		if s.sessionReferencesMissingSnapshot(ctx, existing) {
			s.reconcileNamespace(ctx, existing.Namespace)
			s.discardSession(existing)
			existing = nil
		}
	}
	if existing != nil {
		if existing.ArchiveSHA256 != req.ArchiveSHA256 {
			return nil, httpError(http.StatusConflict, "idempotency key reused with different archive checksum")
		}
		existing.UploadedBytes = s.currentUploadSize(existing.UploadID)
		existing.UpdatedAt = utcNow()
		existing.ExpiresAt = utcAfter(s.sessionTTL())
		if err := s.saveSession(existing); err != nil {
			return nil, httpError(http.StatusInternalServerError, "failed to persist upload session")
		}
		return s.buildSessionResponse(existing), nil
	}

	// Check for committed duplicate
	dup, err := s.index.FindDuplicate(ctx, req.EdgeID+"/"+req.EdgeInstanceID+"/"+req.JobName, req.ArchiveSHA256)
	if err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to check duplicate upload")
	}
	if dup != nil {
		return s.buildCommittedDuplicateResponse(req.ArchiveSizeBytes, dup.StoredAs), nil
	}

	// Validate capacity
	if err := s.validateNewReservation(req.ArchiveSizeBytes); err != nil {
		return nil, err
	}

	namespace := req.EdgeID + "/" + req.EdgeInstanceID + "/" + req.JobName
	storedName := buildSnapshotFilename(req.JobName, req.Timestamp, req.Fingerprint)
	now := utcNow()
	uploadID, err := randomHex(16)
	if err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to create upload session")
	}
	session := &UploadSession{
		UploadID:         uploadID,
		IdempotencyKey:   req.IdempotencyKey,
		Namespace:        namespace,
		Filename:         storedName,
		EdgeID:           req.EdgeID,
		EdgeInstanceID:   req.EdgeInstanceID,
		JobName:          req.JobName,
		Fingerprint:      req.Fingerprint,
		Timestamp:        req.Timestamp,
		ArchiveFormat:    req.ArchiveFormat,
		ArchiveSizeBytes: req.ArchiveSizeBytes,
		ArchiveSHA256:    req.ArchiveSHA256,
		SourceAddress:    sourceAddr,
		UploadedBytes:    0,
		Status:           "initiated",
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        utcAfter(s.sessionTTL()),
	}
	if err := s.saveSession(session); err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to persist upload session")
	}
	if err := s.writeKeyMapping(req.IdempotencyKey, session.UploadID); err != nil {
		s.discardSession(session)
		return nil, httpError(http.StatusInternalServerError, "failed to persist upload session")
	}
	return s.buildSessionResponse(session), nil
}

func (s *Service) AppendChunk(ctx context.Context, uploadID string, offset int64, body io.Reader) (*ChunkResponse, error) {
	l := s.sessionLock(uploadID)
	l.Lock()
	defer l.Unlock()

	session, err := s.loadSession(uploadID)
	if err != nil {
		return nil, err
	}
	currentSize := s.currentUploadSize(uploadID)

	if session.Status == "completed" {
		return &ChunkResponse{
			UploadID:      uploadID,
			Status:        session.Status,
			NextOffset:    session.ArchiveSizeBytes,
			ReceivedBytes: 0,
		}, nil
	}

	if offset != currentSize {
		return nil, httpErrorJSON(http.StatusConflict, map[string]interface{}{
			"status":      "offset_mismatch",
			"next_offset": currentSize,
			"upload_id":   uploadID,
		})
	}

	stagePath := s.uploadDataPath(uploadID)
	os.MkdirAll(filepath.Dir(stagePath), 0o755)

	f, err := os.OpenFile(stagePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to persist upload chunk")
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	var bytesReceived int64
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			bytesReceived += int64(n)
			if currentSize+bytesReceived > session.ArchiveSizeBytes {
				return nil, httpError(http.StatusBadRequest, "chunk exceeds declared upload size")
			}
			if _, werr := f.Write(buf[:n]); werr != nil {
				return nil, httpError(http.StatusInternalServerError, "failed to persist upload chunk")
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, httpError(http.StatusInternalServerError, "failed to read chunk")
		}
	}
	if err := f.Sync(); err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to persist upload chunk")
	}

	session.UploadedBytes = currentSize + bytesReceived
	if session.UploadedBytes >= session.ArchiveSizeBytes {
		session.Status = "uploaded"
	} else {
		session.Status = "in_progress"
	}
	session.UpdatedAt = utcNow()
	session.ExpiresAt = utcAfter(s.sessionTTL())
	if err := s.saveSession(session); err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to persist upload session")
	}

	return &ChunkResponse{
		UploadID:      uploadID,
		Status:        session.Status,
		NextOffset:    session.UploadedBytes,
		ReceivedBytes: bytesReceived,
	}, nil
}

func (s *Service) FinalizeUpload(ctx context.Context, uploadID string) (*FinalizeResponse, error) {
	session, err := s.loadSession(uploadID)
	if err != nil {
		return nil, err
	}

	if session.Status == "completed" {
		if s.sessionReferencesMissingSnapshot(ctx, session) {
			s.reconcileNamespace(ctx, session.Namespace)
			s.discardSession(session)
			return nil, httpError(http.StatusConflict, "stored snapshot missing; re-initiate upload")
		}
		storedAs := session.Filename
		if session.StoredAs != nil {
			storedAs = *session.StoredAs
		}
		return &FinalizeResponse{Status: "ok", StoredAs: storedAs, Pruned: session.Pruned}, nil
	}

	// Acquire per-namespace lock
	nsLock := s.locks.Lock(session.Namespace)
	nsLock.Lock()
	defer nsLock.Unlock()

	// Re-read under lock
	session, err = s.loadSession(uploadID)
	if err != nil {
		return nil, err
	}
	currentSize := s.currentUploadSize(uploadID)
	if currentSize != session.ArchiveSizeBytes {
		return nil, httpErrorJSON(http.StatusConflict, map[string]interface{}{
			"status":      "incomplete_upload",
			"next_offset": currentSize,
			"upload_id":   uploadID,
		})
	}

	stagedPath := s.uploadDataPath(uploadID)
	actualSHA, err := sha256File(stagedPath)
	if err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to checksum upload")
	}
	if actualSHA != session.ArchiveSHA256 {
		os.Remove(stagedPath)
		session.UploadedBytes = 0
		session.Status = "checksum_retry_required"
		session.UpdatedAt = utcNow()
		session.ExpiresAt = utcAfter(s.sessionTTL())
		if err := s.saveSession(session); err != nil {
			return nil, httpError(http.StatusInternalServerError, "failed to persist upload session")
		}
		return nil, httpErrorJSON(http.StatusConflict, map[string]interface{}{
			"status":      "checksum_mismatch",
			"next_offset": 0,
			"upload_id":   uploadID,
		})
	}

	hookCtx := s.hookContext(session, stagedPath)
	s.hooks.RunCommand(s.settings.HookPreCommand, "pre", hookCtx)

	hookStatus := "error"
	hookStoredAs := session.Filename
	hookPruned := 0
	hookDuplicate := false
	var result *FinalizeResponse

	// Check duplicate again under lock
	dup, err := s.index.FindDuplicate(ctx, session.Namespace, actualSHA)
	if err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to check duplicate upload")
	}
	if dup != nil {
		os.Remove(stagedPath)
		session.UploadedBytes = session.ArchiveSizeBytes
		session.Status = "completed"
		storedAs := dup.StoredAs
		session.StoredAs = &storedAs
		session.Pruned = 0
		session.UpdatedAt = utcNow()
		session.ExpiresAt = utcAfter(s.sessionTTL())
		if err := s.saveSession(session); err != nil {
			return nil, httpError(http.StatusInternalServerError, "failed to persist upload session")
		}
		hookStatus = "ok"
		hookStoredAs = dup.StoredAs
		hookDuplicate = true
		result = &FinalizeResponse{Status: "ok", StoredAs: dup.StoredAs, Pruned: 0, Duplicate: true}
	} else {
		storedAs, storeErr := s.backend.Store(session.Namespace, session.Filename, stagedPath)
		if storeErr != nil {
			s.runPostHook(hookCtx, hookStatus, hookStoredAs, hookPruned, hookDuplicate)
			return nil, httpError(http.StatusInternalServerError, "failed to store archive")
		}

		pruned, _ := services.PruneOldSnapshots(s.backend, session.Namespace, s.settings.RetentionKeepLast)

		files, _ := s.backend.List(session.Namespace)
		var sizeBytes int64
		var mtime float64
		for _, f := range files {
			if f.Filename == storedAs {
				sizeBytes = f.SizeBytes
				mtime = f.Mtime
				break
			}
		}

		s.index.UpsertSnapshot(ctx, session.Namespace, store.SnapshotEntry{
			StoredAs:    storedAs,
			ArchiveSHA:  actualSHA,
			Fingerprint: session.Fingerprint,
			Timestamp:   session.Timestamp,
			SizeBytes:   sizeBytes,
			Mtime:       mtime,
		})
		storageFiles := storageFilesToIndexFiles(files)
		s.index.ReconcileNamespace(ctx, session.Namespace, storageFiles)

		session.UploadedBytes = session.ArchiveSizeBytes
		session.Status = "completed"
		session.StoredAs = &storedAs
		session.Pruned = pruned
		session.UpdatedAt = utcNow()
		session.ExpiresAt = utcAfter(s.sessionTTL())
		if err := s.saveSession(session); err != nil {
			return nil, httpError(http.StatusInternalServerError, "failed to persist upload session")
		}

		hookStatus = "ok"
		hookStoredAs = storedAs
		hookPruned = pruned
		result = &FinalizeResponse{Status: "ok", StoredAs: storedAs, Pruned: pruned}
	}

	s.runPostHook(hookCtx, hookStatus, hookStoredAs, hookPruned, hookDuplicate)
	return result, nil
}

func (s *Service) CleanupStaleUploads() {
	cutoff := time.Now().UTC()
	entries, _ := os.ReadDir(s.uploadRoot)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(s.uploadRoot, e.Name(), "metadata.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var sess UploadSession
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}
		expiresAt, err := time.ParseInLocation(timeFormat, sess.ExpiresAt, time.UTC)
		if err != nil || expiresAt.After(cutoff) {
			continue
		}
		keyPath := s.keyMappingPath(sess.IdempotencyKey)
		os.Remove(keyPath)
		os.RemoveAll(filepath.Join(s.uploadRoot, e.Name()))
	}
}

func (s *Service) CleanupLoop(ctx context.Context, intervalSeconds int) {
	ticker := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.CleanupStaleUploads()
		}
	}
}

func (s *Service) ReconcileNamespace(ctx context.Context, namespace string) {
	files, _ := s.backend.List(namespace)
	s.index.ReconcileNamespace(ctx, namespace, storageFilesToIndexFiles(files))
}

func (s *Service) registerEdge(ctx context.Context, meta UploadMetadata, credHash *string) error {
	instID := strings.TrimSpace(meta.EdgeInstanceID)
	if instID == "" {
		return nil
	}

	edgeLock := s.locks.Lock("edge-registry:" + meta.EdgeID)
	edgeLock.Lock()
	defer edgeLock.Unlock()

	existing, err := s.index.GetEdgeRegistration(ctx, meta.EdgeID, instID)
	if err != nil {
		return err
	}
	now := utcNow()

	var reg *store.EdgeRegistration
	if existing != nil {
		reg = existing
	} else {
		reg = &store.EdgeRegistration{
			EdgeID:         meta.EdgeID,
			EdgeInstanceID: instID,
			FirstSeenAt:    now,
			LastSeenAt:     now,
		}
	}
	reg.LastSeenAt = now
	if meta.EncryptionKeyFingerprint != nil {
		reg.EncryptionKeyFingerprint = meta.EncryptionKeyFingerprint
	}
	if meta.AdvertisedURL != nil {
		reg.AdvertisedURL = meta.AdvertisedURL
	}
	if credHash != nil && *credHash != "" {
		reg.CredentialHash = credHash
	}
	return s.index.UpsertEdgeRegistration(ctx, reg)
}

func (s *Service) runPostHook(hookCtx map[string]interface{}, status, storedAs string, pruned int, duplicate bool) {
	final := map[string]interface{}{}
	for k, v := range hookCtx {
		final[k] = v
	}
	final["status"] = status
	final["stored_as"] = storedAs
	final["pruned"] = pruned
	final["duplicate"] = duplicate
	s.hooks.RunCommand(s.settings.HookPostCommand, "post", final)
	if status == "ok" {
		s.ntfy.PublishBestEffort(s.settings, final)
	}
}

func (s *Service) hookContext(session *UploadSession, stagedPath string) map[string]interface{} {
	return map[string]interface{}{
		"edge_id":            session.EdgeID,
		"edge_instance_id":   session.EdgeInstanceID,
		"job_name":           session.JobName,
		"upload_id":          session.UploadID,
		"namespace":          session.Namespace,
		"filename":           session.Filename,
		"fingerprint":        session.Fingerprint,
		"timestamp":          session.Timestamp,
		"archive_sha256":     session.ArchiveSHA256,
		"archive_size_bytes": session.ArchiveSizeBytes,
		"source_address":     session.SourceAddress,
		"staged_path":        stagedPath,
	}
}

func (s *Service) validateNewReservation(archiveSizeBytes int64) error {
	if archiveSizeBytes > s.settings.MaxUploadSizeBytes() {
		return httpError(http.StatusRequestEntityTooLarge, "upload too large")
	}
	reserved := s.reservedBytes()
	stagingFree := diskFree(s.stagingDir)
	if archiveSizeBytes+reserved > stagingFree {
		return httpError(507, "insufficient staging storage")
	}
	backupFree := diskFree(s.settings.BackupRoot)
	if archiveSizeBytes+reserved > backupFree {
		return httpError(507, "insufficient backup storage")
	}
	return nil
}

func (s *Service) reservedBytes() int64 {
	var total int64
	entries, _ := os.ReadDir(s.uploadRoot)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		metaPath := filepath.Join(s.uploadRoot, e.Name(), "metadata.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var sess UploadSession
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}
		if activeSessionStatuses[sess.Status] {
			total += sess.ArchiveSizeBytes
		}
	}
	return total
}

func diskFree(path string) int64 {
	// Walk up to find an existing directory
	p := path
	for {
		info, err := os.Stat(p)
		if err == nil && info.IsDir() {
			break
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	_, _, free, err := storage.DiskUsage(p)
	if err != nil {
		return 1<<62 - 1 // very large if unknown
	}
	return free
}

func (s *Service) sessionLock(uploadID string) *sync.Mutex {
	l, _ := s.sessionLocks.LoadOrStore(uploadID, &sync.Mutex{})
	return l.(*sync.Mutex)
}

func (s *Service) buildSessionResponse(sess *UploadSession) *SessionResponse {
	nextOffset := sess.UploadedBytes
	if sess.Status == "completed" {
		nextOffset = sess.ArchiveSizeBytes
	}
	chunkSize := s.settings.UploadChunkSizeBytes()
	if chunkSize > sess.ArchiveSizeBytes {
		chunkSize = sess.ArchiveSizeBytes
	}
	if chunkSize < 1 {
		chunkSize = 1
	}
	return &SessionResponse{
		UploadID:                  sess.UploadID,
		Status:                    sess.Status,
		NextOffset:                nextOffset,
		ArchiveSizeBytes:          sess.ArchiveSizeBytes,
		RecommendedChunkSizeBytes: chunkSize,
		StoredAs:                  sess.StoredAs,
		Pruned:                    sess.Pruned,
		Duplicate:                 false,
	}
}

func (s *Service) buildCommittedDuplicateResponse(archiveSizeBytes int64, storedAs string) *SessionResponse {
	chunkSize := s.settings.UploadChunkSizeBytes()
	if chunkSize > archiveSizeBytes {
		chunkSize = archiveSizeBytes
	}
	if chunkSize < 1 {
		chunkSize = 1
	}
	return &SessionResponse{
		UploadID:                  "committed-" + storedAs,
		Status:                    "completed",
		NextOffset:                archiveSizeBytes,
		ArchiveSizeBytes:          archiveSizeBytes,
		RecommendedChunkSizeBytes: chunkSize,
		StoredAs:                  &storedAs,
		Pruned:                    0,
		Duplicate:                 true,
	}
}

func (s *Service) sessionReferencesMissingSnapshot(ctx context.Context, sess *UploadSession) bool {
	if sess.Status != "completed" {
		return false
	}
	storedName := sess.Filename
	if sess.StoredAs != nil {
		storedName = *sess.StoredAs
	}
	files, _ := s.backend.List(sess.Namespace)
	for _, f := range files {
		if f.Filename == storedName {
			return false
		}
	}
	return true
}

func (s *Service) reconcileNamespace(ctx context.Context, namespace string) {
	files, _ := s.backend.List(namespace)
	s.index.ReconcileNamespace(ctx, namespace, storageFilesToIndexFiles(files))
}

func (s *Service) discardSession(sess *UploadSession) {
	os.Remove(s.keyMappingPath(sess.IdempotencyKey))
	os.RemoveAll(s.sessionDir(sess.UploadID))
}

func (s *Service) sessionDir(uploadID string) string {
	return filepath.Join(s.uploadRoot, uploadID)
}

func (s *Service) metadataPath(uploadID string) string {
	return filepath.Join(s.sessionDir(uploadID), "metadata.json")
}

func (s *Service) uploadDataPath(uploadID string) string {
	return filepath.Join(s.sessionDir(uploadID), "archive.part")
}

func (s *Service) keyMappingPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.keyRoot, hex.EncodeToString(sum[:])+".json")
}

func (s *Service) writeKeyMapping(key, uploadID string) error {
	data, err := jsonMarshal(map[string]string{"idempotency_key": key, "upload_id": uploadID})
	if err != nil {
		return err
	}
	tmpPath := s.keyMappingPath(key) + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.keyMappingPath(key))
}

func (s *Service) loadSessionForKey(key string) *UploadSession {
	path := s.keyMappingPath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	uploadID := strings.TrimSpace(m["upload_id"])
	if uploadID == "" {
		return nil
	}
	sess, err := s.loadSession(uploadID)
	if err != nil {
		os.Remove(path)
		return nil
	}
	return sess
}

func (s *Service) loadSession(uploadID string) (*UploadSession, error) {
	metaPath := s.metadataPath(uploadID)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, httpError(http.StatusNotFound, "upload session not found")
		}
		return nil, httpError(http.StatusInternalServerError, "failed to load upload session")
	}
	var sess UploadSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to load upload session")
	}
	return &sess, nil
}

func (s *Service) saveSession(sess *UploadSession) error {
	dir := s.sessionDir(sess.UploadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := jsonMarshal(sess)
	if err != nil {
		return err
	}
	tmpPath := filepath.Join(dir, "metadata.tmp")
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.metadataPath(sess.UploadID))
}

func (s *Service) currentUploadSize(uploadID string) int64 {
	info, err := os.Stat(s.uploadDataPath(uploadID))
	if err != nil {
		return 0
	}
	return info.Size()
}

func (s *Service) sessionTTL() time.Duration {
	s.mu.Lock()
	ttl := time.Duration(s.settings.UploadSessionTTLHours) * time.Hour
	s.mu.Unlock()
	return ttl
}

// --- helpers ---

func utcNow() string {
	return time.Now().UTC().Format(timeFormat)
}

func utcAfter(d time.Duration) string {
	return time.Now().UTC().Add(d).Format(timeFormat)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var safeComponentRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func ValidateNamespaceComponent(value, fieldName string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || !safeComponentRE.MatchString(normalized) {
		return "", fmt.Errorf("%s contains invalid characters", fieldName)
	}
	return normalized, nil
}

func buildSnapshotFilename(jobName, timestamp, fingerprint string) string {
	t, err := time.ParseInLocation(timeFormat, timestamp, time.UTC)
	if err != nil {
		return fmt.Sprintf("%s__%s__%s.tar.zst", jobName, timestamp, fingerprintPrefix(fingerprint))
	}
	tStr := t.Format("2006-01-02T15-04-05Z")
	return fmt.Sprintf("%s__%s__%s.tar.zst", jobName, tStr, fingerprintPrefix(fingerprint))
}

func fingerprintPrefix(fingerprint string) string {
	if len(fingerprint) <= 8 {
		return fingerprint
	}
	return fingerprint[:8]
}

func storageFilesToIndexFiles(files []storage.StorageFile) []store.StorageFile {
	out := make([]store.StorageFile, len(files))
	for i, f := range files {
		out[i] = store.StorageFile{Filename: f.Filename, SizeBytes: f.SizeBytes, Mtime: f.Mtime}
	}
	return out
}

// HTTPError wraps an HTTP error with status code.
type HTTPError struct {
	Code    int
	Message interface{}
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http %d: %v", e.Code, e.Message)
}

func httpError(code int, msg string) *HTTPError {
	return &HTTPError{Code: code, Message: msg}
}

func httpErrorJSON(code int, body interface{}) *HTTPError {
	return &HTTPError{Code: code, Message: body}
}

// SourceAddress extracts client IP from request headers.
func SourceAddress(r *http.Request) *string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		candidate := strings.TrimSpace(parts[0])
		if candidate != "" {
			return &candidate
		}
	}
	if fwd := r.Header.Get("Forwarded"); fwd != "" {
		first := strings.SplitN(fwd, ",", 2)[0]
		for _, part := range strings.Split(first, ";") {
			p := strings.TrimSpace(part)
			if !strings.HasPrefix(strings.ToLower(p), "for=") {
				continue
			}
			candidate := strings.TrimPrefix(p, "for=")
			candidate = strings.TrimPrefix(strings.TrimSuffix(candidate, `"`), `"`)
			if strings.HasPrefix(candidate, "[") && strings.HasSuffix(candidate, "]") {
				candidate = candidate[1 : len(candidate)-1]
			}
			if strings.HasPrefix(candidate, "_") {
				return nil
			}
			if candidate != "" {
				return &candidate
			}
		}
	}
	host := r.RemoteAddr
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	if host == "" {
		return nil
	}
	return &host
}
