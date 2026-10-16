package api

import (
	"context"

	"github.com/3to1go/shared/metrics"
	"github.com/3to1go/station/internal/storage"
)

func (a *App) collectMetrics(ctx context.Context) ([]metrics.Gauge, error) {
	namespaces, err := a.snapIndex.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}
	registrations, err := a.snapIndex.ListScoutRegistrations(ctx, nil)
	if err != nil {
		return nil, err
	}
	s := a.Settings()
	var values []metrics.Gauge
	add := func(name, help string, value float64, labels map[string]string) {
		values = append(values, metrics.NewGauge("three_to_one_go_station_"+name, help, value, labels))
	}
	add("uploads_paused", "Whether new upload sessions are paused.", metrics.Bool(s.UploadsPaused), nil)
	add("storage_healthy", "Whether the snapshot storage backend is available.", metrics.Bool(a.backend.Healthcheck()), nil)
	add("retention_keep_last", "Maximum snapshots retained per job and Scout instance.", float64(s.RetentionKeepLast), nil)
	// DiskUsage is a filesystem capacity probe; scraping never walks the archive tree.
	if total, _, free, err := storage.DiskUsage(s.BackupRoot); err == nil {
		add("disk_total_bytes", "Capacity of the filesystem containing the snapshot folder.", float64(total), nil)
		add("disk_free_bytes", "Free space on the filesystem containing the snapshot folder.", float64(free), nil)
	}
	for _, registration := range registrations {
		labels := map[string]string{"scout_id": registration.ScoutID, "scout_instance_id": registration.ScoutInstanceID}
		add("scout_last_seen_timestamp_seconds", "Unix time of the last contact from this Scout instance.", metrics.Timestamp(registration.LastSeenAt), labels)
	}
	var jobs, snapshots int
	var storedBytes int64
	for _, namespace := range namespaces {
		for _, job := range namespace.Jobs {
			labels := map[string]string{"scout_id": namespace.ScoutID, "scout_instance_id": namespace.ScoutInstanceID, "job_name": job.JobName}
			var size int64
			var latest float64
			for _, snapshot := range job.Snapshots {
				size += snapshot.SizeBytes
				latest = max(latest, snapshot.Mtime)
			}
			add("job_snapshots", "Number of retained snapshots for this job and Scout instance.", float64(job.SnapshotCount), labels)
			add("job_stored_bytes", "Total indexed archive bytes for this job and Scout instance.", float64(size), labels)
			add("job_latest_snapshot_timestamp_seconds", "Unix modification time of the latest stored archive, or zero if none.", latest, labels)
			jobs++
			snapshots += job.SnapshotCount
			storedBytes += size
		}
	}
	add("scout_instances", "Number of registered Scout instances.", float64(len(registrations)), nil)
	add("jobs", "Number of indexed jobs across all Scout instances.", float64(jobs), nil)
	add("snapshots", "Number of retained snapshots across all Scout instances.", float64(snapshots), nil)
	add("stored_bytes", "Total indexed archive bytes across all Scout instances.", float64(storedBytes), nil)
	return values, nil
}
