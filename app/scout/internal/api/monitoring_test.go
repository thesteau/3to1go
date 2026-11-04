package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/3to1go/scout/internal/config"
	"github.com/3to1go/scout/internal/store"
	"github.com/3to1go/shared/auth"
)

// monitoringUserStore adds automation tokens to the mock user store.
type monitoringUserStore struct {
	*mockUserStore
	owner  *store.User
	token  *auth.AutomationToken
	err    error
	stored []auth.AutomationToken
}

func (m *monitoringUserStore) CreateAutomationToken(_ context.Context, _ int, _ string, token auth.AutomationToken) error {
	m.stored = append(m.stored, token)
	return nil
}

func (m *monitoringUserStore) ListAutomationTokens(context.Context, int) ([]auth.AutomationToken, error) {
	return m.stored, nil
}

func (m *monitoringUserStore) RevokeAutomationToken(context.Context, int, string) (bool, error) {
	return true, nil
}

func (m *monitoringUserStore) UserForAutomationToken(context.Context, string) (*store.User, *auth.AutomationToken, error) {
	return m.owner, m.token, m.err
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

func TestScoutMetricsRequireMonitoringToken(t *testing.T) {
	admin := &store.User{ID: 1, Username: "admin", IsAdmin: true}
	users := &monitoringUserStore{mockUserStore: &mockUserStore{}, owner: admin, token: &auth.AutomationToken{Scopes: []string{auth.MonitoringScope}}}
	runner := defaultRunner()
	runner.settings.MetricsEnabled = true
	runner.settings.MonitoringTokenRequired = true
	handler := newTestAppFull(users, runner, defaultScheduler()).Handler()

	rr := scrapeWithBearer(handler, "")
	if rr.Code != http.StatusUnauthorized || rr.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("no token: %d %q", rr.Code, rr.Header().Get("WWW-Authenticate"))
	}
	if rr := scrapeWithBearer(handler, "3to1go_api_secret"); rr.Code != http.StatusOK {
		t.Fatalf("monitoring token: %d %s", rr.Code, rr.Body.String())
	}
	// A JWT, such as the Station token this Scout holds, is not accepted.
	if rr := scrapeWithBearer(handler, "eyJhbGciOiJFZERTQSJ9.e30.signature"); rr.Code != http.StatusUnauthorized {
		t.Fatalf("Station token: %d", rr.Code)
	}
	users.token = &auth.AutomationToken{Scopes: []string{"read", "manage"}}
	if rr := scrapeWithBearer(handler, "3to1go_api_secret"); rr.Code != http.StatusUnauthorized {
		t.Fatalf("other scopes: %d", rr.Code)
	}
	users.err = errors.New("private database details")
	if rr := scrapeWithBearer(handler, "3to1go_api_secret"); rr.Code != http.StatusServiceUnavailable || strings.Contains(rr.Body.String(), "private") {
		t.Fatalf("database failure: %d %s", rr.Code, rr.Body.String())
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

func TestScoutAcceptsMonitoringScope(t *testing.T) {
	users := &monitoringUserStore{mockUserStore: adminUserStore()}
	handler := newTestAppFull(users, defaultRunner(), defaultScheduler()).Handler()
	rr := doAuthRequest(handler, "POST", "/api/automation-tokens", map[string]any{"name": "prometheus", "scopes": []string{auth.MonitoringScope}})
	if rr.Code != http.StatusCreated || len(users.stored) != 1 {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
}

func TestScoutMonitoringSettingSurvivesPartialSaves(t *testing.T) {
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
