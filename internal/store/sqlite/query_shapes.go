package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// IncrementQueryShapeStats adds inc to its query_shape_stats_daily row.
func (db *DB) IncrementQueryShapeStats(ctx context.Context, inc models.QueryShapeIncrement) error {
	shape, err := json.Marshal(inc.Usage.Shape)
	if err != nil {
		return fmt.Errorf("error encoding query shape: %w", err)
	}
	var lookback sql.NullInt64
	if inc.Usage.MaxLookbackSeconds != nil {
		lookback = sql.NullInt64{Int64: *inc.Usage.MaxLookbackSeconds, Valid: true}
	}
	if err := db.writeQueries.IncrementQueryShapeStats(ctx, sqlc.IncrementQueryShapeStatsParams{
		BucketDate:      inc.BucketDate,
		SourceID:        int64(inc.SourceID),
		QueryLanguage:   string(inc.Usage.QueryLanguage),
		ShapeHash:       inc.Usage.ShapeHash,
		Shape:           string(shape),
		QueryCount:      inc.Usage.QueryCount,
		TotalDurationMs: inc.Usage.TotalDurationMs,
		TotalRows:       inc.Usage.TotalRows,
		MaxLookbackS:    lookback,
	}); err != nil {
		return fmt.Errorf("error incrementing query shape stats: %w", err)
	}
	return nil
}

// ListQueryShapeStats returns a source's analyzed shapes since a date, summed
// over days, most used first.
func (db *DB) ListQueryShapeStats(ctx context.Context, sourceID models.SourceID, since string, limit int) ([]models.QueryShapeUsage, error) {
	rows, err := db.readQueries.ListQueryShapeStats(ctx, sqlc.ListQueryShapeStatsParams{
		SourceID: int64(sourceID),
		Since:    since,
		Limit:    int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("error listing query shape stats: %w", err)
	}
	out := make([]models.QueryShapeUsage, 0, len(rows))
	for _, r := range rows {
		var shape models.FilterShape
		if err := json.Unmarshal([]byte(r.Shape), &shape); err != nil {
			return nil, fmt.Errorf("error decoding query shape %s: %w", r.ShapeHash, err)
		}
		out = append(out, models.QueryShapeUsage{
			QueryLanguage:      models.QueryLanguage(r.QueryLanguage),
			ShapeHash:          r.ShapeHash,
			Shape:              shape,
			QueryCount:         r.QueryCount,
			TotalDurationMs:    r.TotalDurationMs,
			TotalRows:          r.TotalRows,
			MaxLookbackSeconds: lookbackOf(r.LookbackRows, r.MaxLookbackS),
		})
	}
	return out, nil
}

// QueryShapeCoverage summarizes every rollup row of a source since a date.
func (db *DB) QueryShapeCoverage(ctx context.Context, sourceID models.SourceID, since string) (models.QueryShapeCoverage, error) {
	r, err := db.readQueries.QueryShapeCoverage(ctx, sqlc.QueryShapeCoverageParams{SourceID: int64(sourceID), Since: since})
	if err != nil {
		return models.QueryShapeCoverage{}, fmt.Errorf("error reading query shape coverage: %w", err)
	}
	return models.QueryShapeCoverage{
		Days:               r.Days,
		FilteredDays:       r.FilteredDays,
		QueryCount:         r.QueryCount,
		UnanalyzedCount:    r.UnanalyzedCount,
		OverflowCount:      r.OverflowCount,
		NullLookbackCount:  r.NullLookbackCount,
		MaxLookbackSeconds: lookbackOf(r.LookbackRows, r.MaxLookbackS),
	}, nil
}

// PruneQueryShapeStats deletes the rollup rows before a date.
func (db *DB) PruneQueryShapeStats(ctx context.Context, before string) error {
	if err := db.writeQueries.PruneQueryShapeStats(ctx, before); err != nil {
		return fmt.Errorf("error pruning query shape stats: %w", err)
	}
	return nil
}

// lookbackOf returns max, or nil when no row in the aggregate had a lookback.
func lookbackOf(rows, maxLookback int64) *int64 {
	if rows == 0 {
		return nil
	}
	return &maxLookback
}
