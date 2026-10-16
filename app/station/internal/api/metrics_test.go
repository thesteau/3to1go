package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/3to1go/station/internal/config"
	"github.com/3to1go/station/internal/store"
)

func scrapeMetrics(handler http.Handler) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/metrics", nil))
	return rr
}

func TestMetricsDisabledAndNoSessionLookup(t *testing.T) {
	idx := &mockSnapIndex{listNSErr: errors.New("private database details")}
	app := newTestApp(t, nil, nil, nil, idx)
	// A nil user store would panic if a scrape tried to look up a UI session.
	app.userStore = nil
	handler := app.Handler()
	if rr := scrapeMetrics(handler); rr.Code != http.StatusNotFound {
		t.Fatalf("default: %d %s", rr.Code, rr.Body.String())
	}
	s := *app.Settings()
	s.MetricsEnabled = true
	app.ApplySettings(&s)
	if rr := scrapeMetrics(handler); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "private") {
		t.Fatalf("collection error: %d %s", rr.Code, rr.Body.String())
	}
	idx.listNSErr = nil
	idx.listRegsErr = errors.New("private registration details")
	if rr := scrapeMetrics(handler); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("registration error: %d %s", rr.Code, rr.Body.String())
	}
	idx.listRegsErr = nil
	if rr := scrapeMetrics(handler); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "three_to_one_go_station_snapshots 0") {
		t.Fatalf("enabled: %d %s", rr.Code, rr.Body.String())
	}
	s.MetricsEnabled = false
	app.ApplySettings(&s)
	if rr := scrapeMetrics(handler); rr.Code != http.StatusNotFound {
		t.Fatalf("disabled again: %d", rr.Code)
	}
}

func TestMetricsSettingsRequireAdmin(t *testing.T) {
	for _, tc := range []struct {
		name string
		user *store.User
		want int
	}{
		{"signed out", nil, http.StatusUnauthorized},
		{"non-admin", regularUser(), http.StatusForbidden},
		{"admin", adminUser(), http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t, &mockUserStore{sessionUser: tc.user}, nil, nil, nil)
			t.Cleanup(app.Shutdown)
			handler := app.Handler()
			payload := config.SettingsToPayload(app.Settings())
			payload.MetricsEnabled = true
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, jsonReq("POST", "/api/settings", payload))
			if rr.Code != tc.want || app.Settings().MetricsEnabled != (tc.want == http.StatusOK) {
				t.Fatalf("save: %d, enabled %v, body %s", rr.Code, app.Settings().MetricsEnabled, rr.Body.String())
			}
			if tc.want == http.StatusOK {
				if rr := scrapeMetrics(handler); rr.Code != http.StatusOK {
					t.Fatalf("admin enable did not apply immediately: %d", rr.Code)
				}
				payload.MetricsEnabled = false
				rr = httptest.NewRecorder()
				handler.ServeHTTP(rr, jsonReq("POST", "/api/settings", payload))
				if rr.Code != http.StatusOK {
					t.Fatal(rr.Body.String())
				}
				if rr := scrapeMetrics(handler); rr.Code != http.StatusNotFound {
					t.Fatalf("admin disable did not apply immediately: %d", rr.Code)
				}
			}
		})
	}
}

func TestMetricsSnapshotStateAndPrivacy(t *testing.T) {
	secret := "private-token-key-and-url"
	idx := &mockSnapIndex{
		listRegs: []store.ScoutRegistration{{ScoutID: "scout", ScoutInstanceID: "instance", LastSeenAt: "2026-10-15T00:00:00Z", CredentialHash: &secret, AdvertisedURL: &secret, EncryptionKeyFingerprint: &secret}},
		listNS: []store.NamespaceEntry{{ScoutID: "scout", ScoutInstanceID: "instance", Jobs: []store.SnapshotJob{
			{JobName: "documents", SnapshotCount: 2, Snapshots: []store.SnapshotMini{{Name: "private-filename", SizeBytes: 100, Mtime: 200}, {SizeBytes: 250, Mtime: 300}}},
			{JobName: "photos", SnapshotCount: 1, Snapshots: []store.SnapshotMini{{SizeBytes: 500, Mtime: 400}}},
		}}},
	}
	app := newTestApp(t, nil, nil, nil, idx)
	app.userStore = nil
	s := *app.Settings()
	s.MetricsEnabled = true
	app.ApplySettings(&s)
	handler := app.Handler()
	rr := scrapeMetrics(handler)
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	body := rr.Body.String()
	for _, line := range []string{
		"three_to_one_go_station_jobs 2", "three_to_one_go_station_snapshots 3", "three_to_one_go_station_stored_bytes 850",
		`three_to_one_go_station_job_snapshots{job_name="documents",scout_id="scout",scout_instance_id="instance"} 2`,
		`three_to_one_go_station_job_stored_bytes{job_name="documents",scout_id="scout",scout_instance_id="instance"} 350`,
		`three_to_one_go_station_job_latest_snapshot_timestamp_seconds{job_name="documents",scout_id="scout",scout_instance_id="instance"} 300`,
	} {
		if !strings.Contains(body, line) {
			t.Errorf("missing %s", line)
		}
	}
	for _, private := range []string{secret, "private-filename", s.BackupRoot, s.IssuerKeyPath, s.IndexDatabaseURL} {
		if private != "" && strings.Contains(body, private) {
			t.Errorf("scrape leaked %q", private)
		}
	}
	idx.listNS = nil
	if rr := scrapeMetrics(handler); strings.Contains(rr.Body.String(), `job_name="documents"`) {
		t.Fatal("deleted job still exported")
	}
}
