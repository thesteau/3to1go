package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/3to1go/edge/internal/store"
	"github.com/3to1go/edge/static"
)

// App holds all edge server state.
type App struct {
	runner    edgeRunner
	scheduler schedulerFacade
	userStore userStorer
	logger    *slog.Logger
}

// NewApp constructs the App from its dependencies.
func NewApp(
	runner edgeRunner,
	scheduler schedulerFacade,
	userStore userStorer,
	logger *slog.Logger,
) *App {
	return &App{
		runner:    runner,
		scheduler: scheduler,
		userStore: userStore,
		logger:    logger,
	}
}

// Handler builds and returns the HTTP ServeMux for the edge server.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	// Static assets
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.Files))))

	// SPA root
	mux.HandleFunc("GET /", a.handleIndex)
	mux.HandleFunc("GET /health", a.handleHealth)

	// Session (public)
	mux.HandleFunc("GET /api/session/me", a.handleSessionMe)
	mux.HandleFunc("POST /api/session/login", a.handleLogin)
	mux.HandleFunc("POST /api/session/logout", a.handleLogout)
	mux.HandleFunc("POST /api/session/change-password", a.handleChangePassword)

	// Users
	mux.HandleFunc("GET /api/users", a.handleListUsers)
	mux.HandleFunc("POST /api/users", a.handleCreateUser)
	mux.HandleFunc("PUT /api/users/{user_id}", a.handleUpdateUser)
	mux.HandleFunc("DELETE /api/users/{user_id}", a.handleDeleteUser)

	// System status + scheduler
	mux.HandleFunc("GET /api/status", a.handleStatus)
	mux.HandleFunc("POST /api/run-now", a.handleRunNow)

	// Directories + jobs
	mux.HandleFunc("GET /api/directories", a.handleListDirectories)
	mux.HandleFunc("POST /api/directories/save-job", a.handleSaveJob)
	mux.HandleFunc("POST /api/directories/delete-job", a.handleDeleteJob)
	mux.HandleFunc("POST /api/directories/force-send", a.handleForceSend)

	// Recovery
	mux.HandleFunc("POST /api/recovery/preview", a.handleRecoveryPreview)
	mux.HandleFunc("POST /api/recovery/restore", a.handleRecoveryRestore)

	// Settings
	mux.HandleFunc("GET /api/settings", a.handleGetSettings)
	mux.HandleFunc("POST /api/settings", a.handleSaveSettings)

	// Ntfy
	mux.HandleFunc("GET /api/ntfy", a.handleGetNtfy)
	mux.HandleFunc("POST /api/ntfy", a.handleSaveNtfy)
	mux.HandleFunc("POST /api/ntfy/test", a.handleTestNtfy)

	// Certificates
	mux.HandleFunc("GET /api/certificates", a.handleGetCertificates)
	mux.HandleFunc("POST /api/certificates/files", a.handleUploadCertificate)
	mux.HandleFunc("DELETE /api/certificates/files/{filename}", a.handleDeleteCertificate)

	// Hooks
	mux.HandleFunc("GET /api/hooks", a.handleGetHooks)
	mux.HandleFunc("POST /api/hooks", a.handleSaveHooks)
	mux.HandleFunc("POST /api/hooks/files", a.handleUploadHookFile)
	mux.HandleFunc("GET /api/hooks/files/{filename}", a.handleViewHookFile)
	mux.HandleFunc("DELETE /api/hooks/files/{filename}", a.handleDeleteHookFile)

	// Encryption key
	mux.HandleFunc("GET /api/encryption-key", a.handleGetEncryptionKey)

	return newRateLimiter().middleware(a.sessionMiddleware(mux))
}

func (a *App) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		cookie, _ := r.Cookie(store.SessionCookie)
		var token string
		if cookie != nil {
			token = cookie.Value
		}
		user, _ := a.userStore.UserForSession(r.Context(), token)
		ctx := context.WithValue(r.Context(), contextKeyUser, user)
		r = r.WithContext(ctx)

		if isPublicPath(path) {
			next.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(path, "/api/") {
			if user == nil {
				writeError(w, http.StatusUnauthorized, "login required")
				return
			}
			if user.MustChangePassword &&
				path != "/api/session/change-password" &&
				path != "/api/session/me" &&
				path != "/api/session/logout" {
				writeError(w, http.StatusForbidden, "password change required")
				return
			}
		}

		next.ServeHTTP(w, r)
	})
}

func isPublicPath(path string) bool {
	switch path {
	case "/api/session/me", "/api/session/login", "/api/session/logout", "/api/session/change-password":
		return true
	}
	return path == "/" ||
		strings.HasPrefix(path, "/static/") ||
		strings.HasPrefix(path, "/health")
}

type contextKey string

const contextKeyUser contextKey = "user"

func currentUser(r *http.Request) *store.User {
	u, _ := r.Context().Value(contextKeyUser).(*store.User)
	return u
}

func requireUser(w http.ResponseWriter, r *http.Request) *store.User {
	u := currentUser(r)
	if u == nil {
		writeError(w, http.StatusUnauthorized, "login required")
		return nil
	}
	return u
}

func requireAdmin(w http.ResponseWriter, r *http.Request) *store.User {
	u := requireUser(w, r)
	if u == nil {
		return nil
	}
	if !u.IsAdmin {
		writeError(w, http.StatusForbidden, "admin required")
		return nil
	}
	return u
}

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
