package store

import (
	"context"
	"strconv"
	"time"
)

// SettingGetter reads a raw setting value.
type SettingGetter interface {
	GetSetting(ctx context.Context, key string) (string, error)
}

// SettingsReader parses settings and falls back to caller-provided defaults.
type SettingsReader struct {
	SettingGetter
}

// GetSettingWithDefault retrieves a setting value or returns the default on a read error.
func (s SettingsReader) GetSettingWithDefault(ctx context.Context, key, defaultValue string) string {
	value, err := s.GetSetting(ctx, key)
	if err != nil {
		return defaultValue
	}
	return value
}

// GetBoolSetting retrieves a boolean setting value.
func (s SettingsReader) GetBoolSetting(ctx context.Context, key string, defaultValue bool) bool {
	value, err := s.GetSetting(ctx, key)
	if err != nil {
		return defaultValue
	}
	boolVal, err := strconv.ParseBool(value)
	if err != nil {
		return defaultValue
	}
	return boolVal
}

// GetIntSetting retrieves an integer setting value.
func (s SettingsReader) GetIntSetting(ctx context.Context, key string, defaultValue int) int {
	value, err := s.GetSetting(ctx, key)
	if err != nil {
		return defaultValue
	}
	intVal, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return intVal
}

// GetFloat64Setting retrieves a float64 setting value.
func (s SettingsReader) GetFloat64Setting(ctx context.Context, key string, defaultValue float64) float64 {
	value, err := s.GetSetting(ctx, key)
	if err != nil {
		return defaultValue
	}
	floatVal, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return defaultValue
	}
	return floatVal
}

// GetDurationSetting retrieves a duration setting value.
func (s SettingsReader) GetDurationSetting(ctx context.Context, key string, defaultValue time.Duration) time.Duration {
	value, err := s.GetSetting(ctx, key)
	if err != nil {
		return defaultValue
	}
	durationVal, err := time.ParseDuration(value)
	if err != nil || durationVal <= 0 {
		return defaultValue
	}
	return durationVal
}
