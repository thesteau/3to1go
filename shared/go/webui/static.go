package webui

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

// asset is one prepared file: its content, an optional gzip copy, and a tag
// that changes when the content does.
type asset struct {
	content     []byte
	gzipped     []byte
	etag        string
	contentType string
}

// Files compiled into the binary have no modification time, so without a tag
// the browser downloads every one again on each refresh. With one, it asks
// whether the file changed and usually gets an empty 304 back.
type assetCache struct {
	read   func(string) ([]byte, error)
	assets sync.Map // name -> *asset
}

func (c *assetCache) get(name string) (*asset, error) {
	if cached, ok := c.assets.Load(name); ok {
		return cached.(*asset), nil
	}
	content, err := c.read(name)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(content)
	a := &asset{
		content:     content,
		etag:        `"` + hex.EncodeToString(sum[:8]) + `"`,
		contentType: mime.TypeByExtension(path.Ext(name)),
	}
	if a.contentType == "" {
		a.contentType = http.DetectContentType(content)
	}
	if compressible(a.contentType) {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, err := zw.Write(content)
		if err == nil {
			err = zw.Close()
		}
		// Serve uncompressed content if compression failed or did not help.
		if err == nil && buf.Len() < len(content) {
			a.gzipped = buf.Bytes()
		}
	}
	c.assets.Store(name, a)
	return a, nil
}

func compressible(contentType string) bool {
	return strings.HasPrefix(contentType, "text/") ||
		strings.Contains(contentType, "javascript") ||
		strings.Contains(contentType, "json") ||
		strings.Contains(contentType, "svg")
}

// serve writes the asset, or 304 when the browser already has this version.
// "no-cache" makes the browser check each time, so a new build shows at once.
func (c *assetCache) serve(w http.ResponseWriter, r *http.Request, name string) {
	a, err := c.get(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "no-cache")
	h.Set("ETag", a.etag)
	h.Set("Content-Type", a.contentType)
	h.Add("Vary", "Accept-Encoding")
	content := a.content
	if a.gzipped != nil && acceptsGzip(r) {
		h.Set("Content-Encoding", "gzip")
		content = a.gzipped
	}
	// ServeContent answers If-None-Match with 304 and handles range requests.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(content))
}

func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.TrimSpace(strings.SplitN(part, ";", 2)[0]) == "gzip" {
			return true
		}
	}
	return false
}

// StaticHandler serves files from the embedded file system with cache tags and
// gzip. Mount it under a prefix with http.StripPrefix.
func StaticHandler(files fs.FS) http.Handler {
	cache := &assetCache{read: func(name string) ([]byte, error) { return fs.ReadFile(files, name) }}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		cache.serve(w, r, name)
	})
}
