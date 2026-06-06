package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/3to1go/central/internal/api"
	"github.com/3to1go/central/internal/config"
	"github.com/3to1go/central/internal/ingest"
	"github.com/3to1go/central/internal/services"
	"github.com/3to1go/central/internal/storage"
	"github.com/3to1go/central/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if err := run(logger); err != nil {
		logger.Error("startup failed", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx := context.Background()

	// Load initial settings (no DB payload yet)
	settings, err := config.BuildSettings(nil)
	if err != nil {
		return fmt.Errorf("build settings: %w", err)
	}

	// Connect to PostgreSQL
	pool, err := pgxpool.New(ctx, settings.IndexDatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping postgres: %w", err)
	}

	// Initialize stores
	userStore := store.NewUserStore(pool)
	credStore := store.NewCredentialStore(pool)
	settingsStore := store.NewSettingsStore(pool)
	snapIndex := store.NewSnapshotIndex(pool)
	uploadSessionStore := ingest.NewPGSessionStore(pool)

	// Run migrations
	if err := userStore.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("user schema: %w", err)
	}
	if err := credStore.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("credential schema: %w", err)
	}
	if err := settingsStore.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("settings schema: %w", err)
	}
	if err := snapIndex.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("snapshot index schema: %w", err)
	}
	if err := uploadSessionStore.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("upload session schema: %w", err)
	}
	if err := userStore.EnsureDefaultAdmin(ctx, initialAdminPassword()); err != nil {
		return fmt.Errorf("ensure admin: %w", err)
	}

	// Load persisted settings from DB and rebuild
	payload, err := settingsStore.Load(ctx)
	if err != nil {
		logger.Warn("failed to load persisted settings, using defaults", "error", err)
	}
	if payload != nil {
		settings, err = config.BuildSettings(payload)
		if err != nil {
			logger.Warn("invalid persisted settings, using defaults", "error", err)
			settings, _ = config.BuildSettings(nil)
		}
	}

	// Adjust log level
	logLevel := parseLogLevel(settings.LogLevel)
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))

	// Storage backend
	backend := storage.NewLocalBackend(settings.BackupRoot)
	if err := os.MkdirAll(settings.BackupRoot, 0o755); err != nil {
		return fmt.Errorf("create backup root: %w", err)
	}
	if err := os.MkdirAll(settings.StagingDir, 0o755); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}

	// Services
	lockMgr := services.NewNamespaceLockManager()
	hooks := services.NewHookManager(config.HookScriptsDir(), logger)
	certs := services.NewCertManager(config.TrustedCertificatesDir())
	ntfy := services.NewNtfyPublisher(logger)

	ingestSvc, err := ingest.New(settings, backend, snapIndex, lockMgr, hooks, ntfy, uploadSessionStore)
	if err != nil {
		return fmt.Errorf("initialize ingest service: %w", err)
	}

	app := api.NewApp(
		settings, userStore, credStore, settingsStore, snapIndex,
		backend, ingestSvc, hooks, certs, ntfy, logger,
	)
	app.RestartCleanupLoop(settings.UploadCleanupIntervalS)
	app.StartCredentialCleanupLoop()

	addr := net.JoinHostPort(settings.HTTPHost, fmt.Sprint(settings.HTTPPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:      app.Handler(),
		ReadTimeout:  10 * time.Minute,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  30 * time.Second,
	}

	logger.Info("server starting", "addr", addr)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		logger.Info("shutting down server")
		app.Shutdown()
		shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}()

	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "DEBUG":
		return slog.LevelDebug
	case "WARNING", "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func initialAdminPassword() string {
	if value := os.Getenv("INITIAL_ADMIN_PASSWORD"); value != "" {
		return value
	}
	return store.DefaultAdminPassword
}
