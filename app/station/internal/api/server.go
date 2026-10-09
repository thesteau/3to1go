package api

import (
	"context"
	"crypto/ed25519"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/3to1go/shared/auth"
	"github.com/3to1go/shared/certificates"
	"github.com/3to1go/shared/hooks"
	"github.com/3to1go/shared/httpx"
	"github.com/3to1go/station/internal/config"
	"github.com/3to1go/station/internal/ingest"
	"github.com/3to1go/station/internal/services/verify"
	"github.com/3to1go/station/internal/signing"
	"github.com/3to1go/station/internal/storage"
	"github.com/3to1go/station/internal/store"
	"github.com/go-chi/chi/v5"
)

type userStorer interface {
	UserForSession(ctx context.Context, token string) (*store.User, error)
	Authenticate(ctx context.Context, username, password string) (*store.User, error)
	CreateSession(ctx context.Context, userID int) (string, error)
	DeleteSession(ctx context.Context, token string) error
	DeleteSessionsForUser(ctx context.Context, userID int) error
	DeleteOtherSessionsForUser(context.Context, int, string) error
	ListUsers(ctx context.Context) ([]*store.User, error)
	CreateUser(ctx context.Context, username, password string, isAdmin bool) (*store.User, error)
	UpdateUser(ctx context.Context, userID int, username, password *string, isAdmin, mustChangePassword *bool) (*store.User, error)
	DeleteUser(ctx context.Context, userID int) error
	ChangePassword(ctx context.Context, userID int, currentPassword, newPassword string) (*store.User, error)
}

type credStorer interface {
	Bind(context.Context, string, string, string) error
	List(context.Context) ([]store.CredentialInfo, error)
	Verify(ctx context.Context, token string, pub ed25519.PublicKey) (*store.CredentialRecord, error)
	Mint(ctx context.Context, priv ed25519.PrivateKey, ttlDays int, scopes ...signing.CredentialScope) (string, error)
	Revoke(ctx context.Context, tokenHash string) (int64, error)
	CleanupExpired(ctx context.Context) (int64, error)
}

type settingsStorer interface {
	Save(ctx context.Context, p *config.SettingsPayload) error
}

type snapIndexer interface {
	GetScoutRegistration(ctx context.Context, scoutID, instID string) (*store.ScoutRegistration, error)
	DeleteScoutRegistration(ctx context.Context, scoutID, instID string) error
	DeleteInstanceEntries(ctx context.Context, scoutID, instID string) error
	DeleteArchiveSizes(ctx context.Context, scoutID, instID string) error
	HasNamespaceEntries(ctx context.Context, scoutID, instID string) (bool, error)
	UpsertScoutRegistration(ctx context.Context, r *store.ScoutRegistration) error
	ListScoutRegistrations(ctx context.Context, scoutIDFilter *string) ([]store.ScoutRegistration, error)
	ListNamespaces(ctx context.Context) ([]store.NamespaceEntry, error)
	restoreRequestStore
}

type ingestSvc interface {
	AuthorizeUpload(context.Context, string, string) error
	StartUpload(ctx context.Context, req ingest.UploadInitRequest, sourceAddr, credHash *string, sourceTLS bool) (*ingest.SessionResponse, error)
	AppendChunk(ctx context.Context, uploadID string, offset int64, body io.Reader) (*ingest.ChunkResponse, error)
	FinalizeUpload(ctx context.Context, uploadID string) (*ingest.FinalizeResponse, error)
	ReconcileNamespace(ctx context.Context, namespace string)
	CleanupLoop(ctx context.Context, intervalSeconds int)
	UpdateSettings(settings *config.Settings)
}

type storageBackend interface {
	Healthcheck() bool
	DiskInfo() (total, used, free int64)
	List(namespace string) ([]storage.StorageFile, error)
}

type certManager interface {
	Snapshot() map[string]any
	SaveUploadedFile(filename string, content []byte) (certificates.CertFileInfo, error)
	DeleteFile(filename string) error
}

type hookManager interface {
	Snapshot(preCommand, postCommand string) map[string]any
	SaveUploadedFile(filename string, content []byte) (hooks.HookFileInfo, error)
	ReadTextFile(filename string) (string, string, error)
	DeleteFile(filename string) error
}

type ntfyPublisher interface {
	Snapshot(s *config.Settings) map[string]any
	PublishTest(cfg map[string]any) error
}

// App holds all server state.
type App struct {
	mu            sync.RWMutex
	settings      *config.Settings
	userStore     userStorer
	credStore     credStorer
	settingsStore settingsStorer
	snapIndex     snapIndexer
	backend       storageBackend
	ingest        ingestSvc
	hooks         hookManager
	certs         certManager
	ntfy          ntfyPublisher
	verify        *verify.Service
	logger        *slog.Logger

	cleanupCancel     context.CancelFunc
	credCleanupCancel context.CancelFunc
}

func NewApp(
	settings *config.Settings,
	userStore userStorer,
	credStore credStorer,
	settingsStore settingsStorer,
	snapIndex snapIndexer,
	backend storageBackend,
	ingestSvc ingestSvc,
	hooks hookManager,
	certs certManager,
	ntfy ntfyPublisher,
	verify *verify.Service,
	logger *slog.Logger,
) *App {
	return &App{
		settings:      settings,
		userStore:     userStore,
		credStore:     credStore,
		settingsStore: settingsStore,
		snapIndex:     snapIndex,
		backend:       backend,
		ingest:        ingestSvc,
		hooks:         hooks,
		certs:         certs,
		ntfy:          ntfy,
		verify:        verify,
		logger:        logger,
	}
}

func (a *App) Settings() *config.Settings {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.settings
}

func (a *App) ApplySettings(s *config.Settings) {
	a.mu.Lock()
	a.settings = s
	a.mu.Unlock()
	a.ingest.UpdateSettings(s)
}

func (a *App) RestartCleanupLoop(intervalSeconds int) {
	if a.cleanupCancel != nil {
		a.cleanupCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cleanupCancel = cancel
	go a.ingest.CleanupLoop(ctx, intervalSeconds)
}

func (a *App) StartCredentialCleanupLoop() {
	ctx, cancel := context.WithCancel(context.Background())
	a.credCleanupCancel = cancel
	go func() {
		ticker := time.NewTicker(12 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				removed, err := a.credStore.CleanupExpired(context.Background())
				if err != nil {
					a.logger.Error("station_token_cleanup_failed", "error", err)
				} else if removed > 0 {
					a.logger.Info("station_token_cleanup", "removed", removed)
				}
			}
		}
	}()
}

func (a *App) Shutdown() {
	if a.cleanupCancel != nil {
		a.cleanupCancel()
	}
	if a.credCleanupCancel != nil {
		a.credCleanupCancel()
	}
}

// Handler builds and returns the HTTP router.
func (a *App) Handler() http.Handler {
	r := chi.NewRouter()

	// Static files (app.js, app.css)
	// Send template URLs to the assembled shell.
	r.Handle("/static/", http.RedirectHandler("/", http.StatusMovedPermanently))
	r.Handle("/static/index.html", http.RedirectHandler("/", http.StatusMovedPermanently))
	r.Handle("/static/*", http.StripPrefix("/static/", staticFiles))

	// SPA root
	r.Get("/", a.handleIndex)
	r.Get("/health", a.handleHealth)
	r.Get("/health/ready", a.handleHealthReady)

	// Session (public)
	r.Get("/api/session/me", a.handleSessionMe)
	r.Post("/api/session/login", a.handleLogin)
	r.Post("/api/session/logout", a.handleLogout)
	access := &auth.Handler{Store: a.userStore, CookieName: store.SessionCookie}
	r.Post("/api/session/logout-all", access.LogoutAll)
	r.Get("/api/automation-tokens", access.ListAutomationTokens)
	r.Post("/api/automation-tokens", access.CreateAutomationToken)
	r.Delete("/api/automation-tokens/{token_id}", httpx.WithPathValues(access.RevokeAutomationToken, "token_id"))
	r.Post("/api/session/change-password", a.handleChangePassword)

	// Users (auth required)
	r.Get("/api/users", a.handleListUsers)
	r.Post("/api/users", a.handleCreateUser)
	r.Put("/api/users/{user_id}", httpx.WithPathValues(a.handleUpdateUser, "user_id"))
	r.Delete("/api/users/{user_id}", httpx.WithPathValues(a.handleDeleteUser, "user_id"))
	// Overview + settings
	r.Get("/api/overview", a.handleOverview)
	r.Post("/api/settings", a.handleSaveSettings)
	r.Post("/api/admin/uploads/pause", a.handlePauseUploads)
	r.Post("/api/admin/uploads/resume", a.handleResumeUploads)

	// Instances
	r.Delete("/api/instances/{scout_id}/{scout_instance_id}", httpx.WithPathValues(a.handleDeleteInstance, "scout_id", "scout_instance_id"))

	// Credentials
	r.Get("/api/credentials", a.handleListCredentials)
	r.Delete("/api/credentials/{token_hash}", httpx.WithPathValues(a.handleRevokeCredentialByHash, "token_hash"))
	r.Post("/api/credentials/mint", a.handleMintCredential)
	r.Delete("/api/credentials/instances/{scout_id}/{scout_instance_id}", httpx.WithPathValues(a.handleRevokeCredential, "scout_id", "scout_instance_id"))

	// Certificates
	r.Get("/api/certificates", a.handleGetCertificates)
	r.Post("/api/certificates/files", a.handleUploadCertificate)
	r.Delete("/api/certificates/files/{filename}", httpx.WithPathValues(a.handleDeleteCertificate, "filename"))

	// Verify
	r.Get("/api/admin/verify", a.handleGetVerifyStatus)
	r.Post("/api/admin/verify", a.handleRunVerify)

	// Ntfy
	r.Get("/api/ntfy", a.handleGetNtfy)
	r.Post("/api/ntfy", a.handleSaveNtfy)
	r.Post("/api/ntfy/test", a.handleTestNtfy)

	// Hooks
	r.Get("/api/hooks", a.handleGetHooks)
	r.Post("/api/hooks", a.handleSaveHooks)
	r.Post("/api/hooks/files", a.handleUploadHookFile)
	r.Get("/api/hooks/files/{filename}", httpx.WithPathValues(a.handleViewHookFile, "filename"))
	r.Delete("/api/hooks/files/{filename}", httpx.WithPathValues(a.handleDeleteHookFile, "filename"))

	// Snapshots (auth required for UI downloads)
	r.Get("/api/snapshots/{scout_id}/{scout_instance_id}/{job_name}/{filename}", httpx.WithPathValues(a.handleDownloadSnapshotForInstance, "scout_id", "scout_instance_id", "job_name", "filename"))
	r.Delete("/api/snapshots/{scout_id}/{scout_instance_id}/{job_name}/{filename}", httpx.WithPathValues(a.handleDeleteSnapshotForInstance, "scout_id", "scout_instance_id", "job_name", "filename"))
	r.Get("/api/snapshots/{scout_id}/{job_name}/{filename}", httpx.WithPathValues(a.handleDownloadSnapshot, "scout_id", "job_name", "filename"))
	r.Delete("/api/snapshots/{scout_id}/{job_name}/{filename}", httpx.WithPathValues(a.handleDeleteSnapshot, "scout_id", "job_name", "filename"))
	r.Post("/api/snapshots/{scout_id}/{scout_instance_id}/{job_name}/{filename}/restore", httpx.WithPathValues(a.handleRequestRestore, "scout_id", "scout_instance_id", "job_name", "filename"))

	// Backup uploads (Bearer JWT auth, no session)
	r.Post("/backup/uploads/initiate", a.handleInitiateUpload)
	r.Put("/backup/uploads/{upload_id}/chunk", httpx.WithPathValues(a.handleAppendChunk, "upload_id"))
	r.Post("/backup/uploads/{upload_id}/finalize", httpx.WithPathValues(a.handleFinalizeUpload, "upload_id"))

	// Recovery (Bearer JWT auth)
	r.Get("/backup/recovery/{scout_id}/{scout_instance_id}/{job_name}/latest", httpx.WithPathValues(a.handleDownloadLatest, "scout_id", "scout_instance_id", "job_name"))
	r.Get("/backup/recovery/{scout_id}/{scout_instance_id}/{job_name}/by-fingerprint", httpx.WithPathValues(a.handleDownloadByFingerprint, "scout_id", "scout_instance_id", "job_name"))
	r.Get("/backup/recovery/{scout_id}/{scout_instance_id}/{job_name}/archive/{filename}", httpx.WithPathValues(a.handleDownloadExactSnapshot, "scout_id", "scout_instance_id", "job_name", "filename"))
	r.Get("/backup/recovery/{scout_id}/{scout_instance_id}/requests/{request_id}/archive", httpx.WithPathValues(a.handleDownloadRestoreArchive, "scout_id", "scout_instance_id", "request_id"))
	r.Get("/backup/recovery/{scout_id}/{scout_instance_id}/requests", httpx.WithPathValues(a.handleListRestoreRequests, "scout_id", "scout_instance_id"))
	r.Post("/backup/recovery/{scout_id}/{scout_instance_id}/requests/{request_id}", httpx.WithPathValues(a.handleDecideRestoreRequest, "scout_id", "scout_instance_id", "request_id"))

	return a.requestLogger(httpx.NewRateLimiter(specsForPath).Middleware(a.sessionMiddleware(r)))
}

func (a *App) requestLogger(next http.Handler) http.Handler {
	return httpx.RequestLogger(a.logger, func(path string) bool {
		return strings.HasPrefix(path, "/static/") || path == "/health" || path == "/health/ready"
	}, next)
}

func (a *App) sessionMiddleware(next http.Handler) http.Handler {
	return a.accountHandler().Middleware(next, isPublicPath)
}

func isPublicPath(path string) bool {
	switch path {
	case "/api/session/me", "/api/session/login", "/api/session/logout", "/api/session/logout-all", "/api/session/change-password":
		return true
	}
	return path == "/" ||
		strings.HasPrefix(path, "/static/") ||
		strings.HasPrefix(path, "/health") ||
		strings.HasPrefix(path, "/backup/uploads/") ||
		strings.HasPrefix(path, "/backup/recovery/")
}

const contextKeyUser = auth.ContextKeyUser

func currentUser(r *http.Request) *store.User { return auth.CurrentUser(r) }

func requireUser(w http.ResponseWriter, r *http.Request) *store.User { return auth.RequireUser(w, r) }

func requireAdmin(w http.ResponseWriter, r *http.Request) *store.User { return auth.RequireAdmin(w, r) }
