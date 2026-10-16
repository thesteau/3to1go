package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/3to1go/scout/internal/config"
	"github.com/3to1go/scout/internal/services/directories"
	"github.com/3to1go/scout/internal/services/state"
)

func TestMetricsDisabledAndNoSessionLookup(t *testing.T) {
	runner := defaultRunner()
	runner.metricsErr = errors.New("must not collect while disabled")
	// A nil user store would panic if a scrape tried to look up a UI session.
	app := newTestAppFull(nil, runner, defaultScheduler())
	handler := app.Handler()
	if rr := doRequest(handler, "GET", "/metrics", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("default: %d %s", rr.Code, rr.Body.String())
	}
	runner.settings.MetricsEnabled = true
	if rr := doRequest(handler, "GET", "/metrics", nil); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "must not") {
		t.Fatalf("collection error: %d %s", rr.Code, rr.Body.String())
	}
	runner.metricsErr = nil
	if rr := doRequest(handler, "GET", "/metrics", nil); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "three_to_one_go_scout_jobs 0") {
		t.Fatalf("enabled: %d %s", rr.Code, rr.Body.String())
	}
	runner.settings.MetricsEnabled = false
	if rr := doRequest(handler, "GET", "/metrics", nil); rr.Code != http.StatusNotFound {
		t.Fatalf("disabled again: %d", rr.Code)
	}
}

func TestMetricsSettingsRequireAdmin(t *testing.T) {
	for _, tc := range []struct {
		name string
		user *mockUserStore
		want int
	}{
		{"signed out", &mockUserStore{}, http.StatusUnauthorized},
		{"non-admin", regularUserStore(), http.StatusForbidden},
		{"admin", adminUserStore(), http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := defaultRunner()
			app := newTestAppFull(tc.user, runner, defaultScheduler())
			handler := app.Handler()
			payload := config.SettingsToPayload(runner.settings)
			payload.MetricsEnabled = true
			rr := doAuthRequest(handler, "POST", "/api/settings", payload)
			if rr.Code != tc.want || runner.settings.MetricsEnabled != (tc.want == http.StatusOK) {
				t.Fatalf("save: %d, enabled %v, body %s", rr.Code, runner.settings.MetricsEnabled, rr.Body.String())
			}
			if tc.want == http.StatusOK {
				if rr := doRequest(handler, "GET", "/metrics", nil); rr.Code != http.StatusOK {
					t.Fatalf("admin enable did not apply immediately: %d", rr.Code)
				}
				payload.MetricsEnabled = false
				if rr := doAuthRequest(handler, "POST", "/api/settings", payload); rr.Code != http.StatusOK {
					t.Fatal(rr.Body.String())
				}
				if rr := doRequest(handler, "GET", "/metrics", nil); rr.Code != http.StatusNotFound {
					t.Fatalf("admin disable did not apply immediately: %d", rr.Code)
				}
			}
		})
	}
}

func TestMetricsJobStateAndPrivacy(t *testing.T) {
	runner := defaultRunner()
	runner.settings.MetricsEnabled = true
	runner.settings.ScoutCredential = "secret-station-token"
	runner.settings.ScanRoot = "/private/scan/root"
	size := int64(2048)
	runner.metricsJobs = []directories.DirectoryEntry{
		{RelativePath: "documents", AbsolutePath: "/private/scan/root/documents", State: state.JobState{
			LastSuccessfulUpload: "2026-10-15T00:00:00Z", PendingArchive: "/private/spool/archive.tar.zst",
			PendingArchiveSize: &size, LastBackupSizeBytes: &size, UploadAttemptCount: 3,
			ManualInterventionRequired: true, LastErrorDetail: "private error detail",
		}},
		{RelativePath: "new-job", ConfigError: "private config detail"},
	}
	sched := defaultScheduler()
	sched.snapshot = map[string]any{"state": "running"}
	app := newTestAppFull(nil, runner, sched)
	handler := app.Handler()
	rr := doRequest(handler, "GET", "/metrics", nil)
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	body := rr.Body.String()
	for _, line := range []string{
		"three_to_one_go_scout_jobs 2", "three_to_one_go_scout_cycle_running 1",
		`three_to_one_go_scout_job_pending_upload{job_path="documents"} 1`,
		`three_to_one_go_scout_job_upload_attempts{job_path="documents"} 3`,
		`three_to_one_go_scout_job_manual_intervention_required{job_path="documents"} 1`,
		`three_to_one_go_scout_job_last_backup_size_bytes{job_path="documents"} 2048`,
		`three_to_one_go_scout_job_last_successful_upload_timestamp_seconds{job_path="new-job"} 0`,
		`three_to_one_go_scout_job_config_valid{job_path="new-job"} 0`,
	} {
		if !strings.Contains(body, line) {
			t.Errorf("missing %s", line)
		}
	}
	for _, secret := range []string{"secret-station-token", "/private", "private error detail", "private config detail", "base64key", "abc123"} {
		if strings.Contains(body, secret) {
			t.Errorf("scrape leaked %q", secret)
		}
	}
	runner.metricsJobs = nil
	if rr := doRequest(handler, "GET", "/metrics", nil); strings.Contains(rr.Body.String(), `job_path="documents"`) {
		t.Fatal("deleted job still exported")
	}
}
