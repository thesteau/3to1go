package api

import (
	"errors"
	"net/http"
	"testing"
)

func TestFileActionPermissionsAndErrors(t *testing.T) {
	for _, route := range []string{"clear-staged", "exclude"} {
		t.Run(route, func(t *testing.T) {
			runner := defaultRunner()
			path := "/api/directories/" + route
			app := newTestAppFull(regularUserStore(), runner, defaultScheduler())
			body := map[string]string{"relative_path": "nested/file"}
			if rr := doAuthRequest(app.Handler(), "POST", path, body); rr.Code != http.StatusForbidden {
				t.Fatalf("non-admin: %d", rr.Code)
			}
			if runner.pathActionPath != "" {
				t.Fatal("called runner without authorization")
			}
			app = newTestAppFull(adminUserStore(), runner, defaultScheduler())
			if rr := doAuthRequest(app.Handler(), "POST", path, body); rr.Code != http.StatusOK {
				t.Fatalf("admin: %d %s", rr.Code, rr.Body.String())
			}
			if runner.pathActionPath != "nested/file" {
				t.Fatal("lost path")
			}
			runner.pathActionErr = errors.New("busy")
			if rr := doAuthRequest(app.Handler(), "POST", path, body); rr.Code != http.StatusBadRequest {
				t.Fatalf("error: %d", rr.Code)
			}
			if rr := doAuthRequest(app.Handler(), "POST", path, map[string]string{}); rr.Code != http.StatusBadRequest {
				t.Fatalf("empty path: %d", rr.Code)
			}
		})
	}
	app := newTestAppFull(regularUserStore(), defaultRunner(), defaultScheduler())
	if rr := doAuthRequest(app.Handler(), "POST", "/api/cancel-operation", nil); rr.Code != http.StatusForbidden {
		t.Fatalf("cancel permission: %d", rr.Code)
	}
}

func TestBrowseAndFolderSizeRoutes(t *testing.T) {
	for _, route := range []string{"browse", "size"} {
		runner := defaultRunner()
		app := newTestAppFull(regularUserStore(), runner, defaultScheduler())
		if rr := doAuthRequest(app.Handler(), "GET", "/api/directories/"+route+"?relative_path=nested%2Ffolder", nil); rr.Code != http.StatusOK {
			t.Fatalf("%s: %d", route, rr.Code)
		}
		if runner.pathActionPath != "nested/folder" {
			t.Fatalf("path=%q", runner.pathActionPath)
		}
	}
}
