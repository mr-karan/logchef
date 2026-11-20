package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// auditQueryProvider answers like succeedingQueryProvider once query, when
// set, returns nil, and fails the query with the error it returns before a
// row. A windowed search returns one window of one row.
type auditQueryProvider struct {
	succeedingQueryProvider
	query func(context.Context) error
}

func (p *auditQueryProvider) QueryLogs(ctx context.Context, s *models.Source, r datasource.QueryRequest) (*models.QueryResult, error) {
	if p.query != nil {
		if err := p.query(ctx); err != nil {
			return nil, err
		}
	}
	return p.succeedingQueryProvider.QueryLogs(ctx, s, r)
}

func (p *auditQueryProvider) QueryLogsStream(ctx context.Context, s *models.Source, r datasource.QueryRequest, w datasource.StreamWriter) (models.QueryStats, error) {
	if p.query != nil {
		if err := p.query(ctx); err != nil {
			return models.QueryStats{}, err
		}
	}
	return p.succeedingQueryProvider.QueryLogsStream(ctx, s, r, w)
}

// failWith fails every query with err, or none when err is nil.
func failWith(err error) func(context.Context) error {
	if err == nil {
		return nil
	}
	return func(context.Context) error { return err }
}

func (p *auditQueryProvider) PrepareWindowedQuery(_ *models.Source, req datasource.WindowedRequest) (*datasource.WindowedPlan, error) {
	return &datasource.WindowedPlan{Request: req}, nil
}

func (p *auditQueryProvider) StreamWindowedQuery(_ context.Context, _ *models.Source, _ *datasource.WindowedPlan, emit func(datasource.WindowedEvent) error) error {
	if err := emit(datasource.WindowedEvent{Type: datasource.WindowRows, Status: datasource.WindowComplete, Logs: []map[string]any{previewTestRow}}); err != nil {
		return err
	}
	return emit(datasource.WindowedEvent{Type: datasource.WindowEnd, Complete: true})
}

// queryAuditEnv is an audit env with one source of sourceType that provider
// answers.
type queryAuditEnv struct {
	*auditRouteEnv
	base       string
	sourceID   models.SourceID
	teamID     models.TeamID
	sourceName string
}

func newQueryAuditEnv(t *testing.T, sourceType models.SourceType, query func(context.Context) error) *queryAuditEnv {
	t.Helper()
	env := &queryAuditEnv{sourceName: "query-audit-" + string(sourceType)}
	setup := func(s *auditEnvSetup) {
		s.opts.Config.Query = config.QueryConfig{DefaultPreviewLimit: 100, MaxPreviewLimit: 1000, DefaultTimeoutSeconds: 10}
		s.opts.Datasources = datasource.NewService(s.db, s.opts.Logger)
		s.opts.Datasources.Register(&auditQueryProvider{succeedingQueryProvider: succeedingQueryProvider{sourceType: sourceType}, query: query})
		ctx := context.Background()
		source := &models.Source{Name: env.sourceName, SourceType: sourceType}
		switch sourceType {
		case models.SourceTypeClickHouse:
		case models.SourceTypeVictoriaLogs:
			source.ConnectionConfig = json.RawMessage(`{"url":"http://localhost:9428"}`)
		case models.SourceTypeLoki:
			source.ConnectionConfig = json.RawMessage(`{"url":"http://localhost:3100"}`)
		}
		if err := s.db.CreateSource(ctx, source); err != nil {
			t.Fatal(err)
		}
		team := &models.Team{Name: "query-audit"}
		if err := s.db.CreateTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
		if err := s.db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
			t.Fatal(err)
		}
		env.sourceID, env.teamID = source.ID, team.ID
		env.base = fmt.Sprintf("/api/v1/teams/%d/sources/%d", team.ID, source.ID)
	}
	env.auditRouteEnv = newAuditRouteEnv(t, setup, withAuditLogLines())
	return env
}

// settledEvents waits for the background writer to store a first event,
// then shuts the server down, which drains the queue, and returns every
// stored event.
func (e *queryAuditEnv) settledEvents() []models.AuditEvent {
	e.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(e.events()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if err := e.s.Shutdown(context.Background()); err != nil {
		e.t.Fatal(err)
	}
	return e.events()
}

func (e *queryAuditEnv) historyRows() int {
	e.t.Helper()
	return len(e.history())
}

func (e *queryAuditEnv) history() []*models.QueryHistory {
	e.t.Helper()
	rows, err := e.db.ListQueryHistory(context.Background(), e.admin.ID, 10)
	if err != nil {
		e.t.Fatal(err)
	}
	return rows
}

// longQuery is past the 1 KiB that a plain audit string keeps, so the test
// tells full text from a truncated one.
var longQuery = "SELECT * FROM logs WHERE " + strings.Repeat("service != 'noise' AND ", 60) + "level = 'error'"

// Every executed query records one query.run event with its full text and
// result stats, written by the background writer that also writes history.
func TestAuditQueryRunRecordsEachQuery(t *testing.T) {
	t.Parallel()
	const from, to = "2026-09-01T00:00:00Z", "2026-09-01T01:00:00Z"
	timeRange := fmt.Sprintf(`"start_time":%q,"end_time":%q`, from, to)
	for _, tc := range []struct {
		name       string
		sourceType models.SourceType
		path, body string
		query      string
		language   models.QueryLanguage
		mode       string
		withStats  bool
	}{
		{"buffered", models.SourceTypeVictoriaLogs, "/logs/query",
			fmt.Sprintf(`{"query_text":%q,"limit":50,%s}`, longQuery, timeRange), longQuery, models.QueryLanguageLogsQL, "buffered", true},
		{"loki", models.SourceTypeLoki, "/logs/query",
			fmt.Sprintf(`{"query_text":"{app=\"api\"}","limit":50,%s}`, timeRange), `{app="api"}`, models.QueryLanguageLogQL, "buffered", true},
		{"stream", models.SourceTypeClickHouse, "/logs/query",
			fmt.Sprintf(`{"query_text":%q,"limit":50,%s}`, longQuery, timeRange), longQuery, models.QueryLanguageClickHouseSQL, "stream", true},
		{"logchefql", models.SourceTypeClickHouse, "/logchefql/query",
			`{"query":"level=\"error\"","limit":50,"start_time":"2026-09-01 00:00:00","end_time":"2026-09-01 01:00:00","timezone":"UTC"}`,
			`level="error"`, models.QueryLanguageLogchefQL, "stream", true},
		{"windowed", models.SourceTypeVictoriaLogs, "/logs/windowed",
			fmt.Sprintf(`{"query_language":"logsql","query_text":"error","limit":50,%s}`, timeRange), "error", models.QueryLanguageLogsQL, "windowed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newQueryAuditEnv(t, tc.sourceType, nil)
			resp := e.do(http.MethodPost, e.base+tc.path, tc.body, e.cookie)
			wantStatus(t, resp, http.StatusOK)

			events := e.settledEvents()
			if len(events) != 1 {
				t.Fatalf("stored %d events, want one query.run: %+v", len(events), events)
			}
			ev := events[0]
			checkActorAndTarget(t, ev, string(audit.QueryRun), e.admin, "source", strconv.Itoa(int(e.sourceID)))
			if ev.TargetLabel != e.sourceName || ev.SourceID == nil || *ev.SourceID != e.sourceID || ev.TeamID == nil || *ev.TeamID != e.teamID {
				t.Errorf("target label %q source %v team %v", ev.TargetLabel, ev.SourceID, ev.TeamID)
			}
			if ev.HTTPMethod != http.MethodPost || ev.Route != teamSourcePrefix+tc.path || ev.Status != http.StatusOK {
				t.Errorf("request %s %s %d", ev.HTTPMethod, ev.Route, ev.Status)
			}
			d := detailOf(t, ev)
			if d["query"] != tc.query || d["language"] != string(tc.language) || d["mode"] != tc.mode {
				t.Errorf("query %q language %v mode %v", d["query"], d["language"], d["mode"])
			}
			if d["time_from"] != from || d["time_to"] != to || d["limit_requested"] != float64(50) || d["rows_returned"] != float64(1) {
				t.Errorf("range %v..%v limit %v rows %v", d["time_from"], d["time_to"], d["limit_requested"], d["rows_returned"])
			}
			if id, _ := d["query_id"].(string); id == "" {
				t.Errorf("query_id missing: %v", d)
			}
			if tc.withStats && (d["duration_ms"] != float64(7) || d["rows_read"] != float64(0) || d["result_truncated"] != false || d["limit_applied"] == nil) {
				t.Errorf("stats %v", d)
			}
			if !strings.Contains(auditLogLines(e.logs.String()), "level = 'error'") && tc.query == longQuery {
				t.Error("the audit log line does not carry the query text")
			}
			if rows := e.history(); len(rows) != 1 || rows[0].QueryLanguage != tc.language {
				t.Errorf("history %+v, want one row in %s", rows, tc.language)
			}
		})
	}
}

// A query that fails records one failed query.run event with its text and
// the error class, and no history row.
func TestAuditQueryRunRecordsFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		sourceType models.SourceType
		queryErr   error
		body       string
		status     int
		reason     string
	}{
		{"stream upstream error", models.SourceTypeClickHouse, &datasource.UpstreamError{Message: "boom"},
			fmt.Sprintf(`{"query_text":%q}`, longQuery), http.StatusBadGateway, "upstream_error"},
		{"buffered upstream error", models.SourceTypeVictoriaLogs, &datasource.UpstreamError{Message: "boom"},
			fmt.Sprintf(`{"query_text":%q}`, longQuery), http.StatusBadGateway, "upstream_error"},
		{"invalid time range", models.SourceTypeClickHouse, nil,
			fmt.Sprintf(`{"query_text":%q,"start_time":"yesterday"}`, longQuery), http.StatusBadRequest, "invalid_query"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newQueryAuditEnv(t, tc.sourceType, failWith(tc.queryErr))
			wantStatus(t, e.do(http.MethodPost, e.base+"/logs/query", tc.body, e.cookie), tc.status)

			events := e.settledEvents()
			if len(events) != 1 {
				t.Fatalf("stored %d events, want one query.run: %+v", len(events), events)
			}
			ev := events[0]
			if ev.Action != string(audit.QueryRun) || ev.Outcome != models.AuditOutcomeFailure || ev.Reason != tc.reason || ev.Status != tc.status {
				t.Errorf("event %s %s %q status %d, want query.run failure %q %d", ev.Action, ev.Outcome, ev.Reason, ev.Status, tc.reason, tc.status)
			}
			if d := detailOf(t, ev); d["query"] != longQuery || d["rows_returned"] != float64(0) {
				t.Errorf("detail query %d bytes rows %v", len(fmt.Sprint(d["query"])), d["rows_returned"])
			}
			if got := e.historyRows(); got != 0 {
				t.Errorf("history rows %d, want none for a failed query", got)
			}
		})
	}
}

// A query that panics records a failed query.run event with the status that
// the recover middleware answers, and no history row.
func TestAuditQueryRunRecordsPanic(t *testing.T) {
	t.Parallel()
	e := newQueryAuditEnv(t, models.SourceTypeVictoriaLogs, func(context.Context) error { panic("provider bug") })
	wantStatus(t, e.do(http.MethodPost, e.base+"/logs/query", `{"query_text":"error"}`, e.cookie), http.StatusInternalServerError)

	events := e.settledEvents()
	if len(events) != 1 {
		t.Fatalf("stored %d events, want one query.run: %+v", len(events), events)
	}
	ev := events[0]
	if ev.Action != string(audit.QueryRun) || ev.Outcome != models.AuditOutcomeFailure || ev.Reason != "internal_error" || ev.Status != http.StatusInternalServerError {
		t.Errorf("event %s %s %q status %d, want query.run failure internal_error 500", ev.Action, ev.Outcome, ev.Reason, ev.Status)
	}
	if got := e.historyRows(); got != 0 {
		t.Errorf("history rows %d, want none for a panicked query", got)
	}
}

// A query still running when shutdown starts is cancelled by it, and its
// event and history row are stored before the server stops.
func TestAuditQueryRunRecordsQueryEndingAtShutdown(t *testing.T) {
	t.Parallel()
	running := make(chan struct{})
	e := newQueryAuditEnv(t, models.SourceTypeVictoriaLogs, func(ctx context.Context) error {
		close(running)
		<-ctx.Done()
		return ctx.Err()
	})
	req := httptest.NewRequest(http.MethodPost, e.base+"/logs/query", strings.NewReader(`{"query_text":"error"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(e.cookie)
	answered := make(chan error, 1)
	go func() {
		resp, err := e.s.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second, FailOnTimeout: true})
		if err == nil {
			err = resp.Body.Close()
		}
		answered <- err
	}()
	<-running
	if err := e.s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-answered; err != nil {
		t.Fatal(err)
	}

	events := e.events()
	if len(events) != 1 || events[0].Action != string(audit.QueryRun) || events[0].Reason != "cancelled" {
		t.Fatalf("stored %+v, want one cancelled query.run", events)
	}
	if got := e.historyRows(); got != 1 {
		t.Errorf("history rows %d, want the cancelled query's row", got)
	}
}
