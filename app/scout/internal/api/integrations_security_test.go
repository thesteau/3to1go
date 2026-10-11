package api

import (
	"encoding/json"
	"net/http"
	"strings"
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
			{http.MethodGet, "/api/hooks"},
			{http.MethodPost, "/api/hooks"},
			{http.MethodPost, "/api/hooks/files"},
			{http.MethodGet, "/api/hooks/files/notify.sh"},
			{http.MethodDelete, "/api/hooks/files/notify.sh"},
			{http.MethodGet, "/api/settings"},
			{http.MethodPost, "/api/settings"},
		} {
			response := doAuthRequest(app.Handler(), route.method, route.path, map[string]string{})
			if response.Code != tc.want {
				t.Errorf("%s %s: status %d, want %d", route.method, route.path, response.Code, tc.want)
			}
		}
	}
}

func TestStatusDoesNotExposeIntegrationCommandsToNonAdmins(t *testing.T) {
	runner := defaultRunner()
	runner.statusSnapshot["settings"] = map[string]any{
		"hook_pre_command":  "private-pre-command",
		"hook_post_command": "private-post-command",
	}
	for _, admin := range []bool{false, true} {
		app := newTestAppFull(&mockUserStore{sessionUser: &store.User{ID: 1, IsAdmin: admin}}, runner, defaultScheduler())
		response := doAuthRequest(app.Handler(), http.MethodGet, "/api/status", nil)
		if response.Code != http.StatusOK {
			t.Fatalf("admin=%v: status %d", admin, response.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if _, included := body["settings"]; included != admin {
			t.Errorf("admin=%v: settings included=%v", admin, included)
		}
		if _, included := body["scheduler"]; !included {
			t.Error("dashboard status missing scheduler")
		}
		if !admin && strings.Contains(response.Body.String(), "private-") {
			t.Error("non-admin response contains script commands")
		}
	}
	if _, present := runner.statusSnapshot["settings"]; !present {
		t.Error("filtering a response modified the original settings snapshot")
	}
}
