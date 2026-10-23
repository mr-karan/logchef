package store

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// SettingGetter reads a raw setting value.
type SettingGetter interface {
	GetSetting(ctx context.Context, key string) (string, error)
}

// GetSettingWithDefault retrieves a setting value or returns the default on a read error.
func GetSettingWithDefault(ctx context.Context, s SettingGetter, key, defaultValue string) string {
	value, err := getSetting(ctx, s, key)
	if err != nil {
		return defaultValue
	}
	return value
}

// GetBoolSetting retrieves a boolean setting value.
func GetBoolSetting(ctx context.Context, s SettingGetter, key string, defaultValue bool) bool {
	value, err := getSetting(ctx, s, key)
	if err != nil {
		return defaultValue
	}
	boolVal, err := strconv.ParseBool(value)
	if err != nil {
		slog.WarnContext(ctx, "invalid setting; using default", "key", key, "error", "invalid boolean value")
		return defaultValue
	}
	return boolVal
}

// GetIntSetting retrieves an integer setting value.
func GetIntSetting(ctx context.Context, s SettingGetter, key string, defaultValue int) int {
	value, err := getSetting(ctx, s, key)
	if err != nil {
		return defaultValue
	}
	intVal, err := strconv.Atoi(value)
	if err != nil {
		slog.WarnContext(ctx, "invalid setting; using default", "key", key, "error", "invalid integer value")
		return defaultValue
	}
	return intVal
}

// GetFloat64Setting retrieves a float64 setting value.
func GetFloat64Setting(ctx context.Context, s SettingGetter, key string, defaultValue float64) float64 {
	value, err := getSetting(ctx, s, key)
	if err != nil {
		return defaultValue
	}
	floatVal, err := strconv.ParseFloat(value, 64)
	if err != nil {
		slog.WarnContext(ctx, "invalid setting; using default", "key", key, "error", "invalid float64 value")
		return defaultValue
	}
	return floatVal
}

// GetDurationSetting retrieves a duration setting value; zero and negative values fall back to the default.
func GetDurationSetting(ctx context.Context, s SettingGetter, key string, defaultValue time.Duration) time.Duration {
	value, err := getSetting(ctx, s, key)
	if err != nil {
		return defaultValue
	}
	durationVal, err := time.ParseDuration(value)
	if err != nil || durationVal <= 0 {
		slog.WarnContext(ctx, "invalid setting; using default", "key", key, "error", "duration must be valid and positive")
		return defaultValue
	}
	return durationVal
}

func getSetting(ctx context.Context, s SettingGetter, key string) (string, error) {
	value, err := s.GetSetting(ctx, key)
	if err != nil && !errors.Is(err, models.ErrNotFound) {
		slog.WarnContext(ctx, "failed to read setting; using default", "key", key, "error", err)
	}
	return value, err
}
