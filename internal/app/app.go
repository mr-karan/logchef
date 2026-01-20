package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/server"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/logger"
)

// App represents the core application context, holding dependencies and configuration.
type App struct {
	Config     *config.Config
	SQLite     *sqlite.DB
	ClickHouse *clickhouse.Manager
	Logger     *slog.Logger
	server     *server.Server
	WebFS      http.FileSystem
	BuildInfo  string
	Version    string
	Alerts     *alerts.Manager
}

// Options contains configuration needed when creating a new App instance.
type Options struct {
	ConfigPath string
	WebFS      http.FileSystem // Web filesystem for serving static files.
	BuildInfo  string
	Version    string
}

// New creates and configures a new App instance.
func New(opts Options) (*App, error) {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	app := &App{
		Config:    cfg,
		Logger:    logger.New(cfg.Logging.Level == "debug"),
		WebFS:     opts.WebFS,
		BuildInfo: opts.BuildInfo,
		Version:   opts.Version,
	}

	return app, nil
}

// Initialize sets up application components like database connections,
// the OIDC provider, and the HTTP server.
func (a *App) Initialize(ctx context.Context) error {
	var err error

	// Initialize SQLite database.
	sqliteOpts := sqlite.Options{
		Config: a.Config.SQLite,
		Logger: a.Logger,
	}
	a.SQLite, err = sqlite.New(sqliteOpts)
	if err != nil {
		return fmt.Errorf("failed to initialize sqlite: %w", err)
	}

	// Initialize admin users based on configuration.
	if err := core.InitAdminUsers(ctx, a.SQLite, a.Logger, a.Config.Auth.AdminEmails); err != nil {
		a.Logger.Error("failed to initialize admin users", "error", err)
		return fmt.Errorf("failed to initialize admin users: %w", err)
	}

	// Seed system settings from config.toml on first boot (if database is empty).
	if err := a.seedSystemSettings(ctx); err != nil {
		// Don't fail initialization - migration defaults will be used.
		a.Logger.Warn("failed to seed system settings from config", "error", err)
	}

	// Load runtime configuration: merge static config.toml with database settings.
	// Database settings override config.toml for non-essential settings.
	a.Config = config.LoadRuntimeConfig(ctx, a.Config, a.SQLite)
	a.Logger.Info("runtime configuration loaded from database and config.toml")

	// Initialize ClickHouse connection manager.
	a.ClickHouse = clickhouse.NewManager(a.Logger)

	// Initialize OIDC Provider.
	// This is optional; if OIDC is not configured, auth features relying on it might be disabled.
	oidcProvider, err := auth.NewOIDCProvider(ctx, &a.Config.OIDC, a.Logger)
	if err != nil {
		if errors.Is(err, auth.ErrOIDCProviderNotConfigured) {
			a.Logger.Warn("OIDC provider not configured, skipping OIDC setup")
			// oidcProvider will be nil; dependent features should handle this.
		} else {
			return fmt.Errorf("failed to initialize OIDC provider: %w", err)
		}
	}

	// Load existing sources from SQLite into the ClickHouse manager
	// to establish connections for querying.
	sources, err := a.SQLite.ListSources(ctx)
	if err != nil {
		return fmt.Errorf("failed to list sources: %w", err)
	}
	for _, source := range sources {
		a.Logger.Info("initializing source connection",
			"source_id", source.ID,
			"table", source.Connection.TableName)
		if err := a.ClickHouse.AddSource(ctx, source); err != nil {
			// Log failure but continue initialization.
			// The health check system will attempt to recover these connections.
			a.Logger.Warn("failed to initialize source connection, will attempt recovery via health checks",
				"source_id", source.ID,
				"error", err)
		}
	}

	// Start background health checks for the ClickHouse manager.
	// Use 0 to trigger the default interval defined in the manager.
	a.ClickHouse.StartBackgroundHealthChecks(0)

	// Initialize alerts manager with dynamic senders that read config from DB
	emailSender := alerts.NewDynamicEmailSender(a.SQLite, a.Logger)
	webhookSender := alerts.NewDynamicWebhookSender(a.SQLite, a.Logger)
	alertSender := alerts.NewMultiSender(emailSender, webhookSender)

	a.Alerts = alerts.NewManager(alerts.Options{
		Config:     a.Config.Alerts,
		DB:         a.SQLite,
		ClickHouse: a.ClickHouse,
		Logger:     a.Logger,
		Sender:     alertSender,
	})

	// Initialize HTTP server with alerts manager for manual resolution.
	serverOpts := server.ServerOptions{
		Config:        a.Config,
		SQLite:        a.SQLite,
		ClickHouse:    a.ClickHouse,
		AlertsManager: a.Alerts,
		OIDCProvider:  oidcProvider,
		FS:            a.WebFS,
		Logger:        a.Logger,
		BuildInfo:     a.BuildInfo,
		Version:       a.Version,
	}
	a.server = server.New(serverOpts)

	// Start the alerts evaluation loop.
	a.Alerts.Start(ctx)

	return nil
}

// Start begins the application's main execution loop (starts the HTTP server).
func (a *App) Start() error {
	if a.server == nil {
		return fmt.Errorf("server not initialized")
	}
	a.Logger.Info("starting server")
	return a.server.Start()
}

// Shutdown gracefully stops all application components with timeouts.
//
//nolint:contextcheck // Shutdown receives its own context from caller (e.g., signal handler)
func (a *App) Shutdown(ctx context.Context) error {
	a.Logger.Info("shutting down application")

	// Ensure a shutdown context with timeout exists.
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
	}

	// Create derived contexts with shorter timeouts for each component
	serverCtx, serverCancel := context.WithTimeout(ctx, 5*time.Second)
	defer serverCancel()

	clickhouseCtx, clickhouseCancel := context.WithTimeout(ctx, 8*time.Second)
	defer clickhouseCancel()

	if a.Alerts != nil {
		a.Logger.Info("stopping alert manager")
		a.Alerts.Stop()
	}

	// Shutdown server first to stop accepting new requests.
	if a.server != nil {
		a.Logger.Info("shutting down HTTP server")

		serverDone := make(chan error, 1)
		go func() {
			serverDone <- a.server.Shutdown(serverCtx)
		}()

		// Wait for server shutdown or timeout
		select {
		case err := <-serverDone:
			if err != nil {
				a.Logger.Error("error shutting down server", "error", err)
			} else {
				a.Logger.Info("HTTP server shut down successfully")
			}
		case <-serverCtx.Done():
			a.Logger.Warn("timeout shutting down HTTP server, continuing")
		}
	}

	// Close ClickHouse manager (stops health checks and closes clients).
	if a.ClickHouse != nil {
		a.Logger.Info("shutting down ClickHouse connections")

		clickhouseDone := make(chan error, 1)
		go func() {
			clickhouseDone <- a.ClickHouse.Close()
		}()

		// Wait for ClickHouse shutdown or timeout
		select {
		case err := <-clickhouseDone:
			if err != nil {
				a.Logger.Error("error closing clickhouse manager", "error", err)
			} else {
				a.Logger.Info("ClickHouse connections closed successfully")
			}
		case <-clickhouseCtx.Done():
			a.Logger.Warn("timeout closing ClickHouse connections, continuing")
		}
	}

	// Close database connections.
	if a.SQLite != nil {
		a.Logger.Info("closing SQLite connection")
		// SQLite should close almost instantly, no need for a separate goroutine
		if err := a.SQLite.Close(); err != nil {
			a.Logger.Error("error closing SQLite", "error", err)
		} else {
			a.Logger.Info("SQLite connection closed successfully")
		}
	}

	a.Logger.Info("application shutdown complete")
	return nil
}

// seedSystemSettings populates the system_settings table from config.toml on first boot.
// This allows users to customize default settings via config.toml before first deployment.
// After seeding, database becomes the source of truth and config.toml can be simplified.
func (a *App) seedSystemSettings(ctx context.Context) error {
	// Check if settings already exist
	settings, err := a.SQLite.ListSettings(ctx)
	if err != nil {
		return fmt.Errorf("failed to check existing settings: %w", err)
	}

	// If settings exist, skip seeding (already initialized or migrated)
	if len(settings) > 0 {
		a.Logger.Info("system settings already exist, skipping seeding from config.toml")
		return nil
	}

	a.Logger.Info("seeding system settings from config.toml (first boot)")

	// Seed alerts settings
	alertsSettings := map[string]struct {
		value       string
		valueType   string
		description string
		isSensitive bool
	}{
		"alerts.enabled": {
			value:       fmt.Sprintf("%t", a.Config.Alerts.Enabled),
			valueType:   "boolean",
			description: "Enable or disable alert evaluation",
			isSensitive: false,
		},
		"alerts.evaluation_interval": {
			value:       a.Config.Alerts.EvaluationInterval.String(),
			valueType:   "duration",
			description: "How often to evaluate alert rules",
			isSensitive: false,
		},
		"alerts.default_lookback": {
			value:       a.Config.Alerts.DefaultLookback.String(),
			valueType:   "duration",
			description: "Default lookback window for alert queries",
			isSensitive: false,
		},
		"alerts.history_limit": {
			value:       fmt.Sprintf("%d", a.Config.Alerts.HistoryLimit),
			valueType:   "number",
			description: "Maximum number of alert history entries to keep per alert",
			isSensitive: false,
		},
		"alerts.smtp_host": {
			value:       "",
			valueType:   "string",
			description: "SMTP host for alert emails",
			isSensitive: false,
		},
		"alerts.smtp_port": {
			value:       "587",
			valueType:   "number",
			description: "SMTP port for alert emails",
			isSensitive: false,
		},
		"alerts.smtp_username": {
			value:       "",
			valueType:   "string",
			description: "SMTP username for alert emails",
			isSensitive: false,
		},
		"alerts.smtp_password": {
			value:       "",
			valueType:   "string",
			description: "SMTP password for alert emails",
			isSensitive: true,
		},
		"alerts.smtp_from": {
			value:       "",
			valueType:   "string",
			description: "From address for alert emails",
			isSensitive: false,
		},
		"alerts.smtp_reply_to": {
			value:       "",
			valueType:   "string",
			description: "Reply-to address for alert emails",
			isSensitive: false,
		},
		"alerts.smtp_security": {
			value:       "starttls",
			valueType:   "string",
			description: "SMTP security mode (none, starttls, tls)",
			isSensitive: false,
		},
		"alerts.external_url": {
			value:       "",
			valueType:   "string",
			description: "External URL for backend API access",
			isSensitive: false,
		},
		"alerts.frontend_url": {
			value:       "",
			valueType:   "string",
			description: "Frontend URL for generating alert links in notifications",
			isSensitive: false,
		},
		"alerts.request_timeout": {
			value:       "5s",
			valueType:   "duration",
			description: "Timeout for alert notification requests",
			isSensitive: false,
		},
		"alerts.tls_insecure_skip_verify": {
			value:       "false",
			valueType:   "boolean",
			description: "Skip TLS certificate verification for alert notifications",
			isSensitive: false,
		},
	}

	for key, setting := range alertsSettings {
		if err := a.SQLite.UpsertSetting(ctx, key, setting.value, setting.valueType, "alerts", setting.description, setting.isSensitive); err != nil {
			a.Logger.Warn("failed to seed alert setting", "key", key, "error", err)
		} else {
			a.Logger.Debug("seeded alert setting", "key", key, "value", setting.value)
		}
	}

	// Seed AI settings
	aiSettings := map[string]struct {
		value       string
		valueType   string
		description string
		isSensitive bool
	}{
		"ai.enabled": {
			value:       fmt.Sprintf("%t", a.Config.AI.Enabled),
			valueType:   "boolean",
			description: "Enable or disable AI-assisted SQL generation",
			isSensitive: false,
		},
		"ai.api_key": {
			value:       a.Config.AI.APIKey,
			valueType:   "string",
			description: "OpenAI API key or compatible provider key",
			isSensitive: true,
		},
		"ai.base_url": {
			value:       a.Config.AI.BaseURL,
			valueType:   "string",
			description: "Base URL for OpenAI-compatible API (empty for default OpenAI)",
			isSensitive: false,
		},
		"ai.model": {
			value:       a.Config.AI.Model,
			valueType:   "string",
			description: "AI model to use for SQL generation",
			isSensitive: false,
		},
		"ai.max_tokens": {
			value:       fmt.Sprintf("%d", a.Config.AI.MaxTokens),
			valueType:   "number",
			description: "Maximum tokens to generate in AI responses",
			isSensitive: false,
		},
		"ai.temperature": {
			value:       fmt.Sprintf("%f", a.Config.AI.Temperature),
			valueType:   "number",
			description: "Temperature for generation (0.0-1.0, lower is more deterministic)",
			isSensitive: false,
		},
	}

	for key, setting := range aiSettings {
		if err := a.SQLite.UpsertSetting(ctx, key, setting.value, setting.valueType, "ai", setting.description, setting.isSensitive); err != nil {
			a.Logger.Warn("failed to seed AI setting", "key", key, "error", err)
		} else {
			a.Logger.Debug("seeded AI setting", "key", key)
		}
	}

	// Seed auth session settings
	authSettings := map[string]struct {
		value       string
		valueType   string
		description string
	}{
		"auth.session_duration": {
			value:       a.Config.Auth.SessionDuration.String(),
			valueType:   "duration",
			description: "Duration of user sessions before expiration",
		},
		"auth.max_concurrent_sessions": {
			value:       fmt.Sprintf("%d", a.Config.Auth.MaxConcurrentSessions),
			valueType:   "number",
			description: "Maximum number of concurrent sessions per user",
		},
		"auth.default_token_expiry": {
			value:       a.Config.Auth.DefaultTokenExpiry.String(),
			valueType:   "duration",
			description: "Default expiration for API tokens (90 days)",
		},
	}

	for key, setting := range authSettings {
		if err := a.SQLite.UpsertSetting(ctx, key, setting.value, setting.valueType, "auth", setting.description, false); err != nil {
			a.Logger.Warn("failed to seed auth setting", "key", key, "error", err)
		} else {
			a.Logger.Debug("seeded auth setting", "key", key, "value", setting.value)
		}
	}

	// Seed server settings
	if err := a.SQLite.UpsertSetting(ctx, "server.frontend_url", a.Config.Server.FrontendURL, "string", "server", "URL of the frontend application for CORS configuration", false); err != nil {
		a.Logger.Warn("failed to seed server.frontend_url", "error", err)
	} else {
		a.Logger.Debug("seeded server.frontend_url", "value", a.Config.Server.FrontendURL)
	}

	a.Logger.Info("system settings seeded from config.toml successfully")
	return nil
}
