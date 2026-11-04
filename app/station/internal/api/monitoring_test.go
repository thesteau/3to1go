package api

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/3to1go/shared/auth"
	"github.com/3to1go/station/internal/config"
	"github.com/3to1go/station/internal/signing"
	"github.com/3to1go/station/internal/store"
)

// mockMonitoringStore checks real signed tokens. Only database state is mocked.
type mockMonitoringStore struct {
	revoked   bool
	verifyErr error
	mintErr   error
	list      []store.MonitoringToken
	listErr   error
	revokeHit bool
	revokeErr error
	verified  int
	mintName  string
	mintTTL   int
}

func (m *mockMonitoringStore) Mint(_ context.Context, priv ed25519.PrivateKey, name string, ttlDays int, routes []string) (string, *store.MonitoringToken, error) {
	m.mintName, m.mintTTL = name, ttlDays
	if m.mintErr != nil {
		return "", nil, m.mintErr
	}
	token, payload, err := signing.MintMonitoringToken(priv, ttlDays, routes)
	if err != nil {
		return "", nil, err
	}
	return token, &store.MonitoringToken{ID: payload.JTI, Name: name, Routes: routes}, nil
}

func (m *mockMonitoringStore) Verify(_ context.Context, token string, pub ed25519.PublicKey) (*store.MonitoringToken, error) {
	m.verified++
	if m.verifyErr != nil {
		return nil, m.verifyErr
	}
	payload, err := signing.VerifyMonitoringToken(token, pub)
	if err != nil || m.revoked {
		return nil, store.ErrInvalidMonitoringToken
	}
	return &store.MonitoringToken{ID: payload.JTI, Routes: payload.Routes}, nil
}

func (m *mockMonitoringStore) List(context.Context) ([]store.MonitoringToken, error) {
	return m.list, m.listErr
}

func (m *mockMonitoringStore) Revoke(context.Context, string) (bool, error) {
	return m.revokeHit, m.revokeErr
}

func (m *mockMonitoringStore) CleanupExpired(context.Context) (int64, error) { return 0, nil }

func monitoringApp(t *testing.T, us userStorer, required bool) (*App, *mockMonitoringStore) {
	t.Helper()
	app := newTestApp(t, us, nil, nil, &mockSnapIndex{})
	tokens := &mockMonitoringStore{}
	app.monitoring = tokens
	s := *app.Settings()
	s.MetricsEnabled = true
	s.BackupReportEnabled = true
	s.MonitoringTokenRequired = required
	app.ApplySettings(&s)
	return app, tokens
}

func mintFor(t *testing.T, app *App, routes ...string) string {
	t.Helper()
	priv, _, err := signing.LoadOrCreateIssuerKeypair(app.Settings().IssuerKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := signing.MintMonitoringToken(priv, 30, routes)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func getWithToken(handler http.Handler, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestMonitoringTokenProtectsRoutes(t *testing.T) {
	for _, path := range []string{"/metrics", "/report"} {
		t.Run(path, func(t *testing.T) {
			app, tokens := monitoringApp(t, nil, true)
			// A nil user store would panic if a request tried to look up a UI session.
			app.userStore = nil
			handler := app.Handler()
			both := mintFor(t, app, signing.MonitoringRouteMetrics, signing.MonitoringRouteReport)

			rr := getWithToken(handler, path, "")
			if rr.Code != http.StatusUnauthorized || rr.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("no token: %d %q", rr.Code, rr.Header().Get("WWW-Authenticate"))
			}
			if tokens.verified != 0 {
				t.Fatal("checked a missing token")
			}
			if rr := getWithToken(handler, path, "not-a-token"); rr.Code != http.StatusUnauthorized {
				t.Fatalf("bad token: %d", rr.Code)
			}
			if rr := getWithToken(handler, path, both); rr.Code != http.StatusOK {
				t.Fatalf("valid token: %d %s", rr.Code, rr.Body.String())
			}
			tokens.revoked = true
			if rr := getWithToken(handler, path, both); rr.Code != http.StatusUnauthorized {
				t.Fatalf("revoked token: %d", rr.Code)
			}
			tokens.revoked = false
			tokens.verifyErr = errors.New("private database details")
			if rr := getWithToken(handler, path, both); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "private") {
				t.Fatalf("database failure: %d %s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestMonitoringTokenRoutesClaim(t *testing.T) {
	app, _ := monitoringApp(t, nil, true)
	handler := app.Handler()
	metricsOnly := mintFor(t, app, signing.MonitoringRouteMetrics)
	if rr := getWithToken(handler, "/metrics", metricsOnly); rr.Code != http.StatusOK {
		t.Fatalf("allowed route: %d", rr.Code)
	}
	if rr := getWithToken(handler, "/report", metricsOnly); rr.Code != http.StatusForbidden {
		t.Fatalf("other route: %d", rr.Code)
	}
}

func TestMonitoringStationTokenRejected(t *testing.T) {
	app, _ := monitoringApp(t, nil, true)
	priv, _, err := signing.LoadOrCreateIssuerKeypair(app.Settings().IssuerKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	station, err := signing.MintCredential(priv, 30)
	if err != nil {
		t.Fatal(err)
	}
	if rr := getWithToken(app.Handler(), "/report", station); rr.Code != http.StatusUnauthorized {
		t.Fatalf("Station token: %d", rr.Code)
	}
}

func TestMonitoringTokenNotRequiredAndDisabled(t *testing.T) {
	app, tokens := monitoringApp(t, nil, false)
	handler := app.Handler()
	for _, path := range []string{"/metrics", "/report"} {
		if rr := getWithToken(handler, path, ""); rr.Code != http.StatusOK {
			t.Fatalf("%s without requirement: %d", path, rr.Code)
		}
	}
	s := *app.Settings()
	s.MetricsEnabled = false
	s.BackupReportEnabled = false
	s.MonitoringTokenRequired = true
	app.ApplySettings(&s)
	for _, path := range []string{"/metrics", "/report"} {
		if rr := getWithToken(handler, path, ""); rr.Code != http.StatusNotFound {
			t.Fatalf("%s disabled: %d", path, rr.Code)
		}
	}
	if tokens.verified != 0 {
		t.Fatal("checked a token while the routes were off")
	}
}

func TestMonitoringTokenRoutesAreRateLimited(t *testing.T) {
	for _, path := range []string{"/metrics", "/report"} {
		if len(specsForPath(path)) == 0 {
			t.Errorf("%s has no rate limit", path)
		}
	}
}

func TestMonitoringTokenAdminAccess(t *testing.T) {
	for _, tc := range []struct {
		name string
		user *store.User
		want int
	}{
		{"signed out", nil, http.StatusUnauthorized},
		{"non-admin", regularUser(), http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, tokens := monitoringApp(t, &mockUserStore{sessionUser: tc.user}, true)
			handler := app.Handler()
			tokens.revokeHit = true
			for _, req := range []*http.Request{
				jsonReq("GET", "/api/monitoring-tokens", nil),
				jsonReq("POST", "/api/monitoring-tokens", map[string]any{"name": "x", "routes": []string{"report"}}),
				jsonReq("DELETE", "/api/monitoring-tokens/id", nil),
			} {
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)
				if rr.Code != tc.want {
					t.Errorf("%s %s: %d", req.Method, req.URL.Path, rr.Code)
				}
			}
			if tokens.mintName != "" {
				t.Fatal("minted a token without admin access")
			}
		})
	}
}

// automationUserStore lets a request authenticate with a manage-scoped automation token.
type automationUserStore struct {
	*mockUserStore
}

func (automationUserStore) CreateAutomationToken(context.Context, int, string, auth.AutomationToken) error {
	return nil
}

func (automationUserStore) ListAutomationTokens(context.Context, int) ([]auth.AutomationToken, error) {
	return nil, nil
}

func (automationUserStore) RevokeAutomationToken(context.Context, int, string) (bool, error) {
	return true, nil
}

func (automationUserStore) UserForAutomationToken(context.Context, string) (*store.User, *auth.AutomationToken, error) {
	return adminUser(), &auth.AutomationToken{Scopes: []string{"manage"}}, nil
}

func TestMonitoringTokenManagementRejectsAutomationTokens(t *testing.T) {
	app, tokens := monitoringApp(t, automationUserStore{&mockUserStore{}}, true)
	handler := app.Handler()
	for _, req := range []*http.Request{
		jsonReq("GET", "/api/monitoring-tokens", nil),
		jsonReq("POST", "/api/monitoring-tokens", map[string]any{"name": "x", "routes": []string{"report"}}),
		jsonReq("DELETE", "/api/monitoring-tokens/id", nil),
	} {
		req.Header.Set("Authorization", "Bearer 3to1go_api_secret")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s: %d %s", req.Method, rr.Code, rr.Body.String())
		}
	}
	if tokens.mintName != "" {
		t.Fatal("automation token minted a monitoring token")
	}
}

func TestMonitoringTokenCreateListRevoke(t *testing.T) {
	app, tokens := monitoringApp(t, &mockUserStore{sessionUser: adminUser()}, true)
	handler := app.Handler()

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, jsonReq("POST", "/api/monitoring-tokens", map[string]any{"name": " prometheus ", "routes": []string{"report", "metrics", "report"}}))
	if rr.Code != http.StatusCreated || rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created struct {
		Token string                `json:"token"`
		Info  store.MonitoringToken `json:"monitoring_token"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if tokens.mintName != "prometheus" || tokens.mintTTL != 90 || !slices.Equal(created.Info.Routes, []string{"metrics", "report"}) {
		t.Fatalf("mint: %q, %d, %+v", tokens.mintName, tokens.mintTTL, created.Info)
	}
	// The returned token works straight away on the routes it names.
	if rr := getWithToken(handler, "/report", created.Token); rr.Code != http.StatusOK {
		t.Fatalf("new token: %d", rr.Code)
	}

	for name, body := range map[string]map[string]any{
		"no name":       {"name": " ", "routes": []string{"report"}},
		"long name":     {"name": strings.Repeat("x", 101), "routes": []string{"report"}},
		"no routes":     {"name": "x", "routes": []string{}},
		"unknown route": {"name": "x", "routes": []string{"overview"}},
		"ttl too long":  {"name": "x", "routes": []string{"report"}, "ttl_days": 3651},
		"negative ttl":  {"name": "x", "routes": []string{"report"}, "ttl_days": -1},
	} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, jsonReq("POST", "/api/monitoring-tokens", body))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, rr.Code)
		}
	}

	tokens.mintErr = errors.New("private database details")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, jsonReq("POST", "/api/monitoring-tokens", map[string]any{"name": "x", "routes": []string{"report"}}))
	if rr.Code != http.StatusInternalServerError || strings.Contains(rr.Body.String(), "private") {
		t.Fatalf("mint failure: %d %s", rr.Code, rr.Body.String())
	}

	tokens.list = []store.MonitoringToken{{ID: "id", Name: "prometheus", Routes: []string{"report"}}}
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, jsonReq("GET", "/api/monitoring-tokens", nil))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"name":"prometheus"`) || strings.Contains(rr.Body.String(), created.Token) {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, jsonReq("DELETE", "/api/monitoring-tokens/missing", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("revoke missing: %d", rr.Code)
	}
	tokens.revokeHit = true
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, jsonReq("DELETE", "/api/monitoring-tokens/id", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rr.Code, rr.Body.String())
	}
}

func TestMonitoringTokenSettingSurvivesPartialSaves(t *testing.T) {
	app, _ := monitoringApp(t, &mockUserStore{sessionUser: adminUser()}, true)
	handler := app.Handler()
	save := func(body any) {
		t.Helper()
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, jsonReq("POST", "/api/settings", body))
		if rr.Code != http.StatusOK {
			t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
		}
	}
	save(map[string]any{"retention_keep_last": 5})
	if !app.Settings().MonitoringTokenRequired {
		t.Fatal("omitted field turned the requirement off")
	}
	off := false
	save(config.SettingsPayload{MonitoringTokenRequired: &off})
	if app.Settings().MonitoringTokenRequired {
		t.Fatal("explicit false ignored")
	}
	save(map[string]any{"retention_keep_last": 5})
	if app.Settings().MonitoringTokenRequired {
		t.Fatal("omitted field turned the requirement on")
	}
}
