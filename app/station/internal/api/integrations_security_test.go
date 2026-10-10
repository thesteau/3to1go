package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/3to1go/station/internal/store"
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
		app := newTestApp(t, &mockUserStore{sessionUser: tc.user}, nil, nil, nil)
		app.integrations = nil
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/api/integrations"},
			{http.MethodPost, "/api/integrations"},
			{http.MethodPost, "/api/integrations/example/test"},
			{http.MethodDelete, "/api/integrations/example"},
		} {
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
			request.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "session-token"})
			response := httptest.NewRecorder()
			app.Handler().ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Errorf("%s %s: status %d, want %d", route.method, route.path, response.Code, tc.want)
			}
		}
	}
}
