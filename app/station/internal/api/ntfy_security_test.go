package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/3to1go/station/internal/store"
)

func TestNtfyRequestsRequireAuthenticatedAdmin(t *testing.T) {
	for _, tc := range []struct {
		name string
		user *store.User
		want int
	}{
		{"signed out", nil, http.StatusUnauthorized},
		{"non-admin", &store.User{ID: 2}, http.StatusForbidden},
		{"password change required", &store.User{ID: 1, IsAdmin: true, MustChangePassword: true}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := newTestApp(t, &mockUserStore{sessionUser: tc.user}, nil, nil, nil)
			// Rejected requests must never reach the notification publisher.
			app.ntfy = nil
			for _, path := range []string{"/api/ntfy", "/api/ntfy/test"} {
				request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"ntfy_url":"http://127.0.0.1:1","ntfy_topic":"test"}`))
				request.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "session-token"})
				response := httptest.NewRecorder()
				app.Handler().ServeHTTP(response, request)
				if response.Code != tc.want {
					t.Errorf("%s: status %d, want %d", path, response.Code, tc.want)
				}
			}
		})
	}
}
