package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backend-v2/internal/config"
	"backend-v2/internal/server"
	"backend-v2/internal/service"
	"backend-v2/internal/sqlite"
	"backend-v2/pkg/logger"
)

func main() {
	// Parse command line flags
	var (
		configPath = flag.String("config", "config.toml", "path to config file")
		logLevel   = flag.String("log-level", "debug", "Log level (debug, info, warn, error)")
		jsonLogs   = flag.Bool("json-logs", true, "Output logs in JSON format")
	)
	flag.Parse()

	// Initialize logger
	if err := logger.Initialize(logger.Config{
		Level:      *logLevel,
		JSONOutput: *jsonLogs,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	log := logger.Default()

	// Load configuration
	log.Info("loading configuration", "path", *configPath)
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Initialize SQLite
	log.Info("initializing SQLite database", "path", cfg.SQLite.Path)
	sqliteDB, err := sqlite.New(sqlite.Options{
		Path: cfg.SQLite.Path,
	})
	if err != nil {
		log.Error("failed to initialize SQLite", "error", err)
		os.Exit(1)
	}
	defer sqliteDB.Close()

	// Create service
	svc := service.New(sqliteDB)

	// Initialize Clickhouse connections
	log.Info("initializing Clickhouse connections")
	sources, err := sqliteDB.ListSources(context.Background())
	if err != nil {
		log.Error("failed to list sources", "error", err)
		os.Exit(1)
	}

	if len(sources) == 0 {
		log.Warn("no sources configured - use the API to create a source")
	}

	// Initialize each source
	var healthyCount int
	for _, source := range sources {
		err := svc.InitializeSource(context.Background(), source)
		if err != nil {
			log.Warn("failed to initialize source",
				"source_id", source.ID,
				"table_name", source.TableName,
				"error", err,
			)
			continue
		}
		healthyCount++
	}

	log.Info("source initialization complete",
		"total", len(sources),
		"healthy", healthyCount,
		"unhealthy", len(sources)-healthyCount,
	)

	// Monitor source health
	go func() {
		for health := range svc.HealthUpdates() {
			if !health.IsHealthy {
				log.Warn("source health check failed",
					"source_id", health.ID,
					"error", health.Error,
				)
			} else {
				log.Debug("source health check passed",
					"source_id", health.ID,
				)
			}
		}
	}()

	// Create and start HTTP server
	srv := server.New(cfg, svc, getWebFS())

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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
