package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/3to1go/shared/auth"
	"github.com/3to1go/station/internal/config"
	"github.com/3to1go/station/internal/signing"
	"github.com/3to1go/station/internal/store"
)

// tokenUserStore records automation tokens. Tokens are really signed and checked;
// only the database is mocked.
type tokenUserStore struct {
	*mockUserStore
	owner   *store.User
	byHash  map[string]auth.AutomationToken
	lookups int
	err     error
}

func newTokenUserStore() *tokenUserStore {
	return &tokenUserStore{
		mockUserStore: &mockUserStore{sessionUser: adminUser()},
		owner:         adminUser(),
		byHash:        map[string]auth.AutomationToken{},
	}
}

func (s *tokenUserStore) CreateAutomationToken(_ context.Context, _ int, hash string, token auth.AutomationToken) error {
	s.byHash[hash] = token
	return nil
}

func (s *tokenUserStore) ListAutomationTokens(context.Context, int) ([]auth.AutomationToken, error) {
	return nil, nil
}

func (s *tokenUserStore) RevokeAutomationToken(_ context.Context, _ int, id string) (bool, error) {
	for hash, token := range s.byHash {
		if token.ID == id {
			delete(s.byHash, hash)
			return true, nil
		}
	}
	return false, nil
}

func (s *tokenUserStore) UserForAutomationToken(_ context.Context, hash string) (*store.User, *auth.AutomationToken, error) {
	s.lookups++
	if s.err != nil {
		return nil, nil, s.err
	}
	token, found := s.byHash[hash]
	if !found {
		return nil, nil, nil
	}
	return s.owner, &token, nil
}

func routeTokenApp(t *testing.T, required bool) (*App, *tokenUserStore, http.Handler) {
	t.Helper()
	users := newTokenUserStore()
	app := newTestApp(t, users, nil, nil, &mockSnapIndex{})
	s := *app.Settings()
	s.MetricsEnabled = true
	s.BackupReportEnabled = true
	s.MonitoringTokenRequired = required
	app.ApplySettings(&s)
	return app, users, app.Handler()
}

func issueToken(t *testing.T, handler http.Handler, scopes ...string) (string, string) {
	t.Helper()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, jsonReq("POST", "/api/automation-tokens", map[string]any{"name": "prometheus", "scopes": scopes}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Token string               `json:"token"`
		Info  auth.AutomationToken `json:"automation_token"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Token, body.Info.ID
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

func TestRouteTokenProtectsMetricsAndReport(t *testing.T) {
	for _, path := range []string{"/metrics", "/report"} {
		t.Run(path, func(t *testing.T) {
			_, users, handler := routeTokenApp(t, true)
			token, id := issueToken(t, handler, auth.ScopeMetrics, auth.ScopeReport)

			rr := getWithToken(handler, path, "")
			if rr.Code != http.StatusUnauthorized || rr.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("no token: %d %q", rr.Code, rr.Header().Get("WWW-Authenticate"))
			}
			if rr := getWithToken(handler, path, "not-a-token"); rr.Code != http.StatusUnauthorized {
				t.Fatalf("bad token: %d", rr.Code)
			}
			if rr := getWithToken(handler, path, "3to1go_api_secret"); rr.Code != http.StatusUnauthorized {
				t.Fatalf("old token format: %d", rr.Code)
			}
			if users.lookups != 0 {
				t.Fatal("looked up a token that failed its signature check")
			}
			if rr := getWithToken(handler, path, token); rr.Code != http.StatusOK {
				t.Fatalf("valid token: %d %s", rr.Code, rr.Body.String())
			}

			users.err = errors.New("private database details")
			if rr := getWithToken(handler, path, token); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "private") {
				t.Fatalf("database failure: %d %s", rr.Code, rr.Body.String())
			}
			users.err = nil

			rr = httptest.NewRecorder()
			handler.ServeHTTP(rr, jsonReq("DELETE", "/api/automation-tokens/"+id, nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("revoke: %d %s", rr.Code, rr.Body.String())
			}
			if rr := getWithToken(handler, path, token); rr.Code != http.StatusUnauthorized {
				t.Fatalf("revoked token: %d", rr.Code)
			}
		})
	}
}

func TestRouteTokenLimitedToItsScopes(t *testing.T) {
	_, _, handler := routeTokenApp(t, true)
	metricsOnly, _ := issueToken(t, handler, auth.ScopeMetrics)
	if rr := getWithToken(handler, "/metrics", metricsOnly); rr.Code != http.StatusOK {
		t.Fatalf("allowed route: %d", rr.Code)
	}
	if rr := getWithToken(handler, "/report", metricsOnly); rr.Code != http.StatusForbidden {
		t.Fatalf("other route: %d", rr.Code)
	}
	manage, _ := issueToken(t, handler, "manage")
	if rr := getWithToken(handler, "/report", manage); rr.Code != http.StatusForbidden {
		t.Fatalf("manage scope: %d", rr.Code)
	}
}

func TestRouteTokenGrantsNoAPIAccess(t *testing.T) {
	_, _, handler := routeTokenApp(t, true)
	token, _ := issueToken(t, handler, auth.ScopeMetrics, auth.ScopeReport)
	if rr := getWithToken(handler, "/api/overview", token); rr.Code != http.StatusForbidden {
		t.Fatalf("overview: %d", rr.Code)
	}
}

func TestStationTokenRejectedOnRoutesAndAPI(t *testing.T) {
	app, _, handler := routeTokenApp(t, true)
	priv, _, err := signing.LoadOrCreateIssuerKeypair(app.Settings().IssuerKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	station, err := signing.MintCredential(priv, 30)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/report", "/metrics", "/api/overview"} {
		if rr := getWithToken(handler, path, station); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", path, rr.Code)
		}
	}
}

func TestAutomationTokenRejectedAsStationToken(t *testing.T) {
	app, _, handler := routeTokenApp(t, true)
	token, _ := issueToken(t, handler, "manage")
	if _, err := signing.VerifyCredential(token, app.Settings().IssuerPublicKey); err == nil {
		t.Fatal("automation token passed as a Station token")
	}
}

func TestRouteTokenNotRequiredAndRouteDisabled(t *testing.T) {
	app, users, handler := routeTokenApp(t, false)
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
	if users.lookups != 0 {
		t.Fatal("checked a token while the routes were off")
	}
}

func TestRouteTokenRoutesAreRateLimited(t *testing.T) {
	for _, path := range []string{"/metrics", "/report"} {
		if len(specsForPath(path)) == 0 {
			t.Errorf("%s has no rate limit", path)
		}
	}
}

func TestRouteTokenSettingSurvivesPartialSaves(t *testing.T) {
	app, _, handler := routeTokenApp(t, true)
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
