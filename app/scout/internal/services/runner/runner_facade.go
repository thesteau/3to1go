package runner

import (
	"fmt"

	"github.com/3to1go/scout/internal/config"
)

// CurrentSettings returns the active settings under the runner lock.
func (r *ScoutRunner) CurrentSettings() *config.Settings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Settings
}

// StatusSnapshot returns the /api/status payload.
func (r *ScoutRunner) StatusSnapshot() map[string]any {
	r.mu.Lock()
	s := r.Settings
	r.mu.Unlock()
	return BuildStatusResponse(s, r.EncryptionKeyFingerprint(), r.UploadClient.CircuitBreaker)
}

// DirectoriesSnapshot returns the /api/directories payload.
func (r *ScoutRunner) DirectoriesSnapshot() map[string]any {
	r.mu.Lock()
	s := r.Settings
	r.mu.Unlock()
	return BuildDirectoryResponse(s, r.DirService)
}

// NtfySnapshot delegates to the embedded NtfyPublisher.
func (r *ScoutRunner) NtfySnapshot(cfg *config.Settings) map[string]any {
	return r.NtfyPublisher.Snapshot(cfg)
}

// TestNtfy delegates to the embedded NtfyPublisher.
func (r *ScoutRunner) TestNtfy(ntfyURL, ntfyTopic, messageTemplate string) error {
	return r.NtfyPublisher.PublishTest(ntfyURL, ntfyTopic, messageTemplate)
}

// CertSnapshot delegates to the embedded CertManager.
func (r *ScoutRunner) CertSnapshot() map[string]any {
	return r.CertManager.Snapshot()
}

// SaveCertFile delegates to the embedded CertManager.
func (r *ScoutRunner) SaveCertFile(filename string, content []byte) (any, error) {
	return r.CertManager.SaveUploadedFile(filename, content)
}

// DeleteCertFile delegates to the embedded CertManager.
func (r *ScoutRunner) DeleteCertFile(filename string) error {
	return r.CertManager.DeleteFile(filename)
}

// HookSnapshot delegates to the embedded HookManager.
func (r *ScoutRunner) HookSnapshot(preCmd, postCmd string) map[string]any {
	return r.HookManager.Snapshot(preCmd, postCmd)
}

// SaveHookFile delegates to the embedded HookManager.
func (r *ScoutRunner) SaveHookFile(filename string, content []byte) (any, error) {
	return r.HookManager.SaveUploadedFile(filename, content)
}

// ReadHookFile delegates to the embedded HookManager.
func (r *ScoutRunner) ReadHookFile(filename string) (string, string, error) {
	return r.HookManager.ReadTextFile(filename)
}

// DeleteHookFile delegates to the embedded HookManager.
func (r *ScoutRunner) DeleteHookFile(filename string) error {
	return r.HookManager.DeleteFile(filename)
}

// SaveJob delegates to the embedded DirectoryService.
func (r *ScoutRunner) SaveJob(relativePath string, cfg map[string]any) (any, error) {
	if !r.cycleLock.TryLock() {
		return nil, fmt.Errorf("a backup or recovery operation is running; try again when it finishes")
	}
	defer r.cycleLock.Unlock()
	return r.DirService.SaveJob(relativePath, cfg)
}

// DeleteJob delegates to the embedded DirectoryService.
func (r *ScoutRunner) DeleteJob(relativePath string) error {
	if !r.cycleLock.TryLock() {
		return fmt.Errorf("a backup or recovery operation is running; try again when it finishes")
	}
	defer r.cycleLock.Unlock()
	job, _ := r.DirService.LoadJob(relativePath)
	if err := r.DirService.DeleteJob(relativePath); err != nil {
		return err
	}
	// The job is already gone, so leftover anomaly history is only logged.
	if job != nil {
		if err := r.Anomalies.Delete(job.RootPath); err != nil {
			r.logger.Warn("anomaly_history_failed", "job_name", job.JobName, "error", err)
		}
	}
	return nil
}
