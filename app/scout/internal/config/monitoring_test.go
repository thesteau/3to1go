package config

import (
	"encoding/json"
	"testing"
)

func TestMonitoringTokenDefaultsForNewAndExistingInstalls(t *testing.T) {
	on, off := true, false
	for _, tc := range []struct {
		name    string
		payload *SettingsPayload
		want    bool
	}{
		{"new install has no saved settings", nil, true},
		{"existing install saved before this field", &SettingsPayload{MetricsEnabled: true}, false},
		{"turned on", &SettingsPayload{MonitoringTokenRequired: &on}, true},
		{"turned off", &SettingsPayload{MonitoringTokenRequired: &off}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := BuildSettings(tc.payload)
			if err != nil {
				t.Fatal(err)
			}
			if s.MonitoringTokenRequired != tc.want {
				t.Fatalf("required = %v, want %v", s.MonitoringTokenRequired, tc.want)
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
			if err != nil || restored.MonitoringTokenRequired != tc.want {
				t.Fatalf("restored = %+v, error %v", restored, err)
			}
		})
	}
}

func TestOlderSavedSettingsLeaveMonitoringTokenOff(t *testing.T) {
	var stored SettingsPayload
	if err := json.Unmarshal([]byte(`{"metrics_enabled":true,"scout_id":"scout-01"}`), &stored); err != nil {
		t.Fatal(err)
	}
	s, err := BuildSettings(&stored)
	if err != nil {
		t.Fatal(err)
	}
	if s.MonitoringTokenRequired || !s.MetricsEnabled {
		t.Fatalf("upgrade changed access: %+v", s)
	}
}
