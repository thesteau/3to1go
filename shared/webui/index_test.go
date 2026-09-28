package webui

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestIndexHasStylesAndDialogsWithoutRequests(t *testing.T) {
	files := fstest.MapFS{
		"index.html":             {Data: []byte(`<link rel="stylesheet" href="/static/css/base.css"><main>Loading...</main><!-- app-dialogs --><script defer src="/static/js/app.js"></script>`)},
		"css/base.css":           {Data: []byte("body { color: red; }")},
		"html/login-dialog.html": {Data: []byte(`<dialog id="login-dialog"></dialog>`)},
	}
	page, err := Index(files)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<style>body", `<dialog id="login-dialog">`, "<main>Loading...", "<script defer"} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(string(page), "<!-- app-dialogs -->") || strings.Contains(string(page), "stylesheet") {
		t.Fatal("shell still needs fragment or style requests")
	}
}
