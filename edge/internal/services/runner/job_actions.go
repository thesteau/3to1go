package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/3to1go/edge/internal/services/directories"
)

func (r *EdgeRunner) BrowseFiles(path string) ([]directories.FileEntry, error) {
	r.mu.Lock()
	d := r.DirService
	r.mu.Unlock()
	return d.Browse(path)
}

func (r *EdgeRunner) DirectoryChildren(path string) ([]directories.DirectoryNode, error) {
	r.mu.Lock()
	d := r.DirService
	r.mu.Unlock()
	return d.ListChildren(path)
}

func (r *EdgeRunner) FolderSize(ctx context.Context, path string) (map[string]any, error) {
	r.mu.Lock()
	d := r.DirService
	r.mu.Unlock()
	return d.FolderSize(ctx, path)
}

func (r *EdgeRunner) ExcludePath(path string) error {
	if !r.cycleLock.TryLock() {
		return fmt.Errorf("a backup or recovery operation is running; try again when it finishes")
	}
	defer r.cycleLock.Unlock()
	return r.DirService.ExcludePath(path)
}

// ClearStagedBackup discards only this job's pending archive and retry metadata.
func (r *EdgeRunner) ClearStagedBackup(path string) error {
	if !r.cycleLock.TryLock() {
		return fmt.Errorf("a backup or recovery operation is running; try again when it finishes")
	}
	defer r.cycleLock.Unlock()
	job, err := r.DirService.LoadJob(path)
	if err != nil {
		return err
	}
	unlock := r.LockManager.Acquire(job.RootPath)
	if unlock == nil {
		return fmt.Errorf("job is running")
	}
	defer unlock()
	s := r.StateStore.Get(job.RootPath)
	if s.PendingArchive != "" {
		// Pending paths originate in the spool. Refuse unexpected paths rather
		// than turning a stale/corrupt state record into arbitrary file deletion.
		spool, err := filepath.Abs(r.Settings.SpoolDir)
		if err != nil {
			return err
		}
		archive, err := filepath.Abs(s.PendingArchive)
		if err != nil {
			return err
		}
		if filepath.Dir(archive) != spool {
			return fmt.Errorf("staged archive is outside the current spool directory")
		}
		if err := os.Remove(archive); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove staged archive: %w", err)
		}
	}
	s.PendingArchive = ""
	r.clearPendingArchive(&s)
	s.UploadAttemptCount = 0
	s.ManualInterventionRequired = false
	s.LastErrorCategory = ""
	s.LastErrorDetail = ""
	s.LastUploadStartedAt = ""
	s.LastUploadUpdatedAt = utcNow()
	s.LastStatus = "staged_backup_cleared"
	return r.StateStore.Set(job.RootPath, s)
}
