package overview

import (
	"context"
	"testing"

	"github.com/3to1go/station/internal/config"
	"github.com/3to1go/station/internal/storage"
	"github.com/3to1go/station/internal/store"
)

func TestStatusString_OK(t *testing.T) {
	backend := storage.NewLocalBackend(t.TempDir())
	got := statusString(backend)
	if got != "ok" {
		t.Errorf("got %q, want ok", got)
	}
}

func TestStatusString_Degraded(t *testing.T) {
	backend := storage.NewLocalBackend("/nonexistent/backup/root/xyz")
	got := statusString(backend)
	if got != "degraded" {
		t.Errorf("got %q, want degraded", got)
	}
}

// mockSnapshotIndexer implements SnapshotIndexer for BuildOverview tests.
type mockSnapshotIndexer struct {
	registrations []store.ScoutRegistration
	namespaces    []store.NamespaceEntry
	regErr        error
	nsErr         error
}

func (m *mockSnapshotIndexer) ListScoutRegistrations(_ context.Context, _ *string) ([]store.ScoutRegistration, error) {
	return m.registrations, m.regErr
}

func (m *mockSnapshotIndexer) ListNamespaces(_ context.Context) ([]store.NamespaceEntry, error) {
	return m.namespaces, m.nsErr
}

func TestBuildOverview_DiskInfoIncluded(t *testing.T) {
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{}
	s := &config.Settings{BackupRoot: t.TempDir()}

	result, err := BuildOverview(context.Background(), s, backend, idx)
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	for _, key := range []string{"disk_total_bytes", "disk_used_bytes", "disk_free_bytes"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q in overview result", key)
		}
	}
	// Free must be non-negative; total must be >= free
	free, _ := result["disk_free_bytes"].(int64)
	total, _ := result["disk_total_bytes"].(int64)
	if free < 0 {
		t.Errorf("disk_free_bytes = %d, must be >= 0", free)
	}
	if total < free {
		t.Errorf("disk_total_bytes (%d) < disk_free_bytes (%d)", total, free)
	}
}

func TestBuildOverview_Empty(t *testing.T) {
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{}
	s := &config.Settings{BackupRoot: t.TempDir(), RetentionKeepLast: 5}

	result, err := BuildOverview(context.Background(), s, backend, idx)
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	scouts, ok := result["scouts"].([]any)
	if !ok {
		t.Fatalf("scouts not []interface{}: %T", result["scouts"])
	}
	if len(scouts) != 0 {
		t.Errorf("expected 0 scouts, got %d", len(scouts))
	}
	if result["status"] != "ok" {
		t.Errorf("status = %v, want ok", result["status"])
	}
}

func TestBuildOverview_WithRegistrations(t *testing.T) {
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{
		registrations: []store.ScoutRegistration{
			{ScoutID: "scout1", ScoutInstanceID: "inst1", FirstSeenAt: "2024-01-01T00:00:00Z", LastSeenAt: "2024-01-01T01:00:00Z"},
		},
		namespaces: []store.NamespaceEntry{
			{ScoutID: "scout1", ScoutInstanceID: "inst1", Jobs: []store.SnapshotJob{{JobName: "backup"}}},
		},
	}
	s := &config.Settings{BackupRoot: t.TempDir()}

	result, err := BuildOverview(context.Background(), s, backend, idx)
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	scouts := result["scouts"].([]any)
	if len(scouts) != 1 {
		t.Fatalf("expected 1 scout, got %d", len(scouts))
	}
	scout := scouts[0].(map[string]any)
	if scout["scout_id"] != "scout1" {
		t.Errorf("scout_id = %v, want scout1", scout["scout_id"])
	}
	instances := scout["instances"].([]any)
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
}

func TestBuildOverview_RegistrationError(t *testing.T) {
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{regErr: context.DeadlineExceeded}
	s := &config.Settings{}

	_, err := BuildOverview(context.Background(), s, backend, idx)
	if err == nil {
		t.Fatal("expected error when ListScoutRegistrations fails")
	}
}

func TestBuildOverview_NamespaceError(t *testing.T) {
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{nsErr: context.DeadlineExceeded}
	s := &config.Settings{}

	_, err := BuildOverview(context.Background(), s, backend, idx)
	if err == nil {
		t.Fatal("expected error when ListNamespaces fails")
	}
}

func TestBuildOverview_NamespaceWithNewScout(t *testing.T) {
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{
		// No registrations — scout only appears via namespace
		namespaces: []store.NamespaceEntry{
			{ScoutID: "scout2", ScoutInstanceID: "inst2", Jobs: []store.SnapshotJob{{JobName: "job1"}}},
		},
	}
	s := &config.Settings{}

	result, err := BuildOverview(context.Background(), s, backend, idx)
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	scouts := result["scouts"].([]any)
	if len(scouts) != 1 {
		t.Fatalf("expected 1 scout from namespace-only, got %d", len(scouts))
	}
	scout := scouts[0].(map[string]any)
	if scout["scout_id"] != "scout2" {
		t.Errorf("scout_id = %v, want scout2", scout["scout_id"])
	}
}

func TestBuildOverview_MultipleRegistrationsSameScout(t *testing.T) {
	credHash := "abc123"
	advURL := "https://scout.example.com"
	keyFP := "fp123"
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{
		registrations: []store.ScoutRegistration{
			{ScoutID: "scout1", ScoutInstanceID: "inst1", FirstSeenAt: "2024-01-01T00:00:00Z", LastSeenAt: "2024-01-01T01:00:00Z"},
			// Second registration for same inst updates fields
			{ScoutID: "scout1", ScoutInstanceID: "inst1", AdvertisedURL: &advURL, EncryptionKeyFingerprint: &keyFP, CredentialHash: &credHash},
		},
	}
	s := &config.Settings{}

	result, err := BuildOverview(context.Background(), s, backend, idx)
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	scouts := result["scouts"].([]any)
	if len(scouts) != 1 {
		t.Fatalf("expected 1 scout, got %d", len(scouts))
	}
	scout := scouts[0].(map[string]any)
	instances := scout["instances"].([]any)
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	inst := instances[0].(map[string]any)
	if inst["credential_configured"] != true {
		t.Errorf("credential_configured should be true")
	}
}
