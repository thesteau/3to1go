package runner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/3to1go/scout/internal/backup"
	"github.com/3to1go/scout/internal/encryption"
	"github.com/3to1go/scout/internal/services/recovery"
	"github.com/3to1go/scout/internal/services/upload"
	"github.com/3to1go/shared/protocol"
)

func (r *ScoutRunner) RestoreRequests(ctx context.Context) ([]protocol.RestoreRequest, error) {
	r.mu.Lock()
	client, scoutID, hasKey := r.UploadClient, r.Settings.ScoutID, len(r.encKey) == 32
	configured := r.Settings.StationURL != "" && r.Settings.ScoutCredential != ""
	r.mu.Unlock()
	if !hasKey || !configured {
		return []protocol.RestoreRequest{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return client.ListRestoreRequests(ctx, scoutID)
}

// RestoreDestination allows a deleted folder to be recreated while rejecting
// absolute paths, traversal, symlinks and Scout runtime folders.
func restoreDestination(scanRoot, relativePath string) (string, error) {
	if relativePath == "" || filepath.IsAbs(relativePath) || filepath.VolumeName(relativePath) != "" {
		return "", fmt.Errorf("choose a destination relative to the scan root")
	}
	root, err := filepath.Abs(scanRoot)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	relativePath = filepath.Clean(filepath.FromSlash(relativePath))
	if relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("destination must remain within scan root")
	}
	current := root
	if backup.IsRuntimePath(current) {
		return "", fmt.Errorf("cannot restore into a runtime directory")
	}
	if relativePath != "." {
		for _, part := range strings.Split(relativePath, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err != nil && !os.IsNotExist(err) {
				return "", err
			}
			if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
				return "", fmt.Errorf("destination must contain only directories, without symlinks")
			}
			if backup.IsRuntimePath(current) {
				return "", fmt.Errorf("cannot restore into a runtime directory")
			}
		}
	}
	return current, nil
}

// restoreRequestGone reports Station's answer for a request that expired or
// was already decided between listing it and recording the decision.
func restoreRequestGone(err error) bool {
	var failure *upload.UploadFailure
	return errors.As(err, &failure) && failure.StatusCode == http.StatusConflict
}

func (r *ScoutRunner) DecideRestoreRequest(ctx context.Context, id, decision, relativePath string, providedKey ...string) (any, error) {
	if decision != "accept" && decision != "reject" {
		return nil, fmt.Errorf("invalid decision")
	}
	if !r.cycleLock.TryLock() {
		return nil, &recovery.RecoveryError{Message: "another operation is running; retry when it finishes", StatusCode: 409}
	}
	defer r.cycleLock.Unlock()
	requests, err := r.RestoreRequests(ctx)
	if err != nil {
		return nil, err
	}
	var selected *protocol.RestoreRequest
	for i := range requests {
		if requests[i].ID == id {
			selected = &requests[i]
			break
		}
	}
	// Station drops requests after one hour. Rejecting one that is gone only
	// dismisses it; accepting one is refused before anything is downloaded.
	if selected == nil {
		if decision == "reject" {
			return map[string]string{"status": "rejected"}, nil
		}
		return nil, &recovery.RecoveryError{Message: "this restore request expired; click Restore in Station again", StatusCode: 409}
	}
	r.mu.Lock()
	client, settings := r.UploadClient, r.Settings
	r.mu.Unlock()
	if decision == "reject" {
		if err := client.DecideRestoreRequest(ctx, settings.ScoutID, id, "rejected"); err != nil && !restoreRequestGone(err) {
			return nil, err
		}
		return map[string]string{"status": "rejected"}, nil
	}
	if relativePath == "" {
		relativePath = selected.JobName
	}
	if relativePath == "." || relativePath == ".." || strings.ContainsAny(relativePath, `/\`) {
		return nil, fmt.Errorf("choose a folder name directly under the scan root")
	}
	r.mu.Lock()
	key := append([]byte(nil), r.encKey...)
	r.mu.Unlock()
	crossDevice := selected.SourceScoutID != "" && (selected.SourceScoutID != settings.ScoutID || selected.SourceInstanceID != selected.ScoutInstanceID)
	if crossDevice {
		if len(providedKey) == 0 || providedKey[0] == "" {
			return nil, fmt.Errorf("provide the original snapshot's Scout key")
		}
		key, err = encryption.KeyFromBase64(providedKey[0])
		if err != nil {
			return nil, err
		}
	}
	destination, err := restoreDestination(settings.ScanRoot, relativePath)
	if err != nil {
		return nil, err
	}
	if err := client.DecideRestoreRequest(ctx, settings.ScoutID, id, "accepted"); err != nil {
		if restoreRequestGone(err) {
			return nil, &recovery.RecoveryError{Message: "this restore request expired; click Restore in Station again", StatusCode: 409}
		}
		return nil, err
	}
	job := &backup.JobDefinition{RootPath: destination, JobName: selected.JobName}
	result, err := r.Recovery.RecoverRequest(ctx, job, selected.Filename, key, func(path string) (string, error) {
		return client.DownloadRestoreRequest(ctx, settings.ScoutID, id, path)
	})
	if err != nil {
		return nil, err
	}
	result.RelativePath = relativePath
	if err := client.DecideRestoreRequest(ctx, settings.ScoutID, id, "completed"); err != nil {
		return nil, &recovery.RecoveryError{Message: "files restored, but Station could not record completion; reject the request to dismiss it", StatusCode: 502}
	}
	return result, nil
}
