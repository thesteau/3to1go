package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/3to1go/scout/internal/config"
	"github.com/3to1go/scout/internal/store"
	"github.com/3to1go/shared/auth"
)

// tokenUserStore records automation tokens. Tokens are really signed and checked;
// only the database is mocked.
type tokenUserStore struct {
	*mockUserStore
	owner  *store.User
	byHash map[string]auth.AutomationToken
	err    error
}

func newTokenUserStore() *tokenUserStore {
	return &tokenUserStore{
		mockUserStore: adminUserStore(),
		owner:         &store.User{ID: 1, Username: "admin", IsAdmin: true},
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
	if s.err != nil {
		return nil, nil, s.err
	}
	token, found := s.byHash[hash]
	if !found {
		return nil, nil, nil
	}
	return s.owner, &token, nil
}

func issueScoutToken(t *testing.T, handler http.Handler, scopes ...string) (string, string) {
	t.Helper()
	rr := doAuthRequest(handler, "POST", "/api/automation-tokens", map[string]any{"name": "prometheus", "scopes": scopes})
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

func scrapeWithBearer(handler http.Handler, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/metrics", nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr
}

func TestScoutMetricsRequireSignedToken(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	users := newTokenUserStore()
	runner := defaultRunner()
	runner.settings.MetricsEnabled = true
	runner.settings.MonitoringTokenRequired = true
	handler := newTestAppFull(users, runner, defaultScheduler()).Handler()
	token, id := issueScoutToken(t, handler, auth.ScopeMetrics)

	rr := scrapeWithBearer(handler, "")
	if rr.Code != http.StatusUnauthorized || rr.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("no token: %d %q", rr.Code, rr.Header().Get("WWW-Authenticate"))
	}
	if rr := scrapeWithBearer(handler, token); rr.Code != http.StatusOK {
		t.Fatalf("signed token: %d %s", rr.Code, rr.Body.String())
	}
	for name, bad := range map[string]string{
		"tampered":         token + "x",
		"unsigned":         "eyJhbGciOiJub25lIn0.e30.",
		"old token format": "3to1go_api_secret",
	} {
		if rr := scrapeWithBearer(handler, bad); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", name, rr.Code)
		}
	}
	readOnly, _ := issueScoutToken(t, handler, "read", "manage")
	if rr := scrapeWithBearer(handler, readOnly); rr.Code != http.StatusForbidden {
		t.Fatalf("other scopes: %d", rr.Code)
	}
	users.err = errors.New("private database details")
	if rr := scrapeWithBearer(handler, token); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "private") {
		t.Fatalf("database failure: %d %s", rr.Code, rr.Body.String())
	}
	users.err = nil
	if rr := doAuthRequest(handler, "DELETE", "/api/automation-tokens/"+id, nil); rr.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rr.Code, rr.Body.String())
	}
	if rr := scrapeWithBearer(handler, token); rr.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token: %d", rr.Code)
	}
}

func TestScoutMetricsScopeGrantsNoAPIAccess(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	handler := newTestAppFull(newTokenUserStore(), defaultRunner(), defaultScheduler()).Handler()
	token, _ := issueScoutToken(t, handler, auth.ScopeMetrics)
	req := httptest.NewRequest("GET", "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status: %d", rr.Code)
	}
}

func TestScoutHasNoReportScope(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	handler := newTestAppFull(newTokenUserStore(), defaultRunner(), defaultScheduler()).Handler()
	rr := doAuthRequest(handler, "POST", "/api/automation-tokens", map[string]any{"name": "x", "scopes": []string{auth.ScopeReport}})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("report scope: %d %s", rr.Code, rr.Body.String())
	}
}

func TestScoutMetricsTokenOffAndDisabled(t *testing.T) {
	// A nil user store would panic if a request tried to look up a token or session.
	runner := defaultRunner()
	runner.settings.MetricsEnabled = true
	handler := newTestAppFull(nil, runner, defaultScheduler()).Handler()
	if rr := scrapeWithBearer(handler, ""); rr.Code != http.StatusOK {
		t.Fatalf("not required: %d", rr.Code)
	}
	runner.settings.MetricsEnabled = false
	runner.settings.MonitoringTokenRequired = true
	if rr := scrapeWithBearer(handler, ""); rr.Code != http.StatusNotFound {
		t.Fatalf("disabled: %d", rr.Code)
	}
}

func TestScoutMetricsAreRateLimited(t *testing.T) {
	if len(specsForPath("/metrics")) == 0 {
		t.Fatal("/metrics has no rate limit")
	}
}

func TestScoutTokenSettingSurvivesPartialSaves(t *testing.T) {
	runner := defaultRunner()
	runner.settings.MonitoringTokenRequired = true
	handler := newTestAppFull(adminUserStore(), runner, defaultScheduler()).Handler()
	if rr := doAuthRequest(handler, "POST", "/api/settings", map[string]any{"max_depth": 5}); rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	if !runner.settings.MonitoringTokenRequired {
		t.Fatal("omitted field turned the requirement off")
	}
	off := false
	if rr := doAuthRequest(handler, "POST", "/api/settings", config.SettingsPayload{MonitoringTokenRequired: &off}); rr.Code != http.StatusOK {
		t.Fatal(rr.Body.String())
	}
	if runner.settings.MonitoringTokenRequired {
		t.Fatal("explicit false ignored")
	}
}
