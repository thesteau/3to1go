package services

import (
	"context"

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/storage"
	"github.com/3to1go/central/internal/store"
)

// SnapshotIndexer is the subset of store.SnapshotIndex used by BuildOverview.
type SnapshotIndexer interface {
	ListEdgeRegistrations(ctx context.Context, edgeIDFilter *string) ([]store.EdgeRegistration, error)
	ListNamespaces(ctx context.Context) ([]store.NamespaceEntry, error)
}

// BuildOverview assembles the dashboard data.
func BuildOverview(ctx context.Context, s *config.Settings, backend *storage.LocalBackend, idx SnapshotIndexer) (map[string]any, error) {
	registrations, err := idx.ListEdgeRegistrations(ctx, nil)
	if err != nil {
		return nil, err
	}
	namespaces, err := idx.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	type edgeEntry struct {
		EdgeID    string `json:"edge_id"`
		Instances []any  `json:"instances"`
	}

	edgeMap := map[string]*edgeEntry{}
	var edges []*edgeEntry

	type instKey struct{ edgeID, instID string }
	instMap := map[instKey]map[string]any{}

	for _, reg := range registrations {
		edge := edgeMap[reg.EdgeID]
		if edge == nil {
			edge = &edgeEntry{EdgeID: reg.EdgeID}
			edgeMap[reg.EdgeID] = edge
			edges = append(edges, edge)
		}
		key := instKey{reg.EdgeID, reg.EdgeInstanceID}
		inst := instMap[key]
		if inst == nil {
			inst = map[string]any{
				"edge_instance_id":           reg.EdgeInstanceID,
				"instance_label":             reg.EdgeInstanceID,
				"advertised_url":             reg.AdvertisedURL,
				"encryption_key_fingerprint": reg.EncryptionKeyFingerprint,
				"first_seen_at":              reg.FirstSeenAt,
				"last_seen_at":               reg.LastSeenAt,
				"credential_configured":      reg.CredentialHash != nil && *reg.CredentialHash != "",
				"jobs":                       []any{},
			}
			instMap[key] = inst
			edge.Instances = append(edge.Instances, inst)
		} else {
			if reg.AdvertisedURL != nil {
				inst["advertised_url"] = reg.AdvertisedURL
			}
			if reg.EncryptionKeyFingerprint != nil {
				inst["encryption_key_fingerprint"] = reg.EncryptionKeyFingerprint
			}
			if reg.CredentialHash != nil && *reg.CredentialHash != "" {
				inst["credential_configured"] = true
			}
		}
	}

	for _, ns := range namespaces {
		edge := edgeMap[ns.EdgeID]
		if edge == nil {
			edge = &edgeEntry{EdgeID: ns.EdgeID}
			edgeMap[ns.EdgeID] = edge
			edges = append(edges, edge)
		}
		key := instKey{ns.EdgeID, ns.EdgeInstanceID}
		inst := instMap[key]
		if inst == nil {
			inst = map[string]any{
				"edge_instance_id":           ns.EdgeInstanceID,
				"instance_label":             ns.EdgeInstanceID,
				"advertised_url":             nil,
				"encryption_key_fingerprint": nil,
				"first_seen_at":              nil,
				"last_seen_at":               nil,
				"credential_configured":      false,
				"jobs":                       []any{},
			}
			instMap[key] = inst
			edge.Instances = append(edge.Instances, inst)
		}
		inst["jobs"] = ns.Jobs
	}

	// Convert edge slice to []interface{}
	edgesOut := make([]any, len(edges))
	for i, e := range edges {
		edgesOut[i] = map[string]any{
			"edge_id":   e.EdgeID,
			"instances": e.Instances,
		}
	}

	return map[string]any{
		"status":              statusString(backend),
		"backup_dir":          s.BackupRoot,
		"retention_keep_last": s.RetentionKeepLast,
		"settings":            config.SettingsToPayload(s),
		"edges":               edgesOut,
	}, nil
}

func statusString(backend *storage.LocalBackend) string {
	if backend.Healthcheck() {
		return "ok"
	}
	return "degraded"
}
