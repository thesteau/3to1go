package runner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/encryption"
	"github.com/3to1go/edge/internal/identity"
	"github.com/3to1go/edge/internal/services/certificates"
	"github.com/3to1go/edge/internal/services/directories"
	"github.com/3to1go/edge/internal/services/hooks"
	"github.com/3to1go/edge/internal/services/locks"
	"github.com/3to1go/edge/internal/services/ntfy"
	"github.com/3to1go/edge/internal/services/recovery"
	"github.com/3to1go/edge/internal/services/state"
	"github.com/3to1go/edge/internal/services/upload"
)

// EdgeRunner owns all runtime services and drives backup cycles.
type EdgeRunner struct {
	mu sync.Mutex

	Settings  *config.Settings
	logger    *slog.Logger
	encKey    []byte
	cycleLock sync.Mutex

	StateStore    *state.StateStore
	UploadClient  *upload.UploadClient
	LockManager   *locks.JobLockManager
	HookManager   *hooks.HookManager
	CertManager   *certificates.CertManager
	NtfyPublisher *ntfy.NtfyPublisher
	DirService    *directories.DirectoryService
	Recovery      *recovery.RecoveryService
}

// uploadWork is the handoff between the compress goroutines and the serial upload worker.
type uploadWork struct {
	job    *backup.JobDefinition
	state  state.JobState
	unlock func()
}

// NewEdgeRunner creates and initialises the runner from settings and a cert manager.
func NewEdgeRunner(settings *config.Settings, logger *slog.Logger, certMgr *certificates.CertManager) (*EdgeRunner, error) {
	if err := os.MkdirAll(settings.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	if err := os.MkdirAll(settings.SpoolDir, 0o755); err != nil {
		return nil, fmt.Errorf("create spool dir: %w", err)
	}

	encKey, err := encryption.LoadOrCreate(config.EncryptionKeyPath())
	if err != nil {
		return nil, fmt.Errorf("encryption key: %w", err)
	}

	stateStore, err := state.NewStateStore(settings.StateDir)
	if err != nil {
		return nil, fmt.Errorf("state store: %w", err)
	}

	uploadClient := upload.NewUploadClient(settings, encKey, certMgr)
	lockMgr := locks.NewJobLockManager()
	hookMgr := hooks.NewHookManager(config.HookScriptsDir(), logger)
	ntfyPub := ntfy.NewNtfyPublisher(logger)
	dirSvc := directories.NewDirectoryService(settings, logger, stateStore)
	recoverySvc := recovery.NewRecoveryService(settings, logger, stateStore, uploadClient, encKey)

	return &EdgeRunner{
		Settings:      settings,
		logger:        logger,
		encKey:        encKey,
		StateStore:    stateStore,
		UploadClient:  uploadClient,
		LockManager:   lockMgr,
		HookManager:   hookMgr,
		CertManager:   certMgr,
		NtfyPublisher: ntfyPub,
		DirService:    dirSvc,
		Recovery:      recoverySvc,
	}, nil
}

// Logger satisfies CycleRunner.
func (r *EdgeRunner) Logger() *slog.Logger {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.logger
}

// CronSchedule satisfies CycleRunner.
func (r *EdgeRunner) CronSchedule() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Settings.CronSchedule
}

// RunCycle runs one full backup cycle; returns false if skipped.
func (r *EdgeRunner) RunCycle() bool {
	r.mu.Lock()
	settings := r.Settings
	r.mu.Unlock()

	if strings.TrimSpace(settings.EdgeCredential) == "" {
		r.logger.Warn("cycle_skipped", "reason", "edge_credential_missing")
		return false
	}
	if settings.UploadsPaused {
		r.logger.Info("cycle_skipped", "reason", "uploads_paused")
		return false
	}

	jobs, _ := backup.DiscoverJobs(settings.ScanRoot, settings.MaxDepth, func(format string, args ...any) {
		r.logger.Warn(fmt.Sprintf(format, args...))
	})
	if len(jobs) == 0 {
		r.cleanupStaleArchives(settings)
		return true
	}

	workCh := make(chan *uploadWork, len(jobs))

	var wg sync.WaitGroup
	for _, job := range jobs {
		wg.Go(func() {
			r.prepareJob(job, settings, workCh)
		})
	}

	go func() {
		wg.Wait()
		close(workCh)
	}()

	for w := range workCh {
		func(w *uploadWork) {
			defer w.unlock()
			r.uploadPendingArchive(w.job, &w.state, settings)
			r.finishJob(w.job, settings)
		}(w)
	}

	r.cleanupStaleArchives(settings)
	return true
}

// prepareJob scans, fingerprints, and compresses one job; sends to workCh if ready to upload.
func (r *EdgeRunner) prepareJob(job *backup.JobDefinition, settings *config.Settings, workCh chan<- *uploadWork) {
	unlock := r.LockManager.Acquire(job.RootPath)
	if unlock == nil {
		r.logger.Info("job_locked", "job_name", job.JobName, "path", job.RootPath)
		return
	}

	s := r.StateStore.Get(job.RootPath)
	s.JobName = job.JobName
	r.HookManager.RunCommand(settings.HookPreCommand, "pre", r.hookContext(job, &s, settings))

	ready, err := r.prepareArchiveLocked(job, &s, settings, false)
	if err != nil {
		r.logger.Error("unexpected_exception", "job_name", job.JobName, "path", job.RootPath, "error", err)
		s.LastStatus = "unexpected_exception"
		s.LastErrorCategory = "unexpected"
		s.LastErrorDetail = err.Error()
		s.LastUploadUpdatedAt = utcNow()
		r.StateStore.Set(job.RootPath, s)
		r.finishJob(job, settings)
		unlock()
		return
	}

	if !ready {
		r.finishJob(job, settings)
		unlock()
		return
	}

	fresh := r.StateStore.Get(job.RootPath)
	fresh.JobName = job.JobName
	workCh <- &uploadWork{job: job, state: fresh, unlock: unlock}
}

// finishJob runs the post-hook and publishes ntfy if the last status was success.
func (r *EdgeRunner) finishJob(job *backup.JobDefinition, settings *config.Settings) {
	s := r.StateStore.Get(job.RootPath)
	s.JobName = job.JobName
	ctx := r.hookContext(job, &s, settings)
	r.HookManager.RunCommand(settings.HookPostCommand, "post", ctx)
	if s.LastStatus == "success" {
		ntfyCtx := make(map[string]string, len(ctx))
		for k, v := range ctx {
			ntfyCtx[k] = fmt.Sprintf("%v", v)
		}
		r.NtfyPublisher.PublishBestEffort(settings, ntfyCtx)
	}
}

// ForceSendJob forces a single named job through the upload pipeline.
func (r *EdgeRunner) ForceSendJob(ctx context.Context, jobName string) (map[string]any, error) {
	r.mu.Lock()
	settings := r.Settings
	r.mu.Unlock()

	normalized := strings.TrimSpace(jobName)
	if normalized == "" {
		return nil, fmt.Errorf("job_name is required")
	}

	jobs, _ := backup.DiscoverJobs(settings.ScanRoot, settings.MaxDepth, nil)
	var matched []*backup.JobDefinition
	for _, j := range jobs {
		if j.JobName == normalized {
			matched = append(matched, j)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("job not found")
	}
	if len(matched) > 1 {
		return nil, fmt.Errorf("multiple jobs share that job_name")
	}
	job := matched[0]

	if !r.cycleLock.TryLock() {
		return map[string]any{"status": "already_running", "job_name": normalized}, nil
	}
	defer r.cycleLock.Unlock()

	s := r.StateStore.Get(job.RootPath)
	cleared := s.ManualInterventionRequired
	if cleared {
		s.ManualInterventionRequired = false
		s.NextRetryAt = ""
		s.LastStatus = "manual_retry_requested"
		r.StateStore.Set(job.RootPath, s)
	}
	r.processJobLocked(job, &s, settings, true)

	return map[string]any{
		"status":               "started",
		"job_name":             normalized,
		"manual_retry_cleared": cleared,
	}, nil
}

// RecoverJob runs recovery for a job identified by relative_path.
func (r *EdgeRunner) RecoverJob(ctx context.Context, relativePath, fingerprint string) (map[string]any, error) {
	job, err := r.DirService.LoadJob(relativePath)
	if err != nil {
		return nil, err
	}
	if !r.cycleLock.TryLock() {
		return map[string]any{"status": "already_running", "relative_path": relativePath}, nil
	}
	defer r.cycleLock.Unlock()

	result, err := r.Recovery.Recover(ctx, job, fingerprint)
	if err != nil {
		return nil, err
	}
	result.RelativePath = relativePath
	return map[string]any{
		"status":               result.Status,
		"job_name":             result.JobName,
		"relative_path":        result.RelativePath,
		"snapshot_filename":    result.SnapshotFilename,
		"snapshot_fingerprint": result.SnapshotFingerprint,
		"restored_files":       result.RestoredFiles,
	}, nil
}

// PreviewRecovery returns the list of files that would be restored without writing.
func (r *EdgeRunner) PreviewRecovery(ctx context.Context, relativePath, fingerprint string) (map[string]any, error) {
	job, err := r.DirService.LoadJob(relativePath)
	if err != nil {
		return nil, err
	}
	if !r.cycleLock.TryLock() {
		return map[string]any{"status": "already_running", "relative_path": relativePath}, nil
	}
	defer r.cycleLock.Unlock()

	result, err := r.Recovery.Preview(ctx, job, fingerprint)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"status":               result.Status,
		"job_name":             result.JobName,
		"relative_path":        relativePath,
		"snapshot_filename":    result.SnapshotFilename,
		"snapshot_fingerprint": result.SnapshotFingerprint,
		"entries":              result.Entries,
		"total_files":          result.TotalFiles,
		"replace_count":        result.ReplaceCount,
		"add_count":            result.AddCount,
	}, nil
}

// UpdateSettings rebuilds all services with new settings while the cycle lock is held.
func (r *EdgeRunner) UpdateSettings(settings *config.Settings) error {
	if !r.cycleLock.TryLock() {
		return fmt.Errorf("cannot update settings while a backup cycle is running")
	}
	defer r.cycleLock.Unlock()
	return r.applySettings(settings)
}

func (r *EdgeRunner) applySettings(settings *config.Settings) error {
	os.MkdirAll(settings.StateDir, 0o755)
	os.MkdirAll(settings.SpoolDir, 0o755)

	stateStore, err := state.NewStateStore(settings.StateDir)
	if err != nil {
		return err
	}
	uploadClient := upload.NewUploadClient(settings, r.encKey, r.CertManager)
	recoverySvc := recovery.NewRecoveryService(settings, r.logger, stateStore, uploadClient, r.encKey)
	dirSvc := directories.NewDirectoryService(settings, r.logger, stateStore)

	r.mu.Lock()
	r.Settings = settings
	r.StateStore = stateStore
	r.UploadClient = uploadClient
	r.DirService = dirSvc
	r.Recovery = recoverySvc
	r.HookManager.SetLogger(r.logger)
	r.NtfyPublisher.SetLogger(r.logger)
	r.mu.Unlock()
	return nil
}

// EncryptionKeyFingerprint returns the SHA-256 fingerprint of the loaded encryption key.
func (r *EdgeRunner) EncryptionKeyFingerprint() string {
	return encryption.KeyFingerprint(r.encKey)
}

// EncryptionKeyBase64 returns the key as URL-safe base64.
func (r *EdgeRunner) EncryptionKeyBase64() string {
	return encryption.KeyAsBase64(r.encKey)
}

// RotateEncryptionKey generates a new key, persists it, and hot-reloads all services.
// Callers should warn users that existing snapshots are only decryptable with the old key.
func (r *EdgeRunner) RotateEncryptionKey() (string, error) {
	if !r.cycleLock.TryLock() {
		return "", fmt.Errorf("cannot rotate key while a backup cycle is running")
	}
	defer r.cycleLock.Unlock()

	newKey := make([]byte, 32)
	if _, err := rand.Read(newKey); err != nil {
		return "", fmt.Errorf("generate new key: %w", err)
	}
	keyPath := config.EncryptionKeyPath()
	if err := os.WriteFile(keyPath, newKey, 0o600); err != nil {
		return "", fmt.Errorf("write new key: %w", err)
	}

	r.mu.Lock()
	settings := r.Settings
	r.encKey = newKey
	r.UploadClient = upload.NewUploadClient(settings, newKey, r.CertManager)
	r.Recovery = recovery.NewRecoveryService(settings, r.logger, r.StateStore, r.UploadClient, newKey)
	r.mu.Unlock()

	return encryption.KeyFingerprint(newKey), nil
}

// InstallationID returns the persistent edge instance ID.
func (r *EdgeRunner) InstallationID() string {
	return identity.LoadOrCreate(config.InstallationIDPath())
}

// ----- internal job processing -----

func (r *EdgeRunner) prepareArchiveLocked(job *backup.JobDefinition, s *state.JobState, settings *config.Settings, forceSend bool) (bool, error) {
	if !s.ManualInterventionRequired {
		retry := r.checkRetry(job, s)
		if retry == "waiting" {
			return false, nil
		}
		if retry == "upload_now" {
			return true, nil
		}
	}

	r.setActivePhase(job, s, "scanning", 5)
	s.LastErrorDetail = ""
	s.LastErrorCategory = ""
	r.StateStore.Set(job.RootPath, *s)

	files, err := backup.BuildFileList(job, func(format string, args ...any) {
		r.logger.Warn(fmt.Sprintf(format, args...))
	})
	if err != nil {
		return false, err
	}
	if len(files) == 0 {
		r.clearPendingArchive(s)
		s.LastStatus = "skipped_empty"
		s.ManualInterventionRequired = false
		r.StateStore.Set(job.RootPath, *s)
		r.logger.Info("skipped_empty", "job_name", job.JobName, "path", job.RootPath)
		return false, nil
	}

	fingerprint := backup.ComputeFingerprint(files)
	if !forceSend && fingerprint == s.LastSuccessfulFingerprint {
		r.clearPendingArchive(s)
		s.LastStatus = "skipped_unchanged"
		s.ManualInterventionRequired = false
		r.StateStore.Set(job.RootPath, *s)
		r.logger.Info("skipped_unchanged", "job_name", job.JobName, "fingerprint", fingerprint[:8])
		return false, nil
	}

	if s.ManualInterventionRequired && s.PendingArchive != "" && s.PendingFingerprint == fingerprint {
		if _, err := os.Stat(s.PendingArchive); err == nil {
			s.LastStatus = "manual_intervention_required"
			r.StateStore.Set(job.RootPath, *s)
			r.logger.Warn("manual_intervention_required",
				"job_name", job.JobName,
				"archive", filepath.Base(s.PendingArchive),
				"detail", s.LastErrorDetail)
			return false, nil
		}
	}

	archivePath, timestamp, err := r.createPendingArchive(job, files, fingerprint, settings, s)
	if err != nil {
		return false, err
	}

	prevPending := s.PendingArchive
	fi, _ := os.Stat(archivePath)
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	sha256sum, _ := sha256File(archivePath)

	s.PendingArchive = archivePath
	s.PendingArchiveSize = &size
	s.PendingArchiveSHA256 = sha256sum
	s.PendingFingerprint = fingerprint
	s.PendingTimestamp = timestamp
	s.UploadID = ""
	s.UploadOffset = 0
	s.UploadAttemptCount = 0
	s.CurrentChunkSizeBytes = nil
	s.NextRetryAt = ""
	s.LastErrorDetail = ""
	s.LastErrorCategory = ""
	s.LastStoredAs = ""
	s.LastPruned = 0
	s.LastDuplicate = false
	s.LastUploadStartedAt = ""
	s.LastUploadUpdatedAt = ""
	s.ActivePhase = "archive_created"
	s.ActivePhasePercent = 50
	s.ManualInterventionRequired = false
	s.LastStatus = "archive_created"
	r.StateStore.Set(job.RootPath, *s)

	if prevPending != "" && prevPending != archivePath {
		os.Remove(prevPending)
	}
	return true, nil
}

func (r *EdgeRunner) createPendingArchive(job *backup.JobDefinition, files []*backup.DiscoveredFile, fingerprint string, settings *config.Settings, s *state.JobState) (string, string, error) {
	now := time.Now().UTC()
	timestamp := backup.TimestampForAPI(now)
	archiveName := backup.BuildArchiveName(job.JobName, now, fingerprint)
	archivePath := filepath.Join(settings.SpoolDir, archiveName)

	r.setActivePhase(job, s, "compressing", 18)
	if err := backup.CreateArchive(archivePath, files); err != nil {
		return "", "", err
	}

	r.setActivePhase(job, s, "encrypting", 40)
	tmpPath := archivePath + ".enc.tmp"
	if err := encryption.EncryptFile(r.encKey, archivePath, tmpPath); err != nil {
		os.Remove(archivePath)
		return "", "", err
	}
	os.Remove(archivePath)
	if err := os.Rename(tmpPath, archivePath); err != nil {
		os.Remove(tmpPath)
		return "", "", err
	}

	r.setActivePhase(job, s, "archive_created", 50)
	r.logger.Info("archive_created", "job_name", job.JobName, "archive", archiveName)
	return archivePath, timestamp, nil
}

func (r *EdgeRunner) checkRetry(job *backup.JobDefinition, s *state.JobState) string {
	if s.PendingFingerprint == "" {
		return "none"
	}
	retryAt := parseUTCTime(s.NextRetryAt)

	if s.PendingArchive == "" {
		if retryAt != nil && retryAt.After(time.Now().UTC()) {
			s.LastStatus = "waiting_retry"
			r.StateStore.Set(job.RootPath, *s)
			r.logger.Info("waiting_retry", "job_name", job.JobName, "archive", "rebuild_required", "retry_at", s.NextRetryAt)
			return "waiting"
		}
		return "none"
	}

	if _, err := os.Stat(s.PendingArchive); os.IsNotExist(err) {
		r.clearPendingArchive(s)
		s.LastStatus = "skipped_missing"
		r.StateStore.Set(job.RootPath, *s)
		r.logger.Warn("skipped_missing", "job_name", job.JobName, "pending_archive", s.PendingArchive)
		return "none"
	}

	if retryAt != nil && retryAt.After(time.Now().UTC()) {
		s.LastStatus = "waiting_retry"
		r.StateStore.Set(job.RootPath, *s)
		r.logger.Info("waiting_retry", "job_name", job.JobName, "archive", filepath.Base(s.PendingArchive), "retry_at", s.NextRetryAt)
		return "waiting"
	}

	r.logger.Info("retry_pending", "job_name", job.JobName, "archive", filepath.Base(s.PendingArchive))
	return "upload_now"
}

func (r *EdgeRunner) processJobLocked(job *backup.JobDefinition, s *state.JobState, settings *config.Settings, forceSend bool) {
	if forceSend && s.PendingArchive != "" {
		if _, err := os.Stat(s.PendingArchive); err == nil {
			s.NextRetryAt = ""
			s.ManualInterventionRequired = false
			s.LastStatus = "force_send_requested"
			r.StateStore.Set(job.RootPath, *s)
			r.logger.Info("force_send_pending", "job_name", job.JobName, "archive", filepath.Base(s.PendingArchive))
			r.uploadPendingArchive(job, s, settings)
			return
		}
	}

	ready, err := r.prepareArchiveLocked(job, s, settings, forceSend)
	if err != nil {
		r.logger.Error("unexpected_exception", "job_name", job.JobName, "error", err)
		return
	}
	if ready {
		r.uploadPendingArchive(job, s, settings)
	}
}

func (r *EdgeRunner) uploadPendingArchive(job *backup.JobDefinition, s *state.JobState, settings *config.Settings) bool {
	if s.PendingArchive == "" || s.PendingFingerprint == "" || s.PendingTimestamp == "" {
		return false
	}
	if _, err := os.Stat(s.PendingArchive); os.IsNotExist(err) {
		r.clearPendingArchive(s)
		s.LastStatus = "skipped_missing"
		r.StateStore.Set(job.RootPath, *s)
		return false
	}

	jobName := s.JobName
	if jobName == "" {
		jobName = job.JobName
	}
	if s.PendingArchiveSize == nil {
		if fi, err := os.Stat(s.PendingArchive); err == nil {
			sz := fi.Size()
			s.PendingArchiveSize = &sz
		}
	}
	if s.PendingArchiveSHA256 == "" {
		if sha, err := sha256File(s.PendingArchive); err == nil {
			s.PendingArchiveSHA256 = sha
		}
	}

	now := utcNow()
	s.ActivePhase = "uploading"
	s.ActivePhasePercent = 50
	s.LastStatus = "uploading"
	s.LastUploadStartedAt = now
	s.LastUploadUpdatedAt = now
	s.NextRetryAt = ""
	s.ManualInterventionRequired = false
	r.StateStore.Set(job.RootPath, *s)

	var preferredChunk int64
	if s.CurrentChunkSizeBytes != nil {
		preferredChunk = *s.CurrentChunkSizeBytes
	}

	progress := func(uploadID string, offset, chunkSize int64) {
		s.UploadID = uploadID
		s.UploadOffset = offset
		sz := chunkSize
		s.CurrentChunkSizeBytes = &sz
		s.LastUploadUpdatedAt = utcNow()
		s.ActivePhase = "uploading"
		s.ActivePhasePercent = uploadPhasePercent(offset, s.PendingArchiveSize)
		s.LastStatus = "uploading"
		r.StateStore.Set(job.RootPath, *s)
	}

	result, err := r.UploadClient.UploadArchive(
		context.Background(),
		settings.EdgeID, jobName,
		s.PendingFingerprint, s.PendingTimestamp,
		s.PendingArchive, s.PendingArchiveSHA256,
		s.UploadID, s.UploadOffset, preferredChunk,
		progress,
	)

	archiveName := filepath.Base(s.PendingArchive)
	if err != nil {
		uf, isUploadFail := err.(*upload.UploadFailure)
		cat := "unexpected"
		if isUploadFail {
			cat = uf.Category
		}
		r.logger.Error("upload_failure",
			"job_name", jobName, "archive", archiveName,
			"category", cat, "detail", err)

		s.UploadAttemptCount++
		s.LastErrorDetail = err.Error()
		if isUploadFail {
			s.LastErrorCategory = uf.Category
			if uf.Retryable {
				if uf.Category == "circuit_open" {
					s.LastStatus = "circuit_open"
				} else {
					s.LastStatus = "retry_scheduled"
				}
				s.NextRetryAt = utcAfterSeconds(r.retryDelaySeconds(s.UploadAttemptCount, uf))
				s.ManualInterventionRequired = false
			} else {
				s.LastStatus = "manual_intervention_required"
				s.NextRetryAt = ""
				s.ManualInterventionRequired = true
			}
			if uf.Retryable && !settings.KeepLocalPending {
				r.discardPendingArchiveFile(s)
			}
		} else {
			s.LastErrorCategory = "unexpected"
			s.LastStatus = "retry_scheduled"
			s.NextRetryAt = utcAfterSeconds(r.retryDelaySeconds(s.UploadAttemptCount, nil))
			if !settings.KeepLocalPending {
				r.discardPendingArchiveFile(s)
			}
		}
		s.LastUploadUpdatedAt = utcNow()
		r.StateStore.Set(job.RootPath, *s)
		return false
	}

	os.Remove(s.PendingArchive)
	s.LastSuccessfulFingerprint = s.PendingFingerprint
	s.LastSuccessfulUpload = s.PendingTimestamp
	s.LastErrorDetail = ""
	s.LastErrorCategory = ""
	s.UploadAttemptCount = 0
	s.LastStatus = "success"
	s.LastStoredAs = result.StoredAs
	s.LastPruned = result.Pruned
	s.LastDuplicate = result.Duplicate
	s.NextRetryAt = ""
	s.ManualInterventionRequired = false
	r.clearPendingArchive(s)
	s.LastUploadUpdatedAt = utcNow()
	r.StateStore.Set(job.RootPath, *s)

	r.logger.Info("upload_success",
		"job_name", jobName, "archive", archiveName,
		"stored_as", result.StoredAs, "pruned", result.Pruned, "duplicate", result.Duplicate)
	return true
}

func (r *EdgeRunner) retryDelaySeconds(attemptCount int, uf *upload.UploadFailure) int {
	r.mu.Lock()
	settings := r.Settings
	r.mu.Unlock()
	if uf != nil && uf.RetryAfterSeconds != nil {
		return *uf.RetryAfterSeconds
	}
	exp := math.Pow(2, max(0.0, float64(attemptCount-1)))
	delay := float64(settings.UploadRetryBaseDelaySeconds) * exp
	if delay > float64(settings.UploadRetryMaxDelaySeconds) {
		delay = float64(settings.UploadRetryMaxDelaySeconds)
	}
	return int(delay)
}

func (r *EdgeRunner) setActivePhase(job *backup.JobDefinition, s *state.JobState, phase string, pct int) {
	now := utcNow()
	if s.LastUploadStartedAt == "" {
		s.LastUploadStartedAt = now
	}
	s.LastUploadUpdatedAt = now
	s.ActivePhase = phase
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	s.ActivePhasePercent = pct
	s.LastStatus = phase
	r.StateStore.Set(job.RootPath, *s)
}

func (r *EdgeRunner) clearPendingArchive(s *state.JobState) {
	if s.PendingArchive != "" {
		os.Remove(s.PendingArchive)
	}
	s.PendingArchive = ""
	s.PendingArchiveSize = nil
	s.PendingArchiveSHA256 = ""
	s.PendingFingerprint = ""
	s.PendingTimestamp = ""
	s.UploadID = ""
	s.UploadOffset = 0
	s.CurrentChunkSizeBytes = nil
	s.NextRetryAt = ""
	s.ActivePhase = ""
	s.ActivePhasePercent = 0
}

func (r *EdgeRunner) discardPendingArchiveFile(s *state.JobState) {
	if s.PendingArchive != "" {
		os.Remove(s.PendingArchive)
	}
	s.PendingArchive = ""
	s.PendingArchiveSize = nil
	s.PendingArchiveSHA256 = ""
	s.UploadID = ""
	s.UploadOffset = 0
	s.CurrentChunkSizeBytes = nil
	s.ActivePhase = ""
	s.ActivePhasePercent = 0
}

func (r *EdgeRunner) cleanupStaleArchives(settings *config.Settings) {
	referenced := r.StateStore.ReferencedPendingArchives()
	entries, err := os.ReadDir(settings.SpoolDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.zst") {
			continue
		}
		full := filepath.Join(settings.SpoolDir, e.Name())
		if !referenced[full] {
			os.Remove(full)
		}
	}
}

func (r *EdgeRunner) hookContext(job *backup.JobDefinition, s *state.JobState, settings *config.Settings) map[string]any {
	return map[string]any{
		"edge_id":             settings.EdgeID,
		"edge_instance_id":    identity.LoadOrCreate(config.InstallationIDPath()),
		"job_name":            job.JobName,
		"job_root":            job.RootPath,
		"state_key":           job.RootPath,
		"last_status":         s.LastStatus,
		"last_error_category": s.LastErrorCategory,
		"last_error_detail":   s.LastErrorDetail,
		"stored_as":           s.LastStoredAs,
		"pruned":              s.LastPruned,
		"duplicate":           s.LastDuplicate,
		"pending_archive":     s.PendingArchive,
		"pending_fingerprint": s.PendingFingerprint,
		"pending_timestamp":   s.PendingTimestamp,
		"upload_id":           s.UploadID,
		"upload_offset":       s.UploadOffset,
		"next_retry_at":       s.NextRetryAt,
	}
}

func utcNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

func utcAfterSeconds(seconds int) string {
	return time.Now().UTC().Add(time.Duration(seconds) * time.Second).Format("2006-01-02T15:04:05Z")
}

func parseUTCTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.ParseInLocation("2006-01-02T15:04:05Z", s, time.UTC)
	if err != nil {
		return nil
	}
	return &t
}

func uploadPhasePercent(uploaded int64, total *int64) int {
	if total == nil || *total <= 0 {
		return 50
	}
	pct := min(int(float64(uploaded)/float64(*total)*100), 100)
	if pct < 0 {
		pct = 0
	}
	return 50 + pct/2
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
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
