package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/3to1go/edge/internal/api"
	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/services/certificates"
	"github.com/3to1go/edge/internal/services/runner"
	"github.com/3to1go/edge/internal/services/scheduler"
	"github.com/3to1go/edge/internal/services/state"
	"github.com/3to1go/edge/internal/store"
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

	// Load initial settings (env only, no DB payload yet).
	settings, err := config.BuildSettings(nil)
	if err != nil {
		return fmt.Errorf("build settings: %w", err)
	}

	// Open SQLite database.
	dbPath := config.AppDatabasePath()
	db, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	// Initialise stores.
	userStore := store.NewUserStore(db)
	settingsStore := store.NewSettingsStore(db)
	stateStore := state.NewStateStore(db)

	if err := userStore.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("user schema: %w", err)
	}
	if err := settingsStore.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("settings schema: %w", err)
	}
	if err := stateStore.EnsureSchema(ctx); err != nil {
		return fmt.Errorf("state schema: %w", err)
	}
	if err := userStore.EnsureDefaultAdmin(ctx, initialAdminPassword()); err != nil {
		return fmt.Errorf("ensure admin: %w", err)
	}

	// Load persisted settings from DB, then merge env overrides.
	payload, err := settingsStore.Load(ctx)
	if err != nil {
		logger.Warn("failed to load persisted settings, using defaults", "error", err)
	}
	if payload != nil {
		if rebuilt, err := config.BuildSettings(payload); err != nil {
			logger.Warn("invalid persisted settings, using defaults", "error", err)
		} else {
			settings = rebuilt
		}
	}

	// Adjust log level now that settings are known.
	logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: parseLogLevel(settings.LogLevel)}))

	// Migrate job state from the legacy JSON file (no-op if already done or not present).
	if err := stateStore.MigrateFromFile(filepath.Join(settings.StateDir, "edge-state.json")); err != nil {
		logger.Warn("state migration from file failed", "error", err)
	}

	// Certificate manager (needed before runner for TLS transport).
	certMgr := certificates.NewCertManager(config.TrustedCertificatesDir())

	// Build the runner.
	edgeRunner, err := runner.NewEdgeRunner(settings, logger, certMgr, stateStore)
	if err != nil {
		return fmt.Errorf("init runner: %w", err)
	}

	// Build the scheduler.
	sched, err := scheduler.NewSchedulerController(edgeRunner)
	if err != nil {
		return fmt.Errorf("init scheduler: %w", err)
	}
	sched.Start()

	// Build the HTTP app.
	app := api.NewApp(edgeRunner, sched, userStore, settingsStore, logger)

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

	logger.Info("server starting", "addr", addr, "edge_id", settings.EdgeID)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-quit
		logger.Info("shutting down server")
		sched.Stop()
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
