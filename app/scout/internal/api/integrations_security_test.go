package api

import (
	"net/http"
	"testing"

	"github.com/3to1go/scout/internal/store"
)

func TestIntegrationsRequireAuthenticatedAdmin(t *testing.T) {
	for _, tc := range []struct {
		user *store.User
		want int
	}{
		{nil, http.StatusUnauthorized},
		{&store.User{ID: 2}, http.StatusForbidden},
		{&store.User{ID: 1, IsAdmin: true, MustChangePassword: true}, http.StatusForbidden},
	} {
		app := newTestApp(&mockUserStore{sessionUser: tc.user})
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/integrations"},
			{http.MethodPost, "/api/integrations"},
			{http.MethodPost, "/api/integrations/example/test"},
			{http.MethodDelete, "/api/integrations/example"},
		} {
			response := doAuthRequest(app.Handler(), route.method, route.path, map[string]string{})
			if response.Code != tc.want {
				t.Errorf("%s %s: status %d, want %d", route.method, route.path, response.Code, tc.want)
			}
		}
	}
}
