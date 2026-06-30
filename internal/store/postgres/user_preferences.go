package postgres

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// GetUserPreferencesJSON retrieves the raw preferences JSON for a user.
func (s *Store) GetUserPreferencesJSON(ctx context.Context, userID models.UserID) (string, error) {
	row, err := s.q.GetUserPreferences(ctx, int64(userID))
	if err != nil {
		if notFound(err) {
			return "", models.ErrNotFound
		}
		return "", fmt.Errorf("failed to get user preferences for user %d: %w", userID, err)
	}
	return row.PreferencesJson, nil
}

// UpsertUserPreferencesJSON inserts or updates the raw preferences JSON for a user.
func (s *Store) UpsertUserPreferencesJSON(ctx context.Context, userID models.UserID, preferencesJSON string) error {
	err := s.q.UpsertUserPreferences(ctx, sqlc.UpsertUserPreferencesParams{
		UserID:          int64(userID),
		PreferencesJson: preferencesJSON,
	})
	if err != nil {
		return fmt.Errorf("failed to upsert user preferences for user %d: %w", userID, err)
	}
	return nil
}
