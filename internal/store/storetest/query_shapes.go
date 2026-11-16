package storetest

import (
	"context"
	"reflect"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func testQueryShapeStats(t *testing.T, ctx context.Context, s store.Store) {
	const source = models.SourceID(424242) // the rollup has no foreign key
	const (
		day1 = "2026-07-10"
		day2 = "2026-07-11"
	)
	eq := models.FilterShape{{Column: "request_id", Kind: models.MatchEq}}
	nested := models.FilterShape{{Column: "attrs", Path: []string{"a", "b"}, Access: models.AccessNested, Kind: models.MatchEq}}
	inc := func(day, hash string, shape models.FilterShape, count, durationMs, rows int64, lookback *int64) {
		t.Helper()
		err := s.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{BucketDate: day, SourceID: source, Usage: models.QueryShapeUsage{
			QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: hash, Shape: shape,
			QueryCount: count, TotalDurationMs: durationMs, TotalRows: rows, MaxLookbackSeconds: lookback,
		}})
		if err != nil {
			t.Fatalf("IncrementQueryShapeStats(%s, %s): %v", day, hash, err)
		}
	}
	// Two increments of one shape on one day sum the counters, keep the
	// larger lookback, and a later NULL lookback does not erase it.
	inc(day1, "aaaaaaaaaaaaaaaa", eq, 1, 10, 5, new(int64(600)))
	inc(day1, "aaaaaaaaaaaaaaaa", eq, 1, 20, 7, new(int64(3600)))
	inc(day1, "aaaaaaaaaaaaaaaa", eq, 1, 30, 1, new(int64(60)))
	inc(day1, "aaaaaaaaaaaaaaaa", eq, 1, 40, 0, nil)
	// The same shape on another day sums across days in the listing.
	inc(day2, "aaaaaaaaaaaaaaaa", eq, 2, 0, 0, new(int64(7200)))
	// A shape whose only query had no start time.
	inc(day2, "bbbbbbbbbbbbbbbb", nested, 1, 5, 2, nil)
	inc(day2, models.ShapeHashUnanalyzed, models.FilterShape{}, 3, 0, 0, new(int64(60)))
	inc(day2, models.ShapeHashOverflow, models.FilterShape{}, 4, 0, 0, new(int64(60)))
	inc(day2, "cccccccccccccccc", eq, 1, 0, 0, new(int64(60)))

	got, err := s.ListQueryShapeStats(ctx, source, day1, 2)
	if err != nil {
		t.Fatalf("ListQueryShapeStats: %v", err)
	}
	want := []models.QueryShapeUsage{
		{QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: "aaaaaaaaaaaaaaaa", Shape: eq, QueryCount: 6, TotalDurationMs: 100, TotalRows: 13, MaxLookbackSeconds: new(int64(7200))},
		{QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: "bbbbbbbbbbbbbbbb", Shape: nested, QueryCount: 1, TotalDurationMs: 5, TotalRows: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListQueryShapeStats = %+v\nwant %+v", got, want)
	}
	if day1Only, err := s.ListQueryShapeStats(ctx, source, day1, 10); err != nil || len(day1Only) != 3 {
		t.Fatalf("ListQueryShapeStats(limit 10) = %d rows / %v, want the 3 analyzed shapes", len(day1Only), err)
	}

	// Coverage counts every row, including the one past the listing's limit
	// and the two special hashes.
	coverage, err := s.QueryShapeCoverage(ctx, source, day1)
	if err != nil {
		t.Fatalf("QueryShapeCoverage: %v", err)
	}
	wantCoverage := models.QueryShapeCoverage{Days: 2, QueryCount: 15, UnanalyzedCount: 3, OverflowCount: 4, NullLookbackCount: 1, MaxLookbackSeconds: new(int64(7200))}
	if !reflect.DeepEqual(coverage, wantCoverage) {
		t.Fatalf("QueryShapeCoverage = %+v, want %+v", coverage, wantCoverage)
	}
	if empty, err := s.QueryShapeCoverage(ctx, source+1, day1); err != nil || !reflect.DeepEqual(empty, models.QueryShapeCoverage{}) {
		t.Fatalf("QueryShapeCoverage(unknown source) = %+v / %v, want zero", empty, err)
	}

	// Prune deletes only rows before the cutoff.
	if err := s.PruneQueryShapeStats(ctx, day2); err != nil {
		t.Fatalf("PruneQueryShapeStats: %v", err)
	}
	coverage, err = s.QueryShapeCoverage(ctx, source, day1)
	if err != nil {
		t.Fatalf("QueryShapeCoverage after prune: %v", err)
	}
	wantCoverage = models.QueryShapeCoverage{Days: 1, QueryCount: 11, UnanalyzedCount: 3, OverflowCount: 4, NullLookbackCount: 1, MaxLookbackSeconds: new(int64(7200))}
	if !reflect.DeepEqual(coverage, wantCoverage) {
		t.Fatalf("QueryShapeCoverage after prune = %+v, want %+v", coverage, wantCoverage)
	}
}
