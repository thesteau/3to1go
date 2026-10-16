package config

import (
	"encoding/json"
	"testing"
)

func TestMetricsSettingDefaultsAndPersistence(t *testing.T) {
	for _, payload := range []*SettingsPayload{nil, {}, {MetricsEnabled: true}, {MetricsEnabled: false}} {
		want := payload != nil && payload.MetricsEnabled
		s, err := BuildSettings(payload)
		if err != nil {
			t.Fatal(err)
		}
		if s.MetricsEnabled != want {
			t.Fatalf("enabled = %v, want %v", s.MetricsEnabled, want)
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
		if err != nil || restored.MetricsEnabled != want {
			t.Fatalf("restored = %+v, error %v", restored, err)
		}
	}
}
