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
	Server     ServerConfig       `koanf:"server"`
	SQLite     SQLiteConfig       `koanf:"sqlite"`
	Clickhouse []ClickhouseConfig `koanf:"clickhouse"`
	OIDC       OIDCConfig         `koanf:"oidc"`
	Auth       AuthConfig         `koanf:"auth"`
	Logging    LoggingConfig      `koanf:"logging"`
}

// ServerConfig contains HTTP server settings
type ServerConfig struct {
	Port        int    `koanf:"port"`
	Host        string `koanf:"host"`
	FrontendURL string `koanf:"frontend_url"`
}

// SQLiteConfig contains SQLite database settings
type SQLiteConfig struct {
	Path            string        `koanf:"path"`
	MaxOpenConns    int           `koanf:"max_open_conns"`
	MaxIdleConns    int           `koanf:"max_idle_conns"`
	ConnMaxLifetime time.Duration `koanf:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `koanf:"conn_max_idle_time"`
	BusyTimeout     int           `koanf:"busy_timeout"`
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
	ProviderURL  string   `koanf:"provider_url"`
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
	Level       string `koanf:"level"`
	Format      string `koanf:"format"`
	AddSource   bool   `koanf:"add_source"`
	Environment string `koanf:"environment"`
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

	// Convert minute values to time.Duration
	cfg.SQLite.ConnMaxLifetime = time.Duration(cfg.SQLite.ConnMaxLifetime) * time.Minute
	cfg.SQLite.ConnMaxIdleTime = time.Duration(cfg.SQLite.ConnMaxIdleTime) * time.Minute

	return &cfg, nil
}
