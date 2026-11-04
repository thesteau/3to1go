package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMonitoringAuthorized(t *testing.T) {
	admin := &User{ID: 1, IsAdmin: true}
	monitoring := &AutomationToken{Scopes: []string{MonitoringScope}}
	for _, tc := range []struct {
		name   string
		header string
		user   *User
		token  *AutomationToken
		want   bool
	}{
		{"monitoring scope", "Bearer 3to1go_api_secret", admin, monitoring, true},
		{"no header", "", admin, monitoring, false},
		{"not bearer", "Basic 3to1go_api_secret", admin, monitoring, false},
		{"not an automation token", "Bearer other", admin, monitoring, false},
		{"revoked or expired", "Bearer 3to1go_api_secret", nil, nil, false},
		{"owner lost admin", "Bearer 3to1go_api_secret", &User{ID: 1}, monitoring, false},
		{"owner must change password", "Bearer 3to1go_api_secret", &User{ID: 1, IsAdmin: true, MustChangePassword: true}, monitoring, false},
		{"read scope only", "Bearer 3to1go_api_secret", admin, &AutomationToken{Scopes: []string{"read"}}, false},
		{"manage scope only", "Bearer 3to1go_api_secret", admin, &AutomationToken{Scopes: []string{"manage"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &automationStoreMock{sessionStore: &sessionStore{}, user: tc.user, info: tc.token}
			h := &Handler{Store: s}
			r := httptest.NewRequest("GET", "/metrics", nil)
			r.Header.Set("Authorization", tc.header)
			got, err := h.MonitoringAuthorized(r)
			if err != nil || got != tc.want {
				t.Fatalf("authorized = %v, error %v", got, err)
			}
		})
	}
}

type failingAutomationStore struct {
	*automationStoreMock
}

func (failingAutomationStore) UserForAutomationToken(context.Context, string) (*User, *AutomationToken, error) {
	return nil, nil, errors.New("database down")
}

func TestMonitoringAuthorizedStoreFailure(t *testing.T) {
	h := &Handler{Store: failingAutomationStore{&automationStoreMock{sessionStore: &sessionStore{}}}}
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.Header.Set("Authorization", "Bearer 3to1go_api_secret")
	if got, err := h.MonitoringAuthorized(r); err == nil || got {
		t.Fatalf("authorized = %v, error %v", got, err)
	}
}

func TestMonitoringAuthorizedWithoutAutomationStore(t *testing.T) {
	h := &Handler{Store: &sessionStore{}}
	r := httptest.NewRequest("GET", "/metrics", nil)
	r.Header.Set("Authorization", "Bearer 3to1go_api_secret")
	if got, err := h.MonitoringAuthorized(r); err != nil || got {
		t.Fatalf("authorized = %v, error %v", got, err)
	}
}

func TestMonitoringScopeGrantsNoAPIAccess(t *testing.T) {
	for _, path := range []string{"/api/status", "/api/overview", "/api/settings", "/api/encryption-key", "/api/directories"} {
		s := &automationStoreMock{sessionStore: &sessionStore{}, user: &User{ID: 1, IsAdmin: true}, info: &AutomationToken{Scopes: []string{MonitoringScope}}}
		h := &Handler{Store: s, CookieName: "session"}
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Authorization", "Bearer 3to1go_api_secret")
		w := httptest.NewRecorder()
		h.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Errorf("%s reached the handler", path)
		}), func(string) bool { return false }).ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
}

func TestMonitoringScopeOnlyWhereAllowed(t *testing.T) {
	create := func(allow bool, scopes string) int {
		s := &automationStoreMock{sessionStore: &sessionStore{}, user: &User{ID: 1, IsAdmin: true}}
		h := &Handler{Store: s, AllowMonitoringScope: allow}
		r := httptest.NewRequest("POST", "/api/automation-tokens", strings.NewReader(`{"name":"metrics","scopes":`+scopes+`}`))
		r = r.WithContext(context.WithValue(r.Context(), ContextKeyUser, s.user))
		w := httptest.NewRecorder()
		h.CreateAutomationToken(w, r)
		return w.Code
	}
	if code := create(true, `["monitoring"]`); code != http.StatusCreated {
		t.Fatalf("allowed: %d", code)
	}
	if code := create(true, `["read","backup","restore","manage","monitoring"]`); code != http.StatusCreated {
		t.Fatalf("all scopes: %d", code)
	}
	if code := create(false, `["monitoring"]`); code != http.StatusBadRequest {
		t.Fatalf("not allowed: %d", code)
	}
	if code := create(false, `["read","backup","restore","manage"]`); code != http.StatusCreated {
		t.Fatalf("existing scopes: %d", code)
	}
}
