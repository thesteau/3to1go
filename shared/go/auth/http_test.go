package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type sessionStore struct {
	Store
	user  *User
	token string
}

func (s *sessionStore) Authenticate(context.Context, string, string) (*User, error) {
	return s.user, nil
}

func (s *sessionStore) CreateSession(context.Context, int) (string, error) {
	return "session-token", nil
}

func (s *sessionStore) UserForSession(_ context.Context, token string) (*User, error) {
	s.token = token
	if token != "session-token" {
		return nil, nil
	}
	return s.user, nil
}

func TestSessionCookieIsolation(t *testing.T) {
	t.Setenv("SESSION_COOKIE_SECURE", "true")
	for _, name := range []string{"three_to_one_go_session", "three_to_one_go_scout_session"} {
		t.Run(name, func(t *testing.T) {
			store := &sessionStore{user: &User{ID: 1}}
			h := &Handler{Store: store, CookieName: name}
			response := httptest.NewRecorder()
			h.Login(response, httptest.NewRequest("POST", "/api/session/login", strings.NewReader(`{"username":"admin","password":"secret"}`)))
			cookies := response.Result().Cookies()
			if response.Code != 200 || len(cookies) != 1 {
				t.Fatalf("login: %d, cookies: %v", response.Code, cookies)
			}
			cookie := cookies[0]
			if cookie.Name != name || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
				t.Fatalf("cookie: %+v", cookie)
			}
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if CurrentUser(r) == nil {
					t.Error("missing authenticated user")
				}
				w.WriteHeader(http.StatusNoContent)
			})
			middleware := h.Middleware(next, func(string) bool { return false })
			request := httptest.NewRequest("GET", "/api/settings", nil)
			request.AddCookie(cookie)
			response = httptest.NewRecorder()
			middleware.ServeHTTP(response, request)
			if response.Code != 204 {
				t.Fatalf("authenticated status: %d", response.Code)
			}
			request = httptest.NewRequest("GET", "/api/settings", nil)
			request.AddCookie(&http.Cookie{Name: "another-app", Value: "session-token"})
			response = httptest.NewRecorder()
			middleware.ServeHTTP(response, request)
			if response.Code != 401 || store.token != "" {
				t.Fatalf("foreign cookie accepted: %d", response.Code)
			}
		})
	}
}

func TestMiddlewarePasswordChangeAndPublicPaths(t *testing.T) {
	h := &Handler{Store: &sessionStore{user: &User{ID: 1, MustChangePassword: true}}, CookieName: "session"}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	middleware := h.Middleware(next, func(path string) bool { return path == "/api/session/change-password" })
	for path, want := range map[string]int{"/api/settings": 403, "/api/session/change-password": 204} {
		request := httptest.NewRequest("POST", path, nil)
		request.AddCookie(&http.Cookie{Name: "session", Value: "session-token"})
		response := httptest.NewRecorder()
		middleware.ServeHTTP(response, request)
		if response.Code != want {
			t.Errorf("%s: got %d, want %d", path, response.Code, want)
		}
	}
}
