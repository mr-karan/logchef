package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backend-v2/internal/auth"
	"backend-v2/internal/config"
	"backend-v2/internal/server"
	"backend-v2/internal/service"
	"backend-v2/internal/sqlite"
	"backend-v2/pkg/logger"
)

var (
	// Build information, set by linker flags
	version     = "dev"
	commit      = "none"
	buildTime   = "unknown"
	buildString = "unknown"
)

func main() {
	// Parse command line flags
	var (
		configPath = flag.String("config", "config.toml", "path to config file")
		logLevel   = flag.String("log-level", "debug", "Log level (debug, info, warn, error)")
		env        = flag.String("env", "development", "Environment (development, production)")
	)
	flag.Parse()

	// Initialize logger
	logger.Initialize(*logLevel)
	log := logger.Default()

	// Create base context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Log startup information
	log.Info("starting server",
		"version", version,
		"commit", commit,
		"env", *env,
	)

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("failed to load config",
			"error", err,
			"path", *configPath,
		)
		os.Exit(1)
	}

	// Initialize SQLite
	log.Info("initializing sqlite",
		"path", cfg.SQLite.Path,
	)
	sqliteDB, err := sqlite.New(sqlite.Options{
		Path: cfg.SQLite.Path,
	})
	if err != nil {
		log.Error("failed to initialize sqlite", "error", err)
		os.Exit(1)
	}
	defer sqliteDB.Close()

	// Initialize auth service
	log.Info("initializing auth service",
		"provider", cfg.OIDC.ProviderURL,
	)
	authService, err := auth.NewService(cfg, sqliteDB)
	if err != nil {
		log.Error("failed to initialize auth service", "error", err)
		os.Exit(1)
	}

	// Create service
	svc := service.New(sqliteDB)

	// Initialize Clickhouse connections
	log.Info("initializing clickhouse connections")
	sources, err := sqliteDB.ListSources(context.Background())
	if err != nil {
		log.Error("failed to list sources", "error", err)
		os.Exit(1)
	}

	if len(sources) == 0 {
		log.Warn("no sources configured")
	}

	// Initialize each source
	var healthyCount int
	for _, source := range sources {
		err := svc.InitializeSource(context.Background(), source)
		if err != nil {
			log.Warn("failed to initialize source",
				"source_id", source.ID,
				"error", err,
			)
			continue
		}
		healthyCount++
	}

	log.Info("source initialization complete",
		"total", len(sources),
		"healthy", healthyCount,
	)

	// Create and start HTTP server
	srv := server.New(cfg, svc, authService, getWebFS())

	// Handle graceful shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	go func() {
		if err := srv.Start(); err != nil {
			log.Error("server error", "error", err)
			shutdown <- os.Interrupt
		}
	}()

	log.Info("server started", "address", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port))

	// Wait for shutdown signal
	<-shutdown
	log.Info("shutting down server")

	// Create shutdown context with timeout
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown server and cleanup
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("error during server shutdown", "error", err)
	}

	if err := svc.Close(); err != nil {
		log.Error("error during service cleanup", "error", err)
	}

	log.Info("shutdown complete")
}
