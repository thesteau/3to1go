package config

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestBackupReportSettingDefaultsAndPersistence(t *testing.T) {
	t.Setenv("ISSUER_KEY_FILE", filepath.Join(t.TempDir(), "issuer.key"))
	t.Setenv("INDEX_DATABASE_URL", "postgresql://user:password@localhost/test")
	for _, payload := range []*SettingsPayload{nil, {}, {BackupReportEnabled: true}, {BackupReportEnabled: false}} {
		want := payload != nil && payload.BackupReportEnabled
		s, err := BuildSettings(payload)
		if err != nil {
			t.Fatal(err)
		}
		if s.BackupReportEnabled != want {
			t.Fatalf("enabled = %v, want %v", s.BackupReportEnabled, want)
		}
		raw, err := json.Marshal(SettingsToPayload(s))
		if err != nil {
			t.Fatal(err)
		}
		var stored SettingsPayload
		if err := json.Unmarshal(raw, &stored); err != nil {
			t.Fatal(err)
		}
		restored, err := BuildSettings(&stored)
		if err != nil || restored.BackupReportEnabled != want {
			t.Fatalf("restored = %+v, error %v", restored, err)
		}
	}
}
