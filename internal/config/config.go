package config

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config represents the application configuration
type Config struct {
	Server     ServerConfig     `koanf:"server"`
	SQLite     SQLiteConfig     `koanf:"sqlite"`
	Clickhouse ClickhouseConfig `koanf:"clickhouse"`
	OIDC       OIDCConfig       `koanf:"oidc"`
	Auth       AuthConfig       `koanf:"auth"`
	Logging    LoggingConfig    `koanf:"logging"`
	AI         AIConfig         `koanf:"ai"`
}

// ServerConfig contains HTTP server settings
type ServerConfig struct {
	Port              int           `koanf:"port"`
	Host              string        `koanf:"host"`
	FrontendURL       string        `koanf:"frontend_url"`
	HTTPServerTimeout time.Duration `koanf:"http_server_timeout"`
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

const envPrefix = "LOGCHEF_"

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
		return strings.Replace(strings.ToLower(
			strings.TrimPrefix(s, envPrefix)), "__", ".", -1)
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
