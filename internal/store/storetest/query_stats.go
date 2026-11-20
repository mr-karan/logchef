package storetest

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// testQueryStats seeds query_stats_daily around a 3-day window (2025-01-04
// to 2025-01-06) and its prior window (2025-01-01 to 2025-01-03), with rows
// on both edges and outside, two human users with current queries, one human
// with prior queries only, one service account and one deleted user.
func testQueryStats(t *testing.T, ctx context.Context, s store.Store) {
	usersBefore, err := s.CountActiveUsersByAccountType(ctx)
	if err != nil {
		t.Fatalf("CountActiveUsersByAccountType: %v", err)
	}
	alice := mkUser(t, ctx, s, "qs-alice@test.dev")
	bob := mkUser(t, ctx, s, "qs-bob@test.dev")
	carol := mkUser(t, ctx, s, "qs-carol@test.dev")
	svc := &models.User{Email: "svc-qs@service.logchef.internal", FullName: "Error Digest Worker", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: models.UserAccountTypeService}
	if err := s.CreateUser(ctx, svc); err != nil {
		t.Fatalf("CreateUser(service): %v", err)
	}
	inactive := &models.User{Email: "qs-inactive@test.dev", FullName: "Inactive", Role: models.UserRoleMember, Status: models.UserStatusInactive}
	if err := s.CreateUser(ctx, inactive); err != nil {
		t.Fatalf("CreateUser(inactive): %v", err)
	}
	const ghost = models.UserID(888001) // no users row
	srcA := mkSource(t, ctx, s, "qs_a")
	srcB := mkSource(t, ctx, s, "qs_b")
	// Source A's queries ran in oldTeam, which no longer links it; newTeam
	// does. No team links source B.
	oldTeam, newTeam := &models.Team{Name: "qs-old"}, &models.Team{Name: "qs-new"}
	for _, team := range []*models.Team{oldTeam, newTeam} {
		if err := s.CreateTeam(ctx, team); err != nil {
			t.Fatalf("CreateTeam(%s): %v", team.Name, err)
		}
	}
	if err := s.AddTeamSource(ctx, newTeam.ID, srcA.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}

	inc := func(day string, u models.UserID, team models.TeamID, src models.SourceID, lang models.QueryLanguage, dur int64) {
		t.Helper()
		if err := s.IncrementQueryStats(ctx, models.QueryStatsIncrement{BucketDate: day, UserID: u, TeamID: team, SourceID: src, Language: lang, DurationMs: dur}); err != nil {
			t.Fatalf("IncrementQueryStats(%s,%d,%d): %v", day, u, src, err)
		}
	}
	logchefql, sql, logql := models.QueryLanguageLogchefQL, models.QueryLanguageClickHouseSQL, models.QueryLanguageLogQL
	inc("2024-12-31", alice.ID, oldTeam.ID, srcA.ID, logchefql, 999) // before the prior window
	inc("2025-01-07", alice.ID, oldTeam.ID, srcA.ID, logchefql, 999) // after the window
	inc("2025-01-03", alice.ID, oldTeam.ID, srcA.ID, logchefql, 100) // last prior day
	inc("2025-01-01", bob.ID, 3, srcB.ID, sql, 300)
	inc("2025-01-02", svc.ID, 9, srcB.ID, logchefql, 500)
	inc("2025-01-04", alice.ID, oldTeam.ID, srcA.ID, logchefql, 50) // first current day
	inc("2025-01-04", alice.ID, oldTeam.ID, srcA.ID, logchefql, 70)
	inc("2025-01-05", carol.ID, 3, srcB.ID, logchefql, 10)
	for range 3 {
		inc("2025-01-05", svc.ID, oldTeam.ID, srcA.ID, logchefql, 1000)
	}
	inc("2025-01-06", alice.ID, 3, srcB.ID, sql, 30)
	inc("2025-01-06", ghost, 3, srcB.ID, logql, 40)

	fx := queryStatsFixture{
		s: s, alice: alice, carol: carol, svc: svc, ghost: ghost, srcA: srcA, srcB: srcB, srcATeam: newTeam.ID,
		window: models.QueryStatsFilter{PriorSince: "2025-01-01", Since: "2025-01-04", Until: "2025-01-06"},
	}
	t.Run("summary", func(t *testing.T) { verifyQueryStatsSummary(t, ctx, fx) })
	t.Run("account counts", func(t *testing.T) { verifyQueryCountsByAccountType(t, ctx, fx) })
	t.Run("by day", func(t *testing.T) { verifyQueryStatsByDay(t, ctx, fx) })
	t.Run("by user", func(t *testing.T) { verifyQueryStatsByUser(t, ctx, fx) })
	t.Run("by source", func(t *testing.T) { verifyQueryStatsBySource(t, ctx, fx) })
	t.Run("by language", func(t *testing.T) { verifyQueryStatsByLanguage(t, ctx, fx) })
	t.Run("active users", func(t *testing.T) {
		got, err := s.CountActiveUsersByAccountType(ctx)
		if err != nil {
			t.Fatalf("CountActiveUsersByAccountType: %v", err)
		}
		if got[models.UserAccountTypeHuman]-usersBefore[models.UserAccountTypeHuman] != 3 || got[models.UserAccountTypeService]-usersBefore[models.UserAccountTypeService] != 1 {
			t.Errorf("active users went from %v to %v, want 3 more humans and 1 more service account", usersBefore, got)
		}
	})
}

type queryStatsFixture struct {
	s            store.Store
	alice, carol *models.User
	svc          *models.User
	ghost        models.UserID
	srcA, srcB   *models.Source
	srcATeam     models.TeamID
	window       models.QueryStatsFilter
}

func (fx queryStatsFixture) scoped(account models.UserAccountType, source models.SourceID, user models.UserID) models.QueryStatsFilter {
	f := fx.window
	f.AccountType, f.SourceID, f.UserID = account, source, user
	return f
}

const (
	qsHuman   = models.UserAccountTypeHuman
	qsService = models.UserAccountTypeService
)

func qsTotals(count, duration int64) models.QueryTotals {
	return models.QueryTotals{QueryCount: count, TotalDurationMs: duration}
}

func qsPeriod(count, duration, users, sources int64) models.QueryStatsPeriod {
	return models.QueryStatsPeriod{QueryTotals: qsTotals(count, duration), ActiveUsers: users, ActiveSources: sources}
}

func verifyQueryStatsSummary(t *testing.T, ctx context.Context, fx queryStatsFixture) {
	cases := []struct {
		name   string
		filter models.QueryStatsFilter
		want   models.QueryStatsSummary
	}{
		{"humans", fx.scoped(qsHuman, 0, 0), models.QueryStatsSummary{Current: qsPeriod(4, 160, 2, 2), Prior: qsPeriod(2, 400, 2, 2)}},
		{"service accounts", fx.scoped(qsService, 0, 0), models.QueryStatsSummary{Current: qsPeriod(3, 3000, 1, 1), Prior: qsPeriod(1, 500, 1, 1)}},
		{"all, with the deleted user", fx.scoped("", 0, 0), models.QueryStatsSummary{Current: qsPeriod(8, 3200, 4, 2), Prior: qsPeriod(3, 900, 3, 2)}},
		{"one source", fx.scoped(qsHuman, fx.srcB.ID, 0), models.QueryStatsSummary{Current: qsPeriod(2, 40, 2, 1), Prior: qsPeriod(1, 300, 1, 1)}},
		{"one user", fx.scoped("", 0, fx.alice.ID), models.QueryStatsSummary{Current: qsPeriod(3, 150, 1, 2), Prior: qsPeriod(1, 100, 1, 1)}},
		{"one user on one source", fx.scoped(qsHuman, fx.srcA.ID, fx.alice.ID), models.QueryStatsSummary{Current: qsPeriod(2, 120, 1, 1), Prior: qsPeriod(1, 100, 1, 1)}},
		{"a service account under humans", fx.scoped(qsHuman, 0, fx.svc.ID), models.QueryStatsSummary{}},
	}
	for i := range cases {
		tc := &cases[i]
		got, err := fx.s.QueryStatsSummary(ctx, tc.filter)
		if err != nil || got != tc.want {
			t.Errorf("%s: QueryStatsSummary = %+v / %v, want %+v", tc.name, got, err, tc.want)
		}
	}
}

func verifyQueryCountsByAccountType(t *testing.T, ctx context.Context, fx queryStatsFixture) {
	got, err := fx.s.QueryCountsByAccountType(ctx, fx.scoped(qsHuman, 0, 0))
	want := map[models.UserAccountType]int64{qsHuman: 4, qsService: 3, "": 1}
	if err != nil || !reflect.DeepEqual(accountCounts(got), want) {
		t.Errorf("QueryCountsByAccountType = %+v / %v, want %v", got, err, want)
	}
	got, err = fx.s.QueryCountsByAccountType(ctx, fx.scoped(qsHuman, fx.srcA.ID, 0))
	want = map[models.UserAccountType]int64{qsHuman: 2, qsService: 3}
	if err != nil || !reflect.DeepEqual(accountCounts(got), want) {
		t.Errorf("QueryCountsByAccountType(source A) = %+v / %v, want %v", got, err, want)
	}
}

func verifyQueryStatsByDay(t *testing.T, ctx context.Context, fx queryStatsFixture) {
	got, err := fx.s.QueryStatsByDay(ctx, fx.scoped("", 0, 0))
	want := []models.DailyQueryStat{
		{Date: "2025-01-01", QueryCount: 1, ActiveUsers: 1},
		{Date: "2025-01-02", QueryCount: 1, ActiveUsers: 1},
		{Date: "2025-01-03", QueryCount: 1, ActiveUsers: 1},
		{Date: "2025-01-04", QueryCount: 2, ActiveUsers: 1},
		{Date: "2025-01-05", QueryCount: 4, ActiveUsers: 2},
		{Date: "2025-01-06", QueryCount: 2, ActiveUsers: 2},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryStatsByDay = %+v / %v\nwant %+v", got, err, want)
	}
	got, err = fx.s.QueryStatsByDay(ctx, fx.scoped(qsService, 0, 0))
	want = []models.DailyQueryStat{{Date: "2025-01-02", QueryCount: 1, ActiveUsers: 1}, {Date: "2025-01-05", QueryCount: 3, ActiveUsers: 1}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryStatsByDay(service) = %+v / %v\nwant %+v", got, err, want)
	}
}

func verifyQueryStatsByUser(t *testing.T, ctx context.Context, fx queryStatsFixture) {
	got, err := fx.s.QueryStatsByUser(ctx, fx.scoped(qsHuman, 0, 0), 10)
	want := []models.UserQueryStat{
		{UserID: fx.alice.ID, Email: fx.alice.Email, FullName: fx.alice.FullName, AccountType: qsHuman, ActiveDays: 2, Current: qsTotals(3, 150), Prior: qsTotals(1, 100)},
		{UserID: fx.carol.ID, Email: fx.carol.Email, FullName: fx.carol.FullName, AccountType: qsHuman, ActiveDays: 1, Current: qsTotals(1, 10)},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryStatsByUser(humans) = %+v / %v\nwant %+v", got, err, want)
	}
	// alice and the service account tie at 3; the lower ID ranks first.
	got, err = fx.s.QueryStatsByUser(ctx, fx.scoped("", 0, 0), 10)
	ids := make([]models.UserID, len(got))
	for i, u := range got {
		ids[i] = u.UserID
	}
	if err != nil || !reflect.DeepEqual(ids, []models.UserID{fx.alice.ID, fx.svc.ID, fx.carol.ID, fx.ghost}) {
		t.Fatalf("QueryStatsByUser(all) IDs = %v / %v, want alice, service, carol, deleted", ids, err)
	}
	if got[1].FullName != "Error Digest Worker" || got[1].AccountType != qsService {
		t.Errorf("service account row = %+v, want its full name and type", got[1])
	}
	if g := got[3]; g.Email != "" || g.FullName != "" || g.AccountType != "" || g.Current != qsTotals(1, 40) {
		t.Errorf("deleted user row = %+v, want empty identity and 1 query", g)
	}
	if top, err := fx.s.QueryStatsByUser(ctx, fx.scoped("", 0, 0), 2); err != nil || len(top) != 2 {
		t.Errorf("QueryStatsByUser(limit 2) = %d rows / %v, want 2", len(top), err)
	}
}

func verifyQueryStatsBySource(t *testing.T, ctx context.Context, fx queryStatsFixture) {
	got, err := fx.s.QueryStatsBySource(ctx, fx.scoped(qsHuman, 0, 0), 10)
	want := []models.SourceQueryStat{
		{SourceID: fx.srcA.ID, SourceName: fx.srcA.Name, SourceType: "clickhouse", TeamID: &fx.srcATeam, ServiceQueryCount: 3, Current: qsTotals(2, 120), Prior: qsTotals(1, 100)},
		{SourceID: fx.srcB.ID, SourceName: fx.srcB.Name, SourceType: "clickhouse", Current: qsTotals(2, 40), Prior: qsTotals(1, 300)},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryStatsBySource(humans) = %+v / %v\nwant %+v", got, err, want)
	}
	// Source B has service queries only in the prior window.
	got, err = fx.s.QueryStatsBySource(ctx, fx.scoped(qsService, 0, 0), 10)
	want = []models.SourceQueryStat{{SourceID: fx.srcA.ID, SourceName: fx.srcA.Name, SourceType: "clickhouse", TeamID: &fx.srcATeam, ServiceQueryCount: 3, Current: qsTotals(3, 3000)}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryStatsBySource(service) = %+v / %v\nwant %+v", got, err, want)
	}
	got, err = fx.s.QueryStatsBySource(ctx, fx.scoped("", 0, fx.alice.ID), 1)
	if err != nil || len(got) != 1 || got[0].SourceID != fx.srcA.ID || got[0].ServiceQueryCount != 0 {
		t.Errorf("QueryStatsBySource(alice, limit 1) = %+v / %v, want source A without service queries", got, err)
	}
}

func verifyQueryStatsByLanguage(t *testing.T, ctx context.Context, fx queryStatsFixture) {
	got, err := fx.s.QueryStatsByLanguage(ctx, fx.scoped("", 0, 0))
	want := []models.LanguageQueryCount{{Language: models.QueryLanguageLogchefQL, QueryCount: 6}, {Language: models.QueryLanguageClickHouseSQL, QueryCount: 1}, {Language: models.QueryLanguageLogQL, QueryCount: 1}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("QueryStatsByLanguage = %+v / %v, want %+v", got, err, want)
	}
}

func accountCounts(rows []models.AccountQueryCount) map[models.UserAccountType]int64 {
	out := make(map[models.UserAccountType]int64, len(rows))
	for _, r := range rows {
		out[r.AccountType] = r.QueryCount
	}
	return out
}

// testTopQueryShapes ranks shapes across sources over a 3-day window
// (2025-02-04 to 2025-02-06) and its prior window.
func testTopQueryShapes(t *testing.T, ctx context.Context, s store.Store) {
	srcX := mkSource(t, ctx, s, "shapes_x")
	srcY := mkSource(t, ctx, s, "shapes_y")
	const ghost = models.SourceID(777001) // no sources row
	eq := models.FilterShape{{Column: "request_id", Kind: models.MatchEq}}
	like := models.FilterShape{{Column: "msg", Kind: models.MatchLike}}
	inc := func(day string, src models.SourceID, hash string, shape models.FilterShape, count, durationMs int64, lookback *int64) {
		t.Helper()
		err := s.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{BucketDate: day, SourceID: src, Usage: models.QueryShapeUsage{
			QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: hash, Shape: shape,
			QueryCount: count, TotalDurationMs: durationMs, MaxLookbackSeconds: lookback,
		}})
		if err != nil {
			t.Fatalf("IncrementQueryShapeStats(%s, %s): %v", day, hash, err)
		}
	}
	lookback := func(s int64) *int64 { return &s }
	inc("2025-02-04", srcX.ID, "aaaaaaaaaaaaaaaa", eq, 2, 100, lookback(600))
	inc("2025-02-05", srcX.ID, "aaaaaaaaaaaaaaaa", eq, 1, 50, nil)
	inc("2025-02-02", srcX.ID, "aaaaaaaaaaaaaaaa", eq, 4, 40, lookback(99999)) // prior: no lookback
	inc("2025-02-03", srcX.ID, "cccccccccccccccc", eq, 9, 9000, nil)           // prior only
	inc("2025-02-06", srcY.ID, "bbbbbbbbbbbbbbbb", like, 1, 500, lookback(60))
	inc("2025-01-31", srcY.ID, "dddddddddddddddd", like, 1, 99999, nil) // before the prior window
	inc("2025-02-07", srcY.ID, "dddddddddddddddd", like, 1, 99999, nil) // after the window
	inc("2025-02-04", ghost, "eeeeeeeeeeeeeeee", eq, 1, 10, nil)
	inc("2025-02-05", srcX.ID, models.ShapeHashUnanalyzed, models.FilterShape{}, 5, 90000, nil)
	inc("2025-02-04", srcX.ID, models.ShapeHashOverflow, models.FilterShape{}, 2, 0, nil)
	inc("2025-02-01", srcX.ID, models.ShapeHashUnanalyzed, models.FilterShape{}, 100, 0, nil) // prior

	f := models.QueryShapeFilter{PriorSince: "2025-02-01", Since: "2025-02-04", Until: "2025-02-06"}
	got, err := s.TopQueryShapes(ctx, f, 10)
	logchefql := models.QueryLanguageLogchefQL
	want := []models.QueryShapeStat{
		{SourceID: srcY.ID, SourceName: srcY.Name, SourceType: "clickhouse", QueryLanguage: logchefql, ShapeHash: "bbbbbbbbbbbbbbbb", Shape: like, MaxLookbackSeconds: lookback(60), Current: models.QueryTotals{QueryCount: 1, TotalDurationMs: 500}, Prior: &models.QueryTotals{}},
		{SourceID: srcX.ID, SourceName: srcX.Name, SourceType: "clickhouse", QueryLanguage: logchefql, ShapeHash: "aaaaaaaaaaaaaaaa", Shape: eq, MaxLookbackSeconds: lookback(600), Current: models.QueryTotals{QueryCount: 3, TotalDurationMs: 150}, Prior: &models.QueryTotals{QueryCount: 4, TotalDurationMs: 40}},
		{SourceID: ghost, QueryLanguage: logchefql, ShapeHash: "eeeeeeeeeeeeeeee", Shape: eq, Current: models.QueryTotals{QueryCount: 1, TotalDurationMs: 10}, Prior: &models.QueryTotals{}},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("TopQueryShapes = %+v / %v\nwant %+v", got, err, want)
	}
	if top, err := s.TopQueryShapes(ctx, f, 1); err != nil || len(top) != 1 || top[0].ShapeHash != "bbbbbbbbbbbbbbbb" {
		t.Errorf("TopQueryShapes(limit 1) = %+v / %v, want the slowest shape", top, err)
	}
	onX := f
	onX.SourceID = srcX.ID
	if top, err := s.TopQueryShapes(ctx, onX, 10); err != nil || len(top) != 1 || top[0].ShapeHash != "aaaaaaaaaaaaaaaa" {
		t.Errorf("TopQueryShapes(source X) = %+v / %v, want its one current shape", top, err)
	}
	if n, err := s.CountUnanalyzedQueryShapes(ctx, onX); err != nil || n != 7 {
		t.Errorf("CountUnanalyzedQueryShapes(source X) = %d / %v, want 7", n, err)
	}
	onY := f
	onY.SourceID = srcY.ID
	if n, err := s.CountUnanalyzedQueryShapes(ctx, onY); err != nil || n != 0 {
		t.Errorf("CountUnanalyzedQueryShapes(source Y) = %d / %v, want 0", n, err)
	}
}

// testSchemaAdvisorBySources reads the reports and dismissals of several
// sources at once.
func testSchemaAdvisorBySources(t *testing.T, ctx context.Context, s store.Store) {
	withReport := mkSource(t, ctx, s, "advisor_bulk_p")
	other := mkSource(t, ctx, s, "advisor_bulk_q")
	unanalyzed := mkSource(t, ctx, s, "advisor_bulk_r")
	at := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	for _, src := range []*models.Source{withReport, other} {
		report := &models.StoredSchemaAdvisorReport{SourceID: src.ID, GeneratedAt: at, WindowDays: 30, Report: models.SchemaAdvisorReport{
			Status: models.AdvisorStatusOK, Inputs: []models.AdvisorInput{},
			Findings: []models.SchemaFinding{
				{ID: "1111111111111111", Impact: models.ImpactHigh, Title: "high", Column: "msg", Evidence: []models.AdvisorEvidence{}, DDL: []string{}},
				{ID: "2222222222222222", Impact: models.ImpactMedium, Title: "medium", Column: "level", Evidence: []models.AdvisorEvidence{}, DDL: []string{}},
			},
		}}
		if err := s.UpsertSchemaAdvisorReport(ctx, report); err != nil {
			t.Fatalf("UpsertSchemaAdvisorReport: %v", err)
		}
	}
	if err := s.UpsertSchemaAdvisorDismissal(ctx, &models.SchemaAdvisorDismissal{SourceID: withReport.ID, FindingID: "2222222222222222", Reason: "known", DismissedAt: at}); err != nil {
		t.Fatalf("UpsertSchemaAdvisorDismissal: %v", err)
	}

	ids := []models.SourceID{unanalyzed.ID, other.ID, withReport.ID}
	reports, err := s.ListSchemaAdvisorReports(ctx, ids)
	if err != nil || len(reports) != 2 || reports[0].SourceID != withReport.ID || reports[1].SourceID != other.ID {
		t.Fatalf("ListSchemaAdvisorReports = %+v / %v, want the two analyzed sources by ID", reports, err)
	}
	if got := reports[0].Report.Findings; len(got) != 2 || got[0].Column != "msg" || !reports[0].GeneratedAt.Equal(at) {
		t.Errorf("report = %+v, want its findings and time", reports[0])
	}
	dismissals, err := s.ListSchemaAdvisorDismissalsBySources(ctx, ids)
	if err != nil || len(dismissals) != 1 || dismissals[0].SourceID != withReport.ID || dismissals[0].FindingID != "2222222222222222" {
		t.Errorf("ListSchemaAdvisorDismissalsBySources = %+v / %v, want the one dismissal", dismissals, err)
	}
	if none, err := s.ListSchemaAdvisorDismissalsBySources(ctx, []models.SourceID{other.ID}); err != nil || len(none) != 0 {
		t.Errorf("ListSchemaAdvisorDismissalsBySources(other) = %+v / %v, want none", none, err)
	}
}
