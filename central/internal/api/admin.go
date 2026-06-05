package api

import (
	"net/http"
	"strconv"

	"github.com/3to1go/central/internal/store"
)

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	content, err := readStaticFile("index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

func (a *App) handleSessionMe(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(store.SessionCookie)
	var token string
	if cookie != nil {
		token = cookie.Value
	}
	user, _ := a.userStore.UserForSession(r.Context(), token)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": user != nil,
		"user":          user,
	})
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, err := a.userStore.Authenticate(r.Context(), body.Username, body.Password)
	if err != nil || user == nil {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token, err := a.userStore.CreateSession(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     store.SessionCookie,
		Value:    token,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   7 * 24 * 60 * 60,
		Path:     "/",
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "user": user})
}

func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(store.SessionCookie)
	if cookie != nil {
		a.userStore.DeleteSession(r.Context(), cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:   store.SessionCookie,
		Value:  "",
		MaxAge: -1,
		Path:   "/",
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		writeError(w, http.StatusUnauthorized, "login required")
		return
	}
	var body struct {
		CurrentPassword    string `json:"current_password"`
		NewPassword        string `json:"new_password"`
		ConfirmNewPassword string `json:"confirm_new_password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.NewPassword != body.ConfirmNewPassword {
		writeError(w, http.StatusBadRequest, "new passwords do not match")
		return
	}
	updated, err := a.userStore.ChangePassword(r.Context(), user.ID, body.CurrentPassword, body.NewPassword)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "user": updated})
}

func (a *App) handleListUsers(w http.ResponseWriter, r *http.Request) {
	user := requireUser(w, r)
	if user == nil {
		return
	}
	var users []*store.User
	var err error
	if user.IsAdmin {
		users, err = a.userStore.ListUsers(r.Context())
	} else {
		users = []*store.User{user}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"users": users})
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
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(body.Password) < 5 {
		writeError(w, http.StatusBadRequest, "password must be at least 5 characters")
		return
	}
	user, err := a.userStore.CreateUser(r.Context(), body.Username, body.Password, body.IsAdmin)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "user": user})
}

func (a *App) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	currentU := requireUser(w, r)
	if currentU == nil {
		return
	}
	userID, err := strconv.Atoi(r.PathValue("user_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}

	if !currentU.IsAdmin && currentU.ID != userID {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}

	var body struct {
		Username *string `json:"username"`
		Password *string `json:"password"`
		IsAdmin  *bool   `json:"is_admin"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if !currentU.IsAdmin && body.IsAdmin != nil {
		writeError(w, http.StatusForbidden, "admin required")
		return
	}
	if currentU.ID == userID && body.IsAdmin != nil {
		writeError(w, http.StatusBadRequest, "you cannot change your own admin access")
		return
	}
	if body.Password != nil && *body.Password != "" && (!currentU.IsAdmin || currentU.ID == userID) {
		writeError(w, http.StatusForbidden, "use change password")
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

	updated, err := a.userStore.UpdateUser(r.Context(), userID, body.Username, passPtr, adminPtr, mustChangePwd)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if passPtr != nil {
		a.userStore.DeleteSessionsForUser(r.Context(), userID)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "user": updated})
}

func (a *App) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	userID, err := strconv.Atoi(r.PathValue("user_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user_id")
		return
	}
	if err := a.userStore.DeleteUser(r.Context(), userID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) handleMigrateUploadSessions(w http.ResponseWriter, r *http.Request) {
	if requireAdmin(w, r) == nil {
		return
	}
	migrated, err := a.ingest.MigrateLegacyUploadSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to migrate upload sessions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "ok",
		"migrated": migrated,
	})
}
