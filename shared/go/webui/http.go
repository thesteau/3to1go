package webui

import "net/http"

func ServeIndex(w http.ResponseWriter, r *http.Request, readFile func(string) ([]byte, error)) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	content, err := readFile("index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}
