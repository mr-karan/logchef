package config

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config represents the application configuration
type Config struct {
	Server       ServerConfig       `koanf:"server"`
	SQLite       SQLiteConfig       `koanf:"sqlite"`
	Clickhouse   ClickhouseConfig   `koanf:"clickhouse"`
	OIDC         OIDCConfig         `koanf:"oidc"`
	Auth         AuthConfig         `koanf:"auth"`
	Logging      LoggingConfig      `koanf:"logging"`
	AI           AIConfig           `koanf:"ai"`
	Alerts       AlertsConfig       `koanf:"alerts"`
	Query        QueryConfig        `koanf:"query"`
	Export       ExportConfig       `koanf:"export"`
	Shares       SharesConfig       `koanf:"shares"`
	Provisioning ProvisioningConfig `koanf:"provisioning"`
}

// QueryConfig contains settings for query execution
type QueryConfig struct {
	// MaxLimit is a deprecated alias for MaxPreviewLimit.
	MaxLimit int `koanf:"max_limit"`
	// DefaultPreviewLimit is applied when a preview query does not specify LIMIT.
	DefaultPreviewLimit int `koanf:"default_preview_limit"`
	// MaxPreviewLimit caps browser preview query results.
	MaxPreviewLimit int `koanf:"max_preview_limit"`
	// MaxResponseBytes caps approximate preview response payload size.
	MaxResponseBytes int `koanf:"max_response_bytes"`
	// DefaultTimeoutSeconds is the default ClickHouse max_execution_time for preview queries.
	DefaultTimeoutSeconds int `koanf:"default_timeout_seconds"`
	// MaxTimeoutSeconds caps preview query timeout requests.
	MaxTimeoutSeconds int `koanf:"max_timeout_seconds"`
	// MaxConcurrentPerUser limits active preview queries per user.
	MaxConcurrentPerUser int `koanf:"max_concurrent_per_user"`
	// MaxConcurrentGlobal limits active preview queries globally.
	MaxConcurrentGlobal int `koanf:"max_concurrent_global"`
}

// ExportConfig contains settings for streaming result exports.
type ExportConfig struct {
	MaxRows               int           `koanf:"max_rows"`
	DefaultTimeoutSeconds int           `koanf:"default_timeout_seconds"`
	MaxTimeoutSeconds     int           `koanf:"max_timeout_seconds"`
	MaxConcurrentPerUser  int           `koanf:"max_concurrent_per_user"`
	MaxConcurrentGlobal   int           `koanf:"max_concurrent_global"`
	ArtifactTTL           time.Duration `koanf:"artifact_ttl"`
	Formats               []string      `koanf:"formats"`
}

// SharesConfig contains settings for ad hoc query share links.
type SharesConfig struct {
	DefaultTTL        time.Duration `koanf:"default_ttl"`
	MaxQueryTextBytes int           `koanf:"max_query_text_bytes"`
}

// ServerConfig contains HTTP server settings
type ServerConfig struct {
	Port              int           `koanf:"port"`
	Host              string        `koanf:"host"`
	FrontendURL       string        `koanf:"frontend_url"`
	HTTPServerTimeout time.Duration `koanf:"http_server_timeout"`
	// SecureCookie controls the Secure flag on auth cookies.
	// Set to false for local development over HTTP. Defaults to true.
	SecureCookie *bool `koanf:"secure_cookie"`
}

// IsSecureCookie returns whether cookies should have the Secure flag set.
func (s *ServerConfig) IsSecureCookie() bool {
	if s.SecureCookie == nil {
		return true
	}
	return *s.SecureCookie
}

// SQLiteConfig contains SQLite database settings
type SQLiteConfig struct {
	Path string `koanf:"path"`
}

// ClickhouseConfig contains Clickhouse database settings
type ClickhouseConfig struct {
	Host     string `koanf:"host"`
	Port     int    `koanf:"port"`
	Database string `koanf:"database"`
	Username string `koanf:"username"`
	Password string `koanf:"password"`
}

// OIDCConfig contains OpenID Connect settings
type OIDCConfig struct {
	// Provider URL for OIDC discovery
	ProviderURL string `koanf:"provider_url"` // Base URL for OIDC provider discovery
	// Different endpoints for OIDC flow
	AuthURL  string `koanf:"auth_url"`  // URL for browser auth redirects
	TokenURL string `koanf:"token_url"` // URL for token exchange (server-to-server)

	ClientID     string   `koanf:"client_id"`
	ClientSecret string   `koanf:"client_secret"`
	RedirectURL  string   `koanf:"redirect_url"`
	Scopes       []string `koanf:"scopes"`

	CLIClientID string `koanf:"cli_client_id"`
}

// AuthConfig contains authentication settings
type AuthConfig struct {
	AdminEmails           []string      `koanf:"admin_emails"`
	SessionDuration       time.Duration `koanf:"session_duration"`
	MaxConcurrentSessions int           `koanf:"max_concurrent_sessions"`
	APITokenSecret        string        `koanf:"api_token_secret"`
	DefaultTokenExpiry    time.Duration `koanf:"default_token_expiry"`
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	// Level sets the minimum log level (debug, info, warn, error)
	Level string `koanf:"level"`
}

// AIConfig contains AI service (OpenAI) settings
type AIConfig struct {
	// OpenAI API key
	APIKey string `koanf:"api_key"`
	// Model to use for AI SQL generation (default: gpt-4o)
	Model string `koanf:"model"`
	// MaxTokens is the maximum number of tokens to generate (default: 1024)
	MaxTokens int `koanf:"max_tokens"`
	// Temperature controls randomness in generation (0.0-1.0, default: 0.1)
	Temperature float32 `koanf:"temperature"`
	// Enabled indicates whether AI features are enabled
	Enabled bool `koanf:"enabled"`
	// BaseURL for OpenAI API (default: "", which uses the standard OpenAI API endpoint)
	BaseURL string `koanf:"base_url"`
}

// AlertsConfig controls scheduling behaviour for alert rules.
// SMTP and other delivery settings are stored in the database and managed via Admin UI.
type AlertsConfig struct {
	Enabled            bool          `koanf:"enabled"`
	EvaluationInterval time.Duration `koanf:"evaluation_interval"`
	DefaultLookback    time.Duration `koanf:"default_lookback"`
	HistoryLimit       int           `koanf:"history_limit"`
}

const (
	envPrefix = "LOGCHEF_"

	defaultServerPort         = 8125
	defaultServerHost         = "0.0.0.0"
	defaultHTTPServerTimeout  = 30 * time.Second
	defaultServerSecureCookie = true
	defaultSQLitePath         = "local.db"
	defaultLoggingLevel       = "info"

	defaultAlertsEnabled            = true
	defaultAlertsEvaluationInterval = time.Minute
	defaultAlertsDefaultLookback    = 5 * time.Minute
	defaultAlertsHistoryLimit       = 50

	defaultAIEnabled     = true
	defaultAIBaseURL     = "https://api.openai.com/v1"
	defaultAIModel       = "gpt-4o"
	defaultAIMaxTokens   = 1024
	defaultAITemperature = 0.1

	defaultAuthSessionDuration       = 8 * time.Hour
	defaultAuthMaxConcurrentSessions = 1
	defaultAuthDefaultTokenExpiry    = 2160 * time.Hour

	defaultQueryDefaultPreviewLimit  = 1000
	defaultQueryMaxPreviewLimit      = 100000
	defaultQueryMaxResponseBytes     = 64 * 1024 * 1024
	defaultQueryDefaultTimeoutSecs   = 30
	defaultQueryMaxTimeoutSecs       = 120
	defaultQueryMaxConcurrentPerUser = 3
	defaultQueryMaxConcurrentGlobal  = 30

	defaultExportMaxRows              = 1000000
	defaultExportDefaultTimeoutSecs   = 120
	defaultExportMaxTimeoutSecs       = 600
	defaultExportMaxConcurrentPerUser = 1
	defaultExportMaxConcurrentGlobal  = 5
	defaultExportArtifactTTL          = 24 * time.Hour

	defaultSharesDefaultTTL        = 720 * time.Hour
	defaultSharesMaxQueryTextBytes = 1024 * 1024
)

var defaultExportFormats = []string{"csv", "ndjson"}

var defaultOIDCScopes = []string{"openid", "email", "profile"}

// Load loads the configuration from a file and environment variables.
// Environment variables with the prefix LOGCHEF_ can override file values.
// E.g., LOGCHEF_SERVER__PORT will override server.port
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	// Load configuration from the specified TOML file first.
	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		// Log a warning if the config file fails to load, but proceed to check env vars.
		log.Printf("warning: error loading config file at '%s': %v. Will attempt to load from environment variables.", path, err)
	} else {
		log.Printf("loaded configuration from file: %s", path)
	}

	// Load environment variables with the prefix LOGCHEF_.
	// Env vars will override values from the config file if they exist.
	envCb := func(s string) string {
		// LOGCHEF_SERVER__PORT -> server.port
		return strings.ReplaceAll(strings.ToLower(
			strings.TrimPrefix(s, envPrefix)), "__", ".")
	}
	if err := k.Load(env.Provider(envPrefix, ".", envCb), nil); err != nil {
		// If loading env vars fails, it's a more critical issue for config setup.
		log.Printf("error loading config from environment variables: %v", err)
		return nil, err
	}
	log.Printf("loaded configuration from environment variables with prefix %s", envPrefix)

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		log.Printf("error unmarshaling config: %v", err)
		return nil, err
	}
	applyDefaults(k, &cfg)

	// Load separate provisioning file if specified.
	if cfg.Provisioning.File != "" {
		provPath := cfg.Provisioning.File
		// Resolve relative paths against the config file directory.
		if !filepath.IsAbs(provPath) {
			provPath = filepath.Join(filepath.Dir(path), provPath)
		}
		pk := koanf.New(".")
		if err := pk.Load(file.Provider(provPath), toml.Parser()); err != nil {
			return nil, fmt.Errorf("error loading provisioning file %q: %w", provPath, err)
		}
		log.Printf("loaded provisioning config from: %s", provPath)
		// Unmarshal from root — the standalone file uses top-level keys
		// (manage_sources, [[sources]], [[teams]]) without a [provisioning] prefix.
		if err := pk.Unmarshal("", &cfg.Provisioning); err != nil {
			return nil, fmt.Errorf("error parsing provisioning file: %w", err)
		}
		// Preserve the file path
		cfg.Provisioning.File = provPath
	}

	// Validate required configurations
	if len(cfg.Auth.AdminEmails) == 0 {
		return nil, fmt.Errorf("admin_emails is required in auth configuration (either in file or %sAUTH__ADMIN_EMAILS)", envPrefix)
	}

	// Validate API token secret
	if cfg.Auth.APITokenSecret == "" {
		return nil, fmt.Errorf("api_token_secret is required in auth configuration (either in file or %sAUTH__API_TOKEN_SECRET)", envPrefix)
	}
	if len(cfg.Auth.APITokenSecret) < 32 {
		return nil, fmt.Errorf("api_token_secret must be at least 32 characters long for security")
	}

	// Validate OIDC configuration
	if cfg.OIDC.ProviderURL == "" {
		return nil, fmt.Errorf("provider_url is required in OIDC configuration (either in file or %sOIDC__PROVIDER_URL)", envPrefix)
	}
	if cfg.OIDC.AuthURL == "" {
		return nil, fmt.Errorf("auth_url is required in OIDC configuration (either in file or %sOIDC__AUTH_URL)", envPrefix)
	}
	if cfg.OIDC.TokenURL == "" {
		return nil, fmt.Errorf("token_url is required in OIDC configuration (either in file or %sOIDC__TOKEN_URL)", envPrefix)
	}
	if cfg.OIDC.ClientID == "" {
		return nil, fmt.Errorf("client_id is required in OIDC configuration (either in file or %sOIDC__CLIENT_ID)", envPrefix)
	}
	if cfg.OIDC.RedirectURL == "" {
		return nil, fmt.Errorf("redirect_url is required in OIDC configuration (either in file or %sOIDC__REDIRECT_URL)", envPrefix)
	}

	return &cfg, nil
}

func applyDefaults(k *koanf.Koanf, cfg *Config) {
	if !k.Exists("server.port") {
		cfg.Server.Port = defaultServerPort
	}
	if !k.Exists("server.host") {
		cfg.Server.Host = defaultServerHost
	}
	if !k.Exists("server.http_server_timeout") {
		cfg.Server.HTTPServerTimeout = defaultHTTPServerTimeout
	}
	if !k.Exists("server.secure_cookie") {
		defaultVal := defaultServerSecureCookie
		cfg.Server.SecureCookie = &defaultVal
	}
	if !k.Exists("sqlite.path") {
		cfg.SQLite.Path = defaultSQLitePath
	}
	if !k.Exists("logging.level") {
		cfg.Logging.Level = defaultLoggingLevel
	}
	if !k.Exists("oidc.scopes") {
		cfg.OIDC.Scopes = append([]string(nil), defaultOIDCScopes...)
	}

	if !k.Exists("alerts.enabled") {
		cfg.Alerts.Enabled = defaultAlertsEnabled
	}
	if !k.Exists("alerts.evaluation_interval") {
		cfg.Alerts.EvaluationInterval = defaultAlertsEvaluationInterval
	}
	if !k.Exists("alerts.default_lookback") {
		cfg.Alerts.DefaultLookback = defaultAlertsDefaultLookback
	}
	if !k.Exists("alerts.history_limit") {
		cfg.Alerts.HistoryLimit = defaultAlertsHistoryLimit
	}

	if !k.Exists("ai.enabled") {
		cfg.AI.Enabled = defaultAIEnabled
	}
	if !k.Exists("ai.base_url") {
		cfg.AI.BaseURL = defaultAIBaseURL
	}
	if !k.Exists("ai.model") {
		cfg.AI.Model = defaultAIModel
	}
	if !k.Exists("ai.max_tokens") {
		cfg.AI.MaxTokens = defaultAIMaxTokens
	}
	if !k.Exists("ai.temperature") {
		cfg.AI.Temperature = defaultAITemperature
	}

	if !k.Exists("auth.session_duration") {
		cfg.Auth.SessionDuration = defaultAuthSessionDuration
	}
	if !k.Exists("auth.max_concurrent_sessions") {
		cfg.Auth.MaxConcurrentSessions = defaultAuthMaxConcurrentSessions
	}
	if !k.Exists("auth.default_token_expiry") {
		cfg.Auth.DefaultTokenExpiry = defaultAuthDefaultTokenExpiry
	}

	if !k.Exists("query.default_preview_limit") {
		cfg.Query.DefaultPreviewLimit = defaultQueryDefaultPreviewLimit
	}
	if !k.Exists("query.max_preview_limit") {
		if k.Exists("query.max_limit") && cfg.Query.MaxLimit > 0 {
			cfg.Query.MaxPreviewLimit = cfg.Query.MaxLimit
		} else {
			cfg.Query.MaxPreviewLimit = defaultQueryMaxPreviewLimit
		}
	}
	if !k.Exists("query.max_response_bytes") {
		cfg.Query.MaxResponseBytes = defaultQueryMaxResponseBytes
	}
	if !k.Exists("query.default_timeout_seconds") {
		cfg.Query.DefaultTimeoutSeconds = defaultQueryDefaultTimeoutSecs
	}
	if !k.Exists("query.max_timeout_seconds") {
		cfg.Query.MaxTimeoutSeconds = defaultQueryMaxTimeoutSecs
	}
	if !k.Exists("query.max_concurrent_per_user") {
		cfg.Query.MaxConcurrentPerUser = defaultQueryMaxConcurrentPerUser
	}
	if !k.Exists("query.max_concurrent_global") {
		cfg.Query.MaxConcurrentGlobal = defaultQueryMaxConcurrentGlobal
	}
	if cfg.Query.MaxLimit == 0 {
		cfg.Query.MaxLimit = cfg.Query.MaxPreviewLimit
	}
	if cfg.Query.DefaultPreviewLimit <= 0 {
		cfg.Query.DefaultPreviewLimit = defaultQueryDefaultPreviewLimit
	}
	if cfg.Query.MaxPreviewLimit <= 0 {
		cfg.Query.MaxPreviewLimit = defaultQueryMaxPreviewLimit
	}
	if cfg.Query.DefaultPreviewLimit > cfg.Query.MaxPreviewLimit {
		cfg.Query.DefaultPreviewLimit = cfg.Query.MaxPreviewLimit
	}
	if cfg.Query.MaxTimeoutSeconds <= 0 {
		cfg.Query.MaxTimeoutSeconds = defaultQueryMaxTimeoutSecs
	}
	if cfg.Query.DefaultTimeoutSeconds <= 0 {
		cfg.Query.DefaultTimeoutSeconds = defaultQueryDefaultTimeoutSecs
	}
	if cfg.Query.DefaultTimeoutSeconds > cfg.Query.MaxTimeoutSeconds {
		cfg.Query.DefaultTimeoutSeconds = cfg.Query.MaxTimeoutSeconds
	}

	if !k.Exists("export.max_rows") {
		cfg.Export.MaxRows = defaultExportMaxRows
	}
	if !k.Exists("export.default_timeout_seconds") {
		cfg.Export.DefaultTimeoutSeconds = defaultExportDefaultTimeoutSecs
	}
	if !k.Exists("export.max_timeout_seconds") {
		cfg.Export.MaxTimeoutSeconds = defaultExportMaxTimeoutSecs
	}
	if !k.Exists("export.max_concurrent_per_user") {
		cfg.Export.MaxConcurrentPerUser = defaultExportMaxConcurrentPerUser
	}
	if !k.Exists("export.max_concurrent_global") {
		cfg.Export.MaxConcurrentGlobal = defaultExportMaxConcurrentGlobal
	}
	if !k.Exists("export.artifact_ttl") {
		cfg.Export.ArtifactTTL = defaultExportArtifactTTL
	}
	if !k.Exists("export.formats") {
		cfg.Export.Formats = append([]string(nil), defaultExportFormats...)
	}
	if cfg.Export.MaxRows <= 0 {
		cfg.Export.MaxRows = defaultExportMaxRows
	}
	if cfg.Export.MaxTimeoutSeconds <= 0 {
		cfg.Export.MaxTimeoutSeconds = defaultExportMaxTimeoutSecs
	}
	if cfg.Export.DefaultTimeoutSeconds <= 0 {
		cfg.Export.DefaultTimeoutSeconds = defaultExportDefaultTimeoutSecs
	}
	if cfg.Export.DefaultTimeoutSeconds > cfg.Export.MaxTimeoutSeconds {
		cfg.Export.DefaultTimeoutSeconds = cfg.Export.MaxTimeoutSeconds
	}
	if cfg.Export.ArtifactTTL <= 0 {
		cfg.Export.ArtifactTTL = defaultExportArtifactTTL
	}
	if len(cfg.Export.Formats) == 0 {
		cfg.Export.Formats = append([]string(nil), defaultExportFormats...)
	}

	if !k.Exists("shares.default_ttl") {
		cfg.Shares.DefaultTTL = defaultSharesDefaultTTL
	}
	if !k.Exists("shares.max_query_text_bytes") {
		cfg.Shares.MaxQueryTextBytes = defaultSharesMaxQueryTextBytes
	}
	if cfg.Shares.DefaultTTL <= 0 {
		cfg.Shares.DefaultTTL = defaultSharesDefaultTTL
	}
	if cfg.Shares.MaxQueryTextBytes <= 0 {
		cfg.Shares.MaxQueryTextBytes = defaultSharesMaxQueryTextBytes
	}
}
