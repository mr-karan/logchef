package sqlite

import (
	"context"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// RecordQueryHistory inserts one executed-query record, then prunes the user's
// history down to models.QueryHistoryPerUserCap so it stays bounded. The insert
// populates the entry's ID.
func (db *DB) RecordQueryHistory(ctx context.Context, entry *models.QueryHistory) error {
	if entry == nil {
		return fmt.Errorf("query history entry is required")
	}

	id, err := db.writeQueries.InsertQueryHistory(ctx, sqlc.InsertQueryHistoryParams{
		UserID:        int64(entry.UserID),
		TeamID:        int64(entry.TeamID),
		SourceID:      int64(entry.SourceID),
		QueryText:     entry.QueryText,
		QueryLanguage: string(entry.QueryLanguage),
		DurationMs:    entry.DurationMs,
		RowCount:      entry.RowCount,
	})
	if err != nil {
		db.log.Error("failed to record query history", "error", err, "user_id", entry.UserID)
		return fmt.Errorf("error recording query history: %w", err)
	}
	entry.ID = id

	if err := db.writeQueries.PruneQueryHistoryForUser(ctx, sqlc.PruneQueryHistoryForUserParams{
		UserID: int64(entry.UserID),
		Offset: models.QueryHistoryPerUserCap,
	}); err != nil {
		db.log.Error("failed to prune query history", "error", err, "user_id", entry.UserID)
		return fmt.Errorf("error pruning query history: %w", err)
	}
	return nil
}

// ListQueryHistory returns a user's recent query history, newest first, capped
// at limit.
func (db *DB) ListQueryHistory(ctx context.Context, userID models.UserID, limit int) ([]*models.QueryHistory, error) {
	rows, err := db.readQueries.ListQueryHistory(ctx, sqlc.ListQueryHistoryParams{
		UserID: int64(userID),
		Limit:  int64(limit),
	})
	if err != nil {
		db.log.Error("failed to list query history", "error", err, "user_id", userID)
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
			CreatedAt:     r.CreatedAt,
		})
	}
	return history, nil
}

// ListQueryActivity returns the most recent query_history rows across all
// users, newest first, capped at limit, enriched with user email and source
// name. Because query_history is capped per user, this is a recent window
// rather than all-time analytics.
func (db *DB) ListQueryActivity(ctx context.Context, limit int) ([]models.QueryActivityRecord, error) {
	rows, err := db.readQueries.ListQueryActivity(ctx, int64(limit))
	if err != nil {
		db.log.Error("failed to list query activity", "error", err)
		return nil, fmt.Errorf("error listing query activity: %w", err)
	}

	records := make([]models.QueryActivityRecord, 0, len(rows))
	for i := range rows {
		r := rows[i]
		records = append(records, models.QueryActivityRecord{
			ID:            r.ID,
			UserID:        models.UserID(r.UserID),
			UserEmail:     r.UserEmail,
			TeamID:        models.TeamID(r.TeamID),
			SourceID:      models.SourceID(r.SourceID),
			SourceName:    r.SourceName.String,
			QueryText:     r.QueryText,
			QueryLanguage: models.QueryLanguage(r.QueryLanguage),
			DurationMs:    r.DurationMs,
			RowCount:      r.RowCount,
			CreatedAt:     r.CreatedAt,
		})
	}
	return records, nil
}

// IncrementQueryStats upserts one executed query into the non-pruned
// query_stats_daily rollup, adding 1 to query_count and durationMs to
// total_duration_ms for the composite key.
func (db *DB) IncrementQueryStats(ctx context.Context, bucketDate string, userID models.UserID, teamID models.TeamID, sourceID models.SourceID, language models.QueryLanguage, durationMs int64) error {
	if err := db.writeQueries.IncrementQueryStats(ctx, sqlc.IncrementQueryStatsParams{
		BucketDate:      bucketDate,
		UserID:          int64(userID),
		TeamID:          int64(teamID),
		SourceID:        int64(sourceID),
		QueryLanguage:   string(models.NormalizeQueryLanguage(language)),
		TotalDurationMs: durationMs,
	}); err != nil {
		db.log.Error("failed to increment query stats", "error", err, "user_id", userID, "source_id", sourceID)
		return fmt.Errorf("error incrementing query stats: %w", err)
	}
	return nil
}

// TopSourcesByQueries returns sources ordered by total query count desc (capped
// at limit) over rollup rows with bucket_date >= since.
func (db *DB) TopSourcesByQueries(ctx context.Context, since string, limit int) ([]models.SourceQueryStat, error) {
	rows, err := db.readQueries.TopSourcesByQueries(ctx, sqlc.TopSourcesByQueriesParams{
		BucketDate: since,
		Limit:      int64(limit),
	})
	if err != nil {
		db.log.Error("failed to list top sources by queries", "error", err)
		return nil, fmt.Errorf("error listing top sources by queries: %w", err)
	}
	out := make([]models.SourceQueryStat, 0, len(rows))
	for i := range rows {
		r := rows[i]
		out = append(out, models.SourceQueryStat{
			SourceID:      r.SourceID,
			SourceName:    r.SourceName,
			QueryCount:    r.QueryCount,
			AvgDurationMs: r.AvgDurationMs,
		})
	}
	return out, nil
}

// TopUsersByQueries returns users ordered by total query count desc (capped at
// limit) over rollup rows with bucket_date >= since.
func (db *DB) TopUsersByQueries(ctx context.Context, since string, limit int) ([]models.UserQueryStat, error) {
	rows, err := db.readQueries.TopUsersByQueries(ctx, sqlc.TopUsersByQueriesParams{
		BucketDate: since,
		Limit:      int64(limit),
	})
	if err != nil {
		db.log.Error("failed to list top users by queries", "error", err)
		return nil, fmt.Errorf("error listing top users by queries: %w", err)
	}
	out := make([]models.UserQueryStat, 0, len(rows))
	for i := range rows {
		r := rows[i]
		out = append(out, models.UserQueryStat{
			UserID:     models.UserID(r.UserID),
			UserEmail:  r.UserEmail,
			QueryCount: r.QueryCount,
		})
	}
	return out, nil
}

// QueryVolumeByDay returns per-day total query counts (ascending by date) over
// rollup rows with bucket_date >= since.
func (db *DB) QueryVolumeByDay(ctx context.Context, since string) ([]models.DailyQueryVolume, error) {
	rows, err := db.readQueries.QueryVolumeByDay(ctx, since)
	if err != nil {
		db.log.Error("failed to list query volume by day", "error", err)
		return nil, fmt.Errorf("error listing query volume by day: %w", err)
	}
	out := make([]models.DailyQueryVolume, 0, len(rows))
	for i := range rows {
		r := rows[i]
		out = append(out, models.DailyQueryVolume{
			Date:       r.BucketDate,
			QueryCount: r.QueryCount,
		})
	}
	return out, nil
}
