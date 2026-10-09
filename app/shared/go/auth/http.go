package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/3to1go/shared/buildinfo"
	"github.com/3to1go/shared/httpx"
)

func (a *Handler) SessionMe(w http.ResponseWriter, r *http.Request) {
	if automation := CurrentAutomationToken(r); automation != nil {
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"authenticated": true, "user": CurrentUser(r), "automation_token": automation})
		return
	}
	cookie, _ := r.Cookie(a.CookieName)
	var token string
	if cookie != nil {
		token = cookie.Value
	}
	user, _ := a.Store.UserForSession(r.Context(), token)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"authenticated": user != nil,
		"user":          user,
	})
}

func (a *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := a.Store.Authenticate(r.Context(), body.Username, body.Password)
	if err != nil || user == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token, err := a.Store.CreateSession(r.Context(), user.ID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.CookieName,
		Value:    token,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 60 * 60,
		Path:     "/",
		Secure:   httpx.SessionCookieSecure(),
	})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "user": user})
}

func (a *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(a.CookieName)
	if cookie != nil {
		// Sign-out always succeeds for the browser; a session left behind expires on its own.
		if err := a.Store.DeleteSession(r.Context(), cookie.Value); err != nil {
			slog.Warn("session_delete_failed", "error", err)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     a.CookieName,
		Value:    "",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Path:     "/",
		Secure:   httpx.SessionCookieSecure(),
	})
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// LogoutAll invalidates browser sessions; automation tokens are revoked separately.
func (a *Handler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	user := RequireUser(w, r)
	if user == nil {
		return
	}
	if err := a.Store.DeleteSessionsForUser(r.Context(), user.ID); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to sign out browser sessions")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: a.CookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: httpx.SessionCookieSecure()})
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	if user == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "login required")
		return
	}
	var body struct {
		CurrentPassword    string `json:"current_password"`
		NewPassword        string `json:"new_password"`
		ConfirmNewPassword string `json:"confirm_new_password"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.NewPassword != body.ConfirmNewPassword {
		httpx.WriteError(w, http.StatusBadRequest, "new passwords do not match")
		return
	}
	updated, err := a.Store.ChangePassword(r.Context(), user.ID, body.CurrentPassword, body.NewPassword)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	keep := ""
	if cookie, _ := r.Cookie(a.CookieName); cookie != nil {
		keep = cookie.Value
	}
	if err := a.Store.DeleteOtherSessionsForUser(r.Context(), user.ID, keep); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "password changed, but other browser sessions could not be signed out")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "user": updated})
}

func (a *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	user := RequireUser(w, r)
	if user == nil {
		return
	}
	var users []*User
	var err error
	if user.IsAdmin {
		users, err = a.Store.ListUsers(r.Context())
	} else {
		users = []*User{user}
	}
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	// Admin shows the running version.
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"users": users, "build": buildinfo.Fields()})
}

func (a *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	currentU := RequireUser(w, r)
	if currentU == nil {
		return
	}
	userID, err := strconv.Atoi(r.PathValue("user_id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	if !currentU.IsAdmin && currentU.ID != userID {
		httpx.WriteError(w, http.StatusForbidden, "admin required")
		return
	}

	var body struct {
		Username *string `json:"username"`
		Password *string `json:"password"`
		IsAdmin  *bool   `json:"is_admin"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !currentU.IsAdmin && body.IsAdmin != nil {
		httpx.WriteError(w, http.StatusForbidden, "admin required")
		return
	}
	if currentU.ID == userID && body.IsAdmin != nil {
		httpx.WriteError(w, http.StatusBadRequest, "you cannot change your own admin access")
		return
	}
	if body.Password != nil && *body.Password != "" && (!currentU.IsAdmin || currentU.ID == userID) {
		httpx.WriteError(w, http.StatusForbidden, "use change password")
		return
	}

	var adminPtr *bool
	if currentU.IsAdmin {
		adminPtr = body.IsAdmin
	}

	var passPtr *string
	var mustChangePwd *bool
	if body.Password != nil && *body.Password != "" && currentU.IsAdmin {
		passPtr = body.Password
		t := true
		mustChangePwd = &t
	}

	updated, err := a.Store.UpdateUser(r.Context(), userID, body.Username, passPtr, adminPtr, mustChangePwd)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if passPtr != nil {
		if err := a.Store.DeleteSessionsForUser(r.Context(), userID); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "password changed, but existing sessions could not be signed out")
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "user": updated})
}

func (a *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	if RequireAdmin(w, r) == nil {
		return
	}
	userID, err := strconv.Atoi(r.PathValue("user_id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	if err := a.Store.DeleteUser(r.Context(), userID); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type Store interface {
	UserForSession(context.Context, string) (*User, error)
	Authenticate(context.Context, string, string) (*User, error)
	CreateSession(context.Context, int) (string, error)
	DeleteSession(context.Context, string) error
	ChangePassword(context.Context, int, string, string) (*User, error)
	ListUsers(context.Context) ([]*User, error)
	CreateUser(context.Context, string, string, bool) (*User, error)
	UpdateUser(context.Context, int, *string, *string, *bool, *bool) (*User, error)
	DeleteSessionsForUser(context.Context, int) error
	DeleteOtherSessionsForUser(context.Context, int, string) error
	DeleteUser(context.Context, int) error
}
type Handler struct {
	Store      Store
	CookieName string
}
