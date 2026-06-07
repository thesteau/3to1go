package runner

import (
	"os"

	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/identity"
	"github.com/3to1go/edge/internal/schedule"
	"github.com/3to1go/edge/internal/services/directories"
)

// circuitSnapshotter provides upload circuit breaker status.
// *upload.CircuitBreaker satisfies it.
type circuitSnapshotter interface {
	Snapshot() map[string]any
}

// dirLister lists configured backup directories.
// *directories.DirectoryService satisfies it.
type dirLister interface {
	ListDirectories() ([]directories.DirectoryEntry, error)
}

// BuildStatusResponse returns the full status payload for the /api/status endpoint.
func BuildStatusResponse(settings *config.Settings, keyFingerprint string, circuit circuitSnapshotter) map[string]any {
	instID := identity.LoadOrCreate(config.InstallationIDPath())
	payload := config.SettingsToPayload(settings)
	return map[string]any{
		"edge_id":                    settings.EdgeID,
		"edge_instance_id":           instID,
		"encryption_key_fingerprint": keyFingerprint,
		"scan_root":                  scanDir(settings.ScanRoot),
		"central_url":                settings.CentralURL,
		"advertised_url":             settings.AdvertisedURL,
		"cron_schedule":              settings.CronSchedule,
		"minimum_cycle_gap_minutes":  schedule.MinimumScheduleMinutes,
		"settings_database":          config.AppDatabasePath(),
		"settings":                   payload,
		"settings_status": map[string]any{
			"edge_credential_configured": settings.EdgeCredential != "",
		},
		"upload_circuit": circuit.Snapshot(),
	}
}

// Returns the user-configured SCAN_DIR value rather than the container-internal path.
func scanDir(fallback string) string {
	if v := os.Getenv("SCAN_DIR"); v != "" {
		return v
	}
	return fallback
}

// BuildDirectoryResponse returns the directory list payload for /api/directories.
func BuildDirectoryResponse(settings *config.Settings, dirService dirLister) map[string]any {
	dirs, err := dirService.ListDirectories()
	if err != nil {
		dirs = nil
	}
	return map[string]any{
		"scan_root":   scanDir(settings.ScanRoot),
		"directories": dirs,
	}
}
