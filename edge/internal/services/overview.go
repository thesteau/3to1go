package services

import (
	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/identity"
	"github.com/3to1go/edge/internal/schedule"
)

// BuildStatusResponse returns the full status payload for the /api/status endpoint.
func BuildStatusResponse(settings *config.Settings, keyFingerprint string, circuit circuitSnapshotter) map[string]interface{} {
	instID := identity.LoadOrCreate(config.InstallationIDPath())
	payload := config.SettingsToPayload(settings)
	return map[string]interface{}{
		"edge_id":                       settings.EdgeID,
		"edge_instance_id":              instID,
		"encryption_key_fingerprint":    keyFingerprint,
		"scan_root":                     settings.ScanRoot,
		"central_url":                   settings.CentralURL,
		"advertised_url":                settings.AdvertisedURL,
		"cron_schedule":                 settings.CronSchedule,
		"minimum_cycle_gap_minutes":     schedule.MinimumScheduleMinutes,
		"settings_database":             config.AppDatabasePath(),
		"settings":                      payload,
		"settings_status": map[string]interface{}{
			"edge_credential_configured": settings.EdgeCredential != "",
		},
		"upload_circuit": circuit.Snapshot(),
	}
}

// BuildDirectoryResponse returns the directory list payload for /api/directories.
func BuildDirectoryResponse(settings *config.Settings, dirService *DirectoryService) map[string]interface{} {
	dirs, err := dirService.ListDirectories()
	if err != nil {
		dirs = nil
	}
	return map[string]interface{}{
		"scan_root":   settings.ScanRoot,
		"directories": dirs,
	}
}
