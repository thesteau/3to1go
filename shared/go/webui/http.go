package webui

import "net/http"

// IndexServer serves the assembled page shell with a cache tag and gzip.
func IndexServer(readFile func(string) ([]byte, error)) http.HandlerFunc {
	cache := &assetCache{read: readFile}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if _, err := cache.get("index.html"); err != nil {
			http.Error(w, "index.html not found", http.StatusInternalServerError)
			return
		}
		cache.serve(w, r, "index.html")
	}
}
