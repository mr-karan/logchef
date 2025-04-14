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
	"github.com/mr-karan/logchef/internal/saved_queries"
	"github.com/mr-karan/logchef/internal/server"
	"github.com/mr-karan/logchef/internal/source"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/logger"
)

// App represents the core application context
type App struct {
	Config     *config.Config
	SQLite     *sqlite.DB
	ClickHouse *clickhouse.Manager
	Logger     *slog.Logger
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

	// Initialize core application
	app := &App{
		Config: cfg,
		Logger: logger.New(cfg.Logging.Level == "debug"),
	}

	return app, nil
}

// Initialize sets up core application components
func (a *App) Initialize(ctx context.Context) error {
	var err error
	
	// Initialize SQLite database
	a.SQLite, err = sqlite.New(a.Config.SQLite, a.Logger)
	if err != nil {
		return fmt.Errorf("failed to initialize sqlite: %w", err)
	}

	// Initialize ClickHouse manager
	a.ClickHouse = clickhouse.NewManager(a.Logger)

	// Load existing sources into ClickHouse
	sources, err := a.SQLite.ListSources(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sources: %w", err)
	}

	for _, source := range sources {
		a.Logger.Info("initializing source connection", 
			"source_id", source.ID, 
			"table", source.Connection.TableName)
			
		if err := a.ClickHouse.AddSource(source); err != nil {
			a.Logger.Warn("failed to initialize source",
				"source_id", source.ID,
				"error", err)
		}
	}

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
