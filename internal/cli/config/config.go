// Package config provides configuration management for the LogChef CLI.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config represents the CLI configuration
type Config struct {
	Server   ServerConfig   `koanf:"server"`
	Auth     AuthConfig     `koanf:"auth"`
	Defaults DefaultsConfig `koanf:"defaults"`
	Output   OutputConfig   `koanf:"output"`
	TUI      TUIConfig      `koanf:"tui"`
	Query    QueryConfig    `koanf:"query"`
	Tail     TailConfig     `koanf:"tail"`
}

// ServerConfig holds server connection settings
type ServerConfig struct {
	URL      string        `koanf:"url"`
	Timeout  time.Duration `koanf:"timeout"`
	Insecure bool          `koanf:"insecure"`
}

// AuthConfig holds authentication settings
type AuthConfig struct {
	Token string `koanf:"token"`
}

// DefaultsConfig holds default query parameters
type DefaultsConfig struct {
	Team     string `koanf:"team"`
	Source   string `koanf:"source"`
	Limit    int    `koanf:"limit"`
	Timezone string `koanf:"timezone"`
	Since    string `koanf:"since"`
}

// OutputConfig holds output formatting settings
type OutputConfig struct {
	Format string `koanf:"format"` // table, json, jsonl, csv
	Color  string `koanf:"color"`  // auto, always, never
	Pager  string `koanf:"pager"`  // less -R, or empty for none
	Wrap   bool   `koanf:"wrap"`
}

// TUIConfig holds TUI settings
type TUIConfig struct {
	Theme        string `koanf:"theme"` // dark, light, system
	SidebarWidth int    `koanf:"sidebar_width"`
	DetailsWidth int    `koanf:"details_width"`
	Histogram    bool   `koanf:"histogram"`
}

// QueryConfig holds query settings
type QueryConfig struct {
	HistoryFile string `koanf:"history_file"`
	HistorySize int    `koanf:"history_size"`
	ShowSQL     bool   `koanf:"show_sql"`
}

// TailConfig holds tailing settings
type TailConfig struct {
	RateLimit    int           `koanf:"rate_limit"`
	PollInterval time.Duration `koanf:"poll_interval"`
	BufferSize   int           `koanf:"buffer_size"`
}

// LoadOptions configures how configuration is loaded
type LoadOptions struct {
	ConfigPath string
	Profile    string
}

// Default returns the default configuration
func Default() *Config {
	return &Config{
		Server: ServerConfig{
			URL:     "http://localhost:8080",
			Timeout: 30 * time.Second,
		},
		Defaults: DefaultsConfig{
			Limit:    100,
			Timezone: "Local",
			Since:    "15m",
		},
		Output: OutputConfig{
			Format: "table",
			Color:  "auto",
			Pager:  "less -R",
		},
		TUI: TUIConfig{
			Theme:        "dark",
			SidebarWidth: 20,
			DetailsWidth: 40,
			Histogram:    true,
		},
		Query: QueryConfig{
			HistoryFile: filepath.Join(configDir(), "history"),
			HistorySize: 1000,
		},
		Tail: TailConfig{
			PollInterval: time.Second,
			BufferSize:   1000,
		},
	}
}

// Load loads configuration from file and environment
func Load(opts LoadOptions) (*Config, error) {
	k := koanf.New(".")

	// Start with defaults
	cfg := Default()

	// Determine config file path
	configPath := opts.ConfigPath
	if configPath == "" {
		configPath = filepath.Join(configDir(), "config.toml")
	}

	// Load from file if it exists
	if _, err := os.Stat(configPath); err == nil {
		if err := k.Load(file.Provider(configPath), toml.Parser()); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	}

	// Load from environment variables (LOGCHEF_*)
	if err := k.Load(env.Provider("LOGCHEF_", ".", func(s string) string {
		// LOGCHEF_SERVER_URL -> server.url
		return envToKey(s[8:]) // Strip LOGCHEF_ prefix
	}), nil); err != nil {
		return nil, fmt.Errorf("failed to load env vars: %w", err)
	}

	// Unmarshal into config struct
	if err := k.Unmarshal("", cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Load profile if specified
	if opts.Profile != "" {
		profileKey := fmt.Sprintf("profiles.%s", opts.Profile)
		if k.Exists(profileKey) {
			if err := k.Unmarshal(profileKey, cfg); err != nil {
				return nil, fmt.Errorf("failed to load profile %s: %w", opts.Profile, err)
			}
		}
	}

	return cfg, nil
}

// Save saves configuration to file
func (c *Config) Save() error {
	configPath := filepath.Join(configDir(), "config.toml")

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal to TOML
	k := koanf.New(".")
	if err := k.Load(confmap{c}, nil); err != nil {
		return err
	}

	data, err := k.Marshal(toml.Parser())
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ResolveTimezone returns a ClickHouse-compatible timezone string.
// If timezone is "Local" or empty, it detects the system timezone.
func (c *Config) ResolveTimezone() string {
	tz := c.Defaults.Timezone
	if tz == "" || tz == "Local" {
		return getSystemTimezone()
	}
	return tz
}

func getSystemTimezone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	return "UTC"
}

// configDir returns the configuration directory
func configDir() string {
	// Check XDG_CONFIG_HOME first
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "logchef")
	}

	// Fall back to ~/.config/logchef
	home, err := os.UserHomeDir()
	if err != nil {
		return ".logchef"
	}
	return filepath.Join(home, ".config", "logchef")
}

// ConfigDir returns the configuration directory (exported)
func ConfigDir() string {
	return configDir()
}

// envToKey converts environment variable suffix to config key
// e.g., SERVER_URL -> server.url
func envToKey(s string) string {
	result := ""
	for i, c := range s {
		if c == '_' {
			result += "."
		} else if i > 0 && s[i-1] == '_' {
			result += string(c - 'A' + 'a') // lowercase after underscore
		} else {
			result += string(c - 'A' + 'a') // lowercase
		}
	}
	return result
}

// confmap implements koanf.Provider for Config struct
type confmap struct {
	cfg *Config
}

func (c confmap) ReadBytes() ([]byte, error) { return nil, nil }
func (c confmap) Read() (map[string]any, error) {
	return map[string]any{
		"server": map[string]any{
			"url":      c.cfg.Server.URL,
			"timeout":  c.cfg.Server.Timeout.String(),
			"insecure": c.cfg.Server.Insecure,
		},
		"defaults": map[string]any{
			"team":     c.cfg.Defaults.Team,
			"source":   c.cfg.Defaults.Source,
			"limit":    c.cfg.Defaults.Limit,
			"timezone": c.cfg.Defaults.Timezone,
			"since":    c.cfg.Defaults.Since,
		},
		"output": map[string]any{
			"format": c.cfg.Output.Format,
			"color":  c.cfg.Output.Color,
			"pager":  c.cfg.Output.Pager,
			"wrap":   c.cfg.Output.Wrap,
		},
		"tui": map[string]any{
			"theme":         c.cfg.TUI.Theme,
			"sidebar_width": c.cfg.TUI.SidebarWidth,
			"details_width": c.cfg.TUI.DetailsWidth,
			"histogram":     c.cfg.TUI.Histogram,
		},
		"query": map[string]any{
			"history_file": c.cfg.Query.HistoryFile,
			"history_size": c.cfg.Query.HistorySize,
			"show_sql":     c.cfg.Query.ShowSQL,
		},
		"tail": map[string]any{
			"rate_limit":    c.cfg.Tail.RateLimit,
			"poll_interval": c.cfg.Tail.PollInterval.String(),
			"buffer_size":   c.cfg.Tail.BufferSize,
		},
	}, nil
}
