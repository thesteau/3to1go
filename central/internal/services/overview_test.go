package services

import (
	"context"
	"testing"

	"github.com/relay/central/internal/config"
	"github.com/relay/central/internal/storage"
	"github.com/relay/central/internal/store"
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
	registrations []store.EdgeRegistration
	namespaces    []store.NamespaceEntry
	regErr        error
	nsErr         error
}

func (m *mockSnapshotIndexer) ListEdgeRegistrations(_ context.Context, _ *string) ([]store.EdgeRegistration, error) {
	return m.registrations, m.regErr
}

func (m *mockSnapshotIndexer) ListNamespaces(_ context.Context) ([]store.NamespaceEntry, error) {
	return m.namespaces, m.nsErr
}

func TestBuildOverview_Empty(t *testing.T) {
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{}
	s := &config.Settings{BackupRoot: t.TempDir(), RetentionKeepLast: 5}

	result, err := BuildOverview(context.Background(), s, backend, idx)
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	edges, ok := result["edges"].([]interface{})
	if !ok {
		t.Fatalf("edges not []interface{}: %T", result["edges"])
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges, got %d", len(edges))
	}
	if result["status"] != "ok" {
		t.Errorf("status = %v, want ok", result["status"])
	}
}

func TestBuildOverview_WithRegistrations(t *testing.T) {
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{
		registrations: []store.EdgeRegistration{
			{EdgeID: "edge1", EdgeInstanceID: "inst1", FirstSeenAt: "2024-01-01T00:00:00Z", LastSeenAt: "2024-01-01T01:00:00Z"},
		},
		namespaces: []store.NamespaceEntry{
			{EdgeID: "edge1", EdgeInstanceID: "inst1", Jobs: []store.SnapshotJob{{JobName: "backup"}}},
		},
	}
	s := &config.Settings{BackupRoot: t.TempDir()}

	result, err := BuildOverview(context.Background(), s, backend, idx)
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	edges := result["edges"].([]interface{})
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	edge := edges[0].(map[string]interface{})
	if edge["edge_id"] != "edge1" {
		t.Errorf("edge_id = %v, want edge1", edge["edge_id"])
	}
	instances := edge["instances"].([]interface{})
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
		t.Fatal("expected error when ListEdgeRegistrations fails")
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

func TestBuildOverview_NamespaceWithNewEdge(t *testing.T) {
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{
		// No registrations — edge only appears via namespace
		namespaces: []store.NamespaceEntry{
			{EdgeID: "edge2", EdgeInstanceID: "inst2", Jobs: []store.SnapshotJob{{JobName: "job1"}}},
		},
	}
	s := &config.Settings{}

	result, err := BuildOverview(context.Background(), s, backend, idx)
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	edges := result["edges"].([]interface{})
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge from namespace-only, got %d", len(edges))
	}
	edge := edges[0].(map[string]interface{})
	if edge["edge_id"] != "edge2" {
		t.Errorf("edge_id = %v, want edge2", edge["edge_id"])
	}
}

func TestBuildOverview_MultipleRegistrationsSameEdge(t *testing.T) {
	credHash := "abc123"
	advURL := "https://edge.example.com"
	keyFP := "fp123"
	backend := storage.NewLocalBackend(t.TempDir())
	idx := &mockSnapshotIndexer{
		registrations: []store.EdgeRegistration{
			{EdgeID: "edge1", EdgeInstanceID: "inst1", FirstSeenAt: "2024-01-01T00:00:00Z", LastSeenAt: "2024-01-01T01:00:00Z"},
			// Second registration for same inst updates fields
			{EdgeID: "edge1", EdgeInstanceID: "inst1", AdvertisedURL: &advURL, EncryptionKeyFingerprint: &keyFP, CredentialHash: &credHash},
		},
	}
	s := &config.Settings{}

	result, err := BuildOverview(context.Background(), s, backend, idx)
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	edges := result["edges"].([]interface{})
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	edge := edges[0].(map[string]interface{})
	instances := edge["instances"].([]interface{})
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	inst := instances[0].(map[string]interface{})
	if inst["credential_configured"] != true {
		t.Errorf("credential_configured should be true")
	}
}
