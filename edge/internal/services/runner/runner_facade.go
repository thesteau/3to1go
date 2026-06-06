package runner

import "github.com/3to1go/edge/internal/config"

// CurrentSettings returns the active settings under the runner lock.
func (r *EdgeRunner) CurrentSettings() *config.Settings {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.Settings
}

// StatusSnapshot returns the /api/status payload.
func (r *EdgeRunner) StatusSnapshot() map[string]any {
	r.mu.Lock()
	s := r.Settings
	r.mu.Unlock()
	return BuildStatusResponse(s, r.EncryptionKeyFingerprint(), r.UploadClient.CircuitBreaker)
}

// DirectoriesSnapshot returns the /api/directories payload.
func (r *EdgeRunner) DirectoriesSnapshot() map[string]any {
	r.mu.Lock()
	s := r.Settings
	r.mu.Unlock()
	return BuildDirectoryResponse(s, r.DirService)
}

// NtfySnapshot delegates to the embedded NtfyPublisher.
func (r *EdgeRunner) NtfySnapshot(cfg *config.Settings) map[string]any {
	return r.NtfyPublisher.Snapshot(cfg)
}

// TestNtfy delegates to the embedded NtfyPublisher.
func (r *EdgeRunner) TestNtfy(ntfyURL, ntfyTopic, messageTemplate string) error {
	return r.NtfyPublisher.PublishTest(ntfyURL, ntfyTopic, messageTemplate)
}

// CertSnapshot delegates to the embedded CertManager.
func (r *EdgeRunner) CertSnapshot() map[string]any {
	return r.CertManager.Snapshot()
}

// SaveCertFile delegates to the embedded CertManager.
func (r *EdgeRunner) SaveCertFile(filename string, content []byte) (any, error) {
	return r.CertManager.SaveUploadedFile(filename, content)
}

// DeleteCertFile delegates to the embedded CertManager.
func (r *EdgeRunner) DeleteCertFile(filename string) error {
	return r.CertManager.DeleteFile(filename)
}

// HookSnapshot delegates to the embedded HookManager.
func (r *EdgeRunner) HookSnapshot(preCmd, postCmd string) map[string]any {
	return r.HookManager.Snapshot(preCmd, postCmd)
}

// SaveHookFile delegates to the embedded HookManager.
func (r *EdgeRunner) SaveHookFile(filename string, content []byte) (any, error) {
	return r.HookManager.SaveUploadedFile(filename, content)
}

// ReadHookFile delegates to the embedded HookManager.
func (r *EdgeRunner) ReadHookFile(filename string) (string, string, error) {
	return r.HookManager.ReadTextFile(filename)
}

// DeleteHookFile delegates to the embedded HookManager.
func (r *EdgeRunner) DeleteHookFile(filename string) error {
	return r.HookManager.DeleteFile(filename)
}

// SaveJob delegates to the embedded DirectoryService.
func (r *EdgeRunner) SaveJob(relativePath string, cfg map[string]any) (any, error) {
	return r.DirService.SaveJob(relativePath, cfg)
}

// DeleteJob delegates to the embedded DirectoryService.
func (r *EdgeRunner) DeleteJob(relativePath string) error {
	return r.DirService.DeleteJob(relativePath)
}
