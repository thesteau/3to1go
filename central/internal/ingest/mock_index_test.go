package ingest

import (
	"context"
	"errors"

	"github.com/relay/central/internal/store"
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
