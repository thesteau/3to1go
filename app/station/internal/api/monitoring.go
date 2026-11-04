package api

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/3to1go/shared/auth"
	"github.com/3to1go/shared/httpx"
	"github.com/3to1go/station/internal/signing"
	"github.com/3to1go/station/internal/store"
)

type monitoringTokenStorer interface {
	Mint(ctx context.Context, priv ed25519.PrivateKey, name string, ttlDays int, routes []string) (string, *store.MonitoringToken, error)
	Verify(ctx context.Context, token string, pub ed25519.PublicKey) (*store.MonitoringToken, error)
	List(ctx context.Context) ([]store.MonitoringToken, error)
	Revoke(ctx context.Context, id string) (bool, error)
	CleanupExpired(ctx context.Context) (int64, error)
}

// requireMonitoringToken guards an opt-in route. A disabled route still answers
// 404 from next, so nothing about tokens is revealed while it is off.
func (a *App) requireMonitoringToken(route string, enabled func() bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := a.Settings()
		if !enabled() || !s.MonitoringTokenRequired {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !found {
			auth.WriteMonitoringUnauthorized(w)
			return
		}
		info, err := a.monitoring.Verify(r.Context(), token, s.IssuerPublicKey)
		switch {
		case errors.Is(err, store.ErrInvalidMonitoringToken):
			auth.WriteMonitoringUnauthorized(w)
		case err != nil:
			// Keep database details out of this endpoint.
			httpx.WriteError(w, http.StatusServiceUnavailable, "monitoring token check unavailable")
		case !slices.Contains(info.Routes, route):
			httpx.WriteError(w, http.StatusForbidden, "monitoring token does not allow this route")
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// requireMonitoringAdmin allows browser sessions only, so an automation token
// can't issue itself new access.
func requireMonitoringAdmin(w http.ResponseWriter, r *http.Request) bool {
	if requireAdmin(w, r) == nil {
		return false
	}
	if auth.CurrentAutomationToken(r) != nil {
		httpx.WriteError(w, http.StatusForbidden, "sign in to manage monitoring tokens")
		return false
	}
	return true
}

func (a *App) handleListMonitoringTokens(w http.ResponseWriter, r *http.Request) {
	if !requireMonitoringAdmin(w, r) {
		return
	}
	tokens, err := a.monitoring.List(r.Context())
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list monitoring tokens")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

func (a *App) handleCreateMonitoringToken(w http.ResponseWriter, r *http.Request) {
	if !requireMonitoringAdmin(w, r) {
		return
	}
	var body struct {
		Name    string   `json:"name"`
		Routes  []string `json:"routes"`
		TTLDays int      `json:"ttl_days"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.TTLDays == 0 {
		body.TTLDays = 90
	}
	if body.Name == "" || len(body.Name) > 100 || body.TTLDays < 1 || body.TTLDays > 3650 || len(body.Routes) == 0 {
		httpx.WriteError(w, http.StatusBadRequest, "name, at least one route, and ttl_days between 1 and 3650 are required")
		return
	}
	for _, route := range body.Routes {
		if route != signing.MonitoringRouteMetrics && route != signing.MonitoringRouteReport {
			httpx.WriteError(w, http.StatusBadRequest, "unknown monitoring route")
			return
		}
	}
	slices.Sort(body.Routes)
	body.Routes = slices.Compact(body.Routes)

	priv, _, err := signing.LoadOrCreateIssuerKeypair(a.Settings().IssuerKeyPath)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to load issuer key")
		return
	}
	token, info, err := a.monitoring.Mint(r.Context(), priv, body.Name, body.TTLDays, body.Routes)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create monitoring token")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"token": token, "monitoring_token": info})
}

func (a *App) handleRevokeMonitoringToken(w http.ResponseWriter, r *http.Request) {
	if !requireMonitoringAdmin(w, r) {
		return
	}
	revoked, err := a.monitoring.Revoke(r.Context(), r.PathValue("token_id"))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to revoke monitoring token")
		return
	}
	if !revoked {
		httpx.WriteError(w, http.StatusNotFound, "monitoring token not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
