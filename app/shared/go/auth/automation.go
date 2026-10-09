package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/3to1go/shared/httpx"
)

// Automation tokens belong to the signed-in operator, not a separate account.
// Only the SHA-256 hash of the bearer secret is persisted.
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

func (a *Handler) automationAdmin(w http.ResponseWriter, r *http.Request) (*User, AutomationStore) {
	u := RequireAdmin(w, r)
	if u == nil {
		return nil, nil
	}
	if CurrentAutomationToken(r) != nil {
		httpx.WriteError(w, http.StatusForbidden, "sign in to manage automation tokens")
		return nil, nil
	}
	s, ok := a.Store.(AutomationStore)
	if !ok {
		httpx.WriteError(w, http.StatusServiceUnavailable, "automation token store unavailable")
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
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list automation tokens")
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
	if body.Name == "" || len(body.Name) > 100 || body.TTLDays < 1 || body.TTLDays > 3650 || len(body.Scopes) == 0 || len(body.Scopes) > 4 {
		httpx.WriteError(w, http.StatusBadRequest, "name, at least one scope, and ttl_days between 1 and 3650 are required")
		return
	}
	for _, scope := range body.Scopes {
		if !slices.Contains([]string{"read", "backup", "restore", "manage"}, scope) {
			httpx.WriteError(w, http.StatusBadRequest, "unknown automation scope")
			return
		}
	}
	slices.Sort(body.Scopes)
	body.Scopes = slices.Compact(body.Scopes)
	secret, err := RandomToken()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create automation token")
		return
	}
	id, err := RandomToken()
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create automation token")
		return
	}
	secret = "3to1go_api_" + secret
	now := time.Now().UTC()
	info := AutomationToken{ID: id[:32], Name: body.Name, Scopes: body.Scopes, CreatedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(time.Duration(body.TTLDays) * 24 * time.Hour).Format(time.RFC3339)}
	if err := s.CreateAutomationToken(r.Context(), u.ID, AutomationTokenHash(secret), info); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to save automation token")
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
		httpx.WriteError(w, http.StatusInternalServerError, "failed to revoke automation token")
		return
	}
	if !revoked {
		httpx.WriteError(w, http.StatusNotFound, "automation token not found")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}
