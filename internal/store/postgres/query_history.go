package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// bucketDateParam parses a 'YYYY-MM-DD' string into a pgtype.Date for the DATE
// column params. An unparseable value yields an invalid (NULL) date, which the
// aggregate reads treat as "match nothing" rather than erroring.
func bucketDateParam(bucketDate string) pgtype.Date {
	t, err := time.Parse("2006-01-02", bucketDate)
	if err != nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

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

// IncrementQueryStats upserts one executed query into the non-pruned
// query_stats_daily rollup, adding 1 to query_count and durationMs to
// total_duration_ms for the composite key.
func (s *Store) IncrementQueryStats(ctx context.Context, bucketDate string, userID models.UserID, teamID models.TeamID, sourceID models.SourceID, language models.QueryLanguage, durationMs int64) error {
	if err := s.q.IncrementQueryStats(ctx, sqlc.IncrementQueryStatsParams{
		BucketDate:      bucketDateParam(bucketDate),
		UserID:          int64(userID),
		TeamID:          int64(teamID),
		SourceID:        int64(sourceID),
		QueryLanguage:   string(models.NormalizeQueryLanguage(language)),
		TotalDurationMs: durationMs,
	}); err != nil {
		s.log.Error("failed to increment query stats", "error", err, "user_id", userID, "source_id", sourceID)
		return fmt.Errorf("error incrementing query stats: %w", err)
	}
	return nil
}
