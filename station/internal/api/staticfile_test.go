package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticIndexRedirectsToAssembledShell(t *testing.T) {
	app := newTestApp(t, nil, nil, nil, nil)
	handler := app.Handler()
	for _, path := range []string{"/static/index.html", "/static/"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusMovedPermanently || response.Header().Get("Location") != "/" {
				t.Fatalf("expected redirect to /, got status %d and location %q", response.Code, response.Header().Get("Location"))
			}
			shell := httptest.NewRecorder()
			handler.ServeHTTP(shell, httptest.NewRequest(http.MethodGet, response.Header().Get("Location"), nil))
			if shell.Code != http.StatusOK {
				t.Fatalf("shell returned status %d", shell.Code)
			}
			body := shell.Body.String()
			if !strings.Contains(body, "<style>") || !strings.Contains(body, "<dialog") || strings.Contains(body, "<!-- app-styles -->") || strings.Contains(body, "<!-- app-dialogs -->") {
				t.Fatal("redirect destination did not serve the assembled shell")
			}
		})
	}
	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/static/css/base.css", nil))
	if asset.Code != http.StatusOK || !strings.HasPrefix(asset.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("CSS asset returned status %d and content type %q", asset.Code, asset.Header().Get("Content-Type"))
	}
}
