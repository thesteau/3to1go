package httpx

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func RequestLogger(logger *slog.Logger, skip func(string) bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if skip(path) {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		level := slog.LevelDebug
		// Slow reads show at the normal level too, so they can be found in the logs.
		if r.Method != http.MethodGet || time.Since(start) >= time.Second {
			level = slog.LevelInfo
		}
		logger.Log(r.Context(), level, "request",
			"method", r.Method,
			"path", path,
			"status", rec.status,
			"ms", time.Since(start).Milliseconds(),
		)
	})
}

func WithPathValues(next http.HandlerFunc, names ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, name := range names {
			r.SetPathValue(name, chi.URLParam(r, name))
		}
		next(w, r)
	}
}
