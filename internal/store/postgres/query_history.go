package postgres

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// RecordQueryHistory inserts one executed-query record, then prunes the user's
// history down to models.QueryHistoryPerUserCap so it stays bounded. The insert
// populates the entry's ID.
func (s *Store) RecordQueryHistory(ctx context.Context, entry *models.QueryHistory) error {
	if entry == nil {
		return fmt.Errorf("query history entry is required")
	}

	id, err := s.q.InsertQueryHistory(ctx, sqlc.InsertQueryHistoryParams{
		UserID:        int64(entry.UserID),
		TeamID:        int64(entry.TeamID),
		SourceID:      int64(entry.SourceID),
		QueryText:     entry.QueryText,
		QueryLanguage: string(entry.QueryLanguage),
		DurationMs:    entry.DurationMs,
		RowCount:      entry.RowCount,
	})
	if err != nil {
		s.log.Error("failed to record query history", "error", err, "user_id", entry.UserID)
		return fmt.Errorf("error recording query history: %w", err)
	}
	entry.ID = id

	if err := s.q.PruneQueryHistoryForUser(ctx, sqlc.PruneQueryHistoryForUserParams{
		UserID: int64(entry.UserID),
		Offset: models.QueryHistoryPerUserCap,
	}); err != nil {
		s.log.Error("failed to prune query history", "error", err, "user_id", entry.UserID)
		return fmt.Errorf("error pruning query history: %w", err)
	}
	return nil
}

// ListQueryHistory returns a user's recent query history, newest first, capped
// at limit.
func (s *Store) ListQueryHistory(ctx context.Context, userID models.UserID, limit int) ([]*models.QueryHistory, error) {
	rows, err := s.q.ListQueryHistory(ctx, sqlc.ListQueryHistoryParams{
		UserID: int64(userID),
		Limit:  int32(limit), //nolint:gosec // G115: limit is a small bounded page size
	})
	if err != nil {
		s.log.Error("failed to list query history", "error", err, "user_id", userID)
		return nil, fmt.Errorf("error listing query history: %w", err)
	}

	history := make([]*models.QueryHistory, 0, len(rows))
	for i := range rows {
		r := rows[i]
		history = append(history, &models.QueryHistory{
			ID:            r.ID,
			UserID:        models.UserID(r.UserID),
			TeamID:        models.TeamID(r.TeamID),
			SourceID:      models.SourceID(r.SourceID),
			QueryText:     r.QueryText,
			QueryLanguage: models.QueryLanguage(r.QueryLanguage),
			DurationMs:    r.DurationMs,
			RowCount:      r.RowCount,
			CreatedAt:     r.CreatedAt.Time,
		})
	}
	return history, nil
}
