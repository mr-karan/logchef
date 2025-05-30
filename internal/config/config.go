package config

import (
	"fmt"
	"log"
	"time"

	"github.com/knadh/koanf/parsers/toml"
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
}

// ServerConfig contains HTTP server settings
type ServerConfig struct {
	Port        int    `koanf:"port"`
	Host        string `koanf:"host"`
	FrontendURL string `koanf:"frontend_url"`
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
}

// LoggingConfig contains logging settings
type LoggingConfig struct {
	// Level sets the minimum log level (debug, info, warn, error)
	Level string `koanf:"level"`
}

// Load loads the configuration from a file
func Load(path string) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(file.Provider(path), toml.Parser()); err != nil {
		log.Printf("error loading config: %v", err)
		return nil, err
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		log.Printf("error unmarshaling config: %v", err)
		return nil, err
	}

	// Validate required configurations
	if len(cfg.Auth.AdminEmails) == 0 {
		return nil, fmt.Errorf("admin_emails is required in auth configuration")
	}

	// Validate OIDC configuration
	if cfg.OIDC.ProviderURL == "" {
		return nil, fmt.Errorf("provider_url is required in OIDC configuration")
	}
	if cfg.OIDC.AuthURL == "" {
		return nil, fmt.Errorf("auth_url is required in OIDC configuration")
	}
	if cfg.OIDC.TokenURL == "" {
		return nil, fmt.Errorf("token_url is required in OIDC configuration")
	}
	if cfg.OIDC.ClientID == "" {
		return nil, fmt.Errorf("client_id is required in OIDC configuration")
	}
	if cfg.OIDC.RedirectURL == "" {
		return nil, fmt.Errorf("redirect_url is required in OIDC configuration")
	}

	return &cfg, nil
}
