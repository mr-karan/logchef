package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/reports"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// fakeMailer records each message, or fails every send with err.
type fakeMailer struct {
	mu   sync.Mutex
	err  error
	sent []sentMail
}

type sentMail struct {
	to  string
	msg reports.MailMessage
}

func (m *fakeMailer) Send(_ context.Context, to mail.Address, msg reports.MailMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, sentMail{to: to.Address, msg: msg})
	return nil
}

func (m *fakeMailer) messages() []sentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentMail(nil), m.sent...)
}

// reportEnv is an owner in a team that has a source, a saved query on it,
// and a runner whose datasource answers through query.
type reportEnv struct {
	t      *testing.T
	db     *sqlite.DB
	dbPath string
	owner  *models.User
	team   *models.Team
	source *models.Source
	sq     *models.SavedQuery
	mailer *fakeMailer
	rr     *ReportRunner
	// query answers every panel query; the default returns one row.
	query func(ctx context.Context, req datasource.QueryRequest) (*models.QueryResult, error)
	// histogram answers every count panel; the default has no buckets.
	histogram func(ctx context.Context, req datasource.HistogramRequest) (*datasource.HistogramResult, error)
}

const reportQueryContent = `{"version":1,"sourceId":%d,"limit":100,"content":"SELECT status FROM logs WHERE day = {{day}}","variables":[{"name":"day","type":"day","label":"Day","inputType":"date","value":"","defaultValue":"today"}]}`

func newReportEnv(t *testing.T) *reportEnv {
	t.Helper()
	ctx := context.Background()
	db, dbPath := newTestDBWithPath(t)
	e := &reportEnv{t: t, db: db, dbPath: dbPath, mailer: &fakeMailer{}}
	e.owner = newTestUser(t, db, "owner@example.com", "Owner")
	e.source = newTestSource(t, db, "app_logs")
	e.team = newTestTeam(t, db, "payments", models.TeamRoleMember, e.owner)
	if err := db.AddTeamSource(ctx, e.team.ID, e.source.ID); err != nil {
		t.Fatal(err)
	}
	content := strings.Replace(reportQueryContent, "%d", "1", 1)
	sq, err := db.CreateSavedQuery(ctx, e.source.ID, nil, "5xx by status", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, content, &e.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.sq = sq
	e.query = func(context.Context, datasource.QueryRequest) (*models.QueryResult, error) {
		return &models.QueryResult{
			Columns: []models.ColumnInfo{{Name: "status", Type: "UInt16"}},
			Logs:    []map[string]any{{"status": 503}},
		}, nil
	}
	e.histogram = func(_ context.Context, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
		return &datasource.HistogramResult{Granularity: req.Window}, nil
	}
	provider := &fakeProvider{
		queryLogsFn: func(ctx context.Context, _ *models.Source, req datasource.QueryRequest) (*models.QueryResult, error) {
			return e.query(ctx, req)
		},
		histogramFn: func(ctx context.Context, _ *models.Source, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
			return e.histogram(ctx, req)
		},
	}
	e.rr = &ReportRunner{
		DB: db,
		DS: newFakeDatasourceService(db, discardLogger(), provider),
		Query: config.QueryConfig{
			DefaultPreviewLimit: 1000, MaxPreviewLimit: 10000, MaxResponseBytes: 64 << 20,
			DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 300,
		},
		Mailer:      e.mailer,
		Audit:       audit.New(audit.Options{Store: db, Logger: discardLogger()}),
		Log:         discardLogger(),
		UIURL:       "https://logchef.example.com",
		QueryBudget: 5 * time.Minute,
	}
	return e
}

func (e *reportEnv) input() *ReportInput {
	timeRange := "24h"
	return &ReportInput{
		Name:     "Daily 5xx",
		Source:   ReportSourceRef{Kind: models.ReportSourceSavedQuery, SavedQueryID: &e.sq.ID},
		Schedule: json.RawMessage(`{"frequency":"daily","hour":9,"minute":0}`),
		Timezone: "Asia/Kolkata", TimeRange: &timeRange,
	}
}

func (e *reportEnv) create() *models.Report {
	e.t.Helper()
	r, err := CreateReport(context.Background(), e.db, access.SessionPrincipal(e.owner), e.input())
	if err != nil {
		e.t.Fatalf("CreateReport: %v", err)
	}
	return r.Report
}

// run claims a manual run as actor, without the cooldown, executes it and
// returns the stored result.
func (e *reportEnv) run(r *models.Report, actor *models.User, to ReportRunRecipients) *models.ReportRun {
	e.t.Helper()
	ctx := context.Background()
	claimed, err := e.db.CreateReportRun(ctx, models.ReportRunClaim{
		ReportID: r.ID, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now().UTC(),
		ActorID: &actor.ID, HandlerBudget: reports.HandlerBudget(e.rr.QueryBudget),
	})
	if err != nil {
		e.t.Fatalf("CreateReportRun: %v", err)
	}
	runCtx, cancel := context.WithDeadline(ctx, *claimed.DeadlineAt)
	defer cancel()
	e.rr.Execute(runCtx, claimed, to)
	return e.lastRun(r.ID)
}

func (e *reportEnv) lastRun(id models.ReportID) *models.ReportRun {
	e.t.Helper()
	runs, err := e.db.ListReportRuns(context.Background(), id, 0, 1)
	if err != nil || len(runs) != 1 {
		e.t.Fatalf("ListReportRuns = %v, %v", runs, err)
	}
	return runs[0]
}

func (e *reportEnv) report(id models.ReportID) *models.Report {
	e.t.Helper()
	r, err := e.db.GetReport(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func TestReportRunSendsToTheOwner(t *testing.T) {
	e := newReportEnv(t)
	var raw string
	e.query = func(_ context.Context, req datasource.QueryRequest) (*models.QueryResult, error) {
		raw = req.RawQuery
		return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "status", Type: "UInt16"}}, Logs: []map[string]any{{"status": 503}}}, nil
	}
	r := e.create()
	run := e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunSent || run.Reason != "" {
		t.Fatalf("run = %s/%s, want sent; panels %+v deliveries %+v", run.Status, run.Reason, run.Panels, run.Deliveries)
	}
	if len(run.Panels) != 1 || run.Panels[0].Status != models.ReportPanelOK || run.Panels[0].Rows != 1 {
		t.Errorf("panels = %+v", run.Panels)
	}
	if len(run.Deliveries) != 1 || run.Deliveries[0].Status != models.ReportDeliverySent || run.Deliveries[0].Attempts != 1 {
		t.Errorf("deliveries = %+v", run.Deliveries)
	}
	if run.WindowStart == nil || run.WindowEnd == nil || run.WindowEnd.Sub(*run.WindowStart) != 24*time.Hour {
		t.Errorf("window = %v to %v, want 24h", run.WindowStart, run.WindowEnd)
	}
	sent := e.mailer.messages()
	if len(sent) != 1 || sent[0].to != e.owner.Email || !strings.Contains(sent[0].msg.HTML, "503") || !strings.HasPrefix(sent[0].msg.Subject, "[Logchef] Daily 5xx, ") {
		t.Fatalf("sent = %+v", sent)
	}
	today := run.ScheduledFor.In(mustLocation(t, "Asia/Kolkata")).Format(time.DateOnly)
	if !strings.Contains(raw, "'"+today+"'") {
		t.Errorf("query %q does not carry the day default resolved to %s", raw, today)
	}
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestReportRunOwnerLostTheTeam(t *testing.T) {
	e := newReportEnv(t)
	r := e.create()
	if err := e.db.RemoveTeamMember(context.Background(), e.team.ID, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	run := e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunFailed || run.Reason != models.ReportReasonNoAccess {
		t.Fatalf("run = %s/%s, want failed/no_access", run.Status, run.Reason)
	}
	if n := len(e.mailer.messages()); n != 0 {
		t.Errorf("sent %d messages, want none", n)
	}
}

func TestReportRunAdminOwnerOutsideTheTeam(t *testing.T) {
	e := newReportEnv(t)
	admin := newTestAdmin(t, e.db, "admin@example.com")
	r := &models.Report{
		Name: "Admin's report", OwnerID: &admin.ID, SourceKind: models.ReportSourceSavedQuery, SavedQueryID: &e.sq.ID,
		Schedule: models.ReportSchedule{Frequency: models.ReportHourly}, Timezone: "UTC", TimeRange: "1h",
		RangeAnchor: models.ReportAnchorSendTime, Variables: map[string]any{}, MaxRows: 25,
	}
	if err := e.db.CreateReport(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	run := e.run(r, admin, ReportRunToMe)
	if run.Status != models.ReportRunFailed || run.Reason != models.ReportReasonNoAccess {
		t.Fatalf("run = %s/%s, want failed/no_access: a report never uses the admin bypass", run.Status, run.Reason)
	}
}

func TestReportRunInactiveOwnerPausesTheReport(t *testing.T) {
	e := newReportEnv(t)
	r := e.create()
	e.owner.Status = models.UserStatusInactive
	if err := e.db.UpdateUser(context.Background(), e.owner); err != nil {
		t.Fatal(err)
	}
	run := e.run(r, e.owner, ReportRunToAll)
	if run.Status != models.ReportRunSkipped || run.Reason != models.ReportReasonOwnerInactive {
		t.Fatalf("run = %s/%s, want skipped/owner_inactive", run.Status, run.Reason)
	}
	got := e.report(r.ID)
	if got.State != models.ReportStatePaused || got.PauseReason != models.ReportPauseOwnerInactive {
		t.Errorf("report = %s/%s, want paused/owner_inactive", got.State, got.PauseReason)
	}
	events, err := e.db.ListAuditEvents(context.Background(), models.AuditFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Action != string(audit.ReportPause) || events[0].ActorType != models.AuditActorSystem || events[0].AuthMethod != models.AuditAuthScheduler {
		t.Fatalf("audit events = %+v, want one system report.pause", events)
	}
	if !strings.Contains(string(events[0].Detail), `"owner_inactive"`) {
		t.Errorf("pause detail = %s", events[0].Detail)
	}
}

func TestReportRunThreeFailuresPause(t *testing.T) {
	e := newReportEnv(t)
	e.query = func(context.Context, datasource.QueryRequest) (*models.QueryResult, error) {
		return nil, errors.New("Code: 62. Syntax error near 'SELEC'")
	}
	r := e.create()
	for i := range 3 {
		run := e.run(r, e.owner, ReportRunToMe)
		if run.Status != models.ReportRunFailed || run.Reason != models.ReportReasonAllPanelsFailed {
			t.Fatalf("run %d = %s/%s, want failed/all_panels_failed", i+1, run.Status, run.Reason)
		}
		if !strings.Contains(run.Panels[0].Error, "Syntax error") {
			t.Errorf("run %d panel error = %q, want the query error in the history", i+1, run.Panels[0].Error)
		}
		wantState := models.ReportStateActive
		if i == 2 {
			wantState = models.ReportStatePaused
		}
		if got := e.report(r.ID); got.State != wantState {
			t.Fatalf("after run %d the report is %s, want %s", i+1, got.State, wantState)
		}
	}
	if got := e.report(r.ID); got.PauseReason != models.ReportPauseFailures {
		t.Errorf("pause reason = %s, want failures", got.PauseReason)
	}
	sent := e.mailer.messages()
	if len(sent) != 1 || sent[0].to != e.owner.Email || !strings.Contains(sent[0].msg.Text, "all_panels_failed") || strings.Contains(sent[0].msg.Text, "Syntax error") {
		t.Fatalf("sent = %+v, want one pause notice with the reason and no query error", sent)
	}
}

func TestReportRunTestToAnAdminOutsideTheSource(t *testing.T) {
	e := newReportEnv(t)
	admin := newTestAdmin(t, e.db, "admin@example.com")
	r := e.create()
	run := e.run(r, admin, ReportRunToMe)
	if run.Status != models.ReportRunFailed || run.Reason != models.ReportReasonNoAccess {
		t.Fatalf("run = %s/%s, want failed/no_access", run.Status, run.Reason)
	}
	if len(run.Deliveries) != 1 || run.Deliveries[0].Status != models.ReportDeliverySkipped || run.Deliveries[0].Reason != "no_access" {
		t.Errorf("deliveries = %+v", run.Deliveries)
	}
	if n := len(e.mailer.messages()); n != 0 {
		t.Errorf("sent %d messages to an admin who cannot see the source", n)
	}
}

func TestReportRunSMTPNotConfigured(t *testing.T) {
	e := newReportEnv(t)
	e.mailer.err = &reports.MailError{Class: reports.MailPermanent, Err: reports.ErrMailNotConfigured}
	r := e.create()
	run := e.run(r, e.owner, ReportRunToMe)
	if run.Status != models.ReportRunFailed || run.Reason != models.ReportReasonSMTPNotConfigured {
		t.Fatalf("run = %s/%s, want failed/smtp_not_configured", run.Status, run.Reason)
	}
}

// A slow query fails at the query deadline, and the final update lands
// although the handler's context has ended.
func TestReportRunFinalUpdateAfterTheDeadline(t *testing.T) {
	e := newReportEnv(t)
	e.query = func(ctx context.Context, _ datasource.QueryRequest) (*models.QueryResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	r := e.create()
	ctx := context.Background()
	claimed, err := e.db.CreateReportRun(ctx, models.ReportRunClaim{
		ReportID: r.ID, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now().UTC(),
		ActorID: &e.owner.ID, HandlerBudget: reports.HandlerBudget(e.rr.QueryBudget),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, live := range []time.Duration{300 * time.Millisecond, 0} {
		runCtx, cancel := context.WithTimeout(ctx, live)
		start := time.Now()
		e.rr.Execute(runCtx, claimed, ReportRunToMe)
		cancel()
		if time.Since(start) > 5*time.Second {
			t.Fatalf("Execute ran %s past a handler deadline of %s", time.Since(start), live)
		}
		run := e.lastRun(r.ID)
		if run.Status != models.ReportRunFailed || run.Reason != models.ReportReasonTimeout || run.FinishedAt == nil {
			t.Fatalf("handler deadline %s: run = %+v, want a stored failed/timeout", live, run)
		}
		if live == 0 {
			break
		}
		if claimed, err = e.db.CreateReportRun(ctx, models.ReportRunClaim{
			ReportID: r.ID, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now().UTC(),
			ActorID: &e.owner.ID, HandlerBudget: reports.HandlerBudget(e.rr.QueryBudget),
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// A run waits for a slot inside its own deadline, and fails with timeout
// when none frees in time.
func TestReportRunWaitsForASlot(t *testing.T) {
	e := newReportEnv(t)
	e.rr.Slots = make(chan struct{}, 1)
	e.rr.Slots <- struct{}{}
	r := e.create()
	ctx := context.Background()
	claimed, err := e.db.CreateReportRun(ctx, models.ReportRunClaim{
		ReportID: r.ID, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now().UTC(),
		ActorID: &e.owner.ID, HandlerBudget: reports.HandlerBudget(e.rr.QueryBudget),
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	e.rr.Execute(runCtx, claimed, ReportRunToMe)
	cancel()
	if run := e.lastRun(r.ID); run.Status != models.ReportRunFailed || run.Reason != models.ReportReasonTimeout {
		t.Fatalf("run without a slot = %s/%s, want failed/timeout", run.Status, run.Reason)
	}
	if n := len(e.mailer.messages()); n != 0 {
		t.Fatalf("a run without a slot sent %d messages", n)
	}
	<-e.rr.Slots
	if run := e.run(r, e.owner, ReportRunToMe); run.Status != models.ReportRunSent {
		t.Fatalf("run with a free slot = %s/%s, want sent", run.Status, run.Reason)
	}
	if len(e.rr.Slots) != 0 {
		t.Errorf("the run kept its slot after it ended")
	}
}

// With a 20 minute budget the handler deadline is 25 minutes out. Cleanup
// 16 minutes in leaves the run alone and a manual run gets in-flight;
// cleanup past the deadline plus the grace interrupts it and admits a new
// run. The clock moves by shrinking the grace cleanup is called with.
func TestReportRunBudgetAndCleanup(t *testing.T) {
	e := newReportEnv(t)
	e.rr.QueryBudget = 20 * time.Minute
	r := e.create()
	ctx := context.Background()
	tick := time.Date(2026, 11, 16, 3, 30, 0, 0, time.UTC)
	if _, err := e.db.CreateReportRun(ctx, models.ReportRunClaim{ReportID: r.ID, Trigger: models.ReportTriggerSchedule, ScheduledFor: tick, HandlerBudget: reports.HandlerBudget(e.rr.QueryBudget)}); err != nil {
		t.Fatal(err)
	}
	cleanupAt := func(elapsed time.Duration) int64 {
		n, err := e.db.InterruptStaleReportRuns(ctx, reports.InterruptGrace-elapsed)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := cleanupAt(16 * time.Minute); n != 0 {
		t.Fatalf("cleanup 16 minutes in interrupted %d runs, want 0", n)
	}
	if _, err := e.rr.StartManualRun(ctx, access.SessionPrincipal(e.owner), r.ID); !errors.Is(err, models.ErrReportRunInFlight) {
		t.Fatalf("manual run during the scheduled run = %v, want ErrReportRunInFlight", err)
	}
	if n := cleanupAt(25*time.Minute + reports.InterruptGrace + time.Second); n != 1 {
		t.Fatalf("cleanup past the deadline interrupted %d runs, want 1", n)
	}
	if got := e.lastRun(r.ID); got.Status != models.ReportRunInterrupted || got.Reason != models.ReportReasonProcessStopped {
		t.Errorf("run = %s/%s, want interrupted/process_stopped", got.Status, got.Reason)
	}
	if _, err := e.db.CreateReportRun(ctx, models.ReportRunClaim{ReportID: r.ID, Trigger: models.ReportTriggerSchedule, ScheduledFor: tick.Add(time.Hour), HandlerBudget: time.Minute}); err != nil {
		t.Errorf("a new run after cleanup = %v, want it admitted", err)
	}
}

func TestReportManualRunCooldownAndInFlight(t *testing.T) {
	e := newReportEnv(t)
	r := e.create()
	ctx := context.Background()
	p := access.SessionPrincipal(e.owner)
	run, err := e.rr.StartManualRun(ctx, p, r.ID)
	if err != nil || run.Status != models.ReportRunRunning || run.DeadlineAt == nil {
		t.Fatalf("StartManualRun = %+v, %v", run, err)
	}
	if _, err := e.rr.StartManualRun(ctx, p, r.ID); !errors.Is(err, ErrReportCooldown) {
		t.Errorf("a second manual run within a minute = %v, want ErrReportCooldown", err)
	}
	other := newTestUser(t, e.db, "other@example.com", "Other")
	if _, err := e.rr.StartManualRun(ctx, access.SessionPrincipal(other), r.ID); !errors.Is(err, ErrReportNotFound) {
		t.Errorf("a manual run by a stranger = %v, want ErrReportNotFound", err)
	}
}

func TestReportPreview(t *testing.T) {
	e := newReportEnv(t)
	r := e.create()
	ctx := context.Background()
	preview, err := e.rr.Preview(ctx, access.SessionPrincipal(e.owner), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview.HTML, "503") || !strings.HasPrefix(preview.Subject, "[Logchef] Daily 5xx") {
		t.Errorf("preview = %+v", preview)
	}
	if _, err := e.rr.Preview(ctx, access.SessionPrincipal(e.owner), r.ID); !errors.Is(err, ErrReportCooldown) {
		t.Errorf("a second preview within a minute = %v, want ErrReportCooldown", err)
	}
	if runs, _ := e.db.ListReportRuns(ctx, r.ID, 0, 10); len(runs) != 0 {
		t.Errorf("a preview stored %d runs", len(runs))
	}
	if n := len(e.mailer.messages()); n != 0 {
		t.Errorf("a preview sent %d messages", n)
	}
}

func TestReportValidation(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	p := access.SessionPrincipal(e.owner)
	day := "today"
	long := strings.Repeat("é", 201)
	for _, tc := range []struct {
		name   string
		mutate func(in *ReportInput)
		want   error
		field  string
	}{
		{"empty name", func(in *ReportInput) { in.Name = " " }, ErrInvalidReport, "name"},
		{"long name", func(in *ReportInput) { in.Name = long }, ErrInvalidReport, "name"},
		{"name with a line break", func(in *ReportInput) { in.Name = "a\r\nBcc: x@example.com" }, ErrInvalidReport, "name"},
		{"recipients", func(in *ReportInput) {
			in.Recipients = []json.RawMessage{json.RawMessage(`{"kind":"email","email":"x@example.com"}`)}
		}, ErrInvalidReport, "recipients"},
		{"missing dashboard", func(in *ReportInput) {
			in.Source = ReportSourceRef{Kind: models.ReportSourceDashboard, DashboardID: new(9999)}
		}, ErrInvalidReport, "source.dashboard_id"},
		{"unknown source kind", func(in *ReportInput) { in.Source.Kind = "saved_view" }, ErrInvalidReport, "source.kind"},
		{"missing saved query", func(in *ReportInput) { in.Source.SavedQueryID = new(9999) }, ErrInvalidReport, "source.saved_query_id"},
		{"bad schedule", func(in *ReportInput) { in.Schedule = json.RawMessage(`{"frequency":"daily","minute":0}`) }, ErrInvalidReport, "schedule.hour"},
		{"schedule with an unknown field", func(in *ReportInput) { in.Schedule = json.RawMessage(`{"frequency":"hourly","minute":0,"every":"5m"}`) }, ErrInvalidReport, "schedule"},
		{"no timezone", func(in *ReportInput) { in.Timezone = "" }, ErrInvalidReport, "timezone"},
		{"bad timezone", func(in *ReportInput) { in.Timezone = "Mars/Olympus" }, ErrInvalidReport, "timezone"},
		{"no time range", func(in *ReportInput) { in.TimeRange = nil }, ErrInvalidReport, "time_range"},
		{"day_start with a calendar range", func(in *ReportInput) { in.TimeRange = &day; in.RangeAnchor = models.ReportAnchorDayStart }, ErrInvalidReport, "time_range"},
		{"unknown anchor", func(in *ReportInput) { in.RangeAnchor = "noon" }, ErrInvalidReport, "range_anchor"},
		{"max_rows below 5", func(in *ReportInput) { in.MaxRows = new(4) }, ErrInvalidReport, "max_rows"},
		{"max_rows above 100", func(in *ReportInput) { in.MaxRows = new(101) }, ErrInvalidReport, "max_rows"},
		{"undeclared variable", func(in *ReportInput) { in.Variables = map[string]any{"host": "a"} }, ErrInvalidReport, "variables"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := e.input()
			tc.mutate(in)
			_, err := CreateReport(ctx, e.db, p, in)
			if !errors.Is(err, tc.want) || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("CreateReport = %v, want %v naming %s", err, tc.want, tc.field)
			}
		})
	}

	t.Run("a required variable without a value", func(t *testing.T) {
		content := `{"version":1,"sourceId":1,"limit":10,"content":"SELECT 1 WHERE host = {{host}}","variables":[{"name":"host","type":"text","label":"Host","inputType":"input","value":""}]}`
		sq, err := e.db.CreateSavedQuery(ctx, e.source.ID, nil, "by host", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, content, &e.owner.ID)
		if err != nil {
			t.Fatal(err)
		}
		in := e.input()
		in.Source.SavedQueryID = &sq.ID
		if _, err := CreateReport(ctx, e.db, p, in); !errors.Is(err, ErrInvalidReport) || !strings.Contains(err.Error(), `"host" needs a value`) {
			t.Fatalf("CreateReport = %v", err)
		}
		in.Variables = map[string]any{"host": "api-1"}
		if _, err := CreateReport(ctx, e.db, p, in); err != nil {
			t.Fatalf("with the value: %v", err)
		}
	})

}

func TestReportOwnerMustViewTheSource(t *testing.T) {
	e := newReportEnv(t)
	outsider := newTestUser(t, e.db, "outsider@example.com", "Outsider")
	if _, err := CreateReport(context.Background(), e.db, access.SessionPrincipal(outsider), e.input()); !errors.Is(err, ErrReportForbidden) {
		t.Fatalf("CreateReport = %v, want ErrReportForbidden", err)
	}
}

// An admin edit checks the source against the owner, not the admin.
func TestReportAdminEditChecksTheOwner(t *testing.T) {
	e := newReportEnv(t)
	r := e.create()
	admin := newTestAdmin(t, e.db, "admin@example.com")
	other := newTestSource(t, e.db, "other_logs")
	otherTeam := newTestTeam(t, e.db, "other", models.TeamRoleMember, admin)
	ctx := context.Background()
	if err := e.db.AddTeamSource(ctx, otherTeam.ID, other.ID); err != nil {
		t.Fatal(err)
	}
	sq, err := e.db.CreateSavedQuery(ctx, other.ID, nil, "other", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, `{"version":1,"limit":10,"content":"SELECT 1"}`, &admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := e.input()
	in.Source.SavedQueryID = &sq.ID
	if _, _, err := UpdateReport(ctx, e.db, access.SessionPrincipal(admin), r.ID, in); !errors.Is(err, ErrReportForbidden) {
		t.Fatalf("UpdateReport = %v, want ErrReportForbidden", err)
	}
}

func TestReportClaim(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	r := e.create()
	member := newTestUser(t, e.db, "member@example.com", "Member")
	if err := e.db.AddTeamMember(ctx, e.team.ID, member.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ClaimReport(ctx, e.db, access.SessionPrincipal(member), r.ID); !errors.Is(err, ErrReportForbidden) {
		t.Fatalf("claiming an owned report = %v, want ErrReportForbidden", err)
	}
	if err := e.db.PauseReport(ctx, r.ID, models.ReportPauseOwnerInactive); err != nil {
		t.Fatal(err)
	}
	outsider := newTestUser(t, e.db, "outsider@example.com", "Outsider")
	if _, _, err := ClaimReport(ctx, e.db, access.SessionPrincipal(outsider), r.ID); !errors.Is(err, ErrReportNotFound) {
		t.Fatalf("a claim by a user without the source = %v, want ErrReportNotFound", err)
	}
	before, after, err := ClaimReport(ctx, e.db, access.SessionPrincipal(member), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if *before.OwnerID != e.owner.ID || *after.OwnerID != member.ID || after.State != models.ReportStateActive {
		t.Errorf("claim: before owner %v, after %+v", before.OwnerID, after)
	}
}
