package api

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/3to1go/shared/httpx"
	"github.com/3to1go/station/internal/store"
)

// These types list every public field, so index data such as archive filenames
// can't reach this route by accident.
type reportSnapshot struct {
	SizeBytes  int64  `json:"size_bytes"`
	ModifiedAt string `json:"modified_at"`
	Unusual    bool   `json:"unusual"`
}

type reportJob struct {
	JobName          string           `json:"job_name"`
	SnapshotCount    int              `json:"snapshot_count"`
	StoredBytes      int64            `json:"stored_bytes"`
	LatestSnapshotAt *string          `json:"latest_snapshot_at"`
	Snapshots        []reportSnapshot `json:"snapshots"`
}

type reportInstance struct {
	ScoutID         string      `json:"scout_id"`
	ScoutInstanceID string      `json:"scout_instance_id"`
	LastSeenAt      *string     `json:"last_seen_at"`
	Jobs            []reportJob `json:"jobs"`
}

type backupReport struct {
	SnapshotCount int              `json:"snapshot_count"`
	StoredBytes   int64            `json:"stored_bytes"`
	Instances     []reportInstance `json:"instances"`
}

func (a *App) handleBackupReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !a.Settings().BackupReportEnabled {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	report, err := a.buildBackupReport(r.Context())
	if err != nil {
		// Keep database details out of this public endpoint.
		httpx.WriteError(w, http.StatusServiceUnavailable, "backup report unavailable")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, report)
}

func (a *App) buildBackupReport(ctx context.Context) (backupReport, error) {
	registrations, err := a.snapIndex.ListScoutRegistrations(ctx, nil)
	if err != nil {
		return backupReport{}, err
	}
	namespaces, err := a.snapIndex.ListNamespaces(ctx)
	if err != nil {
		return backupReport{}, err
	}

	report := backupReport{Instances: []reportInstance{}}
	positions := map[[2]string]int{}
	instance := func(scoutID, instanceID string) *reportInstance {
		key := [2]string{scoutID, instanceID}
		position, found := positions[key]
		if !found {
			position = len(report.Instances)
			positions[key] = position
			report.Instances = append(report.Instances, reportInstance{ScoutID: scoutID, ScoutInstanceID: instanceID, Jobs: []reportJob{}})
		}
		return &report.Instances[position]
	}

	// Registrations come first so a Scout with no backups yet is still listed.
	for _, registration := range registrations {
		instance(registration.ScoutID, registration.ScoutInstanceID).LastSeenAt = reportLastSeen(registration.LastSeenAt)
	}
	for _, namespace := range namespaces {
		entry := instance(namespace.ScoutID, namespace.ScoutInstanceID)
		for _, job := range namespace.Jobs {
			reported := newReportJob(job)
			entry.Jobs = append(entry.Jobs, reported)
			report.SnapshotCount += reported.SnapshotCount
			report.StoredBytes += reported.StoredBytes
		}
	}

	slices.SortFunc(report.Instances, func(x, y reportInstance) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(x.ScoutID), strings.ToLower(y.ScoutID)),
			cmp.Compare(strings.ToLower(x.ScoutInstanceID), strings.ToLower(y.ScoutInstanceID)),
		)
	})
	return report, nil
}

func newReportJob(job store.SnapshotJob) reportJob {
	reported := reportJob{
		JobName:       job.JobName,
		SnapshotCount: job.SnapshotCount,
		Snapshots:     make([]reportSnapshot, 0, len(job.Snapshots)),
	}
	var latest float64
	for _, snapshot := range job.Snapshots {
		reported.StoredBytes += snapshot.SizeBytes
		latest = max(latest, snapshot.Mtime)
		reported.Snapshots = append(reported.Snapshots, reportSnapshot{
			SizeBytes:  snapshot.SizeBytes,
			ModifiedAt: reportTime(time.Unix(int64(snapshot.Mtime), 0)),
			// The stored value is a reason; this public route only says whether one exists.
			Unusual: snapshot.Unusual != "",
		})
	}
	if len(job.Snapshots) > 0 {
		at := reportTime(time.Unix(int64(latest), 0))
		reported.LatestSnapshotAt = &at
	}
	return reported
}

func reportLastSeen(value string) *string {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	at := reportTime(t)
	return &at
}

func reportTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
