package config

import (
	"context"
	"log"
	"time"
)

// SettingsStore defines the interface for retrieving settings from the database.
type SettingsStore interface {
	GetSettingWithDefault(ctx context.Context, key, defaultValue string) string
	GetBoolSetting(ctx context.Context, key string, defaultValue bool) bool
	GetIntSetting(ctx context.Context, key string, defaultValue int) int
	GetFloat64Setting(ctx context.Context, key string, defaultValue float64) float64
	GetDurationSetting(ctx context.Context, key string, defaultValue time.Duration) time.Duration
}

// LoadRuntimeConfig loads configuration from both static config and database.
// Database values override static config values for non-essential settings.
func LoadRuntimeConfig(ctx context.Context, staticConfig *Config, store SettingsStore) *Config {
	// Start with the static configuration
	cfg := *staticConfig

	// If no store is provided, return static config only
	if store == nil {
		log.Println("no settings store provided, using static configuration only")
		return &cfg
	}

	// Override with database settings for runtime-configurable values

	// Alerts configuration
	cfg.Alerts.Enabled = store.GetBoolSetting(ctx, "alerts.enabled", cfg.Alerts.Enabled)
	cfg.Alerts.EvaluationInterval = store.GetDurationSetting(ctx, "alerts.evaluation_interval", cfg.Alerts.EvaluationInterval)
	cfg.Alerts.DefaultLookback = store.GetDurationSetting(ctx, "alerts.default_lookback", cfg.Alerts.DefaultLookback)
	cfg.Alerts.HistoryLimit = store.GetIntSetting(ctx, "alerts.history_limit", cfg.Alerts.HistoryLimit)
	cfg.Alerts.SMTPHost = store.GetSettingWithDefault(ctx, "alerts.smtp_host", cfg.Alerts.SMTPHost)
	cfg.Alerts.SMTPPort = store.GetIntSetting(ctx, "alerts.smtp_port", cfg.Alerts.SMTPPort)
	cfg.Alerts.SMTPUsername = store.GetSettingWithDefault(ctx, "alerts.smtp_username", cfg.Alerts.SMTPUsername)
	cfg.Alerts.SMTPPassword = store.GetSettingWithDefault(ctx, "alerts.smtp_password", cfg.Alerts.SMTPPassword)
	cfg.Alerts.SMTPFrom = store.GetSettingWithDefault(ctx, "alerts.smtp_from", cfg.Alerts.SMTPFrom)
	cfg.Alerts.SMTPReplyTo = store.GetSettingWithDefault(ctx, "alerts.smtp_reply_to", cfg.Alerts.SMTPReplyTo)
	cfg.Alerts.SMTPSecurity = store.GetSettingWithDefault(ctx, "alerts.smtp_security", cfg.Alerts.SMTPSecurity)
	cfg.Alerts.ExternalURL = store.GetSettingWithDefault(ctx, "alerts.external_url", cfg.Alerts.ExternalURL)
	cfg.Alerts.FrontendURL = store.GetSettingWithDefault(ctx, "alerts.frontend_url", cfg.Alerts.FrontendURL)
	cfg.Alerts.RequestTimeout = store.GetDurationSetting(ctx, "alerts.request_timeout", cfg.Alerts.RequestTimeout)
	cfg.Alerts.TLSInsecureSkipVerify = store.GetBoolSetting(ctx, "alerts.tls_insecure_skip_verify", cfg.Alerts.TLSInsecureSkipVerify)

	// AI configuration
	cfg.AI.Enabled = store.GetBoolSetting(ctx, "ai.enabled", cfg.AI.Enabled)
	cfg.AI.APIKey = store.GetSettingWithDefault(ctx, "ai.api_key", cfg.AI.APIKey)
	cfg.AI.BaseURL = store.GetSettingWithDefault(ctx, "ai.base_url", cfg.AI.BaseURL)
	cfg.AI.Model = store.GetSettingWithDefault(ctx, "ai.model", cfg.AI.Model)
	cfg.AI.MaxTokens = store.GetIntSetting(ctx, "ai.max_tokens", cfg.AI.MaxTokens)
	cfg.AI.Temperature = float32(store.GetFloat64Setting(ctx, "ai.temperature", float64(cfg.AI.Temperature)))

	// Auth session management
	cfg.Auth.SessionDuration = store.GetDurationSetting(ctx, "auth.session_duration", cfg.Auth.SessionDuration)
	cfg.Auth.MaxConcurrentSessions = store.GetIntSetting(ctx, "auth.max_concurrent_sessions", cfg.Auth.MaxConcurrentSessions)
	cfg.Auth.DefaultTokenExpiry = store.GetDurationSetting(ctx, "auth.default_token_expiry", cfg.Auth.DefaultTokenExpiry)

	// Server frontend URL
	frontendURL := store.GetSettingWithDefault(ctx, "server.frontend_url", cfg.Server.FrontendURL)
	if frontendURL != "" {
		cfg.Server.FrontendURL = frontendURL
	}

	log.Println("runtime configuration loaded (static config + database settings)")
	return &cfg
}
