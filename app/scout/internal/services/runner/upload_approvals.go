package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/3to1go/scout/internal/backup"
	"github.com/3to1go/scout/internal/config"
	"github.com/3to1go/scout/internal/services/state"
)

func largeUploadApproval(job *backup.JobDefinition, settings *config.Settings) state.UploadApproval {
	stationURL := strings.TrimRight(settings.StationURL, "/")
	key := sha256.Sum256([]byte(strings.Join([]string{job.RootPath, job.JobName, settings.ScoutID, stationURL}, "\x00")))
	return state.UploadApproval{ID: hex.EncodeToString(key[:]), JobPath: job.RootPath, JobName: job.JobName,
		ScoutID: settings.ScoutID, StationURL: stationURL, ApprovedAt: utcNow()}
}

func pendingLargeUploadApproval(job *backup.JobDefinition, s *state.JobState, settings *config.Settings) state.UploadApproval {
	jobName := s.JobName
	if jobName == "" {
		jobName = job.JobName
	}
	return largeUploadApproval(&backup.JobDefinition{RootPath: job.RootPath, JobName: jobName}, settings)
}

func (r *ScoutRunner) LargeUploadApprovals(ctx context.Context) ([]state.UploadApproval, error) {
	return r.UploadApprovals.List(ctx)
}

func (r *ScoutRunner) RemoveLargeUploadApproval(ctx context.Context, id string) error {
	if !r.cycleLock.TryLock() {
		return fmt.Errorf("a backup or recovery operation is running; try again when it finishes")
	}
	defer r.cycleLock.Unlock()
	approval, err := r.UploadApprovals.Get(ctx, id)
	if err != nil {
		return err
	}
	if approval == nil {
		return nil
	}
	// Clear any approval retained for this job's current staged archive too.
	s := r.StateStore.Get(approval.JobPath)
	if s.PendingUploadSizeApproved {
		s.PendingUploadSizeApproved = false
		if err := r.StateStore.Set(approval.JobPath, s); err != nil {
			return err
		}
	}
	return r.UploadApprovals.Delete(ctx, id)
}
