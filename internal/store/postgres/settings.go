package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// GetSetting retrieves a setting value. Returns models.ErrNotFound if absent.
func (s *Store) GetSetting(ctx context.Context, key string) (string, error) {
	setting, err := s.q.GetSystemSetting(ctx, key)
	if err != nil {
		if notFound(err) {
			return "", models.ErrNotFound
		}
		return "", fmt.Errorf("failed to get setting %s: %w", key, err)
	}
	return setting.Value, nil
}

// GetSettingWithDefault retrieves a setting value or returns the default.
func (s *Store) GetSettingWithDefault(ctx context.Context, key, defaultValue string) string {
	value, err := s.GetSetting(ctx, key)
	if err != nil {
		return defaultValue
	}
	return value
}

// GetBoolSetting retrieves a boolean setting value, or the default.
func (s *Store) GetBoolSetting(ctx context.Context, key string, defaultValue bool) bool {
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

// GetIntSetting retrieves an integer setting value, or the default.
func (s *Store) GetIntSetting(ctx context.Context, key string, defaultValue int) int {
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

// GetFloat64Setting retrieves a float64 setting value, or the default.
func (s *Store) GetFloat64Setting(ctx context.Context, key string, defaultValue float64) float64 {
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

// GetDurationSetting retrieves a duration setting value, or the default.
func (s *Store) GetDurationSetting(ctx context.Context, key string, defaultValue time.Duration) time.Duration {
	value, err := s.GetSetting(ctx, key)
	if err != nil {
		return defaultValue
	}
	durationVal, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}
	return durationVal
}

// ListSettings retrieves all settings.
func (s *Store) ListSettings(ctx context.Context) ([]*models.SystemSetting, error) {
	rows, err := s.q.ListSystemSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}
	return mapSystemSettings(rows), nil
}

// ListSettingsByCategory retrieves settings for a specific category.
func (s *Store) ListSettingsByCategory(ctx context.Context, category string) ([]*models.SystemSetting, error) {
	rows, err := s.q.ListSystemSettingsByCategory(ctx, category)
	if err != nil {
		return nil, fmt.Errorf("failed to list settings for category %s: %w", category, err)
	}
	return mapSystemSettings(rows), nil
}

func mapSystemSettings(rows []sqlc.SystemSetting) []*models.SystemSetting {
	out := make([]*models.SystemSetting, 0, len(rows))
	for i := range rows {
		r := rows[i]
		out = append(out, &models.SystemSetting{
			Key:         r.Key,
			Value:       r.Value,
			ValueType:   r.ValueType,
			Category:    r.Category,
			Description: textStr(r.Description),
			IsSensitive: r.IsSensitive,
			CreatedAt:   r.CreatedAt.Time,
			UpdatedAt:   r.UpdatedAt.Time,
		})
	}
	return out
}

// UpsertSetting inserts or updates a setting.
func (s *Store) UpsertSetting(ctx context.Context, key, value, valueType, category, description string, isSensitive bool) error {
	err := s.q.UpsertSystemSetting(ctx, sqlc.UpsertSystemSettingParams{
		Key:         key,
		Value:       value,
		ValueType:   valueType,
		Category:    category,
		Description: text(description),
		IsSensitive: isSensitive,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert setting %s: %w", key, err)
	}
	return nil
}

// DeleteSetting deletes a setting.
func (s *Store) DeleteSetting(ctx context.Context, key string) error {
	if err := s.q.DeleteSystemSetting(ctx, key); err != nil {
		return fmt.Errorf("failed to delete setting %s: %w", key, err)
	}
	return nil
}
