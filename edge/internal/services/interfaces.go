package services

import "context"

// jobStateStore is the persistence interface for per-job upload state.
// Consumers define this interface; *StateStore satisfies it.
type jobStateStore interface {
	Get(rootPath string) JobState
	Set(rootPath string, state JobState) error
	Delete(rootPath string) error
}

// snapshotDownloader downloads backup snapshots from central.
// Consumers define this interface; *UploadClient satisfies it.
type snapshotDownloader interface {
	DownloadLatestSnapshot(ctx context.Context, edgeID, jobName, destPath string) (string, error)
	DownloadSnapshotByFingerprint(ctx context.Context, edgeID, jobName, fingerprint, destPath string) (string, error)
}

// circuitSnapshotter provides upload circuit breaker status.
// Consumers define this interface; *CircuitBreaker satisfies it.
type circuitSnapshotter interface {
	Snapshot() map[string]interface{}
}
