package api

import (
	"encoding/json"
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
			{http.MethodGet, "/api/hooks"},
			{http.MethodPost, "/api/hooks"},
			{http.MethodPost, "/api/hooks/files"},
			{http.MethodGet, "/api/hooks/files/notify.sh"},
			{http.MethodDelete, "/api/hooks/files/notify.sh"},
			{http.MethodGet, "/api/overview?section=settings"},
			{http.MethodPost, "/api/settings"},
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

func TestOverviewDoesNotExposeIntegrationCommandsToNonAdmins(t *testing.T) {
	for _, admin := range []bool{false, true} {
		app := newTestApp(t, &mockUserStore{sessionUser: &store.User{ID: 1, IsAdmin: admin}}, nil, nil, nil)
		app.settings.HookPreCommand = "private-pre-command"
		app.settings.HookPostCommand = "private-post-command"
		for _, path := range []string{"/api/overview", "/api/overview?section=snapshots", "/api/overview?section=storage"} {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.AddCookie(&http.Cookie{Name: store.SessionCookie, Value: "session-token"})
			response := httptest.NewRecorder()
			app.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("admin=%v %s: status %d", admin, path, response.Code)
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			_, included := body["settings"]
			if want := admin && !strings.Contains(path, "section=storage"); included != want {
				t.Errorf("admin=%v %s: settings included=%v, want %v", admin, path, included, want)
			}
			if !admin && strings.Contains(response.Body.String(), "private-") {
				t.Error("non-admin response contains script commands")
			}
		}
		if app.settings.HookPostCommand != "private-post-command" {
			t.Error("filtering a response modified saved commands")
		}
	}
}
