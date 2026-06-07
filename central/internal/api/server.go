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

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/ingest"
	"github.com/3to1go/central/internal/services/certificates"
	"github.com/3to1go/central/internal/services/hooks"
	"github.com/3to1go/central/internal/services/verify"
	"github.com/3to1go/central/internal/signing"
	"github.com/3to1go/central/internal/storage"
	"github.com/3to1go/central/internal/store"
	"github.com/3to1go/central/static"
	"github.com/go-chi/chi/v5"
)

type userStorer interface {
	UserForSession(ctx context.Context, token string) (*store.User, error)
	Authenticate(ctx context.Context, username, password string) (*store.User, error)
	CreateSession(ctx context.Context, userID int) (string, error)
	DeleteSession(ctx context.Context, token string) error
	DeleteSessionsForUser(ctx context.Context, userID int) error
	ListUsers(ctx context.Context) ([]*store.User, error)
	CreateUser(ctx context.Context, username, password string, isAdmin bool) (*store.User, error)
	UpdateUser(ctx context.Context, userID int, username, password *string, isAdmin, mustChangePassword *bool) (*store.User, error)
	DeleteUser(ctx context.Context, userID int) error
	ChangePassword(ctx context.Context, userID int, currentPassword, newPassword string) (*store.User, error)
}

type credStorer interface {
	Verify(ctx context.Context, token string, pub ed25519.PublicKey) (*store.CredentialRecord, error)
	Mint(ctx context.Context, priv ed25519.PrivateKey, ttlDays int, scopes ...signing.CredentialScope) (string, error)
	Revoke(ctx context.Context, tokenHash string) (int64, error)
	CleanupExpired(ctx context.Context) (int64, error)
}

type settingsStorer interface {
	Save(ctx context.Context, p *config.SettingsPayload) error
}

type snapIndexer interface {
	GetEdgeRegistration(ctx context.Context, edgeID, instID string) (*store.EdgeRegistration, error)
	DeleteEdgeRegistration(ctx context.Context, edgeID, instID string) error
	DeleteInstanceEntries(ctx context.Context, edgeID, instID string) error
	HasNamespaceEntries(ctx context.Context, edgeID, instID string) (bool, error)
	UpsertEdgeRegistration(ctx context.Context, r *store.EdgeRegistration) error
	ListEdgeRegistrations(ctx context.Context, edgeIDFilter *string) ([]store.EdgeRegistration, error)
	ListNamespaces(ctx context.Context) ([]store.NamespaceEntry, error)
}

type ingestSvc interface {
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
					a.logger.Error("credential_cleanup_failed", "error", err)
				} else if removed > 0 {
					a.logger.Info("credential_cleanup", "removed", removed)
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
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(static.Files))))

	// SPA root
	r.Get("/", a.handleIndex)
	r.Get("/health", a.handleHealth)
	r.Get("/health/ready", a.handleHealthReady)

	// Session (public)
	r.Get("/api/session/me", a.handleSessionMe)
	r.Post("/api/session/login", a.handleLogin)
	r.Post("/api/session/logout", a.handleLogout)
	r.Post("/api/session/change-password", a.handleChangePassword)

	// Users (auth required)
	r.Get("/api/users", a.handleListUsers)
	r.Post("/api/users", a.handleCreateUser)
	r.Put("/api/users/{user_id}", withPathValues(a.handleUpdateUser, "user_id"))
	r.Delete("/api/users/{user_id}", withPathValues(a.handleDeleteUser, "user_id"))
	// Overview + settings
	r.Get("/api/overview", a.handleOverview)
	r.Post("/api/settings", a.handleSaveSettings)
	r.Post("/api/admin/uploads/pause", a.handlePauseUploads)
	r.Post("/api/admin/uploads/resume", a.handleResumeUploads)

	// Instances
	r.Delete("/api/instances/{edge_id}/{edge_instance_id}", withPathValues(a.handleDeleteInstance, "edge_id", "edge_instance_id"))

	// Credentials
	r.Post("/api/credentials/mint", a.handleMintCredential)
	r.Delete("/api/credentials/instances/{edge_id}/{edge_instance_id}", withPathValues(a.handleRevokeCredential, "edge_id", "edge_instance_id"))

	// Certificates
	r.Get("/api/certificates", a.handleGetCertificates)
	r.Post("/api/certificates/files", a.handleUploadCertificate)
	r.Delete("/api/certificates/files/{filename}", withPathValues(a.handleDeleteCertificate, "filename"))

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
	r.Get("/api/hooks/files/{filename}", withPathValues(a.handleViewHookFile, "filename"))
	r.Delete("/api/hooks/files/{filename}", withPathValues(a.handleDeleteHookFile, "filename"))

	// Snapshots (auth required for UI downloads)
	r.Get("/api/snapshots/{edge_id}/{edge_instance_id}/{job_name}/{filename}", withPathValues(a.handleDownloadSnapshotForInstance, "edge_id", "edge_instance_id", "job_name", "filename"))
	r.Delete("/api/snapshots/{edge_id}/{edge_instance_id}/{job_name}/{filename}", withPathValues(a.handleDeleteSnapshotForInstance, "edge_id", "edge_instance_id", "job_name", "filename"))
	r.Get("/api/snapshots/{edge_id}/{job_name}/{filename}", withPathValues(a.handleDownloadSnapshot, "edge_id", "job_name", "filename"))
	r.Delete("/api/snapshots/{edge_id}/{job_name}/{filename}", withPathValues(a.handleDeleteSnapshot, "edge_id", "job_name", "filename"))

	// Backup uploads (Bearer JWT auth, no session)
	r.Post("/backup/uploads/initiate", a.handleInitiateUpload)
	r.Put("/backup/uploads/{upload_id}/chunk", withPathValues(a.handleAppendChunk, "upload_id"))
	r.Post("/backup/uploads/{upload_id}/finalize", withPathValues(a.handleFinalizeUpload, "upload_id"))

	// Recovery (Bearer JWT auth)
	r.Get("/backup/recovery/{edge_id}/{edge_instance_id}/{job_name}/latest", withPathValues(a.handleDownloadLatest, "edge_id", "edge_instance_id", "job_name"))
	r.Get("/backup/recovery/{edge_id}/{edge_instance_id}/{job_name}/by-fingerprint", withPathValues(a.handleDownloadByFingerprint, "edge_id", "edge_instance_id", "job_name"))

	return a.requestLogger(newRateLimiter().middleware(a.sessionMiddleware(r)))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (a *App) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/static/") || path == "/health" || path == "/health/ready" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		level := slog.LevelDebug
		if r.Method != http.MethodGet {
			level = slog.LevelInfo
		}
		a.logger.Log(r.Context(), level, "request",
			"method", r.Method,
			"path", path,
			"status", rec.status,
			"ms", time.Since(start).Milliseconds(),
		)
	})
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
			if user.MustChangePassword {
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
		strings.HasPrefix(path, "/health") ||
		strings.HasPrefix(path, "/backup/uploads/") ||
		strings.HasPrefix(path, "/backup/recovery/")
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
