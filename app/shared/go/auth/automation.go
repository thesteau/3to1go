package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/3to1go/shared/httpx"
)

// Automation tokens belong to the signed-in operator, not a separate account.
// Each token is a signed JWT, and only its SHA-256 hash is persisted.
type AutomationToken struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	CreatedAt string   `json:"created_at"`
	ExpiresAt string   `json:"expires_at"`
}

type AutomationStore interface {
	CreateAutomationToken(context.Context, int, string, AutomationToken) error
	ListAutomationTokens(context.Context, int) ([]AutomationToken, error)
	RevokeAutomationToken(context.Context, int, string) (bool, error)
	UserForAutomationToken(context.Context, string) (*User, *AutomationToken, error)
}

const contextKeyAutomation ContextKey = "automation"

// Route scopes allow one monitoring route and grant nothing under /api/.
const (
	ScopeMetrics = "metrics"
	ScopeReport  = "report"
)

func CurrentAutomationToken(r *http.Request) *AutomationToken {
	token, _ := r.Context().Value(contextKeyAutomation).(*AutomationToken)
	return token
}

func AutomationTokenHash(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(hash[:])
}

func RestrictedAutomation(r *http.Request) bool {
	token := CurrentAutomationToken(r)
	return token != nil && !slices.Contains(token.Scopes, "manage")
}

func automationAllows(r *http.Request, token *AutomationToken) bool {
	path := r.URL.Path
	if path == "/api/session/me" {
		return r.Method == http.MethodGet
	}
	// Managing sign-in and issuing new automation secrets requires a session.
	if strings.HasPrefix(path, "/api/session/") || path == "/api/users" || strings.HasPrefix(path, "/api/users/") || path == "/api/automation-tokens" || strings.HasPrefix(path, "/api/automation-tokens/") {
		return false
	}
	if slices.Contains(token.Scopes, "manage") {
		return true
	}
	if r.Method == http.MethodGet && slices.Contains(token.Scopes, "read") {
		switch path {
		case "/api/status", "/api/directories", "/api/directories/children", "/api/directories/browse", "/api/directories/size", "/api/restore-requests", "/api/admin/verify":
			return true
		case "/api/overview":
			return r.URL.Query().Get("section") != "settings"
		}
	}
	if r.Method == http.MethodPost && slices.Contains(token.Scopes, "backup") {
		switch path {
		case "/api/run-now", "/api/directories/force-send", "/api/cancel-operation", "/api/uploads/pause", "/api/uploads/resume", "/api/admin/uploads/pause", "/api/admin/uploads/resume":
			return true
		}
	}
	if slices.Contains(token.Scopes, "restore") {
		if r.Method == http.MethodPost {
			switch path {
			case "/api/recovery/preview", "/api/recovery/restore", "/api/restore-requests/decision":
				return true
			}
			parts := strings.Split(strings.Trim(path, "/"), "/")
			if len(parts) == 7 && parts[0] == "api" && parts[1] == "snapshots" && parts[6] == "restore" {
				return true
			}
		}
		if r.Method == http.MethodGet && strings.HasPrefix(path, "/api/snapshots/") {
			return true
		}
	}
	return false
}

// automationFromBearer checks the token's signature, then looks it up so a revoked
// token stops working. It returns no user for a token that can't be used, and an
// error only if the key or store couldn't answer.
func (a *Handler) automationFromBearer(ctx context.Context, header string) (*User, *AutomationToken, error) {
	secret, found := strings.CutPrefix(header, "Bearer ")
	s, ok := a.Store.(AutomationStore)
	if !found || !ok || a.SigningKey == nil {
		return nil, nil, nil
	}
	key, err := a.SigningKey()
	if err != nil {
		return nil, nil, err
	}
	if verifyAutomationToken(secret, key.Public().(ed25519.PublicKey)) != nil {
		return nil, nil, nil
	}
	return s.UserForAutomationToken(ctx, AutomationTokenHash(secret))
}

// RequireRouteScope guards an opt-in monitoring route while required reports true.
// Otherwise next answers, so a disabled route still returns its own 404.
func (a *Handler) RequireRouteScope(scope string, required func() bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !required() {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		user, token, err := a.automationFromBearer(r.Context(), r.Header.Get("Authorization"))
		switch {
		case err != nil:
			// Keep key and database details out of this endpoint.
			httpx.WriteError(w, http.StatusServiceUnavailable, "token check unavailable")
		case user == nil || token == nil || !user.IsAdmin || user.MustChangePassword:
			w.Header().Set("WWW-Authenticate", "Bearer")
			httpx.WriteError(w, http.StatusUnauthorized, "auth token required")
		case !slices.Contains(token.Scopes, scope):
			httpx.WriteError(w, http.StatusForbidden, "auth token does not allow this route")
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func (a *Handler) automationAdmin(w http.ResponseWriter, r *http.Request) (*User, AutomationStore) {
	u := RequireAdmin(w, r)
	if u == nil {
		return nil, nil
	}
	if CurrentAutomationToken(r) != nil {
		httpx.WriteError(w, http.StatusForbidden, "sign in to manage auth tokens")
		return nil, nil
	}
	s, ok := a.Store.(AutomationStore)
	if !ok {
		httpx.WriteError(w, http.StatusServiceUnavailable, "auth token store unavailable")
		return nil, nil
	}
	return u, s
}

func (a *Handler) ListAutomationTokens(w http.ResponseWriter, r *http.Request) {
	u, s := a.automationAdmin(w, r)
	if u == nil {
		return
	}
	tokens, err := s.ListAutomationTokens(r.Context(), u.ID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list auth tokens")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

func (a *Handler) CreateAutomationToken(w http.ResponseWriter, r *http.Request) {
	u, s := a.automationAdmin(w, r)
	if u == nil {
		return
	}
	var body struct {
		Name    string   `json:"name"`
		Scopes  []string `json:"scopes"`
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
	allowedScopes := append([]string{"read", "backup", "restore", "manage"}, a.RouteScopes...)
	if body.Name == "" || len(body.Name) > 100 || body.TTLDays < 1 || body.TTLDays > 3650 || len(body.Scopes) == 0 || len(body.Scopes) > len(allowedScopes) {
		httpx.WriteError(w, http.StatusBadRequest, "name, at least one scope, and ttl_days between 1 and 3650 are required")
		return
	}
	for _, scope := range body.Scopes {
		if !slices.Contains(allowedScopes, scope) {
			httpx.WriteError(w, http.StatusBadRequest, "unknown automation scope")
			return
		}
	}
	slices.Sort(body.Scopes)
	body.Scopes = slices.Compact(body.Scopes)
	if a.SigningKey == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable, "auth token signing unavailable")
		return
	}
	key, err := a.SigningKey()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create auth token")
		return
	}
	id, err := RandomToken()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create auth token")
		return
	}
	now := time.Now().UTC()
	expires := now.Add(time.Duration(body.TTLDays) * 24 * time.Hour)
	info := AutomationToken{ID: id[:32], Name: body.Name, Scopes: body.Scopes, CreatedAt: now.Format(time.RFC3339), ExpiresAt: expires.Format(time.RFC3339)}
	secret, err := mintAutomationToken(key, u.ID, info.ID, info.Scopes, now, expires)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create auth token")
		return
	}
	if err := s.CreateAutomationToken(r.Context(), u.ID, AutomationTokenHash(secret), info); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to save auth token")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"token": secret, "automation_token": info})
}

func (a *Handler) RevokeAutomationToken(w http.ResponseWriter, r *http.Request) {
	u, s := a.automationAdmin(w, r)
	if u == nil {
		return
	}
	revoked, err := s.RevokeAutomationToken(r.Context(), u.ID, r.PathValue("token_id"))
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to revoke auth token")
		return
	}
	if !revoked {
		httpx.WriteError(w, http.StatusNotFound, "auth token not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
