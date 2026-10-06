package overview

import (
	"context"
	"os"

	"github.com/3to1go/station/internal/config"
	"github.com/3to1go/station/internal/store"
)

// SnapshotIndexer is the subset of store.SnapshotIndex used by BuildOverview.
type SnapshotIndexer interface {
	ListScoutRegistrations(ctx context.Context, scoutIDFilter *string) ([]store.ScoutRegistration, error)
	ListNamespaces(ctx context.Context) ([]store.NamespaceEntry, error)
}

// StorageProbe is the health-check and disk-info contract used by BuildOverview.
type StorageProbe interface {
	Healthcheck() bool
	DiskInfo() (total, used, free int64)
}

// BuildOverview assembles the dashboard data.
func BuildOverview(ctx context.Context, s *config.Settings, backend StorageProbe, idx SnapshotIndexer) (map[string]any, error) {
	data, err := BuildSnapshotOverview(ctx, s, idx)
	if err != nil {
		return nil, err
	}
	for key, value := range BuildStorageOverview(backend) {
		data[key] = value
	}
	return data, nil
}

// BuildSnapshotOverview does not wait for storage health or disk probes.
func BuildSnapshotOverview(ctx context.Context, s *config.Settings, idx SnapshotIndexer) (map[string]any, error) {
	registrations, err := idx.ListScoutRegistrations(ctx, nil)
	if err != nil {
		return nil, err
	}
	namespaces, err := idx.ListNamespaces(ctx)
	if err != nil {
		return nil, err
	}

	type scoutEntry struct {
		ScoutID   string `json:"scout_id"`
		Instances []any  `json:"instances"`
	}

	scoutMap := map[string]*scoutEntry{}
	var scouts []*scoutEntry

	type instKey struct{ scoutID, instID string }
	instMap := map[instKey]map[string]any{}

	for _, reg := range registrations {
		scout := scoutMap[reg.ScoutID]
		if scout == nil {
			scout = &scoutEntry{ScoutID: reg.ScoutID}
			scoutMap[reg.ScoutID] = scout
			scouts = append(scouts, scout)
		}
		key := instKey{reg.ScoutID, reg.ScoutInstanceID}
		inst := instMap[key]
		if inst == nil {
			inst = map[string]any{
				"scout_instance_id":          reg.ScoutInstanceID,
				"instance_label":             reg.ScoutInstanceID,
				"advertised_url":             reg.AdvertisedURL,
				"encryption_key_fingerprint": reg.EncryptionKeyFingerprint,
				"first_seen_at":              reg.FirstSeenAt,
				"last_seen_at":               reg.LastSeenAt,
				"credential_configured":      reg.CredentialHash != nil && *reg.CredentialHash != "",
				"last_upload_tls":            reg.LastUploadTLS,
				"jobs":                       []any{},
			}
			instMap[key] = inst
			scout.Instances = append(scout.Instances, inst)
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
		scout := scoutMap[ns.ScoutID]
		if scout == nil {
			scout = &scoutEntry{ScoutID: ns.ScoutID}
			scoutMap[ns.ScoutID] = scout
			scouts = append(scouts, scout)
		}
		key := instKey{ns.ScoutID, ns.ScoutInstanceID}
		inst := instMap[key]
		if inst == nil {
			inst = map[string]any{
				"scout_instance_id":          ns.ScoutInstanceID,
				"instance_label":             ns.ScoutInstanceID,
				"advertised_url":             nil,
				"encryption_key_fingerprint": nil,
				"first_seen_at":              nil,
				"last_seen_at":               nil,
				"credential_configured":      false,
				"jobs":                       []any{},
			}
			instMap[key] = inst
			scout.Instances = append(scout.Instances, inst)
		}
		inst["jobs"] = ns.Jobs
	}

	// Convert scout slice to []interface{}
	scoutsOut := make([]any, len(scouts))
	for i, e := range scouts {
		scoutsOut[i] = map[string]any{
			"scout_id":  e.ScoutID,
			"instances": e.Instances,
		}
	}

	return map[string]any{
		"backup_dir":          backupDir(s.BackupRoot),
		"retention_keep_last": s.RetentionKeepLast,
		"settings":            config.SettingsToPayload(s),
		"scouts":              scoutsOut,
	}, nil
}

func BuildStorageOverview(backend StorageProbe) map[string]any {
	total, used, free := backend.DiskInfo()
	return map[string]any{"status": statusString(backend), "disk_total_bytes": total, "disk_used_bytes": used, "disk_free_bytes": free}
}

// Returns the user-configured BACKUP_DIR value rather than the container-internal path.
func backupDir(fallback string) string {
	if v := os.Getenv("BACKUP_DIR"); v != "" {
		return v
	}
	return fallback
}

func statusString(backend StorageProbe) string {
	if backend.Healthcheck() {
		return "ok"
	}
	return "degraded"
}
