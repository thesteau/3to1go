package webui

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestIndexRejectsMissingStylesPlaceholder(t *testing.T) {
	files := fstest.MapFS{
		"index.html":   {Data: []byte(`<main>Loading...</main><!-- app-dialogs -->`)},
		"css/base.css": {Data: []byte("body { color: red; }")},
	}
	page, err := Index(files, "css/base.css")
	if err == nil || !strings.Contains(err.Error(), "<!-- app-styles -->") {
		t.Fatalf("expected a missing styles placeholder error, got %v", err)
	}
	if page != nil {
		t.Fatal("returned a page despite missing styles placeholder")
	}
}

func TestIndexHasStylesAndDialogsWithoutRequests(t *testing.T) {
	files := fstest.MapFS{
		"index.html":             {Data: []byte(`<!-- app-styles --><main>Loading...</main><!-- app-dialogs --><script defer src="/static/js/app.js"></script>`)},
		"css/overrides.css":      {Data: []byte("body { color: blue; }")},
		"css/base.css":           {Data: []byte("body { color: red; }")},
		"html/login-dialog.html": {Data: []byte(`<dialog id="login-dialog"></dialog>`)},
	}
	page, err := Index(files, "css/base.css", "css/overrides.css")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<style>body { color: red; }</style><style>body { color: blue; }</style>", `<dialog id="login-dialog">`, "<main>Loading...", "<script defer"} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(string(page), "<!-- app-dialogs -->") || strings.Contains(string(page), "<!-- app-styles -->") {
		t.Fatal("shell still needs fragment or style requests")
	}
}
