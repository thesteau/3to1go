package api

import (
	"net/http"

	"github.com/3to1go/shared/auth"
	"github.com/3to1go/shared/httpx"
)

// requireMonitoringToken guards /metrics with a monitoring-scoped automation
// token. Disabled metrics still answer 404 from next.
func (a *App) requireMonitoringToken(access *auth.Handler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := a.runner.CurrentSettings()
		if !s.MetricsEnabled || !s.MonitoringTokenRequired {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		authorized, err := access.MonitoringAuthorized(r)
		switch {
		case err != nil:
			// Keep database details out of this endpoint.
			httpx.WriteError(w, http.StatusServiceUnavailable, "monitoring token check unavailable")
		case !authorized:
			auth.WriteMonitoringUnauthorized(w)
		default:
			next.ServeHTTP(w, r)
		}
	})
}
