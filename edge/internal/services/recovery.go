package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/encryption"
)

// RecoveryError is a user-visible error from the recovery process.
type RecoveryError struct {
	Message    string
	StatusCode int
}

func (e *RecoveryError) Error() string { return e.Message }

// RecoveryResult is returned on successful recovery or preview.
type RecoveryResult struct {
	Status              string        `json:"status"`
	JobName             string        `json:"job_name"`
	RelativePath        string        `json:"relative_path,omitempty"`
	SnapshotFilename    string        `json:"snapshot_filename"`
	SnapshotFingerprint string        `json:"snapshot_fingerprint"`
	RestoredFiles       int           `json:"restored_files,omitempty"`
	Entries             interface{}   `json:"entries,omitempty"`
	TotalFiles          int           `json:"total_files,omitempty"`
	ReplaceCount        int           `json:"replace_count,omitempty"`
	AddCount            int           `json:"add_count,omitempty"`
}

// RecoveryService downloads and optionally restores archives from central.
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
	state := r.beginRecovery(job)

	downloadPath := r.tempPath(".download.tar.zst")
	decryptedPath := r.tempPath(".decrypted.tar.zst")
	defer os.Remove(downloadPath)
	defer os.Remove(decryptedPath)

	filename, err := r.downloadSnapshot(ctx, job, fingerprint, downloadPath)
	if err != nil {
		return nil, r.handleError(job, &state, err)
	}

	if err := encryption.DecryptFile(r.encKey, downloadPath, decryptedPath); err != nil {
		re := &RecoveryError{Message: "unable to decrypt snapshot with this Edge key", StatusCode: 409}
		return nil, r.handleError(job, &state, re)
	}

	restored, err := backup.ExtractArchive(decryptedPath, job.RootPath)
	if err != nil {
		return nil, r.handleError(job, &state, &RecoveryError{Message: err.Error(), StatusCode: 500})
	}

	state.LastStatus = "recovered"
	state.LastErrorCategory = ""
	state.LastErrorDetail = ""
	r.stateStore.Set(job.RootPath, state)

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

func (r *RecoveryService) Preview(ctx context.Context, job *backup.JobDefinition, fingerprint string) (*RecoveryResult, error) {
	downloadPath := r.tempPath(".download.tar.zst")
	decryptedPath := r.tempPath(".decrypted.tar.zst")
	defer os.Remove(downloadPath)
	defer os.Remove(decryptedPath)

	filename, err := r.downloadSnapshot(ctx, job, fingerprint, downloadPath)
	if err != nil {
		return nil, r.handlePreviewError(job, err)
	}

	if err := encryption.DecryptFile(r.encKey, downloadPath, decryptedPath); err != nil {
		re := &RecoveryError{Message: "unable to decrypt snapshot with this Edge key", StatusCode: 409}
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
		return r.downloader.DownloadSnapshotByFingerprint(ctx, r.settings.EdgeID, job.JobName, fingerprint, destPath)
	}
	return r.downloader.DownloadLatestSnapshot(ctx, r.settings.EdgeID, job.JobName, destPath)
}

func (r *RecoveryService) beginRecovery(job *backup.JobDefinition) JobState {
	state := r.stateStore.Get(job.RootPath)
	state.JobName = job.JobName
	state.LastStatus = "recovering"
	state.LastErrorCategory = ""
	state.LastErrorDetail = ""
	r.stateStore.Set(job.RootPath, state)
	return state
}

func (r *RecoveryService) handleError(job *backup.JobDefinition, state *JobState, err error) error {
	re := wrapRecoveryError(err, false)
	state.JobName = job.JobName
	state.LastStatus = "recovery_failed"
	state.LastErrorCategory = "recovery"
	state.LastErrorDetail = re.Message
	r.stateStore.Set(job.RootPath, *state)
	r.logger.Error("recovery_failed", "job_name", job.JobName, "path", job.RootPath, "detail", re.Message)
	return re
}

func (r *RecoveryService) handlePreviewError(job *backup.JobDefinition, err error) error {
	re := wrapRecoveryError(err, true)
	r.logger.Warn("recovery_preview_failed", "job_name", job.JobName, "path", job.RootPath, "detail", re.Message)
	return re
}

func wrapRecoveryError(err error, isPreview bool) *RecoveryError {
	if re, ok := err.(*RecoveryError); ok {
		return re
	}
	if uf, ok := err.(*UploadFailure); ok {
		switch {
		case uf.StatusCode == 404:
			return &RecoveryError{Message: "no snapshots found on Central", StatusCode: 404}
		case uf.Category == "unauthorized":
			code := 401
			if !isPreview {
				code = 502
			}
			return &RecoveryError{Message: "Central rejected the recovery request; check the Edge credential", StatusCode: code}
		case uf.Category == "network" || uf.Category == "server" || uf.Category == "rate_limited" || uf.Category == "circuit_open":
			return &RecoveryError{Message: uf.Error(), StatusCode: 502}
		default:
			return &RecoveryError{Message: uf.Error(), StatusCode: 400}
		}
	}
	return &RecoveryError{Message: err.Error(), StatusCode: 500}
}

func (r *RecoveryService) tempPath(suffix string) string {
	os.MkdirAll(r.settings.SpoolDir, 0o755)
	f, _ := os.CreateTemp(r.settings.SpoolDir, "recovery-*"+suffix)
	if f != nil {
		f.Close()
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
