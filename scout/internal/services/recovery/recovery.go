package recovery

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/3to1go/scout/internal/backup"
	"github.com/3to1go/scout/internal/config"
	"github.com/3to1go/scout/internal/encryption"
	"github.com/3to1go/scout/internal/services/state"
	"github.com/3to1go/scout/internal/services/upload"
)

// jobStateStore is the persistence interface for per-job upload state.
// *state.StateStore satisfies it.
type jobStateStore interface {
	Get(rootPath string) state.JobState
	Set(rootPath string, s state.JobState) error
	Delete(rootPath string) error
}

// snapshotDownloader downloads backup snapshots from station.
// *upload.UploadClient satisfies it.
type snapshotDownloader interface {
	DownloadLatestSnapshot(ctx context.Context, scoutID, jobName, destPath string) (string, error)
	DownloadSnapshotByFingerprint(ctx context.Context, scoutID, jobName, fingerprint, destPath string) (string, error)
	DownloadSnapshotByFilename(ctx context.Context, scoutID, jobName, filename, destPath string) (string, error)
}

// RecoveryError is a user-visible error from the recovery process.
type RecoveryError struct {
	Message    string
	StatusCode int
}

func (e *RecoveryError) Error() string { return e.Message }

// RecoveryResult is returned on successful recovery or preview.
type RecoveryResult struct {
	Status              string `json:"status"`
	JobName             string `json:"job_name"`
	RelativePath        string `json:"relative_path,omitempty"`
	SnapshotFilename    string `json:"snapshot_filename"`
	SnapshotFingerprint string `json:"snapshot_fingerprint"`
	RestoredFiles       int    `json:"restored_files,omitempty"`
	Entries             any    `json:"entries,omitempty"`
	TotalFiles          int    `json:"total_files,omitempty"`
	ReplaceCount        int    `json:"replace_count,omitempty"`
	AddCount            int    `json:"add_count,omitempty"`
}

// RecoveryService downloads and optionally restores archives from station.
type RecoveryService struct {
	settings   *config.Settings
	logger     *slog.Logger
	stateStore jobStateStore
	downloader snapshotDownloader
	encKey     []byte
}

func NewRecoveryService(settings *config.Settings, logger *slog.Logger, stateStore jobStateStore, downloader snapshotDownloader, encKey []byte) *RecoveryService {
	return &RecoveryService{
		settings:   settings,
		logger:     logger,
		stateStore: stateStore,
		downloader: downloader,
		encKey:     encKey,
	}
}

func (r *RecoveryService) Recover(ctx context.Context, job *backup.JobDefinition, fingerprint string) (*RecoveryResult, error) {
	jobState := r.beginRecovery(job)

	downloadPath := r.tempPath(".download.tar.zst")
	decryptedPath := r.tempPath(".decrypted.tar.zst")
	defer func() { _ = os.Remove(downloadPath) }()
	defer func() { _ = os.Remove(decryptedPath) }()

	filename, err := r.downloadSnapshot(ctx, job, fingerprint, downloadPath)
	if err != nil {
		return nil, r.handleError(job, &jobState, err)
	}

	if err := encryption.DecryptFile(r.encKey, downloadPath, decryptedPath); err != nil {
		re := &RecoveryError{Message: "unable to decrypt snapshot with this Scout key", StatusCode: 409}
		return nil, r.handleError(job, &jobState, re)
	}

	restored, err := backup.ExtractArchive(decryptedPath, job.RootPath)
	if err != nil {
		return nil, r.handleError(job, &jobState, &RecoveryError{Message: err.Error(), StatusCode: 500})
	}

	jobState.LastStatus = "recovered"
	jobState.LastErrorCategory = ""
	jobState.LastErrorDetail = ""
	r.saveState(job, jobState)

	r.logger.Info("recovery_success",
		"job_name", job.JobName,
		"path", job.RootPath,
		"snapshot", filename,
		"restored_files", restored)

	return &RecoveryResult{
		Status:              "recovered",
		JobName:             job.JobName,
		SnapshotFilename:    filename,
		SnapshotFingerprint: fingerprintFromFilename(filename),
		RestoredFiles:       restored,
	}, nil
}

// RecoverFilename restores one exact archive that Station asked for. The
// destination may not be a job folder, so no job state is recorded for it.
func (r *RecoveryService) RecoverFilename(ctx context.Context, job *backup.JobDefinition, filename string) (*RecoveryResult, error) {
	return r.RecoverRequest(ctx, job, filename, r.encKey, func(path string) (string, error) {
		return r.downloader.DownloadSnapshotByFilename(ctx, r.settings.ScoutID, job.JobName, filename, path)
	})
}

func (r *RecoveryService) RecoverRequest(ctx context.Context, job *backup.JobDefinition, filename string, key []byte, download func(string) (string, error)) (*RecoveryResult, error) {
	downloadPath := r.tempPath(".download.tar.zst")
	decryptedPath := r.tempPath(".decrypted.tar.zst")
	defer func() { _ = os.Remove(downloadPath) }()
	defer func() { _ = os.Remove(decryptedPath) }()

	got, err := download(downloadPath)
	if err != nil {
		return nil, r.handleRequestError(job, err)
	}
	if got != filename {
		return nil, r.handleRequestError(job, &RecoveryError{Message: "Station returned a different snapshot than requested", StatusCode: 502})
	}

	if err := encryption.DecryptFile(key, downloadPath, decryptedPath); err != nil {
		return nil, r.handleRequestError(job, &RecoveryError{Message: "unable to decrypt snapshot with the provided encryption key", StatusCode: 409})
	}

	restored, err := backup.ExtractArchive(decryptedPath, job.RootPath)
	if err != nil {
		return nil, r.handleRequestError(job, &RecoveryError{Message: err.Error(), StatusCode: 500})
	}

	r.logger.Info("restore_request_success",
		"job_name", job.JobName,
		"path", job.RootPath,
		"snapshot", filename,
		"restored_files", restored)

	return &RecoveryResult{
		Status:              "recovered",
		JobName:             job.JobName,
		SnapshotFilename:    filename,
		SnapshotFingerprint: fingerprintFromFilename(filename),
		RestoredFiles:       restored,
	}, nil
}

func (r *RecoveryService) Preview(ctx context.Context, job *backup.JobDefinition, fingerprint string) (*RecoveryResult, error) {
	downloadPath := r.tempPath(".download.tar.zst")
	decryptedPath := r.tempPath(".decrypted.tar.zst")
	defer func() { _ = os.Remove(downloadPath) }()
	defer func() { _ = os.Remove(decryptedPath) }()

	filename, err := r.downloadSnapshot(ctx, job, fingerprint, downloadPath)
	if err != nil {
		return nil, r.handlePreviewError(job, err)
	}

	if err := encryption.DecryptFile(r.encKey, downloadPath, decryptedPath); err != nil {
		re := &RecoveryError{Message: "unable to decrypt snapshot with this Scout key", StatusCode: 409}
		return nil, r.handlePreviewError(job, re)
	}

	preview, err := backup.ListArchiveEntries(decryptedPath, job.RootPath)
	if err != nil {
		return nil, r.handlePreviewError(job, &RecoveryError{Message: err.Error(), StatusCode: 500})
	}

	result := &RecoveryResult{
		Status:              "preview",
		JobName:             job.JobName,
		SnapshotFilename:    filename,
		SnapshotFingerprint: fingerprintFromFilename(filename),
	}
	if v, ok := preview["entries"]; ok {
		result.Entries = v
	}
	if v, ok := preview["total_files"].(int); ok {
		result.TotalFiles = v
	}
	if v, ok := preview["replace_count"].(int); ok {
		result.ReplaceCount = v
	}
	if v, ok := preview["add_count"].(int); ok {
		result.AddCount = v
	}
	return result, nil
}

func (r *RecoveryService) downloadSnapshot(ctx context.Context, job *backup.JobDefinition, fingerprint, destPath string) (string, error) {
	if fingerprint != "" {
		return r.downloader.DownloadSnapshotByFingerprint(ctx, r.settings.ScoutID, job.JobName, fingerprint, destPath)
	}
	return r.downloader.DownloadLatestSnapshot(ctx, r.settings.ScoutID, job.JobName, destPath)
}

func (r *RecoveryService) beginRecovery(job *backup.JobDefinition) state.JobState {
	s := r.stateStore.Get(job.RootPath)
	s.JobName = job.JobName
	s.LastStatus = "recovering"
	s.LastErrorCategory = ""
	s.LastErrorDetail = ""
	r.saveState(job, s)
	return s
}

// saveState persists a job's recovery status; a failed write is logged and does not fail recovery.
func (r *RecoveryService) saveState(job *backup.JobDefinition, s state.JobState) {
	if err := r.stateStore.Set(job.RootPath, s); err != nil {
		r.logger.Error("state_save_failed", "job_name", job.JobName, "error", err)
	}
}

func (r *RecoveryService) handleError(job *backup.JobDefinition, s *state.JobState, err error) error {
	re := wrapRecoveryError(err, false)
	s.JobName = job.JobName
	s.LastStatus = "recovery_failed"
	s.LastErrorCategory = "recovery"
	s.LastErrorDetail = re.Message
	r.saveState(job, *s)
	r.logger.Error("recovery_failed", "job_name", job.JobName, "path", job.RootPath, "detail", re.Message)
	return re
}

func (r *RecoveryService) handlePreviewError(job *backup.JobDefinition, err error) error {
	re := wrapRecoveryError(err, true)
	r.logger.Warn("recovery_preview_failed", "job_name", job.JobName, "path", job.RootPath, "detail", re.Message)
	return re
}

func (r *RecoveryService) handleRequestError(job *backup.JobDefinition, err error) error {
	re := wrapRecoveryError(err, false)
	r.logger.Error("restore_request_failed", "job_name", job.JobName, "path", job.RootPath, "detail", re.Message)
	return re
}

func wrapRecoveryError(err error, isPreview bool) *RecoveryError {
	if re, ok := err.(*RecoveryError); ok {
		return re
	}
	if uf, ok := err.(*upload.UploadFailure); ok {
		switch {
		case uf.StatusCode == 404:
			return &RecoveryError{Message: "no snapshots found on Station", StatusCode: 404}
		case uf.Category == "unauthorized":
			code := 401
			if !isPreview {
				code = 502
			}
			return &RecoveryError{Message: "Station rejected the recovery request; check the Scout credential", StatusCode: code}
		case uf.Category == "network" || uf.Category == "server" || uf.Category == "rate_limited" || uf.Category == "circuit_open":
			return &RecoveryError{Message: uf.Error(), StatusCode: 502}
		default:
			return &RecoveryError{Message: uf.Error(), StatusCode: 400}
		}
	}
	return &RecoveryError{Message: err.Error(), StatusCode: 500}
}

func (r *RecoveryService) tempPath(suffix string) string {
	// A missing spool directory surfaces when the download writes to the returned path.
	_ = os.MkdirAll(r.settings.SpoolDir, 0o755)
	f, _ := os.CreateTemp(r.settings.SpoolDir, "recovery-*"+suffix)
	if f != nil {
		_ = f.Close()
		return f.Name()
	}
	return filepath.Join(r.settings.SpoolDir, fmt.Sprintf("recovery-%d%s", os.Getpid(), suffix))
}

func fingerprintFromFilename(filename string) string {
	parts := strings.Split(filename, "__")
	if len(parts) < 3 {
		return ""
	}
	last := parts[len(parts)-1]
	last = strings.TrimSuffix(last, ".tar.zst")
	return last
}
