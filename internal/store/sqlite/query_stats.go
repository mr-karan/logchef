package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// QueryStatsSummary totals the current and prior windows.
func (db *DB) QueryStatsSummary(ctx context.Context, f models.QueryStatsFilter) (models.QueryStatsSummary, error) {
	r, err := db.readQueries.QueryStatsSummary(ctx, sqlc.QueryStatsSummaryParams{
		Since: f.Since, PriorSince: f.PriorSince, Until: f.Until,
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
	})
	if err != nil {
		return models.QueryStatsSummary{}, fmt.Errorf("error summarizing query stats: %w", err)
	}
	return models.QueryStatsSummary{
		Current: models.QueryStatsPeriod{
			QueryTotals:   models.QueryTotals{QueryCount: r.QueryCount, TotalDurationMs: r.TotalDurationMs},
			ActiveUsers:   r.ActiveUsers,
			ActiveSources: r.ActiveSources,
		},
		Prior: models.QueryStatsPeriod{
			QueryTotals:   models.QueryTotals{QueryCount: r.PriorQueryCount, TotalDurationMs: r.PriorTotalDurationMs},
			ActiveUsers:   r.PriorActiveUsers,
			ActiveSources: r.PriorActiveSources,
		},
	}, nil
}

// QueryCountsByAccountType counts current-window queries per account type.
func (db *DB) QueryCountsByAccountType(ctx context.Context, f models.QueryStatsFilter) ([]models.AccountQueryCount, error) {
	rows, err := db.readQueries.QueryCountsByAccountType(ctx, sqlc.QueryCountsByAccountTypeParams{
		Since: f.Since, Until: f.Until, SourceID: int64(f.SourceID), UserID: int64(f.UserID),
	})
	if err != nil {
		return nil, fmt.Errorf("error counting queries by account type: %w", err)
	}
	out := make([]models.AccountQueryCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.AccountQueryCount{AccountType: models.UserAccountType(r.AccountType), QueryCount: r.QueryCount})
	}
	return out, nil
}

// QueryStatsByDay returns each day of both windows with a query, ascending.
func (db *DB) QueryStatsByDay(ctx context.Context, f models.QueryStatsFilter) ([]models.DailyQueryStat, error) {
	rows, err := db.readQueries.QueryStatsByDay(ctx, sqlc.QueryStatsByDayParams{
		PriorSince: f.PriorSince, Until: f.Until,
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing query stats by day: %w", err)
	}
	out := make([]models.DailyQueryStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.DailyQueryStat{Date: r.BucketDate, QueryCount: r.QueryCount, ActiveUsers: r.ActiveUsers})
	}
	return out, nil
}

// QueryStatsByUser returns the users with a current-window query.
func (db *DB) QueryStatsByUser(ctx context.Context, f models.QueryStatsFilter, limit int) ([]models.UserQueryStat, error) {
	rows, err := db.readQueries.QueryStatsByUser(ctx, sqlc.QueryStatsByUserParams{
		Since: f.Since, PriorSince: f.PriorSince, Until: f.Until,
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
		Limit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing query stats by user: %w", err)
	}
	out := make([]models.UserQueryStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.UserQueryStat{
			UserID:      models.UserID(r.UserID),
			Email:       r.Email,
			FullName:    r.FullName,
			AccountType: models.UserAccountType(r.AccountType),
			ActiveDays:  r.ActiveDays,
			Current:     models.QueryTotals{QueryCount: r.QueryCount, TotalDurationMs: r.TotalDurationMs},
			Prior:       models.QueryTotals{QueryCount: r.PriorQueryCount, TotalDurationMs: r.PriorTotalDurationMs},
		})
	}
	return out, nil
}

// QueryStatsBySource returns the sources with a current-window query.
func (db *DB) QueryStatsBySource(ctx context.Context, f models.QueryStatsFilter, limit int) ([]models.SourceQueryStat, error) {
	rows, err := db.readQueries.QueryStatsBySource(ctx, sqlc.QueryStatsBySourceParams{
		Since: f.Since, PriorSince: f.PriorSince, Until: f.Until,
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
		Limit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing query stats by source: %w", err)
	}
	out := make([]models.SourceQueryStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.SourceQueryStat{
			SourceID:          models.SourceID(r.SourceID),
			SourceName:        r.SourceName,
			SourceType:        r.SourceType,
			TeamID:            nullableTeamID(sql.NullInt64{Int64: r.TeamID, Valid: r.TeamID != 0}),
			ServiceQueryCount: r.ServiceQueryCount,
			Current:           models.QueryTotals{QueryCount: r.QueryCount, TotalDurationMs: r.TotalDurationMs},
			Prior:             models.QueryTotals{QueryCount: r.PriorQueryCount, TotalDurationMs: r.PriorTotalDurationMs},
		})
	}
	return out, nil
}

// QueryStatsByLanguage counts current-window queries per language.
func (db *DB) QueryStatsByLanguage(ctx context.Context, f models.QueryStatsFilter) ([]models.LanguageQueryCount, error) {
	rows, err := db.readQueries.QueryStatsByLanguage(ctx, sqlc.QueryStatsByLanguageParams{
		Since: f.Since, Until: f.Until,
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing query stats by language: %w", err)
	}
	out := make([]models.LanguageQueryCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.LanguageQueryCount{Language: models.QueryLanguage(r.QueryLanguage), QueryCount: r.QueryCount})
	}
	return out, nil
}

// CountActiveUsersByAccountType counts active users per account type.
func (db *DB) CountActiveUsersByAccountType(ctx context.Context) (map[models.UserAccountType]int64, error) {
	rows, err := db.readQueries.CountActiveUsersByAccountType(ctx)
	if err != nil {
		return nil, fmt.Errorf("error counting users by account type: %w", err)
	}
	out := make(map[models.UserAccountType]int64, len(rows))
	for _, r := range rows {
		out[models.UserAccountType(r.AccountType)] = r.UserCount
	}
	return out, nil
}

// TopQueryShapes returns the analyzed shapes with a current-window query.
func (db *DB) TopQueryShapes(ctx context.Context, f models.QueryShapeFilter, limit int) ([]models.QueryShapeStat, error) {
	rows, err := db.readQueries.TopQueryShapes(ctx, sqlc.TopQueryShapesParams{
		Since: f.Since, PriorSince: f.PriorSince, Until: f.Until, SourceID: int64(f.SourceID), Limit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing top query shapes: %w", err)
	}
	out := make([]models.QueryShapeStat, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		var shape models.FilterShape
		if err := json.Unmarshal([]byte(r.Shape), &shape); err != nil {
			return nil, fmt.Errorf("error decoding query shape %s: %w", r.ShapeHash, err)
		}
		out = append(out, models.QueryShapeStat{
			SourceID:           models.SourceID(r.SourceID),
			SourceName:         r.SourceName,
			SourceType:         r.SourceType,
			QueryLanguage:      models.QueryLanguage(r.QueryLanguage),
			ShapeHash:          r.ShapeHash,
			Shape:              shape,
			MaxLookbackSeconds: lookbackOf(r.LookbackRows, r.MaxLookbackS),
			Current:            models.QueryTotals{QueryCount: r.QueryCount, TotalDurationMs: r.TotalDurationMs},
			Prior:              &models.QueryTotals{QueryCount: r.PriorQueryCount, TotalDurationMs: r.PriorTotalDurationMs},
			Scan:               models.ShapeScan{ScannedRuns: r.ScannedCount, RowsRead: r.RowsRead, BytesRead: r.BytesRead},
		})
	}
	return out, nil
}

// CountUnanalyzedQueryShapes counts the current-window queries no single
// shape stands for.
func (db *DB) CountUnanalyzedQueryShapes(ctx context.Context, f models.QueryShapeFilter) (int64, error) {
	n, err := db.readQueries.CountUnanalyzedQueryShapes(ctx, sqlc.CountUnanalyzedQueryShapesParams{
		Since: f.Since, Until: f.Until, SourceID: int64(f.SourceID),
	})
	if err != nil {
		return 0, fmt.Errorf("error counting unanalyzed query shapes: %w", err)
	}
	return n, nil
}

// QueryCostSummary totals the cost of the current and prior windows.
func (db *DB) QueryCostSummary(ctx context.Context, f models.QueryStatsFilter) (models.QueryCostSummary, error) {
	rows, err := db.readQueries.QueryCostSummary(ctx, sqlc.QueryCostSummaryParams{
		Since: f.Since, PriorSince: f.PriorSince, Until: f.Until,
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
	})
	if err != nil {
		return models.QueryCostSummary{}, fmt.Errorf("error summarizing query cost: %w", err)
	}
	var out models.QueryCostSummary
	for _, r := range rows {
		cost := models.RunCost{
			RowsRead:  r.RowsRead,
			BytesRead: r.BytesRead,
			Latency: models.LatencyHistogram{r.LatencyLe100ms, r.LatencyLe250ms, r.LatencyLe500ms, r.LatencyLe1s, r.LatencyLe2500ms,
				r.LatencyLe5s, r.LatencyLe10s, r.LatencyLe30s, r.LatencyLe60s, r.LatencyGt60s},
		}
		if r.IsCurrent != 0 {
			out.Current = cost
		} else {
			out.Prior = cost
		}
	}
	return out, nil
}

// QueryCostBySourceDay returns the cost of each source on each current-window
// day with a successful run.
func (db *DB) QueryCostBySourceDay(ctx context.Context, f models.QueryStatsFilter) ([]models.SourceDayCost, error) {
	rows, err := db.readQueries.QueryCostBySourceDay(ctx, sqlc.QueryCostBySourceDayParams{
		Since: f.Since, Until: f.Until,
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing query cost by source and day: %w", err)
	}
	out := make([]models.SourceDayCost, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.SourceDayCost{
			SourceID: models.SourceID(r.SourceID),
			Date:     r.BucketDate,
			Cost: models.RunCost{
				RowsRead:  r.RowsRead,
				BytesRead: r.BytesRead,
				Latency: models.LatencyHistogram{r.LatencyLe100ms, r.LatencyLe250ms, r.LatencyLe500ms, r.LatencyLe1s, r.LatencyLe2500ms,
					r.LatencyLe5s, r.LatencyLe10s, r.LatencyLe30s, r.LatencyLe60s, r.LatencyGt60s},
			},
		})
	}
	return out, nil
}

// QueryCostCollectedSince returns the first day with a recorded cost or
// failure, "" before any.
func (db *DB) QueryCostCollectedSince(ctx context.Context) (string, error) {
	d, err := db.readQueries.QueryCostCollectedSince(ctx)
	if err != nil {
		return "", fmt.Errorf("error reading when query cost collection began: %w", err)
	}
	return d, nil
}

// IncrementQueryFailureStats upserts one failed query run into the non-pruned
// query_failure_stats_daily rollup.
func (db *DB) IncrementQueryFailureStats(ctx context.Context, inc models.QueryFailureIncrement) error {
	if err := db.writeQueries.IncrementQueryFailureStats(ctx, sqlc.IncrementQueryFailureStatsParams{
		BucketDate:    inc.BucketDate,
		UserID:        int64(inc.UserID),
		TeamID:        int64(inc.TeamID),
		SourceID:      int64(inc.SourceID),
		QueryLanguage: string(models.NormalizeQueryLanguage(inc.Language)),
		Reason:        inc.Reason,
	}); err != nil {
		return fmt.Errorf("error incrementing query failure stats: %w", err)
	}
	return nil
}

// QueryFailuresByReason counts failed runs per reason in both windows.
func (db *DB) QueryFailuresByReason(ctx context.Context, f models.QueryStatsFilter) ([]models.FailureReasonCount, error) {
	rows, err := db.readQueries.QueryFailuresByReason(ctx, sqlc.QueryFailuresByReasonParams{
		Since: f.Since, PriorSince: f.PriorSince, Until: f.Until,
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
	})
	if err != nil {
		return nil, fmt.Errorf("error counting query failures by reason: %w", err)
	}
	out := make([]models.FailureReasonCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.FailureReasonCount{Reason: r.Reason, Count: r.QueryCount, Prior: r.PriorQueryCount})
	}
	return out, nil
}

// QueryFailuresBySource counts current-window failed runs per source.
func (db *DB) QueryFailuresBySource(ctx context.Context, f models.QueryStatsFilter) ([]models.SourceFailureCount, error) {
	rows, err := db.readQueries.QueryFailuresBySource(ctx, sqlc.QueryFailuresBySourceParams{
		Since: f.Since, Until: f.Until,
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
	})
	if err != nil {
		return nil, fmt.Errorf("error counting query failures by source: %w", err)
	}
	out := make([]models.SourceFailureCount, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.SourceFailureCount{SourceID: models.SourceID(r.SourceID), Count: r.QueryCount})
	}
	return out, nil
}

// TopQueryFailures returns the source and user pairs with the most
// current-window failed runs.
func (db *DB) TopQueryFailures(ctx context.Context, f models.QueryStatsFilter, limit int) ([]models.FailurePairStat, error) {
	rows, err := db.readQueries.TopQueryFailures(ctx, sqlc.TopQueryFailuresParams{
		Since: f.Since, Until: f.Until,
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
		Limit: int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing top query failures: %w", err)
	}
	out := make([]models.FailurePairStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.FailurePairStat{
			SourceID:    models.SourceID(r.SourceID),
			SourceName:  r.SourceName,
			SourceType:  r.SourceType,
			UserID:      models.UserID(r.UserID),
			Email:       r.Email,
			FullName:    r.FullName,
			AccountType: models.UserAccountType(r.AccountType),
			Count:       r.QueryCount,
		})
	}
	return out, nil
}
