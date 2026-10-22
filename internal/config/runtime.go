package config

import (
	"context"
	"log"

	"github.com/mr-karan/logchef/internal/store"
)

// LoadRuntimeConfig loads configuration from both static config and database.
// Database values override static config values for non-essential settings.
func LoadRuntimeConfig(ctx context.Context, staticConfig *Config, settings store.SettingGetter) *Config {
	// Start with the static configuration
	cfg := *staticConfig
	if cfg.Alerts.EvaluationInterval <= 0 {
		cfg.Alerts.EvaluationInterval = defaultAlertsEvaluationInterval
	}
	if cfg.Alerts.DefaultLookback <= 0 {
		cfg.Alerts.DefaultLookback = defaultAlertsDefaultLookback
	}
	if cfg.Auth.SessionDuration <= 0 {
		cfg.Auth.SessionDuration = defaultAuthSessionDuration
	}
	if cfg.Auth.DefaultTokenExpiry <= 0 {
		cfg.Auth.DefaultTokenExpiry = defaultAuthDefaultTokenExpiry
	}

	// If no store is provided, return static config only
	if settings == nil {
		log.Println("no settings store provided, using static configuration only")
		return &cfg
	}

	// Override with database settings for runtime-configurable values

	// Alerts configuration (only essential scheduling settings in config struct)
	// SMTP and delivery settings are read directly from DB by the alerts manager
	cfg.Alerts.Enabled = store.GetBoolSetting(ctx, settings, "alerts.enabled", cfg.Alerts.Enabled)
	cfg.Alerts.EvaluationInterval = store.GetDurationSetting(ctx, settings, "alerts.evaluation_interval", cfg.Alerts.EvaluationInterval)
	cfg.Alerts.DefaultLookback = store.GetDurationSetting(ctx, settings, "alerts.default_lookback", cfg.Alerts.DefaultLookback)
	cfg.Alerts.HistoryLimit = store.GetIntSetting(ctx, settings, "alerts.history_limit", cfg.Alerts.HistoryLimit)

	// AI configuration
	cfg.AI.Enabled = store.GetBoolSetting(ctx, settings, "ai.enabled", cfg.AI.Enabled)
	cfg.AI.APIKey = store.GetSettingWithDefault(ctx, settings, "ai.api_key", cfg.AI.APIKey)
	cfg.AI.BaseURL = store.GetSettingWithDefault(ctx, settings, "ai.base_url", cfg.AI.BaseURL)
	cfg.AI.Model = store.GetSettingWithDefault(ctx, settings, "ai.model", cfg.AI.Model)
	cfg.AI.MaxTokens = store.GetIntSetting(ctx, settings, "ai.max_tokens", cfg.AI.MaxTokens)
	cfg.AI.Temperature = float32(store.GetFloat64Setting(ctx, settings, "ai.temperature", float64(cfg.AI.Temperature)))
	cfg.AI.ReasoningEffort = store.GetSettingWithDefault(ctx, settings, "ai.reasoning_effort", cfg.AI.ReasoningEffort)

	// Auth session management
	cfg.Auth.SessionDuration = store.GetDurationSetting(ctx, settings, "auth.session_duration", cfg.Auth.SessionDuration)
	cfg.Auth.MaxConcurrentSessions = store.GetIntSetting(ctx, settings, "auth.max_concurrent_sessions", cfg.Auth.MaxConcurrentSessions)
	cfg.Auth.DefaultTokenExpiry = store.GetDurationSetting(ctx, settings, "auth.default_token_expiry", cfg.Auth.DefaultTokenExpiry)

	// Server frontend URL
	frontendURL := store.GetSettingWithDefault(ctx, settings, "server.frontend_url", cfg.Server.FrontendURL)
	if frontendURL != "" {
		cfg.Server.FrontendURL = frontendURL
	}

	log.Println("runtime configuration loaded (static config + database settings)")
	return &cfg
}
