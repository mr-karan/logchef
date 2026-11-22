package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestQueryShapeRollup runs one LogchefQL and one SQL query through the
// preview endpoints and reads one rollup row for each.
func TestQueryShapeRollup(t *testing.T) {
	s := newDashboardTestServer(t)
	s.config = &config.Config{}
	s.config.Query.DefaultPreviewLimit = 100
	s.config.Query.MaxPreviewLimit = 1000
	s.config.Query.DefaultTimeoutSeconds = 10
	s.queryRecords = make(chan queryRecord, 4)
	s.datasources = datasource.NewService(s.store, s.log)
	s.datasources.Register(&succeedingQueryProvider{sourceType: models.SourceTypeClickHouse})
	source := &models.Source{Name: "shapes", SourceType: models.SourceTypeClickHouse, Connection: models.ConnectionInfo{Host: "ch:9000", Database: "logs", TableName: "app"}}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := s.store.CreateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	user, src := authorizeTestSource(t, s.store, source.ID)
	withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/sql", user, s.handleQueryLogs)
	withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/logchefql", user, s.handleLogchefQLQuery)
	run := func(endpoint, body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/teams/%d/sources/%d/%s", src.TeamID(), source.ID, endpoint), strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status=%d, want 200", endpoint, resp.StatusCode)
		}
		select {
		case rec := <-s.queryRecords:
			if rec.usage == nil {
				t.Fatalf("%s: the query counted no run", endpoint)
			}
			// This server records no audit events.
			rec.event = nil
			s.writeQueryRecord(context.Background(), rec)
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: query history was not recorded", endpoint)
		}
	}
	start := time.Now().UTC().Add(-2 * time.Hour)
	run("logchefql", fmt.Sprintf(`{"query":"level=\"error\"","start_time":%q,"end_time":%q,"timezone":"UTC"}`,
		start.Format(time.DateTime), start.Add(time.Hour).Format(time.DateTime)))
	run("sql", `{"query_text":"SELECT * FROM logs.app WHERE service = 'api' AND status >= 500"}`)

	rows, err := s.store.ListQueryShapeStats(t.Context(), source.ID, time.Now().UTC().AddDate(0, 0, -1).Format(time.DateOnly), time.Now().UTC().AddDate(0, 0, 1).Format(time.DateOnly), 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rollup rows = %+v, want 2", rows)
	}
	hexHash := regexp.MustCompile(`^[0-9a-f]{16}$`)
	byLanguage := map[models.QueryLanguage]models.QueryShapeUsage{}
	for _, row := range rows {
		if !hexHash.MatchString(row.ShapeHash) {
			t.Errorf("shape hash %q is not 16 hex characters", row.ShapeHash)
		}
		if row.QueryCount != 1 || row.TotalDurationMs != 7 || row.TotalRows != 1 {
			t.Errorf("%s counters = %+v, want one query of 7 ms and 1 row", row.QueryLanguage, row)
		}
		byLanguage[row.QueryLanguage] = row
	}
	logchefql := byLanguage[models.QueryLanguageLogchefQL]
	if want := (models.FilterShape{{Column: "level", Kind: models.MatchEq}}); !reflect.DeepEqual(logchefql.Shape, want) {
		t.Errorf("LogchefQL shape = %+v, want %+v", logchefql.Shape, want)
	}
	if l := logchefql.MaxLookbackSeconds; l == nil || *l < 7200 || *l > 7200+60 {
		t.Errorf("LogchefQL lookback = %v, want about 7200 s", l)
	}
	sql := byLanguage[models.QueryLanguageClickHouseSQL]
	if want := (models.FilterShape{{Column: "service", Kind: models.MatchEq}, {Column: "status", Kind: models.MatchRange}}); !reflect.DeepEqual(sql.Shape, want) {
		t.Errorf("SQL shape = %+v, want %+v", sql.Shape, want)
	}
	if sql.MaxLookbackSeconds != nil {
		t.Errorf("SQL lookback = %d, want none for a query without a start time", *sql.MaxLookbackSeconds)
	}
}

// TestQueryShapeOverflow records 501 distinct shapes for one source on one
// day: the 501st counts under the overflow hash. Unanalyzed queries, sources
// that are not ClickHouse and LogsQL never reach the rollup's shape cap.
func TestQueryShapeOverflow(t *testing.T) {
	s := newDashboardTestServer(t)
	ctx := t.Context()
	const sourceID = models.SourceID(7)
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	record := func(query string, language models.QueryLanguage, table string) {
		t.Helper()
		s.recordQueryShape(ctx, now, &queryUsage{run: &models.QueryHistory{
			SourceID: sourceID, QueryText: query, QueryLanguage: language,
			SourceDatabase: "logs", SourceTable: table, DurationMs: 1, RowCount: 1,
		}})
	}
	record(`level=`, models.QueryLanguageLogchefQL, "app") // does not parse
	record(`_time:5m error`, models.QueryLanguageLogsQL, "app")
	record(`level="error"`, models.QueryLanguageLogchefQL, "") // not a ClickHouse source
	for i := range 501 {
		record(fmt.Sprintf(`field_%d="x"`, i), models.QueryLanguageLogchefQL, "app")
	}
	// A shape seen before the cap filled keeps its own row.
	record(`field_0="y"`, models.QueryLanguageLogchefQL, "app")

	day := now.Format(time.DateOnly)
	coverage, err := s.store.QueryShapeCoverage(ctx, sourceID, day, day)
	if err != nil {
		t.Fatal(err)
	}
	want := models.QueryShapeCoverage{Days: 1, FilteredDays: 1, QueryCount: 503, UnanalyzedCount: 1, OverflowCount: 1, NullLookbackCount: 503}
	if !reflect.DeepEqual(coverage, want) {
		t.Fatalf("coverage = %+v, want %+v", coverage, want)
	}
	rows, err := s.store.ListQueryShapeStats(ctx, sourceID, day, day, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 500 || rows[0].QueryCount != 2 || !reflect.DeepEqual(rows[0].Shape, models.FilterShape{{Column: "field_0", Kind: models.MatchEq}}) {
		t.Fatalf("got %d shape rows, first %+v; want 500 with field_0 counted twice", len(rows), rows[0])
	}
}

// TestQueryShapePrunesOncePerDay checks that the first record of a UTC day
// deletes rows older than the retention window and keeps the rest.
func TestQueryShapePrunesOncePerDay(t *testing.T) {
	s := newDashboardTestServer(t)
	ctx := t.Context()
	const sourceID = models.SourceID(9)
	now := time.Date(2026, 11, 16, 0, 5, 0, 0, time.UTC)
	for _, day := range []string{"2026-08-17", "2026-08-18"} { // 91 and 90 days back
		if err := s.store.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{BucketDate: day, SourceID: sourceID, Usage: models.QueryShapeUsage{
			QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: models.ShapeHashUnanalyzed, Shape: models.FilterShape{}, QueryCount: 1,
		}}); err != nil {
			t.Fatal(err)
		}
	}
	s.recordQueryShape(ctx, now, &queryUsage{run: &models.QueryHistory{SourceID: sourceID, QueryText: `level="error"`, QueryLanguage: models.QueryLanguageLogchefQL, SourceDatabase: "logs", SourceTable: "app"}})
	coverage, err := s.store.QueryShapeCoverage(ctx, sourceID, "2026-01-01", "9999-12-31")
	if err != nil {
		t.Fatal(err)
	}
	if coverage.Days != 2 || coverage.QueryCount != 2 {
		t.Fatalf("coverage = %+v, want the 90-day-old row and today's", coverage)
	}
}
