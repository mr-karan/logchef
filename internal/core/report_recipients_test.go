package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/reports"
	"github.com/mr-karan/logchef/pkg/models"
)

func userRow(u *models.User) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"kind":"user","user_id":%d}`, u.ID))
}

func teamRow(t *models.Team) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"kind":"team","team_id":%d}`, t.ID))
}

func deliveriesByTarget(run *models.ReportRun) map[string]models.ReportDelivery {
	out := map[string]models.ReportDelivery{}
	for _, d := range run.Deliveries {
		out[d.Target] = d
	}
	return out
}

func target(u *models.User) string { return strconv.FormatInt(int64(u.ID), 10) }

func mailTo(t *testing.T, sent []sentMail, u *models.User) *reports.MailMessage {
	t.Helper()
	for i := range sent {
		if sent[i].to == u.Email {
			return &sent[i].msg
		}
	}
	return nil
}

// TestDashboardReportLocksPanelsPerRecipient is the first R8 check: a
// dashboard with panels on two teams and recipients who reach both, one,
// or neither. Each message holds exactly the panels its recipient reaches,
// a recipient with none gets a skipped delivery and no message, and a
// deactivated team member gets none.
func TestDashboardReportLocksPanelsPerRecipient(t *testing.T) {
	e := newDashboardReportEnv(t)
	ctx := context.Background()
	mainOnly := newTestUser(t, e.db, "main@example.com", "Main Only")
	both := newTestUser(t, e.db, "both@example.com", "Both")
	neither := newTestUser(t, e.db, "neither@example.com", "Neither")
	gone := newTestUser(t, e.db, "gone@example.com", "Gone")
	for _, u := range []*models.User{mainOnly, both, gone} {
		if err := e.db.AddTeamMember(ctx, e.team.ID, u.ID, models.TeamRoleMember); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.db.AddTeamMember(ctx, e.team2.ID, both.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	// The owner shares a team without sources with neither, so may add them.
	newTestTeam(t, e.db, "lobby", models.TeamRoleMember, e.owner, neither)
	gone.Status = models.UserStatusInactive
	if err := e.db.UpdateUser(ctx, gone); err != nil {
		t.Fatal(err)
	}
	// The Requests stat on the second team counts 7,731 and the chart on the
	// first team counts 3, so each value names the panel it came from.
	e.histogram = func(ctx context.Context, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
		n := 3
		if strings.Contains(req.Query, "other_logs") {
			n = 7731
		}
		return oneBucketHistogram(n)(ctx, req)
	}

	in := e.input("")
	in.Recipients = []json.RawMessage{userRow(mainOnly), userRow(both), userRow(neither), teamRow(e.team)}
	saved, err := CreateReport(ctx, e.db, access.SessionPrincipal(e.owner), in)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}
	run := e.run(saved.Report, e.owner, ReportRunToAll)
	if run.Status != models.ReportRunSent {
		t.Fatalf("run = %s/%s, deliveries %+v", run.Status, run.Reason, run.Deliveries)
	}

	got := deliveriesByTarget(run)
	if d := got[target(neither)]; d.Status != models.ReportDeliverySkipped || d.Reason != string(models.ReportReasonNoAccess) || len(d.LockedPanels) != 4 {
		t.Errorf("neither = %+v, want skipped no_access with every panel locked", d)
	}
	if d := got[target(gone)]; d.Status != models.ReportDeliverySkipped || d.Reason != reports.SkipInactive || d.Via != fmt.Sprintf("team:%d", e.team.ID) {
		t.Errorf("deactivated member = %+v, want skipped inactive through the team", d)
	}
	if d := got[target(mainOnly)]; d.Status != models.ReportDeliverySent || d.Via != "direct" || strings.Join(d.LockedPanels, ",") != "count" {
		t.Errorf("main-only = %+v, want sent directly with count locked", d)
	}
	if d := got[target(both)]; d.Status != models.ReportDeliverySent || len(d.LockedPanels) != 0 {
		t.Errorf("both = %+v, want sent with nothing locked", d)
	}
	if d := got[target(e.owner)]; d.Status != models.ReportDeliverySent || d.Via != fmt.Sprintf("team:%d", e.team.ID) {
		t.Errorf("owner = %+v, want sent through the team row", d)
	}
	if len(run.Deliveries) != 5 {
		t.Errorf("deliveries = %+v, want one each for five people", run.Deliveries)
	}

	sent := e.mailer.messages()
	if len(sent) != 3 {
		t.Fatalf("sent %d messages, want 3 (owner, main-only, both)", len(sent))
	}
	for _, u := range []*models.User{neither, gone} {
		if mailTo(t, sent, u) != nil {
			t.Errorf("%s got a message", u.Email)
		}
	}
	const restricted = "You do not have access to this panel."
	mainMsg, bothMsg := mailTo(t, sent, mainOnly), mailTo(t, sent, both)
	if mainMsg == nil || bothMsg == nil {
		t.Fatal("a recipient who reaches a panel got no message")
	}
	if !strings.Contains(mainMsg.HTML, "1.5 KiB") || !strings.Contains(mainMsg.HTML, "api-1") || strings.Count(mainMsg.HTML, restricted) != 1 {
		t.Errorf("main-only message: want the main tab's data and one locked panel")
	}
	if !strings.Contains(mainMsg.Text, "== Requests ==\n"+restricted+"\n") || strings.Contains(mainMsg.HTML+mainMsg.Text, "7,731") || strings.Contains(mainMsg.HTML+mainMsg.Text, "7731") {
		t.Errorf("main-only message: want the Requests title with the locked line and none of its value:\n%s", mainMsg.Text)
	}
	if !strings.Contains(mainMsg.Text, "| 3\n") {
		t.Errorf("main-only message: want the chart on the first team with its bar of 3:\n%s", mainMsg.Text)
	}
	if !strings.Contains(mainMsg.Text, "Sources: app_logs.") || !strings.Contains(bothMsg.Text, "Sources: app_logs, other_logs.") {
		t.Errorf("footers: main-only %q, both %q; want only the sources each reaches", mainMsg.Text, bothMsg.Text)
	}
	if strings.Contains(bothMsg.HTML, restricted) || !strings.Contains(bothMsg.Text, "\n7,731\n") {
		t.Errorf("both message: want every panel with data")
	}
}

// oneBucketHistogram answers every count panel with n logs in one bucket.
func oneBucketHistogram(n int) func(context.Context, datasource.HistogramRequest) (*datasource.HistogramResult, error) {
	return func(_ context.Context, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
		at := req.StartTime.Truncate(time.Hour).Add(time.Hour)
		return &datasource.HistogramResult{Granularity: req.Window, Data: []datasource.HistogramBucket{{Bucket: at, LogCount: n}}}, nil
	}
}

// TestSavedQueryReportReachedThroughDifferentTeams is the second R8 check:
// two users reach the saved query's source through different teams, and a
// third reaches it through none. Two messages go out.
func TestSavedQueryReportReachedThroughDifferentTeams(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	viaPayments := newTestUser(t, e.db, "pay@example.com", "Via Payments")
	viaBilling := newTestUser(t, e.db, "bill@example.com", "Via Billing")
	outsider := newTestUser(t, e.db, "out@example.com", "Outsider")
	if err := e.db.AddTeamMember(ctx, e.team.ID, viaPayments.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	billing := newTestTeam(t, e.db, "billing", models.TeamRoleMember, viaBilling)
	if err := e.db.AddTeamSource(ctx, billing.ID, e.source.ID); err != nil {
		t.Fatal(err)
	}
	admin := newTestAdmin(t, e.db, "admin@example.com")
	r := e.create()
	in := e.input()
	in.Recipients = []json.RawMessage{userRow(viaPayments), userRow(viaBilling), userRow(outsider)}
	if _, _, err := UpdateReport(ctx, e.db, access.SessionPrincipal(admin), r.ID, in); err != nil {
		t.Fatalf("an admin adds three users: %v", err)
	}

	run := e.run(r, e.owner, ReportRunToAll)
	if run.Status != models.ReportRunSent {
		t.Fatalf("run = %s/%s, deliveries %+v", run.Status, run.Reason, run.Deliveries)
	}
	sent := e.mailer.messages()
	if len(sent) != 2 || mailTo(t, sent, viaPayments) == nil || mailTo(t, sent, viaBilling) == nil {
		t.Fatalf("sent to %+v, want the two users who reach the source", sent)
	}
	if d := deliveriesByTarget(run)[target(outsider)]; d.Status != models.ReportDeliverySkipped || d.Reason != string(models.ReportReasonNoAccess) {
		t.Errorf("outsider = %+v, want skipped no_access", d)
	}
	if d := deliveriesByTarget(run)[target(e.owner)]; d.Target != "" {
		t.Errorf("the owner is no longer a recipient but got delivery %+v", d)
	}
}

func TestReportRecipientSaveRules(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	owner := access.SessionPrincipal(e.owner)
	teammate := newTestUser(t, e.db, "mate@example.com", "Mate")
	if err := e.db.AddTeamMember(ctx, e.team.ID, teammate.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	stranger := newTestUser(t, e.db, "stranger@example.com", "Stranger")
	otherTeam := newTestTeam(t, e.db, "other", models.TeamRoleMember, stranger)
	admin := newTestAdmin(t, e.db, "admin@example.com")

	saved, err := CreateReport(ctx, e.db, owner, e.input())
	if err != nil {
		t.Fatal(err)
	}
	if rows := saved.Recipients; len(rows) != 1 || rows[0].Kind != models.ReportRecipientUser || *rows[0].UserID != e.owner.ID || *rows[0].AddedBy != e.owner.ID {
		t.Errorf("no recipients stores %+v, want the owner as one user row", rows)
	}
	id := saved.Report.ID

	many := make([]json.RawMessage, maxReportRecipientRows+1)
	for i := range many {
		many[i] = json.RawMessage(fmt.Sprintf(`{"kind":"user","user_id":%d}`, 1000+i))
	}
	for name, rows := range map[string][]json.RawMessage{
		"a user outside the owner's teams": {userRow(stranger)},
		"a team the owner is not in":       {teamRow(otherTeam)},
		"an email address":                 {json.RawMessage(`{"kind":"email","email":"x@example.com"}`)},
		"a row listed twice":               {userRow(teammate), userRow(teammate)},
		"a user row with a team id":        {json.RawMessage(fmt.Sprintf(`{"kind":"user","user_id":%d,"team_id":%d}`, teammate.ID, e.team.ID))},
		"an unknown field":                 {json.RawMessage(fmt.Sprintf(`{"kind":"user","user_id":%d,"role":"x"}`, teammate.ID))},
		"more than 50 rows":                many,
	} {
		in := e.input()
		in.Recipients = rows
		if _, _, err := UpdateReport(ctx, e.db, owner, id, in); !errors.Is(err, ErrInvalidReport) {
			t.Errorf("%s: err = %v, want ErrInvalidReport", name, err)
		}
	}

	in := e.input()
	in.Recipients = []json.RawMessage{userRow(teammate), teamRow(e.team)}
	if _, _, err := UpdateReport(ctx, e.db, owner, id, in); err != nil {
		t.Fatalf("the owner adds a teammate and their own team: %v", err)
	}
	in.Recipients = append(in.Recipients, userRow(stranger))
	if _, _, err := UpdateReport(ctx, e.db, access.SessionPrincipal(admin), id, in); err != nil {
		t.Fatalf("an admin adds a stranger: %v", err)
	}
	// A row the report holds stays when the owner saves, whoever added it.
	before, after, err := UpdateReport(ctx, e.db, owner, id, in)
	if err != nil {
		t.Fatalf("the owner saves with the admin's row: %v", err)
	}
	if len(before.Recipients) != 3 || len(after.Recipients) != 3 || *after.Recipients[2].AddedBy != admin.ID {
		t.Errorf("after = %+v, want three rows with the stranger added by the admin", after.Recipients)
	}
}

func TestReportRecipientViewAndRemoval(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	direct := newTestUser(t, e.db, "direct@example.com", "Direct")
	member := newTestUser(t, e.db, "member@example.com", "Member")
	outsider := newTestUser(t, e.db, "out@example.com", "Outsider")
	for _, u := range []*models.User{direct, member} {
		if err := e.db.AddTeamMember(ctx, e.team.ID, u.ID, models.TeamRoleMember); err != nil {
			t.Fatal(err)
		}
	}
	in := e.input()
	in.Variables = map[string]any{"day": "yesterday"}
	in.Recipients = []json.RawMessage{userRow(direct), teamRow(e.team)}
	saved, err := CreateReport(ctx, e.db, access.SessionPrincipal(e.owner), in)
	if err != nil {
		t.Fatal(err)
	}
	id := saved.Report.ID
	deps := ReportDeps{DB: e.db}

	r, full, err := GetReportForViewer(ctx, e.db, access.SessionPrincipal(member), id)
	if err != nil || full {
		t.Fatalf("a team recipient: full = %v, err = %v; want the reduced view", full, err)
	}
	resp, err := BuildReportResponse(ctx, deps, access.SessionPrincipal(member), r)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "Daily 5xx" || resp.Owner == nil || resp.Source.Name != e.sq.Name || resp.Timezone != "Asia/Kolkata" || resp.Schedule.Frequency != models.ReportDaily {
		t.Errorf("reduced view lacks name, owner, source name, schedule or timezone: %+v", resp)
	}
	if resp.Recipients != nil || resp.Variables != nil || resp.LastRun != nil || resp.TimeRange != "" || resp.Source.SavedQueryID != nil || resp.CanEdit {
		t.Errorf("reduced view shows editor fields: %+v", resp)
	}
	if _, _, err := GetReportForViewer(ctx, e.db, access.SessionPrincipal(outsider), id); !errors.Is(err, ErrReportNotFound) {
		t.Errorf("an outsider: err = %v, want ErrReportNotFound", err)
	}
	ownerResp, err := BuildReportResponse(ctx, deps, access.SessionPrincipal(e.owner), saved.Report)
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerResp.Recipients) != 2 || ownerResp.Recipients[0].Name != "Direct" || ownerResp.Recipients[1].Name != e.team.Name {
		t.Errorf("owner's recipients = %+v, want named rows", ownerResp.Recipients)
	}

	received, err := ListReports(ctx, deps, access.SessionPrincipal(member), ReportScopeReceived)
	if err != nil || len(received) != 1 || received[0].ID != id || received[0].Variables != nil {
		t.Errorf("received list = %+v, %v; want the report in its reduced view", received, err)
	}

	if _, err := RemoveMyReportRecipient(ctx, e.db, access.SessionPrincipal(member), id); !errors.Is(err, ErrInvalidReport) {
		t.Errorf("a team recipient removes themself: err = %v, want ErrInvalidReport naming the team", err)
	}
	if _, err := RemoveMyReportRecipient(ctx, e.db, access.SessionPrincipal(outsider), id); !errors.Is(err, ErrReportNotFound) {
		t.Errorf("an outsider removes themself: err = %v, want ErrReportNotFound", err)
	}
	if _, err := RemoveMyReportRecipient(ctx, e.db, access.SessionPrincipal(direct), id); err != nil {
		t.Fatalf("a direct recipient removes themself: %v", err)
	}
	if _, err := RemoveMyReportRecipient(ctx, e.db, access.SessionPrincipal(direct), id); !errors.Is(err, ErrInvalidReport) {
		t.Errorf("a second removal by a team member: err = %v, want ErrInvalidReport", err)
	}
	// Direct is still in the team, so the report still reaches them.
	if _, full, err := GetReportForViewer(ctx, e.db, access.SessionPrincipal(direct), id); err != nil || full {
		t.Errorf("after removal through a team: full = %v, err = %v", full, err)
	}
}

func TestReportRecipientCap(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	for i := range reports.MaxResolvedRecipients + 1 {
		u := newTestUser(t, e.db, fmt.Sprintf("member%03d@example.com", i), "")
		if err := e.db.AddTeamMember(ctx, e.team.ID, u.ID, models.TeamRoleMember); err != nil {
			t.Fatal(err)
		}
	}
	in := e.input()
	in.Recipients = []json.RawMessage{teamRow(e.team)}
	saved, err := CreateReport(ctx, e.db, access.SessionPrincipal(e.owner), in)
	if err != nil {
		t.Fatal(err)
	}
	run := e.run(saved.Report, e.owner, ReportRunToAll)
	if n := len(e.mailer.messages()); n != reports.MaxResolvedRecipients {
		t.Errorf("sent %d messages, want %d", n, reports.MaxResolvedRecipients)
	}
	// The owner and 201 members: the two highest user ids are left out.
	var limited []string
	for _, d := range run.Deliveries {
		if d.Reason == reports.SkipRecipientLimit {
			limited = append(limited, d.Target)
		}
	}
	if len(run.Deliveries) != reports.MaxResolvedRecipients+2 || len(limited) != 2 {
		t.Fatalf("deliveries %d, limited %v", len(run.Deliveries), limited)
	}
	last := run.Deliveries[len(run.Deliveries)-1]
	firstID, _ := strconv.Atoi(run.Deliveries[0].Target)
	lastID, _ := strconv.Atoi(last.Target)
	if last.Reason != reports.SkipRecipientLimit || lastID <= firstID {
		t.Errorf("the cap kept the wrong recipients: first %+v, last %+v", run.Deliveries[0], last)
	}
}

// TestOwnerLockedPanelNamesNoSource: a panel the owner can no longer reach
// is locked for everyone, and no recipient's footer names its source.
func TestOwnerLockedPanelNamesNoSource(t *testing.T) {
	e := newDashboardReportEnv(t)
	ctx := context.Background()
	both := newTestUser(t, e.db, "both@example.com", "Both")
	for _, team := range []*models.Team{e.team, e.team2} {
		if err := e.db.AddTeamMember(ctx, team.ID, both.ID, models.TeamRoleMember); err != nil {
			t.Fatal(err)
		}
	}
	in := e.input("")
	in.Recipients = []json.RawMessage{userRow(both)}
	saved, err := CreateReport(ctx, e.db, access.SessionPrincipal(e.owner), in)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.db.RemoveTeamMember(ctx, e.team2.ID, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	run := e.run(saved.Report, e.owner, ReportRunToAll)
	if run.Status != models.ReportRunPartial || run.Reason != models.ReportReasonPanelLocked {
		t.Fatalf("run = %s/%s, want partial/panel_locked", run.Status, run.Reason)
	}
	msg := mailTo(t, e.mailer.messages(), both)
	if msg == nil {
		t.Fatal("the recipient got no message")
	}
	if !strings.Contains(msg.Text, "The report owner no longer has access to this panel.") {
		t.Errorf("the owner-locked panel lacks its placeholder:\n%s", msg.Text)
	}
	if strings.Contains(msg.Text, "other_logs") || strings.Contains(msg.HTML, "other_logs") || !strings.Contains(msg.Text, "Sources: app_logs.") {
		t.Errorf("the footer names the owner-locked panel's source:\n%s", msg.Text)
	}
}

// lockedKindsBlob has one visible stat on the first team and a stat, a
// table, a count chart, a toplist and a pie on the second team. Every value
// on the second team is one the test can search the email for.
const lockedKindsBlob = `{"version":2,"features":[],"default_time_range":"6h",
"tabs":[{"id":"main","title":"Main","layout":[
  {"id":"visible","x":0,"y":0,"w":6,"h":2},
  {"id":"stat","x":6,"y":0,"w":6,"h":2},
  {"id":"table","x":0,"y":2,"w":6,"h":2},
  {"id":"chart","x":6,"y":2,"w":6,"h":2},
  {"id":"toplist","x":0,"y":4,"w":6,"h":2},
  {"id":"pie","x":6,"y":4,"w":6,"h":2}]}],
"panels":[
 {"id":"visible","kind":"query","title":"Visible stat","team_id":%[1]d,"source_id":%[2]d,
  "query":{"mode":"result","language":"clickhouse-sql","text":"SELECT 1 AS n FROM visible"},
  "viz":{"type":"stat","value":"n","reduce":"sum"}},
 {"id":"stat","kind":"query","title":"Hidden stat","team_id":%[3]d,"source_id":%[4]d,
  "query":{"mode":"result","language":"clickhouse-sql","text":"SELECT 4242 AS n FROM hidden_stat"},
  "viz":{"type":"stat","value":"n","reduce":"sum"}},
 {"id":"table","kind":"query","title":"Hidden table","team_id":%[3]d,"source_id":%[4]d,
  "query":{"mode":"result","language":"clickhouse-sql","text":"SELECT host FROM hidden_table"},
  "viz":{"type":"table"}},
 {"id":"chart","kind":"query","title":"Hidden chart","team_id":%[3]d,"source_id":%[4]d,
  "query":{"mode":"count","language":"clickhouse-sql","text":"SELECT * FROM other_logs","group_by":"host"},
  "viz":{"type":"chart","mark":"bar","stack":"none"}},
 {"id":"toplist","kind":"query","title":"Hidden toplist","team_id":%[3]d,"source_id":%[4]d,
  "query":{"mode":"count","language":"clickhouse-sql","text":"SELECT * FROM other_logs","group_by":"host"},
  "viz":{"type":"toplist"}},
 {"id":"pie","kind":"query","title":"Hidden pie","team_id":%[3]d,"source_id":%[4]d,
  "query":{"mode":"count","language":"clickhouse-sql","text":"SELECT * FROM other_logs","group_by":"host"},
  "viz":{"type":"pie"}}]}`

// TestRecipientLockHidesEveryPanelKind runs a dashboard with each panel
// kind on a team the recipient is not in. The owner's copy shows every
// value; the recipient's copy, in HTML and in text, shows each title with
// the locked line and no value of any of them.
func TestRecipientLockHidesEveryPanelKind(t *testing.T) {
	e := newDashboardReportEnv(t)
	ctx := context.Background()
	mainOnly := newTestUser(t, e.db, "main@example.com", "Main Only")
	if err := e.db.AddTeamMember(ctx, e.team.ID, mainOnly.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	blob := fmt.Sprintf(lockedKindsBlob, e.team.ID, e.source.ID, e.team2.ID, e.source2.ID)
	d, err := CreateDashboard(ctx, e.db, discardLogger(), e.owner, &models.CreateDashboardRequest{Name: "Kinds", Panels: json.RawMessage(blob)})
	if err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}
	e.query = func(_ context.Context, req datasource.QueryRequest) (*models.QueryResult, error) {
		switch {
		case strings.Contains(req.RawQuery, "visible"):
			return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "n", Type: "UInt64"}}, Logs: []map[string]any{{"n": 1}}}, nil
		case strings.Contains(req.RawQuery, "hidden_stat"):
			return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "n", Type: "UInt64"}}, Logs: []map[string]any{{"n": 4242}}}, nil
		case strings.Contains(req.RawQuery, "hidden_table"):
			return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "host", Type: "String"}}, Logs: []map[string]any{{"host": "secret-host-9"}}}, nil
		}
		return nil, fmt.Errorf("unexpected query %q", req.RawQuery)
	}
	e.histogram = func(_ context.Context, req datasource.HistogramRequest) (*datasource.HistogramResult, error) {
		at := req.StartTime.Truncate(time.Hour).Add(time.Hour)
		return &datasource.HistogramResult{Granularity: req.Window, Data: []datasource.HistogramBucket{{Bucket: at, LogCount: 6161, GroupValue: "secret-group"}}}, nil
	}
	in := &ReportInput{
		Name:       "Kinds daily",
		Source:     ReportSourceRef{Kind: models.ReportSourceDashboard, DashboardID: &d.ID},
		Schedule:   json.RawMessage(`{"frequency":"daily","hour":9,"minute":0}`),
		Timezone:   "UTC",
		Recipients: []json.RawMessage{userRow(e.owner), userRow(mainOnly)},
	}
	saved, err := CreateReport(ctx, e.db, access.SessionPrincipal(e.owner), in)
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}
	run := e.run(saved.Report, e.owner, ReportRunToAll)
	if run.Status != models.ReportRunSent {
		t.Fatalf("run = %s/%s, panels %+v", run.Status, run.Reason, run.Panels)
	}
	if got := panelStatuses(run); got != "visible:ok stat:ok table:ok chart:ok toplist:ok pie:ok" {
		t.Fatalf("panels = %q, want every kind to run", got)
	}
	sent := e.mailer.messages()
	ownerMsg, mainMsg := mailTo(t, sent, e.owner), mailTo(t, sent, mainOnly)
	if ownerMsg == nil || mainMsg == nil {
		t.Fatalf("sent %d messages, want one each to the owner and the recipient", len(sent))
	}
	values := []string{"4,242", "secret-host-9", "6,161", "secret-group", "100.0%"}
	for _, v := range values {
		if !strings.Contains(ownerMsg.Text, v) || !strings.Contains(ownerMsg.HTML, v) {
			t.Errorf("the owner's copy lacks %q, so the check below proves nothing", v)
		}
	}
	const locked = "You do not have access to this panel."
	for body, text := range map[string]string{"html": mainMsg.HTML, "text": mainMsg.Text} {
		for _, v := range append(values, "other_logs") {
			if strings.Contains(text, v) {
				t.Errorf("recipient %s shows %q of a locked panel", body, v)
			}
		}
		if n := strings.Count(text, locked); n != 5 {
			t.Errorf("recipient %s has %d locked lines, want 5", body, n)
		}
		for _, title := range []string{"Hidden stat", "Hidden table", "Hidden chart", "Hidden toplist", "Hidden pie"} {
			if !strings.Contains(text, title) {
				t.Errorf("recipient %s lacks the title %q", body, title)
			}
		}
	}
	for _, title := range []string{"Hidden stat", "Hidden table", "Hidden chart", "Hidden toplist", "Hidden pie"} {
		if !strings.Contains(mainMsg.Text, "== "+title+" ==\n"+locked+"\n") {
			t.Errorf("recipient text: %q is not followed by the locked line", title)
		}
	}
	if d := deliveriesByTarget(run)[target(mainOnly)]; strings.Join(d.LockedPanels, ",") != "stat,table,chart,toplist,pie" {
		t.Errorf("recipient delivery locked %v, want every panel on the second team", d.LockedPanels)
	}
}
