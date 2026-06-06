package ingest

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/services/retention"
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

type ingestBackend interface {
	Store(namespace, filename, stagedPath string) (string, error)
	List(namespace string) ([]storage.StorageFile, error)
	Delete(namespace, filename string) error
}

type namespaceLocks interface {
	Lock(namespace string) *sync.Mutex
}

type hookRunner interface {
	RunCommand(command, phase string, hookCtx map[string]any)
}

type ntfyBroadcaster interface {
	PublishBestEffort(s *config.Settings, ctx map[string]any)
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
	backend      ingestBackend
	index        snapshotIndexer
	sessions     UploadSessionStore
	locks        namespaceLocks
	hooks        hookRunner
	ntfy         ntfyBroadcaster
	stagingDir   string
	uploadRoot   string
	keyRoot      string
	sessionLocks sync.Map
	mu           sync.Mutex
}

func New(
	settings *config.Settings,
	backend ingestBackend,
	index snapshotIndexer,
	locks namespaceLocks,
	hooks hookRunner,
	ntfy ntfyBroadcaster,
	sessionStores ...UploadSessionStore,
) (*Service, error) {
	stagingDir := settings.StagingDir
	uploadRoot := filepath.Join(stagingDir, "uploads")
	keyRoot := filepath.Join(uploadRoot, "keys")
	if err := os.MkdirAll(uploadRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create upload staging directory: %w", err)
	}
	if err := os.MkdirAll(keyRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create legacy idempotency key directory: %w", err)
	}
	sessionStore := UploadSessionStore(NewMemorySessionStore())
	if len(sessionStores) > 0 && sessionStores[0] != nil {
		sessionStore = sessionStores[0]
	}
	return &Service{
		settings:   settings,
		backend:    backend,
		index:      index,
		sessions:   sessionStore,
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
	existing := s.loadSessionForKeyContext(ctx, req.IdempotencyKey)
	if existing != nil {
		if s.sessionReferencesMissingSnapshot(ctx, existing) {
			s.reconcileNamespace(ctx, existing.Namespace)
			s.discardSessionContext(ctx, existing)
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
		if err := s.saveSessionContext(ctx, existing); err != nil {
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
	if err := s.validateNewReservationContext(ctx, req.ArchiveSizeBytes); err != nil {
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
	if err := s.saveSessionContext(ctx, session); err != nil {
		s.discardSessionContext(ctx, session)
		return nil, httpError(http.StatusInternalServerError, "failed to persist upload session")
	}
	return s.buildSessionResponse(session), nil
}

func (s *Service) AppendChunk(ctx context.Context, uploadID string, offset int64, body io.Reader) (*ChunkResponse, error) {
	unlock, err := s.sessionStore().Lock(ctx, "upload:"+uploadID)
	if err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to lock upload session")
	}
	defer unlock()

	session, err := s.loadSessionContext(ctx, uploadID)
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
		return nil, httpErrorJSON(http.StatusConflict, map[string]any{
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
	if err := s.saveSessionContext(ctx, session); err != nil {
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
	unlock, err := s.sessionStore().Lock(ctx, "upload:"+uploadID)
	if err != nil {
		return nil, httpError(http.StatusInternalServerError, "failed to lock upload session")
	}
	defer unlock()

	session, err := s.loadSessionContext(ctx, uploadID)
	if err != nil {
		return nil, err
	}

	if session.Status == "completed" {
		if s.sessionReferencesMissingSnapshot(ctx, session) {
			s.reconcileNamespace(ctx, session.Namespace)
			s.discardSessionContext(ctx, session)
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
	session, err = s.loadSessionContext(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	currentSize := s.currentUploadSize(uploadID)
	if currentSize != session.ArchiveSizeBytes {
		return nil, httpErrorJSON(http.StatusConflict, map[string]any{
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
		if err := s.saveSessionContext(ctx, session); err != nil {
			return nil, httpError(http.StatusInternalServerError, "failed to persist upload session")
		}
		return nil, httpErrorJSON(http.StatusConflict, map[string]any{
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
		if err := s.saveSessionContext(ctx, session); err != nil {
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

		pruned, _ := retention.PruneOldSnapshots(s.backend, session.Namespace, s.settings.RetentionKeepLast)

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
		if err := s.saveSessionContext(ctx, session); err != nil {
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
	expired, _ := s.sessionStore().DeleteExpired(context.Background(), cutoff)
	for _, sess := range expired {
		os.RemoveAll(s.sessionDir(sess.UploadID))
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

func (s *Service) MigrateLegacyUploadSessions(ctx context.Context) (int, error) {
	entries, err := os.ReadDir(s.uploadRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read legacy upload sessions: %w", err)
	}

	var migrated int
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "keys" {
			continue
		}
		uploadID := e.Name()
		if _, err := s.sessionStore().Load(ctx, uploadID); err == nil {
			continue
		} else if !errors.Is(err, ErrSessionNotFound) {
			return migrated, fmt.Errorf("check existing upload session %q: %w", uploadID, err)
		}

		data, err := os.ReadFile(s.metadataPath(uploadID))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return migrated, fmt.Errorf("read legacy upload session %q: %w", uploadID, err)
		}
		var sess UploadSession
		if err := json.Unmarshal(data, &sess); err != nil {
			return migrated, fmt.Errorf("parse legacy upload session %q: %w", uploadID, err)
		}
		if strings.TrimSpace(sess.UploadID) == "" {
			sess.UploadID = uploadID
		}
		if err := s.saveSessionContext(ctx, &sess); err != nil {
			return migrated, fmt.Errorf("save migrated upload session %q: %w", sess.UploadID, err)
		}
		migrated++
	}
	return migrated, nil
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
		if credHash != nil && *credHash != "" && reg.CredentialHash != nil && *reg.CredentialHash != "" && *reg.CredentialHash != *credHash {
			return httpError(http.StatusForbidden, "credential is not bound to this edge instance")
		}
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

func (s *Service) runPostHook(hookCtx map[string]any, status, storedAs string, pruned int, duplicate bool) {
	final := map[string]any{}
	maps.Copy(final, hookCtx)
	final["status"] = status
	final["stored_as"] = storedAs
	final["pruned"] = pruned
	final["duplicate"] = duplicate
	s.hooks.RunCommand(s.settings.HookPostCommand, "post", final)
	if status == "ok" {
		s.ntfy.PublishBestEffort(s.settings, final)
	}
}

func (s *Service) hookContext(session *UploadSession, stagedPath string) map[string]any {
	return map[string]any{
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
		"advertised_url":     advertisedURLValue(session.AdvertisedURL),
		"staged_path":        stagedPath,
	}
}

func (s *Service) validateNewReservation(archiveSizeBytes int64) error {
	return s.validateNewReservationContext(context.Background(), archiveSizeBytes)
}

func (s *Service) validateNewReservationContext(ctx context.Context, archiveSizeBytes int64) error {
	if archiveSizeBytes > s.settings.MaxUploadSizeBytes() {
		return httpError(http.StatusRequestEntityTooLarge, "upload too large")
	}
	reserved, err := s.reservedBytesContext(ctx)
	if err != nil {
		return httpError(http.StatusInternalServerError, "failed to calculate reserved upload storage")
	}
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
	total, _ := s.reservedBytesContext(context.Background())
	return total
}

func (s *Service) reservedBytesContext(ctx context.Context) (int64, error) {
	return s.sessionStore().ReservedBytes(ctx)
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
	chunkSize := min(s.settings.UploadChunkSizeBytes(), sess.ArchiveSizeBytes)
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
	chunkSize := min(s.settings.UploadChunkSizeBytes(), archiveSizeBytes)
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
	s.discardSessionContext(context.Background(), sess)
}

func (s *Service) discardSessionContext(ctx context.Context, sess *UploadSession) {
	_ = s.sessionStore().Delete(ctx, sess)
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
	if err := os.MkdirAll(filepath.Dir(s.keyMappingPath(key)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.keyMappingPath(key), data, 0o644)
}

func (s *Service) loadSessionForKey(key string) *UploadSession {
	return s.loadSessionForKeyContext(context.Background(), key)
}

func (s *Service) loadSessionForKeyContext(ctx context.Context, key string) *UploadSession {
	sess, err := s.sessionStore().LoadByIdempotencyKey(ctx, key)
	if err == nil {
		return sess
	}
	data, err := os.ReadFile(s.keyMappingPath(key))
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
	sess, err = s.loadSessionContext(ctx, uploadID)
	if err != nil {
		os.Remove(s.keyMappingPath(key))
		return nil
	}
	return sess
}

func (s *Service) loadSession(uploadID string) (*UploadSession, error) {
	return s.loadSessionContext(context.Background(), uploadID)
}

func (s *Service) loadSessionContext(ctx context.Context, uploadID string) (*UploadSession, error) {
	sess, err := s.sessionStore().Load(ctx, uploadID)
	if err != nil {
		if errors.Is(err, ErrSessionNotFound) {
			data, readErr := os.ReadFile(s.metadataPath(uploadID))
			if readErr == nil {
				var legacy UploadSession
				if err := json.Unmarshal(data, &legacy); err != nil {
					return nil, httpError(http.StatusInternalServerError, "failed to load upload session")
				}
				return &legacy, nil
			}
			return nil, httpError(http.StatusNotFound, "upload session not found")
		}
		return nil, httpError(http.StatusInternalServerError, "failed to load upload session")
	}
	return sess, nil
}

func (s *Service) saveSession(sess *UploadSession) error {
	return s.saveSessionContext(context.Background(), sess)
}

func (s *Service) saveSessionContext(ctx context.Context, sess *UploadSession) error {
	dir := s.sessionDir(sess.UploadID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return s.sessionStore().Save(ctx, sess)
}

func (s *Service) sessionStore() UploadSessionStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = NewMemorySessionStore()
	}
	return s.sessions
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
	Message any
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http %d: %v", e.Code, e.Message)
}

func advertisedURLValue(u *string) string {
	if u == nil || strings.TrimSpace(*u) == "" {
		return "missing_advertised_url"
	}
	return *u
}

func httpError(code int, msg string) *HTTPError {
	return &HTTPError{Code: code, Message: msg}
}

func httpErrorJSON(code int, body any) *HTTPError {
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
		for part := range strings.SplitSeq(first, ";") {
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
