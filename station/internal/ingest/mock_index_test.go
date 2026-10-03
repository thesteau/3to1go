package ingest

import (
	"context"

	"github.com/3to1go/station/internal/store"
)

// mockIndex is a configurable in-memory implementation of snapshotIndexer.
type mockIndex struct {
	findDuplicateResult *store.SnapshotEntry
	findDuplicateErr    error

	upsertSnapshotErr error

	reconcileErr error

	getScoutRegistrationResult *store.ScoutRegistration
	getScoutRegistrationErr    error

	upsertScoutRegistrationErr error

	// archiveSizes is the per-namespace size history, oldest first.
	archiveSizes map[string][]int64
}

func (m *mockIndex) RecentArchiveSizes(_ context.Context, namespace string, limit int) ([]int64, error) {
	sizes := m.archiveSizes[namespace]
	if len(sizes) > limit {
		sizes = sizes[len(sizes)-limit:]
	}
	return sizes, nil
}

func (m *mockIndex) RecordArchiveSize(_ context.Context, namespace string, size int64, keep int) error {
	if m.archiveSizes == nil {
		m.archiveSizes = map[string][]int64{}
	}
	sizes := append(m.archiveSizes[namespace], size)
	if len(sizes) > keep {
		sizes = sizes[len(sizes)-keep:]
	}
	m.archiveSizes[namespace] = sizes
	return nil
}

func (m *mockIndex) FindDuplicate(_ context.Context, _, _ string) (*store.SnapshotEntry, error) {
	return m.findDuplicateResult, m.findDuplicateErr
}

func (m *mockIndex) UpsertSnapshot(_ context.Context, _ string, _ store.SnapshotEntry) error {
	return m.upsertSnapshotErr
}

func (m *mockIndex) ReconcileNamespace(_ context.Context, _ string, _ []store.StorageFile) error {
	return m.reconcileErr
}

func (m *mockIndex) GetScoutRegistration(_ context.Context, _, _ string) (*store.ScoutRegistration, error) {
	return m.getScoutRegistrationResult, m.getScoutRegistrationErr
}

func (m *mockIndex) UpsertScoutRegistration(_ context.Context, _ *store.ScoutRegistration) error {
	return m.upsertScoutRegistrationErr
}
