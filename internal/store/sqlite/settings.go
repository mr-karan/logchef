package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// GetSetting retrieves a setting value from the database.
func (db *DB) GetSetting(ctx context.Context, key string) (string, error) {
	setting, err := db.readQueries.GetSystemSetting(ctx, key)
	if err != nil {
		return "", fmt.Errorf("failed to get setting %s: %w", key, translateNotFound(err))
	}
	return setting.Value, nil
}

// ListSettings retrieves all settings.
func (db *DB) ListSettings(ctx context.Context) ([]*models.SystemSetting, error) {
	settings, err := db.readQueries.ListSystemSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list settings: %w", err)
	}
	return mapSystemSettings(settings), nil
}

// ListSettingsByCategory retrieves settings for a specific category.
func (db *DB) ListSettingsByCategory(ctx context.Context, category string) ([]*models.SystemSetting, error) {
	settings, err := db.readQueries.ListSystemSettingsByCategory(ctx, category)
	if err != nil {
		return nil, fmt.Errorf("failed to list settings for category %s: %w", category, err)
	}
	return mapSystemSettings(settings), nil
}

// mapSystemSettings normalizes sqlc rows into backend-neutral models, folding
// nullable Description and SQLite's 0/1 IsSensitive into Go-native types.
func mapSystemSettings(rows []sqlc.SystemSetting) []*models.SystemSetting {
	out := make([]*models.SystemSetting, 0, len(rows))
	for i := range rows {
		r := rows[i]
		out = append(out, &models.SystemSetting{
			Key:         r.Key,
			Value:       r.Value,
			ValueType:   r.ValueType,
			Category:    r.Category,
			Description: r.Description.String,
			IsSensitive: r.IsSensitive == 1 || models.IsSensitiveSetting(r.Key),
			CreatedAt:   r.CreatedAt,
			UpdatedAt:   r.UpdatedAt,
		})
	}
	return out
}

// UpsertSetting inserts or updates a setting.
func (db *DB) UpsertSetting(ctx context.Context, key, value, valueType, category, description string, isSensitive bool) error {
	isSensitiveInt := int64(0)
	if isSensitive || models.IsSensitiveSetting(key) {
		isSensitiveInt = 1
	}

	err := db.writeQueries.UpsertSystemSetting(ctx, sqlc.UpsertSystemSettingParams{
		Key:         key,
		Value:       value,
		ValueType:   valueType,
		Category:    category,
		Description: sql.NullString{String: description, Valid: description != ""},
		IsSensitive: isSensitiveInt,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert setting %s: %w", key, err)
	}
	return nil
}

// DeleteSetting deletes a setting.
func (db *DB) DeleteSetting(ctx context.Context, key string) error {
	err := db.writeQueries.DeleteSystemSetting(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to delete setting %s: %w", key, err)
	}
	return nil
}
