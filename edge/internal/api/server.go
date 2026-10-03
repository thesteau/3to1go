package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/services/directories"
	"github.com/3to1go/edge/internal/store"
	"github.com/3to1go/shared/auth"
	"github.com/3to1go/shared/httpx"
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
	ClearStagedBackup(relativePath string) error
	CancelOperation() bool
	BrowseFiles(relativePath string) ([]directories.FileEntry, error)
	DirectoryChildren(relativePath string) ([]directories.DirectoryNode, error)
	ExcludePath(relativePath string) error
	FolderSize(ctx context.Context, relativePath string) (map[string]any, error)

	StartForceSendAsync(relativePath string) (map[string]any, error)
	PreviewRecovery(ctx context.Context, relativePath, fingerprint string) (map[string]any, error)
	RecoverJob(ctx context.Context, relativePath, fingerprint string) (map[string]any, error)
}

type schedulerFacade interface {
	Snapshot() map[string]any
	RequestRunNow() string
	ReloadSettings(cronSchedule string) error
}

type settingsStorer interface {
	Save(ctx context.Context, payload *config.SettingsPayload) error
}

// App holds all edge server state.
type App struct {
	runner        edgeRunner
	scheduler     schedulerFacade
	userStore     userStorer
	settingsStore settingsStorer
	logger        *slog.Logger
}

// NewApp constructs the App from its dependencies.
func NewApp(
	runner edgeRunner,
	scheduler schedulerFacade,
	userStore userStorer,
	settingsStore settingsStorer,
	logger *slog.Logger,
) *App {
	return &App{
		runner:        runner,
		scheduler:     scheduler,
		userStore:     userStore,
		settingsStore: settingsStore,
		logger:        logger,
	}
}

// Handler builds and returns the HTTP router for the edge server.
func (a *App) Handler() http.Handler {
	r := chi.NewRouter()

	// Static assets
	// Send template URLs to the assembled shell.
	r.Handle("/static/", http.RedirectHandler("/", http.StatusMovedPermanently))
	r.Handle("/static/index.html", http.RedirectHandler("/", http.StatusMovedPermanently))
	r.Handle("/static/*", http.StripPrefix("/static/", staticFiles))

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
	r.Put("/api/users/{user_id}", httpx.WithPathValues(a.handleUpdateUser, "user_id"))
	r.Delete("/api/users/{user_id}", httpx.WithPathValues(a.handleDeleteUser, "user_id"))

	// System status + scheduler
	r.Get("/api/status", a.handleStatus)
	r.Post("/api/run-now", a.handleRunNow)
	r.Post("/api/uploads/pause", a.handlePauseUploads)
	r.Post("/api/uploads/resume", a.handleResumeUploads)

	// Directories + jobs
	r.Get("/api/directories", a.handleListDirectories)
	r.Post("/api/directories/save-job", a.handleSaveJob)
	r.Post("/api/directories/delete-job", a.handleDeleteJob)
	r.Post("/api/directories/force-send", a.handleForceSend)
	r.Post("/api/directories/clear-staged", a.handleClearStaged)
	r.Post("/api/cancel-operation", a.handleCancelOperation)
	r.Get("/api/directories/children", a.handleDirectoryChildren)
	r.Get("/api/directories/browse", a.handleBrowseFiles)
	r.Get("/api/directories/size", a.handleFolderSize)
	r.Post("/api/directories/exclude", a.handleExcludePath)

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
	r.Delete("/api/certificates/files/{filename}", httpx.WithPathValues(a.handleDeleteCertificate, "filename"))

	// Hooks
	r.Get("/api/hooks", a.handleGetHooks)
	r.Post("/api/hooks", a.handleSaveHooks)
	r.Post("/api/hooks/files", a.handleUploadHookFile)
	r.Get("/api/hooks/files/{filename}", httpx.WithPathValues(a.handleViewHookFile, "filename"))
	r.Delete("/api/hooks/files/{filename}", httpx.WithPathValues(a.handleDeleteHookFile, "filename"))

	// Encryption key
	r.Get("/api/encryption-key", a.handleGetEncryptionKey)
	r.Post("/api/encryption-key/rotate", a.handleRotateEncryptionKey)

	return a.requestLogger(httpx.NewRateLimiter(specsForPath).Middleware(a.sessionMiddleware(r)))
}

func (a *App) requestLogger(next http.Handler) http.Handler {
	return httpx.RequestLogger(a.logger, func(path string) bool { return strings.HasPrefix(path, "/static/") || path == "/health" }, next)
}

func (a *App) sessionMiddleware(next http.Handler) http.Handler {
	return a.accountHandler().Middleware(next, isPublicPath)
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

func requireUser(w http.ResponseWriter, r *http.Request) *store.User { return auth.RequireUser(w, r) }

func requireAdmin(w http.ResponseWriter, r *http.Request) *store.User { return auth.RequireAdmin(w, r) }

func (a *App) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
