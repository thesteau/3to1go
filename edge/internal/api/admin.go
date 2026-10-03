package api

import (
	"net/http"

	"github.com/3to1go/edge/internal/store"
	"github.com/3to1go/shared/auth"
	"github.com/3to1go/shared/httpx"
)

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	serveIndex(w, r)
}

func (a *App) accountHandler() *auth.Handler {
	return &auth.Handler{Store: a.userStore, CookieName: store.SessionCookie}
}

func (a *App) handleSessionMe(w http.ResponseWriter, r *http.Request) {
	a.accountHandler().SessionMe(w, r)
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) { a.accountHandler().Login(w, r) }

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) { a.accountHandler().Logout(w, r) }

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	a.accountHandler().ChangePassword(w, r)
}

func (a *App) handleListUsers(w http.ResponseWriter, r *http.Request) {
	a.accountHandler().ListUsers(w, r)
}

func (a *App) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		IsAdmin  bool   `json:"is_admin"`
	}
	if err := httpx.ReadJSON(r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := a.userStore.CreateUser(r.Context(), body.Username, body.Password, body.IsAdmin)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"status": "ok", "user": user})
}

func (a *App) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	a.accountHandler().UpdateUser(w, r)
}

func (a *App) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	a.accountHandler().DeleteUser(w, r)
}
