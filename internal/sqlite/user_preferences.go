package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// GetUserPreferencesJSON retrieves the raw preferences JSON for a user.
func (db *DB) GetUserPreferencesJSON(ctx context.Context, userID models.UserID) (string, error) {
	row, err := db.readQueries.GetUserPreferences(ctx, int64(userID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("failed to get user preferences for user %d: %w", userID, err)
	}

	return row.PreferencesJson, nil
}

// UpsertUserPreferencesJSON inserts or updates the raw preferences JSON for a user.
func (db *DB) UpsertUserPreferencesJSON(ctx context.Context, userID models.UserID, preferencesJSON string) error {
	err := db.writeQueries.UpsertUserPreferences(ctx, sqlc.UpsertUserPreferencesParams{
		UserID:          int64(userID),
		PreferencesJson: preferencesJSON,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert user preferences for user %d: %w", userID, err)
	}
	return nil
}
