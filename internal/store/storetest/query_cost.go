package storetest

import (
	"context"
	"reflect"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// testQueryCostAndFailures seeds the cost and failure rollups around a 3-day
// window (2025-03-04 to 2025-03-06) and its prior window (2025-03-01 to
// 2025-03-03), with rows outside both, two humans, a service account and a
// deleted user, on two sources.
func testQueryCostAndFailures(t *testing.T, ctx context.Context, s store.Store) {
	hana := mkUser(t, ctx, s, "qc-hana@test.dev")
	ivan := mkUser(t, ctx, s, "qc-ivan@test.dev")
	svc := &models.User{Email: "svc-qc@service.logchef.internal", FullName: "Alert Evaluator", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: models.UserAccountTypeService}
	if err := s.CreateUser(ctx, svc); err != nil {
		t.Fatalf("CreateUser(service): %v", err)
	}
	const ghost = models.UserID(888101) // no users row
	srcP := mkSource(t, ctx, s, "qc_p")
	srcQ := mkSource(t, ctx, s, "qc_q")

	cost := func(day string, u models.UserID, src models.SourceID, durationMs, rowsRead, bytesRead int64) {
		t.Helper()
		err := s.IncrementQueryStats(ctx, models.QueryStatsIncrement{
			BucketDate: day, UserID: u, TeamID: 1, SourceID: src, Language: models.QueryLanguageLogchefQL,
			DurationMs: durationMs, RowsRead: rowsRead, BytesRead: bytesRead, RowsReturned: 1,
		})
		if err != nil {
			t.Fatalf("IncrementQueryStats(%s, %d): %v", day, u, err)
		}
	}
	cost("2025-02-28", hana.ID, srcP.ID, 99_999, 9, 9) // before the prior window
	cost("2025-03-07", hana.ID, srcP.ID, 99_999, 9, 9) // after the window
	cost("2025-03-02", hana.ID, srcP.ID, 300, 10, 1_000)
	cost("2025-03-04", hana.ID, srcP.ID, 50, 100, 2_000)
	cost("2025-03-04", hana.ID, srcP.ID, 70, 100, 2_000)
	cost("2025-03-05", ivan.ID, srcQ.ID, 1_500, 5, 50)
	cost("2025-03-05", svc.ID, srcP.ID, 70_000, 1_000, 1_000_000_000)
	cost("2025-03-06", ghost, srcQ.ID, 30, 1, 10)

	fail := func(day string, u models.UserID, src models.SourceID, reason string, times int) {
		t.Helper()
		for range times {
			err := s.IncrementQueryFailureStats(ctx, models.QueryFailureIncrement{
				BucketDate: day, UserID: u, TeamID: 1, SourceID: src, Language: models.QueryLanguageClickHouseSQL, Reason: reason,
			})
			if err != nil {
				t.Fatalf("IncrementQueryFailureStats(%s, %s): %v", day, reason, err)
			}
		}
	}
	fail("2025-03-02", hana.ID, srcP.ID, "timeout", 1)
	fail("2025-03-04", hana.ID, srcP.ID, "timeout", 2)
	fail("2025-03-05", hana.ID, srcP.ID, "invalid_query", 1)
	fail("2025-03-05", ivan.ID, srcQ.ID, "timeout", 1)
	fail("2025-03-06", svc.ID, srcP.ID, "rejected", 3)
	fail("2025-03-06", ghost, srcQ.ID, "upstream_error", 1)
	fail("2025-03-08", hana.ID, srcP.ID, "timeout", 1) // after the window

	window := models.QueryStatsFilter{PriorSince: "2025-03-01", Since: "2025-03-04", Until: "2025-03-06"}
	scoped := func(account models.UserAccountType, source models.SourceID, user models.UserID) models.QueryStatsFilter {
		f := window
		f.AccountType, f.SourceID, f.UserID = account, source, user
		return f
	}
	// latency builds a histogram from bucket index and count pairs.
	latency := func(pairs ...int64) models.LatencyHistogram {
		var h models.LatencyHistogram
		for i := 0; i < len(pairs); i += 2 {
			h[pairs[i]] = pairs[i+1]
		}
		return h
	}

	t.Run("summary", func(t *testing.T) {
		humansPrior := models.RunCost{Latency: latency(2, 1), RowsRead: 10, BytesRead: 1_000}
		for _, tc := range []struct {
			name   string
			filter models.QueryStatsFilter
			want   models.QueryCostSummary
		}{
			{"humans", scoped(qsHuman, 0, 0), models.QueryCostSummary{
				Current: models.RunCost{Latency: latency(0, 2, 4, 1), RowsRead: 205, BytesRead: 4_050}, Prior: humansPrior}},
			{"all, with the deleted user", scoped("", 0, 0), models.QueryCostSummary{
				Current: models.RunCost{Latency: latency(0, 3, 4, 1, 9, 1), RowsRead: 1_206, BytesRead: 1_000_004_060}, Prior: humansPrior}},
			{"service accounts", scoped(qsService, 0, 0), models.QueryCostSummary{
				Current: models.RunCost{Latency: latency(9, 1), RowsRead: 1_000, BytesRead: 1_000_000_000}}},
			{"one source", scoped(qsHuman, srcQ.ID, 0), models.QueryCostSummary{
				Current: models.RunCost{Latency: latency(4, 1), RowsRead: 5, BytesRead: 50}}},
			{"one user", scoped("", 0, hana.ID), models.QueryCostSummary{
				Current: models.RunCost{Latency: latency(0, 2), RowsRead: 200, BytesRead: 4_000}, Prior: humansPrior}},
			{"nothing", scoped(qsHuman, 0, svc.ID), models.QueryCostSummary{}},
		} {
			got, err := s.QueryCostSummary(ctx, tc.filter)
			if err != nil || got != tc.want {
				t.Errorf("%s: QueryCostSummary = %+v / %v\nwant %+v", tc.name, got, err, tc.want)
			}
		}
	})

	t.Run("by source and day", func(t *testing.T) {
		got, err := s.QueryCostBySourceDay(ctx, scoped("", 0, 0))
		want := []models.SourceDayCost{
			{SourceID: srcP.ID, Date: "2025-03-04", Cost: models.RunCost{Latency: latency(0, 2), RowsRead: 200, BytesRead: 4_000}},
			{SourceID: srcP.ID, Date: "2025-03-05", Cost: models.RunCost{Latency: latency(9, 1), RowsRead: 1_000, BytesRead: 1_000_000_000}},
			{SourceID: srcQ.ID, Date: "2025-03-05", Cost: models.RunCost{Latency: latency(4, 1), RowsRead: 5, BytesRead: 50}},
			{SourceID: srcQ.ID, Date: "2025-03-06", Cost: models.RunCost{Latency: latency(0, 1), RowsRead: 1, BytesRead: 10}},
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("QueryCostBySourceDay(all) = %+v / %v\nwant %+v", got, err, want)
		}
		got, err = s.QueryCostBySourceDay(ctx, scoped(qsHuman, srcP.ID, 0))
		want = want[:1]
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("QueryCostBySourceDay(humans on P) = %+v / %v\nwant %+v", got, err, want)
		}
	})

	t.Run("failures by reason", func(t *testing.T) {
		got, err := s.QueryFailuresByReason(ctx, scoped(qsHuman, 0, 0))
		want := []models.FailureReasonCount{{Reason: "timeout", Count: 3, Prior: 1}, {Reason: "invalid_query", Count: 1}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("QueryFailuresByReason(humans) = %+v / %v\nwant %+v", got, err, want)
		}
		// timeout and rejected tie at 3; the one with more prior failures leads.
		got, err = s.QueryFailuresByReason(ctx, scoped("", 0, 0))
		want = []models.FailureReasonCount{
			{Reason: "timeout", Count: 3, Prior: 1}, {Reason: "rejected", Count: 3},
			{Reason: "invalid_query", Count: 1}, {Reason: "upstream_error", Count: 1},
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("QueryFailuresByReason(all) = %+v / %v\nwant %+v", got, err, want)
		}
		got, err = s.QueryFailuresByReason(ctx, scoped("", srcQ.ID, ivan.ID))
		want = []models.FailureReasonCount{{Reason: "timeout", Count: 1}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("QueryFailuresByReason(ivan on Q) = %+v / %v\nwant %+v", got, err, want)
		}
	})

	t.Run("failures by source", func(t *testing.T) {
		got, err := s.QueryFailuresBySource(ctx, scoped(qsHuman, 0, 0))
		want := []models.SourceFailureCount{{SourceID: srcP.ID, Count: 3}, {SourceID: srcQ.ID, Count: 1}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("QueryFailuresBySource(humans) = %+v / %v\nwant %+v", got, err, want)
		}
		got, err = s.QueryFailuresBySource(ctx, scoped("", 0, 0))
		want = []models.SourceFailureCount{{SourceID: srcP.ID, Count: 6}, {SourceID: srcQ.ID, Count: 2}}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("QueryFailuresBySource(all) = %+v / %v\nwant %+v", got, err, want)
		}
	})

	t.Run("top failures", func(t *testing.T) {
		pair := func(src *models.Source, u *models.User, count int64) models.FailurePairStat {
			return models.FailurePairStat{SourceID: src.ID, SourceName: src.Name, SourceType: "clickhouse", UserID: u.ID, Email: u.Email, FullName: u.FullName, AccountType: u.AccountType, Count: count}
		}
		got, err := s.TopQueryFailures(ctx, scoped("", 0, 0), 5)
		want := []models.FailurePairStat{
			pair(srcP, hana, 3), pair(srcP, svc, 3), pair(srcQ, ivan, 1),
			{SourceID: srcQ.ID, SourceName: srcQ.Name, SourceType: "clickhouse", UserID: ghost, Count: 1},
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("TopQueryFailures(all) = %+v / %v\nwant %+v", got, err, want)
		}
		if got, err := s.TopQueryFailures(ctx, scoped("", 0, 0), 2); err != nil || len(got) != 2 {
			t.Errorf("TopQueryFailures(limit 2) = %d rows / %v, want 2", len(got), err)
		}
		got, err = s.TopQueryFailures(ctx, scoped(qsService, 0, 0), 5)
		if err != nil || !reflect.DeepEqual(got, []models.FailurePairStat{pair(srcP, svc, 3)}) {
			t.Errorf("TopQueryFailures(service) = %+v / %v, want the service account on P", got, err)
		}
	})

	t.Run("shape scan", func(t *testing.T) {
		srcR := mkSource(t, ctx, s, "qc_r")
		eq := models.FilterShape{{Column: "trace_id", Kind: models.MatchEq}}
		shape := func(day string, rowsRead, bytesRead int64) {
			t.Helper()
			err := s.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{
				BucketDate: day, SourceID: srcR.ID, RowsRead: rowsRead, BytesRead: bytesRead,
				Usage: models.QueryShapeUsage{QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: "ffffffffffffffff", Shape: eq, QueryCount: 1, TotalDurationMs: 10},
			})
			if err != nil {
				t.Fatalf("IncrementQueryShapeStats(%s): %v", day, err)
			}
		}
		shape("2025-03-04", 10, 100)
		shape("2025-03-04", 20, 200)
		shape("2025-03-05", 0, 0)      // a day recorded before scans were
		shape("2025-03-02", 999, 9999) // prior
		got, err := s.TopQueryShapes(ctx, models.QueryShapeFilter{PriorSince: "2025-03-01", Since: "2025-03-04", Until: "2025-03-06", SourceID: srcR.ID}, 10)
		want := models.ShapeScan{ScannedRuns: 2, RowsRead: 30, BytesRead: 300}
		if err != nil || len(got) != 1 || got[0].Scan != want || got[0].Current.QueryCount != 3 {
			t.Errorf("TopQueryShapes(source R) = %+v / %v, want 3 runs, scan %+v", got, err, want)
		}
	})

	t.Run("collected since", func(t *testing.T) {
		fail("2020-01-01", hana.ID, srcP.ID, "timeout", 1)
		if got, err := s.QueryCostCollectedSince(ctx); err != nil || got != "2020-01-01" {
			t.Errorf("QueryCostCollectedSince after an early failure = %q / %v, want 2020-01-01", got, err)
		}
		cost("2019-12-31", hana.ID, srcP.ID, 10, 0, 0)
		if got, err := s.QueryCostCollectedSince(ctx); err != nil || got != "2019-12-31" {
			t.Errorf("QueryCostCollectedSince after an earlier run = %q / %v, want 2019-12-31", got, err)
		}
	})
}
