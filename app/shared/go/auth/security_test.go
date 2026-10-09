package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type automationStoreMock struct {
	*sessionStore
	user        *User
	info        *AutomationToken
	hash        string
	createdHash string
	sessionsErr error
	signedOutID int
	keptSession string
}

func (s *automationStoreMock) UserForAutomationToken(_ context.Context, hash string) (*User, *AutomationToken, error) {
	s.hash = hash
	return s.user, s.info, nil
}
func (s *automationStoreMock) CreateAutomationToken(_ context.Context, _ int, hash string, info AutomationToken) error {
	s.createdHash = hash
	s.info = &info
	return nil
}
func (s *automationStoreMock) ListAutomationTokens(context.Context, int) ([]AutomationToken, error) {
	return []AutomationToken{*s.info}, nil
}
func (s *automationStoreMock) RevokeAutomationToken(context.Context, int, string) (bool, error) {
	return true, nil
}
func (s *automationStoreMock) DeleteSessionsForUser(_ context.Context, id int) error {
	s.signedOutID = id
	return s.sessionsErr
}
func (s *automationStoreMock) DeleteOtherSessionsForUser(_ context.Context, id int, keep string) error {
	s.signedOutID = id
	s.keptSession = keep
	return s.sessionsErr
}
func (s *automationStoreMock) ChangePassword(context.Context, int, string, string) (*User, error) {
	return s.user, nil
}

func TestBrowserOriginProtection(t *testing.T) {
	h := &Handler{Store: &sessionStore{user: &User{ID: 1}}, CookieName: "session"}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	// Login is public, but still needs protection against browser writes.
	middleware := h.Middleware(next, func(path string) bool { return path == "/api/session/login" || strings.HasPrefix(path, "/backup/") })
	for _, tc := range []struct {
		name, method, path, origin, site string
		want                             int
	}{
		{"curl", "POST", "/api/settings", "", "", 204},
		{"same origin", "POST", "/api/settings", "http://station.example:6555", "same-origin", 204},
		{"sibling app", "POST", "/api/settings", "http://station.example:6556", "same-site", 403},
		{"sibling domain", "POST", "/api/settings", "http://other.example:6555", "same-site", 403},
		{"older browser", "POST", "/api/settings", "http://other.example:6555", "", 403},
		{"cross site", "POST", "/api/settings", "", "cross-site", 403},
		{"login", "POST", "/api/session/login", "http://other.example", "cross-site", 403},
		{"safe read", "GET", "/api/settings", "http://other.example", "cross-site", 204},
		{"Scout protocol", "POST", "/backup/uploads/initiate", "http://other.example", "cross-site", 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://station.example:6555"+tc.path, nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			r.AddCookie(&http.Cookie{Name: "session", Value: "session-token"})
			w := httptest.NewRecorder()
			middleware.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestAutomationScopesAndAuthentication(t *testing.T) {
	for _, tc := range []struct {
		scopes       []string
		method, path string
		want         int
	}{
		{[]string{"read"}, "GET", "/api/status", 204},
		{[]string{"read"}, "GET", "/api/overview?section=snapshots", 204},
		{[]string{"read"}, "GET", "/api/overview?section=settings", 403},
		{[]string{"read"}, "GET", "/api/encryption-key", 403},
		{[]string{"read"}, "GET", "/api/hooks/files/script.sh", 403},
		{[]string{"read"}, "GET", "/api/settings", 403},
		{[]string{"read"}, "POST", "/api/run-now", 403},
		{[]string{"backup"}, "POST", "/api/run-now", 204},
		{[]string{"backup"}, "POST", "/api/cancel-operation", 204},
		{[]string{"backup"}, "POST", "/api/directories/force-send", 204},
		{[]string{"backup"}, "POST", "/api/directories/delete-job", 403},
		{[]string{"restore"}, "POST", "/api/recovery/restore", 204},
		{[]string{"restore"}, "POST", "/api/snapshots/scout/instance/job/archive.tar.zst/restore", 204},
		{[]string{"restore"}, "GET", "/api/snapshots/scout/instance/job/archive.tar.zst", 204},
		{[]string{"restore"}, "DELETE", "/api/snapshots/scout/instance/job/archive.tar.zst", 403},
		{[]string{"manage"}, "POST", "/api/settings", 204},
		{[]string{"manage"}, "POST", "/api/automation-tokens", 403},
		{[]string{"manage"}, "PUT", "/api/users/1", 403},
		{[]string{"manage"}, "POST", "/api/session/login", 403},
		{[]string{"read"}, "GET", "/api/session/me", 204},
		{[]string{"read"}, "GET", "/api/new-sensitive-route", 403},
	} {
		t.Run(strings.Join(tc.scopes, ",")+tc.method+tc.path, func(t *testing.T) {
			s := &automationStoreMock{sessionStore: &sessionStore{}, user: &User{ID: 1, IsAdmin: true}, info: &AutomationToken{Scopes: tc.scopes}}
			h := &Handler{Store: s, CookieName: "session"}
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("Authorization", "Bearer 3to1go_api_secret")
			w := httptest.NewRecorder()
			h.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if CurrentUser(r) == nil || CurrentAutomationToken(r) == nil {
					t.Fatal("missing automation context")
				}
				w.WriteHeader(204)
			}), func(string) bool { return true }).ServeHTTP(w, r)
			if w.Code != tc.want || s.hash != AutomationTokenHash("3to1go_api_secret") {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	for _, user := range []*User{nil, {ID: 1}, {ID: 1, IsAdmin: true, MustChangePassword: true}} {
		s := &automationStoreMock{sessionStore: &sessionStore{user: &User{ID: 1, IsAdmin: true}}, user: user, info: &AutomationToken{Scopes: []string{"manage"}}}
		h := &Handler{Store: s, CookieName: "session"}
		r := httptest.NewRequest("GET", "/api/settings", nil)
		r.Header.Set("Authorization", "Bearer 3to1go_api_revoked")
		r.AddCookie(&http.Cookie{Name: "session", Value: "session-token"})
		w := httptest.NewRecorder()
		h.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("invalid bearer fell back to cookie") }), func(string) bool { return false }).ServeHTTP(w, r)
		if w.Code != 401 && w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
}

func TestAutomationSecretShownOnlyOnCreation(t *testing.T) {
	s := &automationStoreMock{sessionStore: &sessionStore{}, user: &User{ID: 1, IsAdmin: true}}
	h := &Handler{Store: s}
	r := httptest.NewRequest("POST", "/api/automation-tokens", strings.NewReader(`{"name":"nightly","scopes":["read","read","backup"],"ttl_days":10}`))
	r = r.WithContext(context.WithValue(r.Context(), ContextKeyUser, s.user))
	w := httptest.NewRecorder()
	h.CreateAutomationToken(w, r)
	var body struct {
		Token string          `json:"token"`
		Info  AutomationToken `json:"automation_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 || !strings.HasPrefix(body.Token, "3to1go_api_") || s.createdHash != AutomationTokenHash(body.Token) || len(body.Info.Scopes) != 2 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ListAutomationTokens(w, r)
	if strings.Contains(w.Body.String(), body.Token) || strings.Contains(w.Body.String(), s.createdHash) {
		t.Fatal("list exposed token secret or hash")
	}
}

func TestSessionRevocation(t *testing.T) {
	s := &automationStoreMock{sessionStore: &sessionStore{}, user: &User{ID: 9}}
	h := &Handler{Store: s, CookieName: "session"}
	r := httptest.NewRequest("POST", "/api/session/logout-all", nil)
	r = r.WithContext(context.WithValue(r.Context(), ContextKeyUser, s.user))
	w := httptest.NewRecorder()
	h.LogoutAll(w, r)
	if w.Code != 200 || s.signedOutID != 9 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout-all did not invalidate sessions and cookie")
	}
	s.sessionsErr = errors.New("database unavailable")
	w = httptest.NewRecorder()
	h.LogoutAll(w, r)
	if w.Code != 500 || len(w.Result().Cookies()) != 0 {
		t.Fatal("logout-all failure should retain current cookie")
	}
	s.sessionsErr = nil
	r = httptest.NewRequest("POST", "/api/session/change-password", strings.NewReader(`{"current_password":"before","new_password":"after","confirm_new_password":"after"}`))
	r.AddCookie(&http.Cookie{Name: "session", Value: "keep-this-browser"})
	r = r.WithContext(context.WithValue(r.Context(), ContextKeyUser, s.user))
	w = httptest.NewRecorder()
	h.ChangePassword(w, r)
	if w.Code != 200 || s.keptSession != "keep-this-browser" || s.signedOutID != 9 {
		t.Fatal("password change did not invalidate other sessions")
	}
}
