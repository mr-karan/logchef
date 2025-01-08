package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backend-v2/internal/config"
	"backend-v2/internal/server"
	"backend-v2/internal/service"
	"backend-v2/internal/sqlite"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "config.toml", "path to config file")
	flag.Parse()

	// Initialize structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey {
				return slog.Attr{
					Key:   a.Key,
					Value: slog.StringValue(a.Value.Time().Format(time.RFC3339)),
				}
			}
			return a
		},
	}))
	slog.SetDefault(logger)

	// Load configuration
	logger.Info("loading configuration", "path", *configPath)
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Initialize SQLite
	logger.Info("initializing SQLite database", "path", cfg.SQLite.Path)
	sqliteDB, err := sqlite.New(sqlite.Options{
		Path: cfg.SQLite.Path,
	})
	if err != nil {
		logger.Error("failed to initialize SQLite", "error", err)
		os.Exit(1)
	}
	defer sqliteDB.Close()

	// Create service
	svc := service.New(sqliteDB)

	// Initialize Clickhouse connections
	logger.Info("initializing Clickhouse connections")
	sources, err := sqliteDB.ListSources(context.Background())
	if err != nil {
		logger.Error("failed to list sources", "error", err)
		os.Exit(1)
	}

	if len(sources) == 0 {
		logger.Warn("no sources configured - use the API to create a source")
	}

	// Initialize each source
	var healthyCount int
	for _, source := range sources {
		err := svc.InitializeSource(context.Background(), source)
		if err != nil {
			logger.Warn("failed to initialize source",
				"source_id", source.ID,
				"source_name", source.Name,
				"error", err,
			)
			continue
		}
		healthyCount++
	}

	logger.Info("source initialization complete",
		"total", len(sources),
		"healthy", healthyCount,
		"unhealthy", len(sources)-healthyCount,
	)

	// Monitor source health
	go func() {
		for health := range svc.HealthUpdates() {
			if !health.IsHealthy {
				logger.Warn("source health check failed",
					"source_id", health.ID,
					"error", health.Error,
				)
			} else {
				logger.Debug("source health check passed",
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
			logger.Error("server error", "error", err)
			shutdown <- os.Interrupt
		}
	}()

	logger.Info("server started", "address", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port))

	// Wait for shutdown signal
	<-shutdown
	logger.Info("shutting down server")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown server and cleanup
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("error during server shutdown", "error", err)
	}

	if err := svc.Close(); err != nil {
		logger.Error("error during service cleanup", "error", err)
	}

	logger.Info("shutdown complete")
}
