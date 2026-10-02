package ingest

import (
	"context"
	"errors"

	"github.com/3to1go/central/internal/store"
)

// mockIndex is a configurable in-memory implementation of snapshotIndexer.
type mockIndex struct {
	findDuplicateResult *store.SnapshotEntry
	findDuplicateErr    error

	upsertSnapshotErr error

	reconcileErr error

	getEdgeRegistrationResult *store.EdgeRegistration
	getEdgeRegistrationErr    error

	upsertEdgeRegistrationErr error

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

func (m *mockIndex) GetEdgeRegistration(_ context.Context, _, _ string) (*store.EdgeRegistration, error) {
	return m.getEdgeRegistrationResult, m.getEdgeRegistrationErr
}

func (m *mockIndex) UpsertEdgeRegistration(_ context.Context, _ *store.EdgeRegistration) error {
	return m.upsertEdgeRegistrationErr
}

// errIndex returns an error for every call.
func errIndex() *mockIndex {
	return &mockIndex{
		findDuplicateErr:          errors.New("db error"),
		upsertSnapshotErr:         errors.New("db error"),
		reconcileErr:              errors.New("db error"),
		getEdgeRegistrationErr:    errors.New("db error"),
		upsertEdgeRegistrationErr: errors.New("db error"),
	}
}
