package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/store"
	"github.com/3to1go/edge/static"
	"github.com/go-chi/chi/v5"
)

type userStorer interface {
	EnsureSchema(ctx context.Context) error
	Authenticate(ctx context.Context, username, password string) (*store.User, error)
	CreateSession(ctx context.Context, userID int) (string, error)
	DeleteSession(ctx context.Context, token string) error
	DeleteSessionsForUser(ctx context.Context, userID int) error
	UserForSession(ctx context.Context, token string) (*store.User, error)
	ListUsers(ctx context.Context) ([]*store.User, error)
	GetUserByID(ctx context.Context, id int) (*store.User, error)
	CreateUser(ctx context.Context, username, password string, isAdmin bool) (*store.User, error)
	UpdateUser(ctx context.Context, userID int, username, password *string, isAdmin, mustChangePassword *bool) (*store.User, error)
	DeleteUser(ctx context.Context, userID int) error
	ChangePassword(ctx context.Context, userID int, currentPassword, newPassword string) (*store.User, error)
}

type edgeRunner interface {
	CurrentSettings() *config.Settings
	UpdateSettings(s *config.Settings) error
	EncryptionKeyFingerprint() string
	EncryptionKeyBase64() string
	RotateEncryptionKey() (string, error)

	StatusSnapshot() map[string]any
	DirectoriesSnapshot() map[string]any

	NtfySnapshot(cfg *config.Settings) map[string]any
	TestNtfy(ntfyURL, ntfyTopic, messageTemplate string) error

	CertSnapshot() map[string]any
	SaveCertFile(filename string, content []byte) (any, error)
	DeleteCertFile(filename string) error

	HookSnapshot(preCmd, postCmd string) map[string]any
	SaveHookFile(filename string, content []byte) (any, error)
	ReadHookFile(filename string) (string, string, error)
	DeleteHookFile(filename string) error

	SaveJob(relativePath string, cfg map[string]any) (any, error)
	DeleteJob(relativePath string) error

	ForceSendJob(ctx context.Context, jobName string) (map[string]any, error)
	PreviewRecovery(ctx context.Context, relativePath, fingerprint string) (map[string]any, error)
	RecoverJob(ctx context.Context, relativePath, fingerprint string) (map[string]any, error)
}

type schedulerFacade interface {
	Snapshot() map[string]any
	RequestRunNow() string
	ReloadSettings(cronSchedule string) error
}

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

// Handler builds and returns the HTTP router for the edge server.
func (a *App) Handler() http.Handler {
	r := chi.NewRouter()

	// Static assets
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(static.Files))))

	// SPA root
	r.Get("/", a.handleIndex)
	r.Get("/health", a.handleHealth)

	// Session (public)
	r.Get("/api/session/me", a.handleSessionMe)
	r.Post("/api/session/login", a.handleLogin)
	r.Post("/api/session/logout", a.handleLogout)
	r.Post("/api/session/change-password", a.handleChangePassword)

	// Users
	r.Get("/api/users", a.handleListUsers)
	r.Post("/api/users", a.handleCreateUser)
	r.Put("/api/users/{user_id}", withPathValues(a.handleUpdateUser, "user_id"))
	r.Delete("/api/users/{user_id}", withPathValues(a.handleDeleteUser, "user_id"))

	// System status + scheduler
	r.Get("/api/status", a.handleStatus)
	r.Post("/api/run-now", a.handleRunNow)

	// Directories + jobs
	r.Get("/api/directories", a.handleListDirectories)
	r.Post("/api/directories/save-job", a.handleSaveJob)
	r.Post("/api/directories/delete-job", a.handleDeleteJob)
	r.Post("/api/directories/force-send", a.handleForceSend)

	// Recovery
	r.Post("/api/recovery/preview", a.handleRecoveryPreview)
	r.Post("/api/recovery/restore", a.handleRecoveryRestore)

	// Settings
	r.Get("/api/settings", a.handleGetSettings)
	r.Post("/api/settings", a.handleSaveSettings)

	// Ntfy
	r.Get("/api/ntfy", a.handleGetNtfy)
	r.Post("/api/ntfy", a.handleSaveNtfy)
	r.Post("/api/ntfy/test", a.handleTestNtfy)

	// Certificates
	r.Get("/api/certificates", a.handleGetCertificates)
	r.Post("/api/certificates/files", a.handleUploadCertificate)
	r.Delete("/api/certificates/files/{filename}", withPathValues(a.handleDeleteCertificate, "filename"))

	// Hooks
	r.Get("/api/hooks", a.handleGetHooks)
	r.Post("/api/hooks", a.handleSaveHooks)
	r.Post("/api/hooks/files", a.handleUploadHookFile)
	r.Get("/api/hooks/files/{filename}", withPathValues(a.handleViewHookFile, "filename"))
	r.Delete("/api/hooks/files/{filename}", withPathValues(a.handleDeleteHookFile, "filename"))

	// Encryption key
	r.Get("/api/encryption-key", a.handleGetEncryptionKey)
	r.Post("/api/encryption-key/rotate", a.handleRotateEncryptionKey)

	return newRateLimiter().middleware(a.sessionMiddleware(r))
}

func withPathValues(next http.HandlerFunc, names ...string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		for _, name := range names {
			r.SetPathValue(name, chi.URLParam(r, name))
		}
		next(w, r)
	}
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
