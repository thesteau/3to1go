package webui

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticHandlerTagsCompressesAndRevalidates(t *testing.T) {
	script := strings.Repeat("function hello() { return 1; }\n", 50)
	files := fstest.MapFS{
		"js/app.js":    {Data: []byte(script)},
		"img/logo.png": {Data: []byte("\x89PNG\r\n\x1a\nnot really")},
	}
	handler := StaticHandler(files)

	req := httptest.NewRequest(http.MethodGet, "/js/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("status %d, encoding %q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" || !strings.Contains(rec.Header().Get("Content-Type"), "javascript") {
		t.Errorf("headers = %v", rec.Header())
	}
	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := io.ReadAll(zr); string(body) != script {
		t.Error("gzip body does not match the file")
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}

	// A refresh with the same tag gets an empty 304.
	req = httptest.NewRequest(http.MethodGet, "/js/app.js", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Errorf("revalidation: status %d, %d bytes", rec.Code, rec.Body.Len())
	}

	// Without gzip support, the plain file is sent; images are never gzipped.
	for _, name := range []string{"/js/app.js", "/img/logo.png"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, name, nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Encoding") != "" {
			t.Errorf("%s: status %d, encoding %q", name, rec.Code, rec.Header().Get("Content-Encoding"))
		}
	}

	for _, name := range []string{"/missing.js", "/js/", "/"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, name, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", name, rec.Code)
		}
	}
}

func TestIndexServerTagsTheShell(t *testing.T) {
	serve := IndexServer(func(name string) ([]byte, error) {
		return []byte("<!doctype html><main>Scout</main>"), nil
	})
	rec := httptest.NewRecorder()
	serve(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || rec.Header().Get("ETag") == "" {
		t.Fatalf("status %d, headers %v", rec.Code, rec.Header())
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	rec = httptest.NewRecorder()
	serve(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Errorf("revalidation status %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	serve(rec, httptest.NewRequest(http.MethodGet, "/other", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("other path status %d", rec.Code)
	}
}
