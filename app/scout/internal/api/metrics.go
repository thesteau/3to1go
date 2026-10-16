package api

import (
	"context"

	"github.com/3to1go/shared/metrics"
)

func (a *App) collectMetrics(_ context.Context) ([]metrics.Gauge, error) {
	jobs, discovering, err := a.runner.MetricsSnapshot()
	if err != nil {
		return nil, err
	}
	s := a.runner.CurrentSettings()
	scheduler := a.scheduler.Snapshot()
	var values []metrics.Gauge
	add := func(name, help string, value float64, labels map[string]string) {
		values = append(values, metrics.NewGauge("three_to_one_go_scout_"+name, help, value, labels))
	}
	add("uploads_paused", "Whether scheduled backups are paused.", metrics.Bool(s.UploadsPaused), nil)
	add("jobs", "Number of discovered backup jobs, including invalid configurations.", float64(len(jobs)), nil)
	add("discovering", "Whether the initial search for backup jobs is still running.", metrics.Bool(discovering), nil)
	add("cycle_running", "Whether a scheduled or manually requested backup cycle is running.", metrics.Bool(scheduler["state"] == "running"), nil)
	for _, job := range jobs {
		// Relative job paths distinguish jobs with the same display name.
		labels := map[string]string{"job_path": job.RelativePath}
		state := job.State
		add("job_config_valid", "Whether the job configuration is valid.", metrics.Bool(job.ConfigError == nil), labels)
		add("job_last_successful_upload_timestamp_seconds", "Unix time of the last successful upload, or zero if never uploaded.", metrics.Timestamp(state.LastSuccessfulUpload), labels)
		add("job_pending_upload", "Whether an archive is waiting to upload.", metrics.Bool(state.PendingArchive != ""), labels)
		add("job_manual_intervention_required", "Whether the job requires operator intervention.", metrics.Bool(state.ManualInterventionRequired), labels)
		add("job_upload_attempts", "Upload attempts for the current or most recent archive.", float64(state.UploadAttemptCount), labels)
		add("job_upload_error", "Whether the job has a recorded upload error.", metrics.Bool(state.LastErrorDetail != ""), labels)
		var lastSize, pendingSize int64
		if state.LastBackupSizeBytes != nil {
			lastSize = *state.LastBackupSizeBytes
		}
		if state.PendingArchiveSize != nil {
			pendingSize = *state.PendingArchiveSize
		}
		add("job_last_backup_size_bytes", "Size of the last uploaded archive, or zero if unknown.", float64(lastSize), labels)
		add("job_pending_upload_size_bytes", "Size of the archive waiting to upload, or zero if none.", float64(pendingSize), labels)
	}
	return values, nil
}
