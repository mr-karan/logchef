package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

type queryStatsFixture struct {
	app            *fiber.App
	alice, service *models.User
	hot, quiet     *models.Source
}

// newQueryStatsFixture records, over the last 7 UTC days and the 7 before:
// alice runs 2 current and 1 prior query on hot, and 1 current on quiet; a
// service account runs 5 current queries on hot. hot has a schema advisor
// report with a high finding on request_id, a dismissed medium finding and
// a low finding on msg, and one filter shape on request_id.
func newQueryStatsFixture(t *testing.T) queryStatsFixture {
	t.Helper()
	db := newServerTestDB(t)
	ctx := t.Context()
	s := &Server{store: db, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	f := queryStatsFixture{app: fiber.New()}
	f.app.Get("/api/v1/admin/query-stats", s.handleAdminQueryStats)

	f.alice = &models.User{Email: "alice@example.com", FullName: "Alice Example", Role: models.UserRoleMember, Status: models.UserStatusActive}
	f.service = &models.User{Email: "svc-1@service.logchef.internal", FullName: "Error Digest Worker", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: models.UserAccountTypeService}
	for _, u := range []*models.User{f.alice, f.service} {
		if err := db.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
	}
	f.hot = createStatsSource(t, db, "hot")
	f.quiet = createStatsSource(t, db, "quiet")

	today := time.Now().UTC()
	day := func(ago int) string { return today.AddDate(0, 0, -ago).Format(time.DateOnly) }
	inc := func(ago int, user *models.User, src *models.Source, durationMs int64) {
		t.Helper()
		if err := db.IncrementQueryStats(ctx, models.QueryStatsIncrement{BucketDate: day(ago), UserID: user.ID, TeamID: 1, SourceID: src.ID, Language: models.QueryLanguageLogchefQL, DurationMs: durationMs}); err != nil {
			t.Fatal(err)
		}
	}
	inc(6, f.alice, f.hot, 100) // first day of the 7-day window
	inc(0, f.alice, f.hot, 300)
	inc(7, f.alice, f.hot, 50) // last day of the prior window
	inc(14, f.alice, f.hot, 9999)
	inc(2, f.alice, f.quiet, 20)
	for range 5 {
		inc(1, f.service, f.hot, 1000)
	}

	request := models.FilterShape{{Column: "request_id", Kind: models.MatchEq}}
	for _, u := range []struct {
		ago   int
		hash  string
		shape models.FilterShape
		count int64
	}{
		{1, "aaaaaaaaaaaaaaaa", request, 3},
		{9, "aaaaaaaaaaaaaaaa", request, 1},
		{1, models.ShapeHashUnanalyzed, models.FilterShape{}, 4},
	} {
		err := db.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{BucketDate: day(u.ago), SourceID: f.hot.ID, Usage: models.QueryShapeUsage{
			QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: u.hash, Shape: u.shape, QueryCount: u.count, TotalDurationMs: u.count * 100,
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	finding := func(id, impact, column string) models.SchemaFinding {
		return models.SchemaFinding{ID: id, Impact: impact, Title: impact + " on " + column, Column: column, Evidence: []models.AdvisorEvidence{}, DDL: []string{}}
	}
	if err := db.UpsertSchemaAdvisorReport(ctx, &models.StoredSchemaAdvisorReport{SourceID: f.hot.ID, GeneratedAt: today, WindowDays: 30, Report: models.SchemaAdvisorReport{
		Status: models.AdvisorStatusOK, Inputs: []models.AdvisorInput{},
		Findings: []models.SchemaFinding{
			finding("1111111111111111", models.ImpactLow, "request_id"),
			finding("2222222222222222", models.ImpactHigh, "request_id"),
			finding("3333333333333333", models.ImpactMedium, "level"),
			finding("4444444444444444", models.ImpactLow, "msg"),
		},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertSchemaAdvisorDismissal(ctx, &models.SchemaAdvisorDismissal{SourceID: f.hot.ID, FindingID: "3333333333333333", Reason: "known", DismissedAt: today}); err != nil {
		t.Fatal(err)
	}
	return f
}

func createStatsSource(t *testing.T, db *sqlite.DB, table string) *models.Source {
	t.Helper()
	src := &models.Source{Name: table, MetaTSField: "timestamp", Connection: models.ConnectionInfo{Host: "localhost:9000", Database: "logs", TableName: table}}
	if err := src.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(t.Context(), src); err != nil {
		t.Fatal(err)
	}
	return src
}

func (f queryStatsFixture) get(t *testing.T, query string) (int, core.QueryActivity) {
	t.Helper()
	res, err := f.app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/admin/query-stats"+query, http.NoBody))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var body struct {
		Data core.QueryActivity `json:"data"`
	}
	if res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
	}
	return res.StatusCode, body.Data
}

func TestAdminQueryStatsAccounts(t *testing.T) {
	f := newQueryStatsFixture(t)
	today := time.Now().UTC()
	date := func(ago int) string { return today.AddDate(0, 0, -ago).Format(time.DateOnly) }

	status, got := f.get(t, "?days=7")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if want := (core.DateWindow{Since: date(6), Until: date(0), Days: 7}); got.Window != want {
		t.Errorf("window = %+v, want %+v", got.Window, want)
	}
	if want := (core.DateWindow{Since: date(13), Until: date(7), Days: 7}); got.PriorWindow != want {
		t.Errorf("prior window = %+v, want %+v", got.PriorWindow, want)
	}
	if got.Filters.Account != core.QueryActivityHumans || got.Filters.SourceID != nil || got.Filters.UserID != nil {
		t.Errorf("filters = %+v, want the human default and no scope", got.Filters)
	}
	if want := (core.AccountQueryTotals{Human: 3, Service: 5, All: 8}); got.AccountTotals != want {
		t.Errorf("account totals = %+v, want %+v", got.AccountTotals, want)
	}
	current, prior := got.Summary.Current, got.Summary.Prior
	if current.QueryCount != 3 || current.TotalDurationMs != 420 || current.ActiveUsers != 1 || current.ActiveSources != 2 {
		t.Errorf("current summary = %+v, want alice's 3 queries on 2 sources", current)
	}
	if prior.QueryCount != 1 || prior.TotalDurationMs != 50 || prior.ActiveUsers != 1 {
		t.Errorf("prior summary = %+v, want the one query 7 days ago", prior)
	}
	// alice was active on 3 of 7 current days and 1 of 7 prior days.
	if current.AvgDailyActiveUsers != 3.0/7 || prior.AvgDailyActiveUsers != 1.0/7 {
		t.Errorf("avg daily active = %v / %v, want 3/7 and 1/7", current.AvgDailyActiveUsers, prior.AvgDailyActiveUsers)
	}
	if got.TotalUsers != 1 {
		t.Errorf("total users = %d, want the 1 human", got.TotalUsers)
	}
	if len(got.VolumeByDay) != 7 || got.VolumeByDay[0].Date != date(6) || got.VolumeByDay[6].Date != date(0) || got.VolumeByDay[1].QueryCount != 0 {
		t.Errorf("volume by day = %+v, want 7 zero-filled days ending today", got.VolumeByDay)
	}
	if len(got.TopUsers) != 1 || got.TopUsers[0].UserID != f.alice.ID || got.TopUsers[0].ActiveDays != 3 || got.TopUsers[0].Prior.QueryCount != 1 {
		t.Errorf("top users = %+v, want alice active 3 days with 1 prior query", got.TopUsers)
	}
	svc := got.ServiceSummary
	if svc == nil || svc.AccountCount != 1 || svc.QueryCount != 5 || len(svc.Top) != 1 || svc.Top[0].FullName != "Error Digest Worker" {
		t.Errorf("service summary = %+v, want 1 account named Error Digest Worker with 5 queries", svc)
	}
	if len(got.Sources) != 2 || got.Sources[0].SourceID != f.hot.ID || got.Sources[0].Current.QueryCount != 2 || got.Sources[0].ServiceQueryCount != 5 {
		t.Errorf("sources = %+v, want hot first with 2 human and 5 service queries", got.Sources)
	}

	status, got = f.get(t, "?days=7&account=service")
	if status != http.StatusOK || got.Summary.Current.QueryCount != 5 || got.ServiceSummary != nil || got.TotalUsers != 1 {
		t.Fatalf("service view = %d %+v, want 5 queries and no service summary", status, got.Summary)
	}
	if len(got.TopUsers) != 1 || got.TopUsers[0].UserID != f.service.ID || got.TopUsers[0].AccountType != models.UserAccountTypeService {
		t.Errorf("service top users = %+v", got.TopUsers)
	}

	status, got = f.get(t, "?days=7&account=all")
	if status != http.StatusOK || got.Summary.Current.QueryCount != 8 || got.TotalUsers != 2 || got.ServiceSummary == nil {
		t.Fatalf("all view = %d %+v (users %d), want 8 queries of 2 users", status, got.Summary, got.TotalUsers)
	}
}

func TestAdminQueryStatsScopes(t *testing.T) {
	f := newQueryStatsFixture(t)

	_, got := f.get(t, "?days=7&account=all&source_id="+strconv.FormatInt(int64(f.quiet.ID), 10))
	if got.Filters.SourceID == nil || *got.Filters.SourceID != f.quiet.ID || got.Filters.SourceName != "quiet" {
		t.Errorf("source filter echo = %+v, want quiet", got.Filters)
	}
	if got.Summary.Current.QueryCount != 1 || len(got.Sources) != 1 || got.Sources[0].SourceID != f.quiet.ID || got.ServiceSummary != nil {
		t.Errorf("quiet scope = %+v / %+v / %+v, want its 1 query only", got.Summary.Current, got.Sources, got.ServiceSummary)
	}
	if len(got.Shapes.Rows) != 0 || got.Shapes.UnanalyzedQueryCount != 0 {
		t.Errorf("quiet shapes = %+v, want none", got.Shapes)
	}

	_, got = f.get(t, "?days=7&account=all&user_id="+strconv.FormatInt(int64(f.service.ID), 10))
	if got.Filters.UserName != "Error Digest Worker" || got.Summary.Current.QueryCount != 5 || got.AccountTotals.Human != 0 {
		t.Errorf("service user scope = %+v / %+v / %+v", got.Filters, got.Summary.Current, got.AccountTotals)
	}

	_, got = f.get(t, "?days=7&user_id=999999")
	if got.Filters.UserName != "" || got.Summary.Current.QueryCount != 0 {
		t.Errorf("unknown user scope = %+v / %+v, want an empty page", got.Filters, got.Summary.Current)
	}
}

func TestAdminQueryStatsAdvisor(t *testing.T) {
	f := newQueryStatsFixture(t)
	_, got := f.get(t, "?days=7")

	byID := map[models.SourceID]core.SourceActivity{}
	for _, s := range got.Sources {
		byID[s.SourceID] = s
	}
	if a := byID[f.hot.ID].Advisor; a == nil || *a != (core.AdvisorFindingCounts{Analyzed: true, High: 1, Low: 2}) {
		t.Errorf("hot advisor = %+v, want 1 high and 2 low open findings", a)
	}
	if a := byID[f.quiet.ID].Advisor; a == nil || a.Analyzed {
		t.Errorf("quiet advisor = %+v, want not analyzed", a)
	}

	if got.Shapes.Since != got.Window.Since || got.Shapes.UnanalyzedQueryCount != 4 || len(got.Shapes.Rows) != 1 {
		t.Fatalf("shapes = %+v, want one analyzed shape and 4 unanalyzed queries", got.Shapes)
	}
	row := got.Shapes.Rows[0]
	if row.Current != (models.QueryTotals{QueryCount: 3, TotalDurationMs: 300}) || row.Prior == nil || *row.Prior != (models.QueryTotals{QueryCount: 1, TotalDurationMs: 100}) {
		t.Errorf("shape totals = %+v / %+v", row.Current, row.Prior)
	}
	if want := (&core.ShapeFinding{ID: "2222222222222222", Title: "high on request_id", Impact: models.ImpactHigh}); !reflect.DeepEqual(row.Finding, want) {
		t.Errorf("shape finding = %+v, want the high finding on request_id", row.Finding)
	}

	// The prior 90 days are pruned from the shape rollup.
	_, got = f.get(t, "?days=90")
	if len(got.Shapes.Rows) != 1 || got.Shapes.Rows[0].Prior != nil {
		t.Errorf("90-day shapes = %+v, want no prior totals", got.Shapes.Rows)
	}
}

func TestAdminQueryStatsParams(t *testing.T) {
	f := newQueryStatsFixture(t)
	for _, query := range []string{"?days=abc", "?account=bots", "?source_id=0", "?source_id=x", "?user_id=-1"} {
		if status, _ := f.get(t, query); status != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", query, status)
		}
	}
	for query, days := range map[string]int{"": 30, "?days=0": 1, "?days=1000": 365} {
		if status, got := f.get(t, query); status != http.StatusOK || got.Window.Days != days || len(got.VolumeByDay) != days {
			t.Errorf("%q: status %d, %d days, want %d", query, status, got.Window.Days, days)
		}
	}
}
