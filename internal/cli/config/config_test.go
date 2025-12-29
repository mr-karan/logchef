package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg == nil {
		t.Fatal("Default() returned nil")
	}

	// Verify default values
	if cfg.Server.URL != "http://localhost:8080" {
		t.Errorf("Default() Server.URL = %q, want %q", cfg.Server.URL, "http://localhost:8080")
	}

	if cfg.Server.Timeout != 30*time.Second {
		t.Errorf("Default() Server.Timeout = %v, want %v", cfg.Server.Timeout, 30*time.Second)
	}

	if cfg.Defaults.Limit != 100 {
		t.Errorf("Default() Defaults.Limit = %d, want %d", cfg.Defaults.Limit, 100)
	}

	if cfg.Defaults.Since != "15m" {
		t.Errorf("Default() Defaults.Since = %q, want %q", cfg.Defaults.Since, "15m")
	}

	if cfg.Output.Format != "table" {
		t.Errorf("Default() Output.Format = %q, want %q", cfg.Output.Format, "table")
	}

	if cfg.TUI.Theme != "dark" {
		t.Errorf("Default() TUI.Theme = %q, want %q", cfg.TUI.Theme, "dark")
	}

	if cfg.Query.HistorySize != 1000 {
		t.Errorf("Default() Query.HistorySize = %d, want %d", cfg.Query.HistorySize, 1000)
	}

	if cfg.Tail.PollInterval != time.Second {
		t.Errorf("Default() Tail.PollInterval = %v, want %v", cfg.Tail.PollInterval, time.Second)
	}
}

func TestEnvToKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"SERVER_URL", "server.url"},
		{"AUTH_TOKEN", "auth.token"},
		{"DEFAULTS_TEAM", "defaults.team"},
		{"OUTPUT_FORMAT", "output.format"},
		{"TUI_SIDEBAR_WIDTH", "tui.sidebar.width"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := envToKey(tt.input)
			if got != tt.expected {
				t.Errorf("envToKey(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestConfigDir(t *testing.T) {
	// Test with XDG_CONFIG_HOME set
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", originalXDG)

	os.Setenv("XDG_CONFIG_HOME", "/tmp/test-config")
	dir := ConfigDir()
	expected := "/tmp/test-config/logchef"
	if dir != expected {
		t.Errorf("ConfigDir() with XDG = %q, want %q", dir, expected)
	}

	// Test without XDG_CONFIG_HOME (falls back to ~/.config)
	os.Unsetenv("XDG_CONFIG_HOME")
	dir = ConfigDir()
	home, _ := os.UserHomeDir()
	expected = filepath.Join(home, ".config", "logchef")
	if dir != expected {
		t.Errorf("ConfigDir() without XDG = %q, want %q", dir, expected)
	}
}

func TestLoad_NonExistentFile(t *testing.T) {
	cfg, err := Load(LoadOptions{
		ConfigPath: "/nonexistent/path/config.toml",
	})

	// Should return defaults when file doesn't exist
	if err != nil {
		t.Errorf("Load() with nonexistent file should not error, got %v", err)
	}

	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}

	// Should have default values
	if cfg.Server.URL != "http://localhost:8080" {
		t.Errorf("Load() fallback Server.URL = %q, want default", cfg.Server.URL)
	}
}

func TestLoad_WithEnvVars(t *testing.T) {
	// Save original env vars
	originalURL := os.Getenv("LOGCHEF_SERVER_URL")
	originalToken := os.Getenv("LOGCHEF_AUTH_TOKEN")
	defer func() {
		os.Setenv("LOGCHEF_SERVER_URL", originalURL)
		os.Setenv("LOGCHEF_AUTH_TOKEN", originalToken)
	}()

	// Set test env vars
	os.Setenv("LOGCHEF_SERVER_URL", "https://test.example.com")
	os.Setenv("LOGCHEF_AUTH_TOKEN", "test-token-123")

	cfg, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Server.URL != "https://test.example.com" {
		t.Errorf("Load() Server.URL from env = %q, want %q", cfg.Server.URL, "https://test.example.com")
	}

	if cfg.Auth.Token != "test-token-123" {
		t.Errorf("Load() Auth.Token from env = %q, want %q", cfg.Auth.Token, "test-token-123")
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	originalXDG := os.Getenv("XDG_CONFIG_HOME")
	defer os.Setenv("XDG_CONFIG_HOME", originalXDG)
	os.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Create and save config
	cfg := Default()
	cfg.Server.URL = "https://saved.example.com"
	cfg.Defaults.Team = "test-team"
	cfg.Defaults.Source = "test-source"
	cfg.Auth.Token = "saved-token"

	err := cfg.Save()
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Verify file was created
	configPath := filepath.Join(tmpDir, "logchef", "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatalf("Save() did not create config file at %s", configPath)
	}

	// Load and verify
	loaded, err := Load(LoadOptions{})
	if err != nil {
		t.Fatalf("Load() after Save() error = %v", err)
	}

	if loaded.Server.URL != "https://saved.example.com" {
		t.Errorf("Load() Server.URL = %q, want %q", loaded.Server.URL, "https://saved.example.com")
	}

	if loaded.Defaults.Team != "test-team" {
		t.Errorf("Load() Defaults.Team = %q, want %q", loaded.Defaults.Team, "test-team")
	}

	if loaded.Defaults.Source != "test-source" {
		t.Errorf("Load() Defaults.Source = %q, want %q", loaded.Defaults.Source, "test-source")
	}
}

func TestServerConfig(t *testing.T) {
	cfg := &ServerConfig{
		URL:      "https://example.com",
		Timeout:  60 * time.Second,
		Insecure: false,
	}

	if cfg.URL != "https://example.com" {
		t.Errorf("ServerConfig.URL = %q, want %q", cfg.URL, "https://example.com")
	}

	if cfg.Timeout != 60*time.Second {
		t.Errorf("ServerConfig.Timeout = %v, want %v", cfg.Timeout, 60*time.Second)
	}

	if cfg.Insecure != false {
		t.Errorf("ServerConfig.Insecure = %v, want %v", cfg.Insecure, false)
	}
}

func TestOutputConfig(t *testing.T) {
	tests := []struct {
		format string
		valid  bool
	}{
		{"table", true},
		{"json", true},
		{"jsonl", true},
		{"csv", true},
		{"invalid", true}, // Config accepts any string, validation happens elsewhere
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			cfg := &OutputConfig{Format: tt.format}
			if cfg.Format != tt.format {
				t.Errorf("OutputConfig.Format = %q, want %q", cfg.Format, tt.format)
			}
		})
	}
}
