package services

import (
	"context"
	"fmt"
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
)

// EdgeRunner owns all runtime services and drives backup cycles.
type EdgeRunner struct {
	mu sync.Mutex

	Settings  *config.Settings
	logger    *slog.Logger
	encKey    []byte
	cycleLock sync.Mutex

	StateStore    *StateStore
	UploadClient  *UploadClient
	LockManager   *JobLockManager
	HookManager   *HookManager
	CertManager   *CertManager
	NtfyPublisher *NtfyPublisher
	DirService    *DirectoryService
	Recovery      *RecoveryService
}

// uploadWork is the handoff between the compress goroutines and the serial upload worker.
type uploadWork struct {
	job    *backup.JobDefinition
	state  JobState
	unlock func()
}

// NewEdgeRunner creates and initialises the runner from settings and a cert manager.
func NewEdgeRunner(settings *config.Settings, logger *slog.Logger, certMgr *CertManager) (*EdgeRunner, error) {
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

	stateStore, err := NewStateStore(settings.StateDir)
	if err != nil {
		return nil, fmt.Errorf("state store: %w", err)
	}

	uploadClient := NewUploadClient(settings, encKey, certMgr)
	lockMgr := NewJobLockManager()
	hookMgr := NewHookManager(config.HookScriptsDir(), logger)
	ntfy := NewNtfyPublisher(logger)
	dirSvc := NewDirectoryService(settings, logger, stateStore)
	recovery := NewRecoveryService(settings, logger, stateStore, uploadClient, encKey)

	return &EdgeRunner{
		Settings:      settings,
		logger:        logger,
		encKey:        encKey,
		StateStore:    stateStore,
		UploadClient:  uploadClient,
		LockManager:   lockMgr,
		HookManager:   hookMgr,
		CertManager:   certMgr,
		NtfyPublisher: ntfy,
		DirService:    dirSvc,
		Recovery:      recovery,
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

	state := r.StateStore.Get(job.RootPath)
	state.JobName = job.JobName
	r.HookManager.RunCommand(settings.HookPreCommand, "pre", r.hookContext(job, &state, settings))

	ready, err := r.prepareArchiveLocked(job, &state, settings)
	if err != nil {
		r.logger.Error("unexpected_exception", "job_name", job.JobName, "path", job.RootPath, "error", err)
		state.LastStatus = "unexpected_exception"
		state.LastErrorCategory = "unexpected"
		state.LastErrorDetail = err.Error()
		state.LastUploadUpdatedAt = utcNow()
		r.StateStore.Set(job.RootPath, state)
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
	state := r.StateStore.Get(job.RootPath)
	state.JobName = job.JobName
	ctx := r.hookContext(job, &state, settings)
	r.HookManager.RunCommand(settings.HookPostCommand, "post", ctx)
	if state.LastStatus == "success" {
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

	state := r.StateStore.Get(job.RootPath)
	cleared := state.ManualInterventionRequired
	if cleared {
		state.ManualInterventionRequired = false
		state.NextRetryAt = ""
		state.LastStatus = "manual_retry_requested"
		r.StateStore.Set(job.RootPath, state)
	}
	r.processJobLocked(job, &state, settings, true)

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

	stateStore, err := NewStateStore(settings.StateDir)
	if err != nil {
		return err
	}
	uploadClient := NewUploadClient(settings, r.encKey, r.CertManager)
	recovery := NewRecoveryService(settings, r.logger, stateStore, uploadClient, r.encKey)
	dirSvc := NewDirectoryService(settings, r.logger, stateStore)

	r.mu.Lock()
	r.Settings = settings
	r.StateStore = stateStore
	r.UploadClient = uploadClient
	r.DirService = dirSvc
	r.Recovery = recovery
	r.HookManager.logger = r.logger
	r.NtfyPublisher.logger = r.logger
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

// InstallationID returns the persistent edge instance ID.
func (r *EdgeRunner) InstallationID() string {
	return identity.LoadOrCreate(config.InstallationIDPath())
}

// ----- internal job processing -----

func (r *EdgeRunner) prepareArchiveLocked(job *backup.JobDefinition, state *JobState, settings *config.Settings) (bool, error) {
	if !state.ManualInterventionRequired {
		retry := r.checkRetry(job, state)
		if retry == "waiting" {
			return false, nil
		}
		if retry == "upload_now" {
			return true, nil
		}
	}

	r.setActivePhase(job, state, "scanning", 5)
	state.LastErrorDetail = ""
	state.LastErrorCategory = ""
	r.StateStore.Set(job.RootPath, *state)

	files, err := backup.BuildFileList(job, func(format string, args ...any) {
		r.logger.Warn(fmt.Sprintf(format, args...))
	})
	if err != nil {
		return false, err
	}
	if len(files) == 0 {
		r.clearPendingArchive(state)
		state.LastStatus = "skipped_empty"
		state.ManualInterventionRequired = false
		r.StateStore.Set(job.RootPath, *state)
		r.logger.Info("skipped_empty", "job_name", job.JobName, "path", job.RootPath)
		return false, nil
	}

	fingerprint := backup.ComputeFingerprint(files)
	if fingerprint == state.LastSuccessfulFingerprint {
		r.clearPendingArchive(state)
		state.LastStatus = "skipped_unchanged"
		state.ManualInterventionRequired = false
		r.StateStore.Set(job.RootPath, *state)
		r.logger.Info("skipped_unchanged", "job_name", job.JobName, "fingerprint", fingerprint[:8])
		return false, nil
	}

	if state.ManualInterventionRequired && state.PendingArchive != "" && state.PendingFingerprint == fingerprint {
		if _, err := os.Stat(state.PendingArchive); err == nil {
			state.LastStatus = "manual_intervention_required"
			r.StateStore.Set(job.RootPath, *state)
			r.logger.Warn("manual_intervention_required",
				"job_name", job.JobName,
				"archive", filepath.Base(state.PendingArchive),
				"detail", state.LastErrorDetail)
			return false, nil
		}
	}

	archivePath, timestamp, err := r.createPendingArchive(job, files, fingerprint, settings, state)
	if err != nil {
		return false, err
	}

	prevPending := state.PendingArchive
	fi, _ := os.Stat(archivePath)
	var size int64
	if fi != nil {
		size = fi.Size()
	}
	sha256sum, _ := sha256File(archivePath)

	state.PendingArchive = archivePath
	state.PendingArchiveSize = &size
	state.PendingArchiveSHA256 = sha256sum
	state.PendingFingerprint = fingerprint
	state.PendingTimestamp = timestamp
	state.UploadID = ""
	state.UploadOffset = 0
	state.UploadAttemptCount = 0
	state.CurrentChunkSizeBytes = nil
	state.NextRetryAt = ""
	state.LastErrorDetail = ""
	state.LastErrorCategory = ""
	state.LastStoredAs = ""
	state.LastPruned = 0
	state.LastDuplicate = false
	state.LastUploadStartedAt = ""
	state.LastUploadUpdatedAt = ""
	state.ActivePhase = "archive_created"
	state.ActivePhasePercent = 50
	state.ManualInterventionRequired = false
	state.LastStatus = "archive_created"
	r.StateStore.Set(job.RootPath, *state)

	if prevPending != "" && prevPending != archivePath {
		os.Remove(prevPending)
	}
	return true, nil
}

func (r *EdgeRunner) createPendingArchive(job *backup.JobDefinition, files []*backup.DiscoveredFile, fingerprint string, settings *config.Settings, state *JobState) (string, string, error) {
	now := time.Now().UTC()
	timestamp := backup.TimestampForAPI(now)
	archiveName := backup.BuildArchiveName(job.JobName, now, fingerprint)
	archivePath := filepath.Join(settings.SpoolDir, archiveName)

	r.setActivePhase(job, state, "compressing", 18)
	if err := backup.CreateArchive(archivePath, files); err != nil {
		return "", "", err
	}

	r.setActivePhase(job, state, "encrypting", 40)
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

	r.setActivePhase(job, state, "archive_created", 50)
	r.logger.Info("archive_created", "job_name", job.JobName, "archive", archiveName)
	return archivePath, timestamp, nil
}

func (r *EdgeRunner) checkRetry(job *backup.JobDefinition, state *JobState) string {
	if state.PendingFingerprint == "" {
		return "none"
	}
	retryAt := parseUTCTime(state.NextRetryAt)

	if state.PendingArchive == "" {
		if retryAt != nil && retryAt.After(time.Now().UTC()) {
			state.LastStatus = "waiting_retry"
			r.StateStore.Set(job.RootPath, *state)
			r.logger.Info("waiting_retry", "job_name", job.JobName, "archive", "rebuild_required", "retry_at", state.NextRetryAt)
			return "waiting"
		}
		return "none"
	}

	if _, err := os.Stat(state.PendingArchive); os.IsNotExist(err) {
		r.clearPendingArchive(state)
		state.LastStatus = "skipped_missing"
		r.StateStore.Set(job.RootPath, *state)
		r.logger.Warn("skipped_missing", "job_name", job.JobName, "pending_archive", state.PendingArchive)
		return "none"
	}

	if retryAt != nil && retryAt.After(time.Now().UTC()) {
		state.LastStatus = "waiting_retry"
		r.StateStore.Set(job.RootPath, *state)
		r.logger.Info("waiting_retry", "job_name", job.JobName, "archive", filepath.Base(state.PendingArchive), "retry_at", state.NextRetryAt)
		return "waiting"
	}

	r.logger.Info("retry_pending", "job_name", job.JobName, "archive", filepath.Base(state.PendingArchive))
	return "upload_now"
}

func (r *EdgeRunner) processJobLocked(job *backup.JobDefinition, state *JobState, settings *config.Settings, forceSend bool) {
	if forceSend && state.PendingArchive != "" {
		if _, err := os.Stat(state.PendingArchive); err == nil {
			state.NextRetryAt = ""
			state.ManualInterventionRequired = false
			state.LastStatus = "force_send_requested"
			r.StateStore.Set(job.RootPath, *state)
			r.logger.Info("force_send_pending", "job_name", job.JobName, "archive", filepath.Base(state.PendingArchive))
			r.uploadPendingArchive(job, state, settings)
			return
		}
	}

	ready, err := r.prepareArchiveLocked(job, state, settings)
	if err != nil {
		r.logger.Error("unexpected_exception", "job_name", job.JobName, "error", err)
		return
	}
	if ready {
		r.uploadPendingArchive(job, state, settings)
	}
}

func (r *EdgeRunner) uploadPendingArchive(job *backup.JobDefinition, state *JobState, settings *config.Settings) bool {
	if state.PendingArchive == "" || state.PendingFingerprint == "" || state.PendingTimestamp == "" {
		return false
	}
	if _, err := os.Stat(state.PendingArchive); os.IsNotExist(err) {
		r.clearPendingArchive(state)
		state.LastStatus = "skipped_missing"
		r.StateStore.Set(job.RootPath, *state)
		return false
	}

	jobName := state.JobName
	if jobName == "" {
		jobName = job.JobName
	}
	if state.PendingArchiveSize == nil {
		if fi, err := os.Stat(state.PendingArchive); err == nil {
			sz := fi.Size()
			state.PendingArchiveSize = &sz
		}
	}
	if state.PendingArchiveSHA256 == "" {
		if sha, err := sha256File(state.PendingArchive); err == nil {
			state.PendingArchiveSHA256 = sha
		}
	}

	now := utcNow()
	state.ActivePhase = "uploading"
	state.ActivePhasePercent = 50
	state.LastStatus = "uploading"
	state.LastUploadStartedAt = now
	state.LastUploadUpdatedAt = now
	state.NextRetryAt = ""
	state.ManualInterventionRequired = false
	r.StateStore.Set(job.RootPath, *state)

	var preferredChunk int64
	if state.CurrentChunkSizeBytes != nil {
		preferredChunk = *state.CurrentChunkSizeBytes
	}

	progress := func(uploadID string, offset, chunkSize int64) {
		state.UploadID = uploadID
		state.UploadOffset = offset
		sz := chunkSize
		state.CurrentChunkSizeBytes = &sz
		state.LastUploadUpdatedAt = utcNow()
		state.ActivePhase = "uploading"
		state.ActivePhasePercent = uploadPhasePercent(offset, state.PendingArchiveSize)
		state.LastStatus = "uploading"
		r.StateStore.Set(job.RootPath, *state)
	}

	result, err := r.UploadClient.UploadArchive(
		context.Background(),
		settings.EdgeID, jobName,
		state.PendingFingerprint, state.PendingTimestamp,
		state.PendingArchive, state.PendingArchiveSHA256,
		state.UploadID, state.UploadOffset, preferredChunk,
		progress,
	)

	archiveName := filepath.Base(state.PendingArchive)
	if err != nil {
		uf, isUploadFail := err.(*UploadFailure)
		cat := "unexpected"
		if isUploadFail {
			cat = uf.Category
		}
		r.logger.Error("upload_failure",
			"job_name", jobName, "archive", archiveName,
			"category", cat, "detail", err)

		state.UploadAttemptCount++
		state.LastErrorDetail = err.Error()
		if isUploadFail {
			state.LastErrorCategory = uf.Category
			if uf.Retryable {
				if uf.Category == "circuit_open" {
					state.LastStatus = "circuit_open"
				} else {
					state.LastStatus = "retry_scheduled"
				}
				state.NextRetryAt = utcAfterSeconds(r.retryDelaySeconds(state.UploadAttemptCount, uf))
				state.ManualInterventionRequired = false
			} else {
				state.LastStatus = "manual_intervention_required"
				state.NextRetryAt = ""
				state.ManualInterventionRequired = true
			}
			if uf.Retryable && !settings.KeepLocalPending {
				r.discardPendingArchiveFile(state)
			}
		} else {
			state.LastErrorCategory = "unexpected"
			state.LastStatus = "retry_scheduled"
			state.NextRetryAt = utcAfterSeconds(r.retryDelaySeconds(state.UploadAttemptCount, nil))
			if !settings.KeepLocalPending {
				r.discardPendingArchiveFile(state)
			}
		}
		state.LastUploadUpdatedAt = utcNow()
		r.StateStore.Set(job.RootPath, *state)
		return false
	}

	os.Remove(state.PendingArchive)
	state.LastSuccessfulFingerprint = state.PendingFingerprint
	state.LastSuccessfulUpload = state.PendingTimestamp
	state.LastErrorDetail = ""
	state.LastErrorCategory = ""
	state.UploadAttemptCount = 0
	state.LastStatus = "success"
	state.LastStoredAs = result.StoredAs
	state.LastPruned = result.Pruned
	state.LastDuplicate = result.Duplicate
	state.NextRetryAt = ""
	state.ManualInterventionRequired = false
	r.clearPendingArchive(state)
	state.LastUploadUpdatedAt = utcNow()
	r.StateStore.Set(job.RootPath, *state)

	r.logger.Info("upload_success",
		"job_name", jobName, "archive", archiveName,
		"stored_as", result.StoredAs, "pruned", result.Pruned, "duplicate", result.Duplicate)
	return true
}

func (r *EdgeRunner) retryDelaySeconds(attemptCount int, uf *UploadFailure) int {
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

func (r *EdgeRunner) setActivePhase(job *backup.JobDefinition, state *JobState, phase string, pct int) {
	now := utcNow()
	if state.LastUploadStartedAt == "" {
		state.LastUploadStartedAt = now
	}
	state.LastUploadUpdatedAt = now
	state.ActivePhase = phase
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	state.ActivePhasePercent = pct
	state.LastStatus = phase
	r.StateStore.Set(job.RootPath, *state)
}

func (r *EdgeRunner) clearPendingArchive(state *JobState) {
	if state.PendingArchive != "" {
		os.Remove(state.PendingArchive)
	}
	state.PendingArchive = ""
	state.PendingArchiveSize = nil
	state.PendingArchiveSHA256 = ""
	state.PendingFingerprint = ""
	state.PendingTimestamp = ""
	state.UploadID = ""
	state.UploadOffset = 0
	state.CurrentChunkSizeBytes = nil
	state.NextRetryAt = ""
	state.ActivePhase = ""
	state.ActivePhasePercent = 0
}

func (r *EdgeRunner) discardPendingArchiveFile(state *JobState) {
	if state.PendingArchive != "" {
		os.Remove(state.PendingArchive)
	}
	state.PendingArchive = ""
	state.PendingArchiveSize = nil
	state.PendingArchiveSHA256 = ""
	state.UploadID = ""
	state.UploadOffset = 0
	state.CurrentChunkSizeBytes = nil
	state.ActivePhase = ""
	state.ActivePhasePercent = 0
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

func (r *EdgeRunner) hookContext(job *backup.JobDefinition, state *JobState, settings *config.Settings) map[string]any {
	return map[string]any{
		"edge_id":             settings.EdgeID,
		"edge_instance_id":    identity.LoadOrCreate(config.InstallationIDPath()),
		"job_name":            job.JobName,
		"job_root":            job.RootPath,
		"state_key":           job.RootPath,
		"last_status":         state.LastStatus,
		"last_error_category": state.LastErrorCategory,
		"last_error_detail":   state.LastErrorDetail,
		"stored_as":           state.LastStoredAs,
		"pruned":              state.LastPruned,
		"duplicate":           state.LastDuplicate,
		"pending_archive":     state.PendingArchive,
		"pending_fingerprint": state.PendingFingerprint,
		"pending_timestamp":   state.PendingTimestamp,
		"upload_id":           state.UploadID,
		"upload_offset":       state.UploadOffset,
		"next_retry_at":       state.NextRetryAt,
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
