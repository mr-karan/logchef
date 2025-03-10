package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/identity"
	"github.com/mr-karan/logchef/internal/logs"
	"github.com/mr-karan/logchef/internal/server"
	"github.com/mr-karan/logchef/internal/source"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/logger"
)

// App represents the application and its components
type App struct {
	// Core components
	cfg      *config.Config
	log      *slog.Logger
	sqliteDB *sqlite.DB

	// Domain services
	authService      *auth.Service
	sourceService    *source.Service
	logsService      *logs.Service
	identityService  *identity.Service
	teamQueryService *logs.TeamQueryService

	// HTTP server
	server *server.Server
	webFS  http.FileSystem

	// Build information
	buildInfo string
}

// Options contains configuration for creating a new App
type Options struct {
	// Path to the configuration file
	ConfigPath string
	// Web filesystem for serving static files
	WebFS http.FileSystem
	// Build information
	BuildInfo string
}

// New creates a new App instance
func New(opts Options) (*App, error) {
	// Load configuration
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	log := logger.New(cfg.Logging.Level == "debug")

	// Create app instance
	app := &App{
		cfg:       cfg,
		log:       log,
		webFS:     opts.WebFS,
		buildInfo: opts.BuildInfo,
	}

	return app, nil
}

// Initialize sets up all application components
func (a *App) Initialize(ctx context.Context) error {
	// Initialize SQLite database
	a.log.Info("initializing sqlite database")
	var err error
	a.sqliteDB, err = sqlite.New(sqlite.Options{
		Config: a.cfg.SQLite,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize sqlite: %w", err)
	}

	// Initialize auth service
	a.log.Info("initializing authentication service")
	a.authService, err = auth.New(a.cfg, a.sqliteDB)
	if err != nil {
		return fmt.Errorf("failed to initialize authentication service: %w", err)
	}

	// Create Clickhouse manager
	a.log.Info("initializing clickhouse manager")
	clickhouseManager := clickhouse.NewManager(a.log)

	// Initialize domain-specific services
	a.log.Info("initializing domain services")
	a.sourceService = source.New(a.sqliteDB, clickhouseManager, a.log)
	a.logsService = logs.New(a.sqliteDB, clickhouseManager, a.log)
	a.identityService = identity.New(a.sqliteDB, a.log)

	// Initialize team query service
	a.log.Info("initializing team query service")
	a.teamQueryService = logs.NewTeamQueryService(a.sqliteDB, a.log)

	// Ensure admin user exists
	a.log.Info("ensuring admin users")
	if err := a.identityService.InitAdminUsers(ctx, a.cfg.Auth.AdminEmails); err != nil {
		return fmt.Errorf("failed to ensure admin user: %w", err)
	}

	// Initialize clickhouse connections for existing sources
	a.log.Info("initializing clickhouse connections")
	sources, err := a.sqliteDB.ListSources(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sources: %w", err)
	}

	if len(sources) == 0 {
		a.log.Warn("no sources found in the database")
	}

	// Initialize sources
	healthySources := 0
	for _, source := range sources {
		a.log.Info("initializing source", "source_id", source.ID, "table", source.Connection.TableName)

		// Initialize with source service
		if err := a.sourceService.InitializeSource(ctx, source); err != nil {
			a.log.Warn("failed to initialize source", "source", source.ID, "error", err)
			continue
		}

		healthySources++
	}
	a.log.Info("source initialization completed", "healthy_sources", healthySources, "total_sources", len(sources))

	// Initialize HTTP server
	a.log.Info("initializing http server")
	a.server = server.New(
		a.cfg,
		a.sourceService,
		a.logsService,
		a.identityService,
		a.authService,
		a.webFS,
		a.buildInfo,
		a.teamQueryService,
	)

	return nil
}

// Start begins the application's main execution
func (a *App) Start() error {
	a.log.Info("starting server")
	return a.server.Start()
}

// Shutdown gracefully stops all application components
func (a *App) Shutdown(ctx context.Context) error {
	a.log.Info("shutting down application")

	// Create a timeout context if one wasn't provided
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}

	// Shutdown server first to stop accepting new requests
	if err := a.server.Shutdown(ctx); err != nil {
		a.log.Error("error shutting down server", "error", err)
	}

	// Close database connections
	if a.sqliteDB != nil {
		if err := a.sqliteDB.Close(); err != nil {
			a.log.Error("error closing sqlite", "error", err)
		}
	}

	return nil
}
