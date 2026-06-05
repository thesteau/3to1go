package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/services"
	"github.com/3to1go/central/internal/storage"
	"github.com/3to1go/central/internal/store"
	"github.com/3to1go/central/static"
)

// App holds all server state.
type App struct {
	mu            sync.RWMutex
	settings      *config.Settings
	userStore     userStorer
	credStore     credStorer
	settingsStore settingsStorer
	snapIndex     snapIndexer
	backend       *storage.LocalBackend
	ingest        ingestSvc
	hooks         *services.HookManager
	certs         *services.CertManager
	ntfy          *services.NtfyPublisher
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
	backend *storage.LocalBackend,
	ingestSvc ingestSvc,
	hooks *services.HookManager,
	certs *services.CertManager,
	ntfy *services.NtfyPublisher,
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

// Handler builds and returns the HTTP ServeMux.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	// Static files (app.js, app.css)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static.Files))))

	// SPA root
	mux.HandleFunc("GET /", a.handleIndex)
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /health/ready", a.handleHealthReady)

	// Session (public)
	mux.HandleFunc("GET /api/session/me", a.handleSessionMe)
	mux.HandleFunc("POST /api/session/login", a.handleLogin)
	mux.HandleFunc("POST /api/session/logout", a.handleLogout)
	mux.HandleFunc("POST /api/session/change-password", a.handleChangePassword)

	// Users (auth required)
	mux.HandleFunc("GET /api/users", a.handleListUsers)
	mux.HandleFunc("POST /api/users", a.handleCreateUser)
	mux.HandleFunc("PUT /api/users/{user_id}", a.handleUpdateUser)
	mux.HandleFunc("DELETE /api/users/{user_id}", a.handleDeleteUser)

	// Overview + settings
	mux.HandleFunc("GET /api/overview", a.handleOverview)
	mux.HandleFunc("POST /api/settings", a.handleSaveSettings)

	// Instances
	mux.HandleFunc("DELETE /api/instances/{edge_id}/{edge_instance_id}", a.handleDeleteInstance)

	// Credentials
	mux.HandleFunc("POST /api/credentials/mint", a.handleMintCredential)
	mux.HandleFunc("DELETE /api/credentials/instances/{edge_id}/{edge_instance_id}", a.handleRevokeCredential)

	// Certificates
	mux.HandleFunc("GET /api/certificates", a.handleGetCertificates)
	mux.HandleFunc("POST /api/certificates/files", a.handleUploadCertificate)
	mux.HandleFunc("DELETE /api/certificates/files/{filename}", a.handleDeleteCertificate)

	// Ntfy
	mux.HandleFunc("GET /api/ntfy", a.handleGetNtfy)
	mux.HandleFunc("POST /api/ntfy", a.handleSaveNtfy)
	mux.HandleFunc("POST /api/ntfy/test", a.handleTestNtfy)

	// Hooks
	mux.HandleFunc("GET /api/hooks", a.handleGetHooks)
	mux.HandleFunc("POST /api/hooks", a.handleSaveHooks)
	mux.HandleFunc("POST /api/hooks/files", a.handleUploadHookFile)
	mux.HandleFunc("GET /api/hooks/files/{filename}", a.handleViewHookFile)
	mux.HandleFunc("DELETE /api/hooks/files/{filename}", a.handleDeleteHookFile)

	// Snapshots (auth required for UI downloads)
	mux.HandleFunc("GET /api/snapshots/{edge_id}/{edge_instance_id}/{job_name}/{filename}", a.handleDownloadSnapshotForInstance)
	mux.HandleFunc("DELETE /api/snapshots/{edge_id}/{edge_instance_id}/{job_name}/{filename}", a.handleDeleteSnapshotForInstance)
	mux.HandleFunc("GET /api/snapshots/{edge_id}/{job_name}/{filename}", a.handleDownloadSnapshot)
	mux.HandleFunc("DELETE /api/snapshots/{edge_id}/{job_name}/{filename}", a.handleDeleteSnapshot)

	// Backup uploads (Bearer JWT auth, no session)
	mux.HandleFunc("POST /backup/uploads/initiate", a.handleInitiateUpload)
	mux.HandleFunc("PUT /backup/uploads/{upload_id}/chunk", a.handleAppendChunk)
	mux.HandleFunc("POST /backup/uploads/{upload_id}/finalize", a.handleFinalizeUpload)

	// Recovery (Bearer JWT auth)
	mux.HandleFunc("GET /backup/recovery/{edge_id}/{edge_instance_id}/{job_name}/latest", a.handleDownloadLatest)
	mux.HandleFunc("GET /backup/recovery/{edge_id}/{edge_instance_id}/{job_name}/by-fingerprint", a.handleDownloadByFingerprint)

	return a.sessionMiddleware(mux)
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
