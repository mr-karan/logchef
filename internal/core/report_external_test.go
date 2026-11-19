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
	"github.com/mr-karan/logchef/internal/reports"
	"github.com/mr-karan/logchef/pkg/models"
)

func emailRow(addr string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"kind":"email","email":%q}`, addr))
}

// setExternal stores both external-recipient settings.
func setExternal(t *testing.T, e *reportEnv, enabled bool, domains string) {
	t.Helper()
	ctx := context.Background()
	if err := e.db.UpsertSetting(ctx, SettingReportsExternalRecipients, fmt.Sprint(enabled), "boolean", "reports", "", false); err != nil {
		t.Fatal(err)
	}
	if err := e.db.UpsertSetting(ctx, SettingReportsAllowedEmailDomains, domains, "string", "reports", "", false); err != nil {
		t.Fatal(err)
	}
}

func mailToAddress(sent []sentMail, addr string) *reports.MailMessage {
	for i := range sent {
		if sent[i].to == addr {
			return &sent[i].msg
		}
	}
	return nil
}

func TestExternalRecipientSaveRules(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	owner := access.SessionPrincipal(e.owner)
	saved, err := CreateReport(ctx, e.db, owner, e.input())
	if err != nil {
		t.Fatal(err)
	}
	id := saved.Report.ID
	save := func(p access.Principal, rows ...json.RawMessage) (*SavedReport, error) {
		in := e.input()
		in.Recipients = rows
		_, after, err := UpdateReport(ctx, e.db, p, id, in)
		return after, err
	}
	refused := func(name, want string, rows ...json.RawMessage) {
		t.Helper()
		_, err := save(owner, rows...)
		if !errors.Is(err, ErrInvalidReport) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want ErrInvalidReport naming %q", name, err, want)
		}
	}

	// No setting rows at all is the default: off.
	refused("no settings", "external email recipients are off", emailRow("oncall@example.com"))
	setExternal(t, e, false, "*")
	refused("setting off", "external email recipients are off", emailRow("oncall@example.com"))
	setExternal(t, e, true, "")
	refused("on with no domain", "domain is not one", emailRow("oncall@example.com"))
	setExternal(t, e, true, "example.com")
	refused("domain outside the list", "domain is not one", emailRow("oncall@example.org"))
	refused("subdomain of a listed domain", "domain is not one", emailRow("oncall@mail.example.com"))
	refused("a display name", "not a plain email address", emailRow("On Call <oncall@example.com>"))
	refused("an address listed twice", "listed twice", emailRow("oncall@example.com"), emailRow("ONCALL@example.com"))
	in := e.input()
	in.Recipients = []json.RawMessage{json.RawMessage(`{"kind":"email","email":"a@example.com","user_id":1}`)}
	if _, _, err := UpdateReport(ctx, e.db, owner, id, in); !errors.Is(err, ErrInvalidReport) {
		t.Errorf("an email entry with a user id: err = %v, want ErrInvalidReport", err)
	}

	after, err := save(owner, emailRow("oncall@Example.com"))
	if err != nil {
		t.Fatalf("an allowed address: %v", err)
	}
	if r := after.Recipients; len(r) != 1 || r[0].Kind != models.ReportRecipientEmail || r[0].Email != "oncall@example.com" || *r[0].AddedBy != e.owner.ID {
		t.Errorf("stored = %+v, want one email row with the domain in lower case", r)
	}

	// Turning the setting off keeps the rows a report holds; a save that
	// adds a new address is refused.
	setExternal(t, e, false, "example.com")
	if _, err := save(owner, emailRow("oncall@example.com")); err != nil {
		t.Errorf("a held address after the setting went off: %v", err)
	}
	refused("a new address after the setting went off", "external email recipients are off", emailRow("oncall@example.com"), emailRow("ops@example.com"))
}

// TestKnownAddressIsStoredAsUser is the save-time conversion: an email row
// whose address belongs to a user, in any case, is stored as that user's
// row, whatever the external settings say when the caller may add the
// user, and only when they allow the address otherwise.
func TestKnownAddressIsStoredAsUser(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	owner := access.SessionPrincipal(e.owner)
	mate := newTestUser(t, e.db, "mate@example.com", "Mate")
	if err := e.db.AddTeamMember(ctx, e.team.ID, mate.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	stranger := newTestUser(t, e.db, "stranger@example.com", "Stranger")
	newTestTeam(t, e.db, "elsewhere", models.TeamRoleMember, stranger)
	saved, err := CreateReport(ctx, e.db, owner, e.input())
	if err != nil {
		t.Fatal(err)
	}
	save := func(rows ...json.RawMessage) (*SavedReport, error) {
		in := e.input()
		in.Recipients = rows
		_, after, err := UpdateReport(ctx, e.db, owner, saved.Report.ID, in)
		return after, err
	}

	after, err := save(emailRow("MATE@example.com"))
	if err != nil {
		t.Fatalf("a teammate's address with external recipients off: %v", err)
	}
	if r := after.Recipients; len(r) != 1 || r[0].Kind != models.ReportRecipientUser || *r[0].UserID != mate.ID || r[0].Email != "" {
		t.Errorf("stored = %+v, want the teammate's user row", r)
	}
	if _, err := save(userRow(mate), emailRow("mate@example.com")); !errors.Is(err, ErrInvalidReport) {
		t.Errorf("a user row and that user's address: err = %v, want ErrInvalidReport", err)
	}
	// A user the owner may not add reads like any other external address.
	if _, err := save(emailRow("stranger@example.com")); !errors.Is(err, ErrInvalidReport) || !strings.Contains(err.Error(), "external email recipients are off") {
		t.Errorf("a stranger's address with external recipients off: err = %v", err)
	}
	setExternal(t, e, true, "example.com")
	after, err = save(emailRow("stranger@example.com"))
	if err != nil {
		t.Fatalf("a stranger's address in an allowed domain: %v", err)
	}
	if r := after.Recipients; len(r) != 1 || r[0].Kind != models.ReportRecipientUser || *r[0].UserID != stranger.ID {
		t.Errorf("stored = %+v, want the stranger's user row", r)
	}
}

// TestExternalRecipientDelivery sends a saved-query report to an allowed
// external address, then turns the setting off and narrows the domains:
// the next runs skip the address with the reason.
func TestExternalRecipientDelivery(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	e.rr.MaxAttachmentBytes = 1 << 20
	setExternal(t, e, true, "example.com, partner.example")
	in := e.input()
	in.AttachCSV = true
	in.Recipients = []json.RawMessage{userRow(e.owner), emailRow("oncall@partner.example")}
	saved, err := CreateReport(ctx, e.db, access.SessionPrincipal(e.owner), in)
	if err != nil {
		t.Fatal(err)
	}

	run := e.run(saved.Report, e.owner, ReportRunToAll)
	if run.Status != models.ReportRunSent {
		t.Fatalf("run = %s/%s, deliveries %+v", run.Status, run.Reason, run.Deliveries)
	}
	d := deliveriesByTarget(run)["oncall@partner.example"]
	if d.Class != models.ReportDeliveryExternal || d.Via != "email" || d.Status != models.ReportDeliverySent || len(d.LockedPanels) != 0 {
		t.Errorf("external delivery = %+v, want sent to the external class through the email row", d)
	}
	msg := mailToAddress(e.mailer.messages(), "oncall@partner.example")
	if msg == nil || !strings.Contains(msg.HTML, "503") || len(msg.Attachments) != 1 {
		t.Fatalf("the external address got %+v, want the owner's panel and its file", msg)
	}

	setExternal(t, e, true, "example.com")
	run = e.run(saved.Report, e.owner, ReportRunToAll)
	if d := deliveriesByTarget(run)["oncall@partner.example"]; d.Status != models.ReportDeliverySkipped || d.Reason != reports.SkipDomainNotAllowed {
		t.Errorf("after the domain left the list = %+v, want skipped %s", d, reports.SkipDomainNotAllowed)
	}
	setExternal(t, e, false, "*")
	sentBefore := len(e.mailer.messages())
	run = e.run(saved.Report, e.owner, ReportRunToAll)
	if d := deliveriesByTarget(run)["oncall@partner.example"]; d.Status != models.ReportDeliverySkipped || d.Reason != reports.SkipExternalDisabled || d.Class != models.ReportDeliveryExternal {
		t.Errorf("after the setting went off = %+v, want skipped %s", d, reports.SkipExternalDisabled)
	}
	if run.Status != models.ReportRunSent || len(e.mailer.messages()) != sentBefore+1 {
		t.Errorf("run = %s/%s with %d new messages, want sent to the owner alone", run.Status, run.Reason, len(e.mailer.messages())-sentBefore)
	}
}

// TestExternalAddressThatBecomesAUser: an address added as external that
// later belongs to a user with no team is that user at send time, so the
// next run sends them no panel.
func TestExternalAddressThatBecomesAUser(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	setExternal(t, e, true, "*")
	in := e.input()
	in.AttachCSV = true
	in.Recipients = []json.RawMessage{userRow(e.owner), emailRow("Late@Partner.example")}
	saved, err := CreateReport(ctx, e.db, access.SessionPrincipal(e.owner), in)
	if err != nil {
		t.Fatal(err)
	}
	late := newTestUser(t, e.db, "late@partner.example", "Late")

	run := e.run(saved.Report, e.owner, ReportRunToAll)
	if mailTo(t, e.mailer.messages(), late) != nil {
		t.Fatal("the user behind the address got a message")
	}
	d := deliveriesByTarget(run)[target(late)]
	if d.Class != models.ReportDeliveryUser || d.Via != "email" || d.Status != models.ReportDeliverySkipped || d.Reason != string(models.ReportReasonNoAccess) {
		t.Errorf("delivery = %+v, want the user class through the email row, skipped no_access", d)
	}
	if _, ok := deliveriesByTarget(run)["Late@partner.example"]; ok || len(run.Deliveries) != 2 {
		t.Errorf("deliveries = %+v, want the owner and the user, no external", run.Deliveries)
	}
	// The user sees the report as sent to them and can leave it.
	if _, full, err := GetReportForViewer(ctx, e.db, access.SessionPrincipal(late), saved.Report.ID); err != nil || full {
		t.Errorf("the user reads the report: full = %v, err = %v", full, err)
	}
	if _, err := RemoveMyReportRecipient(ctx, e.db, access.SessionPrincipal(late), saved.Report.ID); err != nil {
		t.Errorf("the user leaves the report: %v", err)
	}
	if rows, _ := e.db.ListReportRecipients(ctx, saved.Report.ID); len(rows) != 1 || rows[0].Kind != models.ReportRecipientUser {
		t.Errorf("rows after leaving = %+v, want the owner's alone", rows)
	}
}

// TestExternalRecipientNeverSeesOwnerLockedPanels runs a dashboard with
// every panel kind on a second team. While the owner is in that team an
// external address receives every panel. Once the owner leaves it, the
// external copy, in HTML and in text, shows each of those panels as its
// title with the owner-locked line and no value, and its footer names no
// source of theirs.
func TestExternalRecipientNeverSeesOwnerLockedPanels(t *testing.T) {
	e := newDashboardReportEnv(t)
	ctx := context.Background()
	setExternal(t, e.reportEnv, true, "partner.example")
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
	const addr = "auditor@partner.example"
	saved, err := CreateReport(ctx, e.db, access.SessionPrincipal(e.owner), &ReportInput{
		Name:       "Kinds daily",
		Source:     ReportSourceRef{Kind: models.ReportSourceDashboard, DashboardID: &d.ID},
		Schedule:   json.RawMessage(`{"frequency":"daily","hour":9,"minute":0}`),
		Timezone:   "UTC",
		Recipients: []json.RawMessage{emailRow(addr)},
	})
	if err != nil {
		t.Fatalf("CreateReport: %v", err)
	}
	values := []string{"4,242", "secret-host-9", "6,161", "secret-group", "100.0%"}
	titles := []string{"Hidden stat", "Hidden table", "Hidden chart", "Hidden toplist", "Hidden pie"}

	run := e.run(saved.Report, e.owner, ReportRunToAll)
	full := mailToAddress(e.mailer.messages(), addr)
	if run.Status != models.ReportRunSent || full == nil {
		t.Fatalf("run = %s/%s, deliveries %+v; want the external address to receive the report", run.Status, run.Reason, run.Deliveries)
	}
	for _, v := range append(values, "other_logs") {
		if !strings.Contains(full.Text, v) || !strings.Contains(full.HTML, v) {
			t.Errorf("while the owner reaches every panel, the external copy lacks %q", v)
		}
	}

	if err := e.db.RemoveTeamMember(ctx, e.team2.ID, e.owner.ID); err != nil {
		t.Fatal(err)
	}
	before := len(e.mailer.messages())
	run = e.run(saved.Report, e.owner, ReportRunToAll)
	if run.Status != models.ReportRunPartial || run.Reason != models.ReportReasonPanelLocked {
		t.Fatalf("run = %s/%s, want partial/panel_locked", run.Status, run.Reason)
	}
	sent := e.mailer.messages()[before:]
	msg := mailToAddress(sent, addr)
	if msg == nil {
		t.Fatal("the external address got no message for the panel the owner still reaches")
	}
	const locked = "The report owner no longer has access to this panel."
	for body, text := range map[string]string{"html": msg.HTML, "text": msg.Text} {
		for _, v := range append(values, "other_logs") {
			if strings.Contains(text, v) {
				t.Errorf("external %s shows %q of a panel the owner cannot reach", body, v)
			}
		}
		if n := strings.Count(text, locked); n != 5 {
			t.Errorf("external %s has %d owner-locked lines, want 5", body, n)
		}
		for _, title := range titles {
			if !strings.Contains(text, title) {
				t.Errorf("external %s lacks the title %q", body, title)
			}
		}
	}
	for _, title := range titles {
		if !strings.Contains(msg.Text, "== "+title+" ==\n"+locked+"\n") {
			t.Errorf("external text: %q is not followed by the owner-locked line", title)
		}
	}
	if !strings.Contains(msg.Text, "\n1\n") || !strings.Contains(msg.Text, "Sources: app_logs.") {
		t.Errorf("external text lacks the panel the owner reaches, or names another source:\n%s", msg.Text)
	}
	if d := deliveriesByTarget(run)[addr]; d.Status != models.ReportDeliverySent || d.Class != models.ReportDeliveryExternal || len(d.LockedPanels) != 0 {
		t.Errorf("external delivery = %+v, want sent with no recipient lock of its own", d)
	}
}
