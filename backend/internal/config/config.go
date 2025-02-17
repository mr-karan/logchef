package config

import (
	"fmt"
	"log"
	"time"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Server     ServerConfig       `koanf:"server"`
	SQLite     SQLiteConfig       `koanf:"sqlite"`
	Clickhouse []ClickhouseConfig `koanf:"clickhouse"`
	OIDC       OIDCConfig         `koanf:"oidc"`
	Auth       AuthConfig         `koanf:"auth"`
}

type ServerConfig struct {
	Port        int    `koanf:"port"`
	Host        string `koanf:"host"`
	FrontendURL string `koanf:"frontend_url"`
}

type SQLiteConfig struct {
	Path string `koanf:"path"`
}

type ClickhouseConfig struct {
	Host     string `koanf:"host"`
	Port     int    `koanf:"port"`
	Database string `koanf:"database"`
	Username string `koanf:"username"`
	Password string `koanf:"password"`
}

type OIDCConfig struct {
	ProviderURL  string   `koanf:"provider_url"`
	ClientID     string   `koanf:"client_id"`
	ClientSecret string   `koanf:"client_secret"`
	RedirectURL  string   `koanf:"redirect_url"` // Callback URL for OIDC provider
	Scopes       []string `koanf:"scopes"`
}

type AuthConfig struct {
	AdminEmails           []string      `koanf:"admin_emails"`
	SessionDuration       time.Duration `koanf:"session_duration"`
	MaxConcurrentSessions int           `koanf:"max_concurrent_sessions"`
}

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

	return &cfg, nil
}
