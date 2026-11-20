package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// QueryStatsSummary totals the current and prior windows.
func (s *Store) QueryStatsSummary(ctx context.Context, f models.QueryStatsFilter) (models.QueryStatsSummary, error) {
	r, err := s.q.QueryStatsSummary(ctx, sqlc.QueryStatsSummaryParams{
		Since: bucketDateParam(f.Since), PriorSince: bucketDateParam(f.PriorSince), Until: bucketDateParam(f.Until),
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
func (s *Store) QueryCountsByAccountType(ctx context.Context, f models.QueryStatsFilter) ([]models.AccountQueryCount, error) {
	rows, err := s.q.QueryCountsByAccountType(ctx, sqlc.QueryCountsByAccountTypeParams{
		Since: bucketDateParam(f.Since), Until: bucketDateParam(f.Until), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
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
func (s *Store) QueryStatsByDay(ctx context.Context, f models.QueryStatsFilter) ([]models.DailyQueryStat, error) {
	rows, err := s.q.QueryStatsByDay(ctx, sqlc.QueryStatsByDayParams{
		PriorSince: bucketDateParam(f.PriorSince), Until: bucketDateParam(f.Until),
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing query stats by day: %w", err)
	}
	out := make([]models.DailyQueryStat, 0, len(rows))
	for _, r := range rows {
		out = append(out, models.DailyQueryStat{Date: r.BucketDate.Time.Format(time.DateOnly), QueryCount: r.QueryCount, ActiveUsers: r.ActiveUsers})
	}
	return out, nil
}

// QueryStatsByUser returns the users with a current-window query.
func (s *Store) QueryStatsByUser(ctx context.Context, f models.QueryStatsFilter, limit int) ([]models.UserQueryStat, error) {
	rows, err := s.q.QueryStatsByUser(ctx, sqlc.QueryStatsByUserParams{
		Since: bucketDateParam(f.Since), PriorSince: bucketDateParam(f.PriorSince), Until: bucketDateParam(f.Until),
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
		RowLimit: int64(limit),
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
func (s *Store) QueryStatsBySource(ctx context.Context, f models.QueryStatsFilter, limit int) ([]models.SourceQueryStat, error) {
	rows, err := s.q.QueryStatsBySource(ctx, sqlc.QueryStatsBySourceParams{
		Since: bucketDateParam(f.Since), PriorSince: bucketDateParam(f.PriorSince), Until: bucketDateParam(f.Until),
		AccountType: string(f.AccountType), SourceID: int64(f.SourceID), UserID: int64(f.UserID),
		RowLimit: int64(limit),
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
			TeamID:            models.TeamID(r.TeamID),
			ServiceQueryCount: r.ServiceQueryCount,
			Current:           models.QueryTotals{QueryCount: r.QueryCount, TotalDurationMs: r.TotalDurationMs},
			Prior:             models.QueryTotals{QueryCount: r.PriorQueryCount, TotalDurationMs: r.PriorTotalDurationMs},
		})
	}
	return out, nil
}

// QueryStatsByLanguage counts current-window queries per language.
func (s *Store) QueryStatsByLanguage(ctx context.Context, f models.QueryStatsFilter) ([]models.LanguageQueryCount, error) {
	rows, err := s.q.QueryStatsByLanguage(ctx, sqlc.QueryStatsByLanguageParams{
		Since: bucketDateParam(f.Since), Until: bucketDateParam(f.Until),
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
func (s *Store) CountActiveUsersByAccountType(ctx context.Context) (map[models.UserAccountType]int64, error) {
	rows, err := s.q.CountActiveUsersByAccountType(ctx)
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
func (s *Store) TopQueryShapes(ctx context.Context, f models.QueryShapeFilter, limit int) ([]models.QueryShapeStat, error) {
	rows, err := s.q.TopQueryShapes(ctx, sqlc.TopQueryShapesParams{
		Since: f.Since, PriorSince: f.PriorSince, Until: f.Until, SourceID: int64(f.SourceID), RowLimit: int64(limit),
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
		})
	}
	return out, nil
}

// CountUnanalyzedQueryShapes counts the current-window queries no single
// shape stands for.
func (s *Store) CountUnanalyzedQueryShapes(ctx context.Context, f models.QueryShapeFilter) (int64, error) {
	n, err := s.q.CountUnanalyzedQueryShapes(ctx, sqlc.CountUnanalyzedQueryShapesParams{
		Since: f.Since, Until: f.Until, SourceID: int64(f.SourceID),
	})
	if err != nil {
		return 0, fmt.Errorf("error counting unanalyzed query shapes: %w", err)
	}
	return n, nil
}
