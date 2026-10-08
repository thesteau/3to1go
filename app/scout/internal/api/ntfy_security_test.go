package api

import (
	"net/http"
	"testing"

	"github.com/3to1go/scout/internal/store"
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
			// No runner is supplied: rejected requests must never reach ntfy.
			app := newTestApp(&mockUserStore{sessionUser: tc.user})
			for _, path := range []string{"/api/ntfy", "/api/ntfy/test"} {
				response := doAuthRequest(app.Handler(), http.MethodPost, path, map[string]string{
					"ntfy_url": "http://127.0.0.1:1", "ntfy_topic": "test",
				})
				if response.Code != tc.want {
					t.Errorf("%s: status %d, want %d", path, response.Code, tc.want)
				}
			}
		})
	}
}
