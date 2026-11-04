package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/3to1go/station/internal/config"
	"github.com/3to1go/station/internal/store"
)

func getBackupReport(handler http.Handler) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest("GET", "/report", nil))
	return rr
}

func enableBackupReport(app *App) {
	s := *app.Settings()
	s.BackupReportEnabled = true
	app.ApplySettings(&s)
}

func TestBackupReportOptInAndNoSessionLookup(t *testing.T) {
	idx := &mockSnapIndex{listNSErr: errors.New("private database details")}
	app := newTestApp(t, nil, nil, nil, idx)
	// A nil user store would panic if a request tried to look up a UI session.
	app.userStore = nil
	handler := app.Handler()
	if rr := getBackupReport(handler); rr.Code != http.StatusNotFound {
		t.Fatalf("default: %d %s", rr.Code, rr.Body.String())
	}
	enableBackupReport(app)
	if rr := getBackupReport(handler); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "private") {
		t.Fatalf("index error: %d %s", rr.Code, rr.Body.String())
	}
	idx.listNSErr = nil
	idx.listRegsErr = errors.New("private registration details")
	if rr := getBackupReport(handler); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "private") {
		t.Fatalf("registration error: %d %s", rr.Code, rr.Body.String())
	}
	idx.listRegsErr = nil
	rr := getBackupReport(handler)
	if rr.Code != http.StatusOK || strings.TrimSpace(rr.Body.String()) != `{"snapshot_count":0,"stored_bytes":0,"instances":[]}` {
		t.Fatalf("empty: %d %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
	s := *app.Settings()
	s.BackupReportEnabled = false
	app.ApplySettings(&s)
	if rr := getBackupReport(handler); rr.Code != http.StatusNotFound {
		t.Fatalf("disabled again: %d", rr.Code)
	}
}

func TestBackupReportIsSeparateFromMetricsSetting(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, &mockSnapIndex{})
	s := *app.Settings()
	s.MetricsEnabled = true
	app.ApplySettings(&s)
	if rr := getBackupReport(app.Handler()); rr.Code != http.StatusNotFound {
		t.Fatalf("metrics setting exposed the backup report: %d", rr.Code)
	}
}

func TestBackupReportSettingRequiresAdmin(t *testing.T) {
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
			app := newTestApp(t, &mockUserStore{sessionUser: tc.user}, nil, nil, &mockSnapIndex{})
			t.Cleanup(app.Shutdown)
			handler := app.Handler()
			payload := config.SettingsToPayload(app.Settings())
			payload.BackupReportEnabled = true
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, jsonReq("POST", "/api/settings", payload))
			if rr.Code != tc.want {
				t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
			}
			want := http.StatusNotFound
			if tc.want == http.StatusOK {
				want = http.StatusOK
			}
			if rr := getBackupReport(handler); rr.Code != want {
				t.Fatalf("after save: %d, want %d", rr.Code, want)
			}
		})
	}
}

// The whole body is compared, so any extra field, such as a filename or key, fails this test.
func TestBackupReportContentAndPrivacy(t *testing.T) {
	secret := "private-token-key-and-url"
	unix := func(year int, month time.Month, day, hour, minute int) float64 {
		return float64(time.Date(year, month, day, hour, minute, 0, 0, time.UTC).Unix())
	}
	idx := &mockSnapIndex{
		listRegs: []store.ScoutRegistration{
			{ScoutID: "Nas-01", ScoutInstanceID: "inst-a", LastSeenAt: "2026-11-03T11:59:00Z", CredentialHash: &secret, AdvertisedURL: &secret, EncryptionKeyFingerprint: &secret},
			{ScoutID: "new-01", ScoutInstanceID: "inst-c", LastSeenAt: "2026-11-03T13:57:00.5+02:00"},
			{ScoutID: "web-01", ScoutInstanceID: "inst-d", LastSeenAt: "not-a-time"},
		},
		listNS: []store.NamespaceEntry{
			{ScoutID: "Nas-01", ScoutInstanceID: "inst-a", Jobs: []store.SnapshotJob{
				{JobName: "documents", SnapshotCount: 2, Snapshots: []store.SnapshotMini{
					{Name: "private-filename", SizeBytes: 250, Mtime: unix(2026, 11, 3, 2, 0) + 0.5, Unusual: "private-unusual-reason"},
					{Name: "private-filename-2", SizeBytes: 100, Mtime: unix(2026, 11, 2, 2, 0)},
				}},
				{JobName: "photos", SnapshotCount: 1, Snapshots: []store.SnapshotMini{{Name: "private-filename-3", SizeBytes: 500, Mtime: unix(2026, 10, 28, 3, 30)}}},
			}},
			// Snapshots without a registration are still reported.
			{ScoutID: "Old-01", ScoutInstanceID: "inst-b", Jobs: []store.SnapshotJob{
				{JobName: "legacy", SnapshotCount: 1, Snapshots: []store.SnapshotMini{{Name: "private-filename-4", SizeBytes: 50, Mtime: unix(2026, 11, 2, 2, 0)}}},
			}},
		},
	}
	app := newTestApp(t, nil, nil, nil, idx)
	app.userStore = nil
	enableBackupReport(app)
	rr := getBackupReport(app.Handler())
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	want := `{"snapshot_count":4,"stored_bytes":900,"instances":[` +
		`{"scout_id":"Nas-01","scout_instance_id":"inst-a","last_seen_at":"2026-11-03T11:59:00Z","jobs":[` +
		`{"job_name":"documents","snapshot_count":2,"stored_bytes":350,"latest_snapshot_at":"2026-11-03T02:00:00Z","snapshots":[` +
		`{"size_bytes":250,"modified_at":"2026-11-03T02:00:00Z","unusual":true},` +
		`{"size_bytes":100,"modified_at":"2026-11-02T02:00:00Z","unusual":false}]},` +
		`{"job_name":"photos","snapshot_count":1,"stored_bytes":500,"latest_snapshot_at":"2026-10-28T03:30:00Z","snapshots":[` +
		`{"size_bytes":500,"modified_at":"2026-10-28T03:30:00Z","unusual":false}]}]},` +
		`{"scout_id":"new-01","scout_instance_id":"inst-c","last_seen_at":"2026-11-03T11:57:00Z","jobs":[]},` +
		`{"scout_id":"Old-01","scout_instance_id":"inst-b","last_seen_at":null,"jobs":[` +
		`{"job_name":"legacy","snapshot_count":1,"stored_bytes":50,"latest_snapshot_at":"2026-11-02T02:00:00Z","snapshots":[` +
		`{"size_bytes":50,"modified_at":"2026-11-02T02:00:00Z","unusual":false}]}]},` +
		`{"scout_id":"web-01","scout_instance_id":"inst-d","last_seen_at":null,"jobs":[]}]}`
	if got := strings.TrimSpace(rr.Body.String()); got != want {
		t.Fatalf("report:\n got %s\nwant %s", got, want)
	}
}
