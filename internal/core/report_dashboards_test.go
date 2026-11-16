package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// dashboardReportEnv adds a second team and source, both reached by the
// owner, and a dashboard with a stat, a chart and a table on the first team
// in tab main, and a count stat on the second team in tab ops.
type dashboardReportEnv struct {
	*reportEnv
	team2     *models.Team
	source2   *models.Source
	dashboard *models.Dashboard
}

const dashboardReportBlob = `{"version":2,"features":[],"default_time_range":"6h",
"tabs":[
 {"id":"main","title":"Main","layout":[
  {"id":"table","x":0,"y":2,"w":12,"h":4},
  {"id":"stat","x":0,"y":0,"w":6,"h":2},
  {"id":"chart","x":6,"y":0,"w":6,"h":2}]},
 {"id":"ops","title":"Ops","layout":[{"id":"count","x":0,"y":0,"w":6,"h":2}]}],
"panels":[
 {"id":"stat","kind":"query","title":"Bytes served","team_id":%[1]d,"source_id":%[2]d,
  "query":{"mode":"result","language":"clickhouse-sql","text":"SELECT sum(bytes) AS n FROM logs WHERE ts BETWEEN {{__from}} AND {{__to}}"},
  "viz":{"type":"stat","value":"n","reduce":"sum","format":{"unit":"bytes"}}},
 {"id":"chart","kind":"query","title":"Errors over time","team_id":%[1]d,"source_id":%[2]d,
  "query":{"mode":"count","language":"clickhouse-sql","text":"SELECT * FROM logs","interval":"auto"},
  "viz":{"type":"chart","mark":"line","stack":"none"}},
 {"id":"table","kind":"query","title":"Recent errors","team_id":%[1]d,"source_id":%[2]d,
  "query":{"mode":"result","language":"clickhouse-sql","text":"SELECT status, host FROM logs LIMIT 5"},
  "viz":{"type":"table","visible_columns":["host","status"]}},
 {"id":"count","kind":"query","title":"Requests","team_id":%[3]d,"source_id":%[4]d,
  "query":{"mode":"count","language":"clickhouse-sql","text":"SELECT * FROM other_logs","interval":"auto"},
  "viz":{"type":"stat","reduce":"sum"}}]}`

func newDashboardReportEnv(t *testing.T) *dashboardReportEnv {
	t.Helper()
	ctx := context.Background()
	e := &dashboardReportEnv{reportEnv: newReportEnv(t)}
	e.source2 = newTestSource(t, e.db, "other_logs")
	e.team2 = newTestTeam(t, e.db, "ops", models.TeamRoleMember, e.owner)
	if err := e.db.AddTeamSource(ctx, e.team2.ID, e.source2.ID); err != nil {
		t.Fatal(err)
	}
	blob := fmt.Sprintf(dashboardReportBlob, e.team.ID, e.source.ID, e.team2.ID, e.source2.ID)
	d, err := CreateDashboard(ctx, e.db, discardLogger(), e.owner, &models.CreateDashboardRequest{Name: "Edge", Panels: json.RawMessage(blob)})
	if err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}
	e.dashboard = d
	e.query = func(_ context.Context, req datasource.QueryRequest) (*models.QueryResult, error) {
		switch {
		case strings.Contains(req.RawQuery, "sum(bytes)"):
			return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "n", Type: "UInt64"}}, Logs: []map[string]any{{"n": 1536}}}, nil
		case strings.Contains(req.RawQuery, "status, host"):
			return &models.QueryResult{
				Columns: []models.ColumnInfo{{Name: "status", Type: "UInt16"}, {Name: "host", Type: "String"}},
				Logs:    []map[string]any{{"status": 503, "host": "api-1"}},
			}, nil
		}
		return nil, fmt.Errorf("unexpected query %q", req.RawQuery)
	}
	return e
}

func (e *dashboardReportEnv) input(tab string) *ReportInput {
	return &ReportInput{
		Name:     "Edge daily",
		Source:   ReportSourceRef{Kind: models.ReportSourceDashboard, DashboardID: &e.dashboard.ID, TabID: tab},
		Schedule: json.RawMessage(`{"frequency":"daily","hour":9,"minute":0}`),
		Timezone: "UTC",
	}
}

func (e *dashboardReportEnv) createDashboardReport(tab string) *models.Report {
	e.t.Helper()
	r, err := CreateReport(context.Background(), e.db, access.SessionPrincipal(e.owner), e.input(tab))
	if err != nil {
		e.t.Fatalf("CreateReport: %v", err)
	}
	return r.Report
}

func panelStatuses(run *models.ReportRun) string {
	var parts []string
	for _, p := range run.Panels {
		parts = append(parts, p.PanelID+":"+string(p.Status))
	}
	return strings.Join(parts, " ")
}

func TestDashboardReportSendsStatChartAndTable(t *testing.T) {
	e := newDashboardReportEnv(t)
	e.histogram = func(_ context.Context, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
		at := req.EndTime.Add(-time.Hour).Truncate(time.Hour)
		return &datasource.HistogramResult{Granularity: req.Window, Data: []datasource.HistogramBucket{{Bucket: at, LogCount: 4321}}}, nil
	}
	r := e.createDashboardReport("main")
	run := e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunSent {
		t.Fatalf("run = %s/%s, want sent; panels %+v deliveries %+v", run.Status, run.Reason, run.Panels, run.Deliveries)
	}
	if got := panelStatuses(run); got != "stat:ok chart:ok table:ok" {
		t.Errorf("panels in reading order = %q", got)
	}
	if run.WindowEnd.Sub(*run.WindowStart) != 6*time.Hour {
		t.Errorf("window = %v to %v, want the dashboard default of 6h", run.WindowStart, run.WindowEnd)
	}
	sent := e.mailer.messages()
	if len(sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(sent))
	}
	html := sent[0].msg.HTML
	for _, want := range []string{"1.5 KiB", "api-1", "503", ">4,321<", "Each bar covers ", "Fixed window", fmt.Sprintf("/dashboards/%d?tab=main", e.dashboard.ID)} {
		if !strings.Contains(html, want) {
			t.Errorf("email lacks %q", want)
		}
	}
	if strings.Contains(html, "This panel is not shown in email.") {
		t.Errorf("the chart is marked unsupported")
	}
	if strings.Count(html, "Fixed window") != 2 {
		t.Errorf("want the fixed-window note on the chart and the table, as the dashboard chip shows it; the stat takes the range")
	}
	stat, chart, table := strings.Index(html, "Bytes served"), strings.Index(html, "Errors over time"), strings.Index(html, "Recent errors")
	if stat >= chart || chart >= table {
		t.Errorf("panels are not in reading order: stat %d, chart %d, table %d", stat, chart, table)
	}
	if host, status := strings.Index(html, ">host<"), strings.Index(html, ">status<"); host < 0 || host > status {
		t.Errorf("table columns are not in visible_columns order")
	}
}

func TestDashboardReportCountStat(t *testing.T) {
	e := newDashboardReportEnv(t)
	var window string
	e.histogram = func(_ context.Context, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
		window = req.Window
		at := req.StartTime.Truncate(time.Hour).Add(time.Hour)
		return &datasource.HistogramResult{Granularity: req.Window, Data: []datasource.HistogramBucket{
			{Bucket: at, LogCount: 1200, GroupValue: "a"}, {Bucket: at, LogCount: 34}, {Bucket: at.Add(time.Hour), LogCount: 1},
		}}, nil
	}
	r := e.createDashboardReport("ops")
	run := e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunSent || panelStatuses(run) != "count:ok" || run.Panels[0].Rows != 3 {
		t.Fatalf("run = %s/%s, panels %+v", run.Status, run.Reason, run.Panels)
	}
	if window != "5m" {
		t.Errorf("histogram window = %q, want 5m, the auto window of a 6h range", window)
	}
	if html := e.mailer.messages()[0].msg.HTML; !strings.Contains(html, ">1,235<") {
		t.Errorf("email lacks the summed count 1,235")
	}

	e.histogram = func(_ context.Context, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
		return &datasource.HistogramResult{Granularity: req.Window}, nil
	}
	run = e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunSent || panelStatuses(run) != "count:empty" {
		t.Fatalf("empty run = %s/%s, panels %+v", run.Status, run.Reason, run.Panels)
	}
	if html := e.mailer.messages()[1].msg.HTML; !strings.Contains(html, ">0<") {
		t.Errorf("an empty count prints 0")
	}

	in := e.input("ops")
	in.SkipIfEmpty = true
	if _, _, err := UpdateReport(context.Background(), e.db, access.SessionPrincipal(e.owner), r.ID, in); err != nil {
		t.Fatal(err)
	}
	run = e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunSkipped || run.Reason != models.ReportReasonEmpty {
		t.Fatalf("skip_if_empty run = %s/%s, want skipped/empty", run.Status, run.Reason)
	}
}

func TestDashboardReportLocksPanelsTheOwnerLost(t *testing.T) {
	e := newDashboardReportEnv(t)
	ctx := context.Background()
	r := e.createDashboardReport("")
	if err := e.db.RemoveTeamMember(ctx, e.team2.ID, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	resp, err := BuildReportResponse(ctx, ReportDeps{DB: e.db, UIURL: "x"}, access.SessionPrincipal(e.owner), e.report(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	if !slicesContainsWarning(resp.Warnings, "panel_locked_for_owner", "count") || resp.Source.Name != "Edge" {
		t.Errorf("response = source %q, warnings %+v; want the dashboard name and count locked", resp.Source.Name, resp.Warnings)
	}
	run := e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunPartial || run.Reason != models.ReportReasonPanelLocked {
		t.Fatalf("run = %s/%s, want partial/panel_locked; panels %+v", run.Status, run.Reason, run.Panels)
	}
	if got := panelStatuses(run); got != "stat:ok chart:empty table:ok count:locked" {
		t.Errorf("panels = %q", got)
	}
	if html := e.mailer.messages()[0].msg.HTML; !strings.Contains(html, "The report owner no longer has access to this panel.") {
		t.Errorf("email lacks the locked placeholder")
	}

	if err := e.db.RemoveTeamMember(ctx, e.team.ID, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	run = e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunFailed || run.Reason != models.ReportReasonNoAccess {
		t.Fatalf("every panel locked: run = %s/%s, want failed/no_access", run.Status, run.Reason)
	}
	if n := len(e.mailer.messages()); n != 1 {
		t.Errorf("sent %d messages in all, want none for the locked run", n)
	}
}

func slicesContainsWarning(ws []ReportWarning, code, panel string) bool {
	for _, w := range ws {
		if w.Code == code && w.PanelID == panel {
			return true
		}
	}
	return false
}

// TestDashboardReportTestToANonMember covers the per-user test until
// per-recipient locking: a caller outside a panel's team gets no email.
func TestDashboardReportTestToANonMember(t *testing.T) {
	e := newDashboardReportEnv(t)
	admin := newTestAdmin(t, e.db, "admin@example.com")
	r := e.createDashboardReport("main")
	run := e.run(r, admin, ReportRunToMe)
	if run.Status != models.ReportRunFailed || len(run.Deliveries) != 1 || run.Deliveries[0].Reason != string(models.ReportReasonNoAccess) {
		t.Fatalf("run = %s/%s, deliveries %+v; want the admin skipped with no_access", run.Status, run.Reason, run.Deliveries)
	}
	if n := len(e.mailer.messages()); n != 0 {
		t.Errorf("sent %d messages, want none", n)
	}
}

func TestDashboardReportUpgradesASchema1Blob(t *testing.T) {
	e := newReportEnv(t)
	blob := fmt.Sprintf(`{"version":1,"layout":[{"id":"t1","x":0,"y":0,"w":12,"h":4}],"panels":[{"id":"t1","title":"Statuses","type":"table","team_id":%d,"source_id":%d,"query":"SELECT status FROM logs","query_language":"clickhouse-sql"}]}`, e.team.ID, e.source.ID)
	d, err := CreateDashboard(context.Background(), e.db, discardLogger(), e.owner, &models.CreateDashboardRequest{Name: "Old", Panels: json.RawMessage(blob)})
	if err != nil {
		t.Fatal(err)
	}
	var limit int
	e.query = func(_ context.Context, req datasource.QueryRequest) (*models.QueryResult, error) {
		limit = req.Limit
		return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "status", Type: "UInt16"}}, Logs: []map[string]any{{"status": 418}}}, nil
	}
	saved, err := CreateReport(context.Background(), e.db, access.SessionPrincipal(e.owner), &ReportInput{
		Name: "Old dashboard", Source: ReportSourceRef{Kind: models.ReportSourceDashboard, DashboardID: &d.ID, TabID: models.DashboardMainTabID},
		Schedule: json.RawMessage(`{"frequency":"hourly","minute":5}`), Timezone: "UTC",
	})
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}
	run := e.run(saved.Report, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunSent || panelStatuses(run) != "t1:ok" {
		t.Fatalf("run = %s/%s, panels %+v", run.Status, run.Reason, run.Panels)
	}
	if limit != models.DashboardUpgradedTableLimit {
		t.Errorf("query limit = %d, want the upgraded table limit %d", limit, models.DashboardUpgradedTableLimit)
	}
	if run.WindowEnd.Sub(*run.WindowStart) != 24*time.Hour {
		t.Errorf("window = %v, want 24h for a dashboard without a default range", run.WindowEnd.Sub(*run.WindowStart))
	}
	if html := e.mailer.messages()[0].msg.HTML; !strings.Contains(html, "418") {
		t.Errorf("email lacks the table row")
	}
}

func TestDashboardReportValidation(t *testing.T) {
	e := newDashboardReportEnv(t)
	ctx := context.Background()
	p := access.SessionPrincipal(e.owner)
	outsider := newTestUser(t, e.db, "outsider@example.com", "Outsider")
	today := "today"
	for _, tc := range []struct {
		name   string
		mutate func(in *ReportInput)
		want   error
		field  string
	}{
		{"unknown tab", func(in *ReportInput) { in.Source.TabID = "nope" }, ErrInvalidReport, "no such tab"},
		{"attach_csv", func(in *ReportInput) { in.AttachCSV = true }, ErrInvalidReport, "attach_csv"},
		{"variables", func(in *ReportInput) { in.Variables = map[string]any{"host": "a"} }, ErrInvalidReport, "variables"},
		{"both sources", func(in *ReportInput) { in.Source.SavedQueryID = &e.sq.ID }, ErrInvalidReport, "saved_query_id"},
		{"no dashboard id", func(in *ReportInput) { in.Source.DashboardID = nil }, ErrInvalidReport, "source.dashboard_id"},
		{"day_start with a calendar range", func(in *ReportInput) {
			in.TimeRange, in.RangeAnchor = &today, models.ReportAnchorDayStart
		}, ErrInvalidReport, "time_range"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := e.input("main")
			tc.mutate(in)
			_, err := CreateReport(ctx, e.db, p, in)
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("CreateReport = %v, want %v naming %s", err, tc.want, tc.field)
			}
		})
	}

	t.Run("an owner who cannot view the dashboard", func(t *testing.T) {
		if _, err := CreateReport(ctx, e.db, access.SessionPrincipal(outsider), e.input("main")); !errors.Is(err, ErrReportForbidden) {
			t.Fatalf("CreateReport = %v, want ErrReportForbidden", err)
		}
	})
	t.Run("no time range keeps the dashboard default", func(t *testing.T) {
		saved, err := CreateReport(ctx, e.db, p, e.input("main"))
		if err != nil {
			t.Fatal(err)
		}
		if r := saved.Report; r.TimeRange != "" || r.SourceKind != models.ReportSourceDashboard || r.TabID != "main" {
			t.Errorf("report = %+v", r)
		}
	})
}

func TestDashboardReportClaim(t *testing.T) {
	e := newDashboardReportEnv(t)
	ctx := context.Background()
	r := e.createDashboardReport("main")
	if err := e.db.DeleteUser(ctx, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	outsider := newTestUser(t, e.db, "outsider@example.com", "Outsider")
	if _, _, err := ClaimReport(ctx, e.db, access.SessionPrincipal(outsider), r.ID); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("claim by a user who cannot view the dashboard = %v, want ErrReportNotFound", err)
	}
	member := newTestUser(t, e.db, "member@example.com", "Member")
	if err := e.db.AddTeamMember(ctx, e.team.ID, member.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	if _, after, err := ClaimReport(ctx, e.db, access.SessionPrincipal(member), r.ID); err != nil || after.OwnerID == nil || *after.OwnerID != member.ID {
		t.Fatalf("claim by a team member = %+v, %v", after, err)
	}
}

// TestDashboardReportCountChartBars covers the 24-bar rule through the run:
// a 7-day range asks for 12-hour buckets, and a 30-day range asks for
// 24 hours and adds pairs of days into 15 bars.
func TestDashboardReportCountChartBars(t *testing.T) {
	e := newDashboardReportEnv(t)
	var windows []string
	e.histogram = func(_ context.Context, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
		windows = append(windows, req.Window)
		var data []datasource.HistogramBucket
		if req.Window == "24h" {
			for day := range 30 {
				data = append(data, datasource.HistogramBucket{Bucket: req.StartTime.AddDate(0, 0, day), LogCount: day + 1})
			}
		}
		return &datasource.HistogramResult{Granularity: req.Window, Data: data}, nil
	}
	r := e.createDashboardReport("main")
	for _, tc := range []struct{ timeRange, window string }{{"7d", "12h"}, {"30d", "24h"}} {
		in := e.input("main")
		in.TimeRange = &tc.timeRange
		in.RangeAnchor = models.ReportAnchorDayStart
		if _, _, err := UpdateReport(context.Background(), e.db, access.SessionPrincipal(e.owner), r.ID, in); err != nil {
			t.Fatal(err)
		}
		windows = nil
		run := e.run(r, e.owner, ReportRunToMe)
		if run.Status != models.ReportRunSent {
			t.Fatalf("%s: run = %s/%s, panels %+v", tc.timeRange, run.Status, run.Reason, run.Panels)
		}
		if len(windows) != 1 || windows[0] != tc.window {
			t.Errorf("%s: histogram windows = %v, want [%s]", tc.timeRange, windows, tc.window)
		}
	}
	text := e.mailer.messages()[1].msg.Text
	if !strings.Contains(text, "Each bar covers 2 days") {
		t.Errorf("the 30-day email does not say each bar covers two days:\n%s", text)
	}
	// Day d holds d+1 logs, so bar b adds days 2b and 2b+1: 4b+3.
	for b := range 15 {
		if !strings.Contains(text, fmt.Sprintf(" | %d\n", 4*b+3)) {
			t.Errorf("the 30-day email lacks bar %d with %d logs", b, 4*b+3)
		}
	}
	if strings.Contains(text, " | 63\n") {
		t.Errorf("the 30-day email has a 16th bar")
	}
}
