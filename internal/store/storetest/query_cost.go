package storetest

import (
	"context"
	"reflect"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// queryCostFixture is the cost and failure rollups seeded around a 3-day
// window (2025-03-04 to 2025-03-06) and its prior window (2025-03-01 to
// 2025-03-03), with rows outside both, two humans, a service account and a
// deleted user, on two sources.
type queryCostFixture struct {
	s               store.Store
	hana, ivan, svc *models.User
	ghost           models.UserID // no users row
	srcP, srcQ      *models.Source
}

func (f *queryCostFixture) cost(t *testing.T, ctx context.Context, day string, u models.UserID, src models.SourceID, durationMs, rowsRead, bytesRead int64) {
	t.Helper()
	err := f.s.IncrementQueryStats(ctx, models.QueryStatsIncrement{
		BucketDate: day, UserID: u, TeamID: 1, SourceID: src, Language: models.QueryLanguageLogchefQL,
		DurationMs: durationMs, RowsRead: rowsRead, BytesRead: bytesRead, RowsReturned: 1,
	})
	if err != nil {
		t.Fatalf("IncrementQueryStats(%s, %d): %v", day, u, err)
	}
}

func (f *queryCostFixture) fail(t *testing.T, ctx context.Context, day string, u models.UserID, src models.SourceID, reason string, times int) {
	t.Helper()
	for range times {
		err := f.s.IncrementQueryFailureStats(ctx, models.QueryFailureIncrement{
			BucketDate: day, UserID: u, TeamID: 1, SourceID: src, Language: models.QueryLanguageClickHouseSQL, Reason: reason,
		})
		if err != nil {
			t.Fatalf("IncrementQueryFailureStats(%s, %s): %v", day, reason, err)
		}
	}
}

// scoped is the seeded window narrowed to an account type, source and user.
func (f *queryCostFixture) scoped(account models.UserAccountType, source models.SourceID, user models.UserID) models.QueryStatsFilter {
	return models.QueryStatsFilter{PriorSince: "2025-03-01", Since: "2025-03-04", Until: "2025-03-06", AccountType: account, SourceID: source, UserID: user}
}

// latencyOf builds a histogram from bucket index and count pairs.
func latencyOf(pairs ...int64) models.LatencyHistogram {
	var h models.LatencyHistogram
	for i := 0; i+1 < len(pairs); i += 2 {
		h[pairs[i]] = pairs[i+1]
	}
	return h
}

func testQueryCostAndFailures(t *testing.T, ctx context.Context, s store.Store) {
	f := &queryCostFixture{s: s, ghost: 888101}
	f.hana = mkUser(t, ctx, s, "qc-hana@test.dev")
	f.ivan = mkUser(t, ctx, s, "qc-ivan@test.dev")
	f.svc = &models.User{Email: "svc-qc@service.logchef.internal", FullName: "Alert Evaluator", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: models.UserAccountTypeService}
	if err := s.CreateUser(ctx, f.svc); err != nil {
		t.Fatalf("CreateUser(service): %v", err)
	}
	f.srcP = mkSource(t, ctx, s, "qc_p")
	f.srcQ = mkSource(t, ctx, s, "qc_q")
	hana, ivan, svc, ghost, srcP, srcQ := f.hana.ID, f.ivan.ID, f.svc.ID, f.ghost, f.srcP.ID, f.srcQ.ID

	f.cost(t, ctx, "2025-02-28", hana, srcP, 99_999, 9, 9) // before the prior window
	f.cost(t, ctx, "2025-03-07", hana, srcP, 99_999, 9, 9) // after the window
	f.cost(t, ctx, "2025-03-02", hana, srcP, 300, 10, 1_000)
	f.cost(t, ctx, "2025-03-04", hana, srcP, 50, 100, 2_000)
	f.cost(t, ctx, "2025-03-04", hana, srcP, 70, 100, 2_000)
	f.cost(t, ctx, "2025-03-05", ivan, srcQ, 1_500, 5, 50)
	f.cost(t, ctx, "2025-03-05", svc, srcP, 70_000, 1_000, 1_000_000_000)
	f.cost(t, ctx, "2025-03-06", ghost, srcQ, 30, 1, 10)

	f.fail(t, ctx, "2025-03-02", hana, srcP, "timeout", 1)
	f.fail(t, ctx, "2025-03-04", hana, srcP, "timeout", 2)
	f.fail(t, ctx, "2025-03-05", hana, srcP, "invalid_query", 1)
	f.fail(t, ctx, "2025-03-05", ivan, srcQ, "timeout", 1)
	f.fail(t, ctx, "2025-03-06", svc, srcP, "rejected", 3)
	f.fail(t, ctx, "2025-03-06", ghost, srcQ, "upstream_error", 1)
	f.fail(t, ctx, "2025-03-08", hana, srcP, "timeout", 1) // after the window

	t.Run("summary", f.testSummary)
	t.Run("by source and day", f.testBySourceDay)
	t.Run("failures by reason", f.testFailuresByReason)
	t.Run("failures by source", f.testFailuresBySource)
	t.Run("top failures", f.testTopFailures)
	t.Run("shape scan", f.testShapeScan)
	t.Run("collected since", f.testCollectedSince)
}

func (f *queryCostFixture) testSummary(t *testing.T) {
	humansPrior := models.RunCost{Latency: latencyOf(2, 1), RowsRead: 10, BytesRead: 1_000}
	cases := []struct {
		name   string
		filter models.QueryStatsFilter
		want   models.QueryCostSummary
	}{
		{"humans", f.scoped(qsHuman, 0, 0), models.QueryCostSummary{
			Current: models.RunCost{Latency: latencyOf(0, 2, 4, 1), RowsRead: 205, BytesRead: 4_050}, Prior: humansPrior}},
		{"all, with the deleted user", f.scoped("", 0, 0), models.QueryCostSummary{
			Current: models.RunCost{Latency: latencyOf(0, 3, 4, 1, 9, 1), RowsRead: 1_206, BytesRead: 1_000_004_060}, Prior: humansPrior}},
		{"service accounts", f.scoped(qsService, 0, 0), models.QueryCostSummary{
			Current: models.RunCost{Latency: latencyOf(9, 1), RowsRead: 1_000, BytesRead: 1_000_000_000}}},
		{"one source", f.scoped(qsHuman, f.srcQ.ID, 0), models.QueryCostSummary{
			Current: models.RunCost{Latency: latencyOf(4, 1), RowsRead: 5, BytesRead: 50}}},
		{"one user", f.scoped("", 0, f.hana.ID), models.QueryCostSummary{
			Current: models.RunCost{Latency: latencyOf(0, 2), RowsRead: 200, BytesRead: 4_000}, Prior: humansPrior}},
		{"nothing", f.scoped(qsHuman, 0, f.svc.ID), models.QueryCostSummary{}},
	}
	for i := range cases {
		tc := &cases[i]
		got, err := f.s.QueryCostSummary(t.Context(), tc.filter)
		if err != nil || got != tc.want {
			t.Errorf("%s: QueryCostSummary = %+v / %v\nwant %+v", tc.name, got, err, tc.want)
		}
	}
}

func (f *queryCostFixture) testBySourceDay(t *testing.T) {
	got, err := f.s.QueryCostBySourceDay(t.Context(), f.scoped("", 0, 0))
	want := []models.SourceDayCost{
		{SourceID: f.srcP.ID, Date: "2025-03-04", Cost: models.RunCost{Latency: latencyOf(0, 2), RowsRead: 200, BytesRead: 4_000}},
		{SourceID: f.srcP.ID, Date: "2025-03-05", Cost: models.RunCost{Latency: latencyOf(9, 1), RowsRead: 1_000, BytesRead: 1_000_000_000}},
		{SourceID: f.srcQ.ID, Date: "2025-03-05", Cost: models.RunCost{Latency: latencyOf(4, 1), RowsRead: 5, BytesRead: 50}},
		{SourceID: f.srcQ.ID, Date: "2025-03-06", Cost: models.RunCost{Latency: latencyOf(0, 1), RowsRead: 1, BytesRead: 10}},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryCostBySourceDay(all) = %+v / %v\nwant %+v", got, err, want)
	}
	got, err = f.s.QueryCostBySourceDay(t.Context(), f.scoped(qsHuman, f.srcP.ID, 0))
	want = want[:1]
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryCostBySourceDay(humans on P) = %+v / %v\nwant %+v", got, err, want)
	}
}

func (f *queryCostFixture) testFailuresByReason(t *testing.T) {
	got, err := f.s.QueryFailuresByReason(t.Context(), f.scoped(qsHuman, 0, 0))
	want := []models.FailureReasonCount{{Reason: "timeout", Count: 3, Prior: 1}, {Reason: "invalid_query", Count: 1}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryFailuresByReason(humans) = %+v / %v\nwant %+v", got, err, want)
	}
	// timeout and rejected tie at 3; the one with more prior failures leads.
	got, err = f.s.QueryFailuresByReason(t.Context(), f.scoped("", 0, 0))
	want = []models.FailureReasonCount{
		{Reason: "timeout", Count: 3, Prior: 1}, {Reason: "rejected", Count: 3},
		{Reason: "invalid_query", Count: 1}, {Reason: "upstream_error", Count: 1},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryFailuresByReason(all) = %+v / %v\nwant %+v", got, err, want)
	}
	got, err = f.s.QueryFailuresByReason(t.Context(), f.scoped("", f.srcQ.ID, f.ivan.ID))
	want = []models.FailureReasonCount{{Reason: "timeout", Count: 1}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryFailuresByReason(f.ivan on Q) = %+v / %v\nwant %+v", got, err, want)
	}
}

func (f *queryCostFixture) testFailuresBySource(t *testing.T) {
	got, err := f.s.QueryFailuresBySource(t.Context(), f.scoped(qsHuman, 0, 0))
	want := []models.SourceFailureCount{{SourceID: f.srcP.ID, Count: 3}, {SourceID: f.srcQ.ID, Count: 1}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryFailuresBySource(humans) = %+v / %v\nwant %+v", got, err, want)
	}
	got, err = f.s.QueryFailuresBySource(t.Context(), f.scoped("", 0, 0))
	want = []models.SourceFailureCount{{SourceID: f.srcP.ID, Count: 6}, {SourceID: f.srcQ.ID, Count: 2}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryFailuresBySource(all) = %+v / %v\nwant %+v", got, err, want)
	}
}

func (f *queryCostFixture) testTopFailures(t *testing.T) {
	pair := func(src *models.Source, u *models.User, count int64) models.FailurePairStat {
		return models.FailurePairStat{SourceID: src.ID, SourceName: src.Name, SourceType: "clickhouse", UserID: u.ID, Email: u.Email, FullName: u.FullName, AccountType: u.AccountType, Count: count}
	}
	got, err := f.s.TopQueryFailures(t.Context(), f.scoped("", 0, 0), 5)
	want := []models.FailurePairStat{
		pair(f.srcP, f.hana, 3), pair(f.srcP, f.svc, 3), pair(f.srcQ, f.ivan, 1),
		{SourceID: f.srcQ.ID, SourceName: f.srcQ.Name, SourceType: "clickhouse", UserID: f.ghost, Count: 1},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("TopQueryFailures(all) = %+v / %v\nwant %+v", got, err, want)
	}
	if got, err := f.s.TopQueryFailures(t.Context(), f.scoped("", 0, 0), 2); err != nil || len(got) != 2 {
		t.Errorf("TopQueryFailures(limit 2) = %d rows / %v, want 2", len(got), err)
	}
	got, err = f.s.TopQueryFailures(t.Context(), f.scoped(qsService, 0, 0), 5)
	if err != nil || !reflect.DeepEqual(got, []models.FailurePairStat{pair(f.srcP, f.svc, 3)}) {
		t.Errorf("TopQueryFailures(service) = %+v / %v, want the service account on P", got, err)
	}
}

func (f *queryCostFixture) testShapeScan(t *testing.T) {
	srcR := mkSource(t, t.Context(), f.s, "qc_r")
	eq := models.FilterShape{{Column: "trace_id", Kind: models.MatchEq}}
	shape := func(day string, rowsRead, bytesRead int64) {
		t.Helper()
		err := f.s.IncrementQueryShapeStats(t.Context(), models.QueryShapeIncrement{
			BucketDate: day, SourceID: srcR.ID, RowsRead: rowsRead, BytesRead: bytesRead,
			Usage: models.QueryShapeUsage{QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: "ffffffffffffffff", Shape: eq, QueryCount: 1, TotalDurationMs: 10},
		})
		if err != nil {
			t.Fatalf("IncrementQueryShapeStats(%s): %v", day, err)
		}
	}
	shape("2025-03-04", 10, 100)
	shape("2025-03-04", 20, 200)
	shape("2025-03-05", 0, 0)      // a run that scanned nothing still counts
	shape("2025-03-02", 999, 9999) // prior
	got, err := f.s.TopQueryShapes(t.Context(), models.QueryShapeFilter{PriorSince: "2025-03-01", Since: "2025-03-04", Until: "2025-03-06", SourceID: srcR.ID}, 10)
	want := models.ShapeScan{ScannedRuns: 3, RowsRead: 30, BytesRead: 300}
	if err != nil || len(got) != 1 || got[0].Scan != want || got[0].Current.QueryCount != 3 {
		t.Errorf("TopQueryShapes(source R) = %+v / %v, want 3 runs, scan %+v", got, err, want)
	}
}

func (f *queryCostFixture) testCollectedSince(t *testing.T) {
	f.fail(t, t.Context(), "2020-01-01", f.hana.ID, f.srcP.ID, "timeout", 1)
	if got, err := f.s.QueryCostCollectedSince(t.Context()); err != nil || got != "2020-01-01" {
		t.Errorf("QueryCostCollectedSince after an early failure = %q / %v, want 2020-01-01", got, err)
	}
	f.cost(t, t.Context(), "2019-12-31", f.hana.ID, f.srcP.ID, 10, 0, 0)
	if got, err := f.s.QueryCostCollectedSince(t.Context()); err != nil || got != "2019-12-31" {
		t.Errorf("QueryCostCollectedSince after an earlier run = %q / %v, want 2019-12-31", got, err)
	}
}
