package provisioning

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func testRecorder(db store.AuditStore) *audit.Recorder {
	return audit.New(audit.Options{Store: db, Logger: quietLogger(), LogEvents: true})
}

// lineRecorder records into db and writes its audit log lines to the
// returned buffer.
func lineRecorder(db store.AuditStore) (*audit.Recorder, *bytes.Buffer) {
	var lines bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&lines, nil))
	return audit.New(audit.Options{Store: db, Logger: log, LogEvents: true}), &lines
}

func auditLines(buf *bytes.Buffer) []string {
	var out []string
	for line := range strings.SplitSeq(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, `"msg":"audit"`) {
			out = append(out, line)
		}
	}
	return out
}

func storedEvents(t *testing.T, db *sqlite.DB) []models.AuditEvent {
	t.Helper()
	events, err := db.ListAuditEvents(context.Background(), models.AuditFilter{Ascending: true, Limit: 100})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	return events
}

type change struct {
	From any `json:"from"`
	To   any `json:"to"`
}

func changesOf(t *testing.T, e models.AuditEvent) map[string]change {
	t.Helper()
	var c map[string]change
	if err := json.Unmarshal(e.Changes, &c); err != nil {
		t.Fatalf("%s changes %s: %v", e.Action, e.Changes, err)
	}
	return c
}

func itoa[T ~int | ~int64](v T) string { return strconv.FormatInt(int64(v), 10) }

func seedSource(t *testing.T, db *sqlite.DB) *models.Source {
	t.Helper()
	src := &models.Source{
		Name:        "src1",
		Connection:  models.ConnectionInfo{Host: "localhost:9000", Database: "default", TableName: "logs"},
		MetaTSField: "timestamp",
	}
	if err := db.CreateSource(context.Background(), src); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	return src
}

func teamLinkConfig(sources ...string) *config.ProvisioningConfig {
	return &config.ProvisioningConfig{
		ManageTeams: true,
		Teams:       []config.ProvisionTeam{{Name: "team1", Description: "Team One", Sources: sources}},
	}
}

func TestReconcileAuditsCommittedChangesWithOneRunID(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()
	src := seedSource(t, db)
	rec, lines := lineRecorder(db)

	if err := Reconcile(ctx, teamLinkConfig("src1"), db, nil, rec, quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	team, err := db.GetTeamByName(ctx, "team1")
	if err != nil {
		t.Fatal(err)
	}

	events := storedEvents(t, db)
	if len(events) != 2 {
		t.Fatalf("stored %d events, want team.create and team_source.link: %+v", len(events), events)
	}
	create, link := events[0], events[1]
	if create.Action != string(audit.TeamCreate) || link.Action != string(audit.TeamSourceLink) {
		t.Fatalf("actions = %q, %q; want team.create, team_source.link", create.Action, link.Action)
	}
	for _, e := range events {
		if e.ActorType != models.AuditActorSystem || e.AuthMethod != models.AuditAuthProvisioning || e.ActorID != nil {
			t.Errorf("%s actor = %s/%s/%v, want system/provisioning without a user", e.Action, e.ActorType, e.AuthMethod, e.ActorID)
		}
		if e.Outcome != models.AuditOutcomeSuccess || e.IP != "" || e.Route != "" {
			t.Errorf("%s outcome %q ip %q route %q, want a success outside HTTP", e.Action, e.Outcome, e.IP, e.Route)
		}
	}
	if create.CredentialID == "" || create.CredentialID != link.CredentialID {
		t.Errorf("run IDs = %q and %q, want one run ID for both", create.CredentialID, link.CredentialID)
	}
	if create.TargetType != "team" || create.TargetID != itoa(team.ID) || create.TargetLabel != "team1" {
		t.Errorf("team.create target = %s %s %q", create.TargetType, create.TargetID, create.TargetLabel)
	}
	if c := changesOf(t, create); c["managed"].To != true || c["description"].To != "Team One" {
		t.Errorf("team.create changes = %+v, want the managed team with its description", c)
	}
	if link.TargetID != itoa(src.ID) || link.TeamID == nil || *link.TeamID != team.ID || link.SourceID == nil || *link.SourceID != src.ID {
		t.Errorf("team_source.link target = %s team %v source %v", link.TargetID, link.TeamID, link.SourceID)
	}

	got := auditLines(lines)
	if len(got) != 2 {
		t.Fatalf("wrote %d audit log lines, want 2: %s", len(got), lines)
	}
	for _, line := range got {
		if !strings.Contains(line, `"stored":true`) || !strings.Contains(line, create.CredentialID) {
			t.Errorf("audit line %s does not say stored true with the run ID", line)
		}
	}

	// A second run with a different config is a new run ID.
	if err := Reconcile(ctx, teamLinkConfig(), db, nil, rec, quietLogger(), nil); err != nil {
		t.Fatal(err)
	}
	cfg := teamLinkConfig()
	cfg.Prune = true
	if err := Reconcile(ctx, cfg, db, nil, rec, quietLogger(), nil); err != nil {
		t.Fatal(err)
	}
	events = storedEvents(t, db)
	unlink := events[len(events)-1]
	if unlink.Action != string(audit.TeamSourceUnlink) || unlink.CredentialID == create.CredentialID {
		t.Errorf("prune recorded %s with run ID %q, want team_source.unlink with a new run ID", unlink.Action, unlink.CredentialID)
	}
}

func TestReconcileAuditsMembersUsersAndSources(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()
	existing := &models.User{Email: "bob@example.com", FullName: "Bob", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: models.UserAccountTypeHuman}
	if err := db.CreateUser(ctx, existing); err != nil {
		t.Fatal(err)
	}
	cfg := &config.ProvisioningConfig{
		ManageSources: true,
		ManageTeams:   true,
		Prune:         true,
		Sources: []config.ProvisionSource{{
			Name:        "src1",
			MetaTSField: "timestamp",
			Connection:  map[string]any{"host": "127.0.0.1:1", "database": "default", "table_name": "logs", "password": "s3cr3t-sentinel"},
		}},
		Teams: []config.ProvisionTeam{{
			Name: "team1",
			Members: []config.ProvisionMember{
				{Email: "alice@example.com", Role: "admin"},
				{Email: "bob@example.com", Role: "member"},
			},
		}},
	}
	rec, lines := lineRecorder(db)
	if err := Reconcile(ctx, cfg, db, newTestDatasourceService(db), rec, quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	cfg.Teams[0].Members = []config.ProvisionMember{{Email: "alice@example.com", Role: "editor"}}
	cfg.Sources[0].Description = "changed"
	if err := Reconcile(ctx, cfg, db, newTestDatasourceService(db), rec, quietLogger(), nil); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}

	var actions []string
	byAction := map[string]models.AuditEvent{}
	for _, e := range storedEvents(t, db) {
		actions = append(actions, e.Action)
		byAction[e.Action] = e
	}
	want := []string{
		"source.create", "team.create",
		"user.create", "team_member.add", // alice is new
		"user.update", "team_member.add", // bob becomes managed and joins
		"source.update",
		"team_member.add", "team_member.remove", // alice's role, bob pruned
	}
	if strings.Join(actions, ",") != strings.Join(want, ",") {
		t.Fatalf("actions = %v, want %v", actions, want)
	}
	if c := changesOf(t, byAction["user.update"]); len(c) != 1 || c["managed"].From != false || c["managed"].To != true {
		t.Errorf("user.update changes = %+v, want only managed false to true", c)
	}
	if c := changesOf(t, byAction["team_member.add"]); len(c) != 1 || c["role"].From != "admin" || c["role"].To != "editor" {
		t.Errorf("role change = %+v, want role admin to editor", c)
	}
	if c := changesOf(t, byAction["source.update"]); len(c) != 1 || c["description"].To != "changed" {
		t.Errorf("source.update changes = %+v, want only the description", c)
	}
	if strings.Contains(lines.String(), "s3cr3t-sentinel") {
		t.Error("a source password reached the audit log lines")
	}
}

func TestReconcileAuditsNothingWithoutACommittedChange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, db *sqlite.DB) *config.ProvisioningConfig
		wantErr bool
	}{
		{"dry run", func(t *testing.T, db *sqlite.DB) *config.ProvisioningConfig {
			seedSource(t, db)
			cfg := teamLinkConfig("src1")
			cfg.DryRun = true
			return cfg
		}, false},
		{"callback fails after the first event", func(t *testing.T, db *sqlite.DB) *config.ProvisioningConfig {
			// team.create is collected, then the link to an unknown source fails.
			return teamLinkConfig("missing")
		}, true},
		{"second run with the same config", func(t *testing.T, db *sqlite.DB) *config.ProvisioningConfig {
			seedSource(t, db)
			cfg := teamLinkConfig("src1")
			if err := Reconcile(context.Background(), cfg, db, nil, testRecorder(db), quietLogger(), nil); err != nil {
				t.Fatal(err)
			}
			return cfg
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newReconcileTestDB(t)
			cfg := tc.prepare(t, db)
			before := len(storedEvents(t, db))
			rec, lines := lineRecorder(db)
			err := Reconcile(context.Background(), cfg, db, nil, rec, quietLogger(), nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Reconcile error = %v, want error %v", err, tc.wantErr)
			}
			if after := len(storedEvents(t, db)); after != before {
				t.Errorf("stored %d new events, want none", after-before)
			}
			if got := auditLines(lines); len(got) != 0 {
				t.Errorf("wrote audit log lines %v, want none", got)
			}
		})
	}
}

type failingAuditStore struct{ store.AuditStore }

func (failingAuditStore) InsertAuditEvent(context.Context, *models.AuditEvent) error {
	return errors.New("audit table unavailable")
}

func TestReconcileCommitsWhenAuditInsertFails(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()
	seedSource(t, db)
	rec, lines := lineRecorder(failingAuditStore{db})

	if err := Reconcile(ctx, teamLinkConfig("src1"), db, nil, rec, quietLogger(), nil); err != nil {
		t.Fatalf("Reconcile with a failing audit store: %v", err)
	}
	team, err := db.GetTeamByName(ctx, "team1")
	if err != nil {
		t.Fatalf("the reconcile did not commit: %v", err)
	}
	if sources, err := db.ListTeamSources(ctx, team.ID); err != nil || len(sources) != 1 {
		t.Fatalf("team sources = %v, %v; want the committed link", sources, err)
	}
	got := auditLines(lines)
	if len(got) != 2 {
		t.Fatalf("wrote %d audit log lines, want 2", len(got))
	}
	for _, line := range got {
		if !strings.Contains(line, `"stored":false`) {
			t.Errorf("audit line %s does not say stored false", line)
		}
	}
}

func TestReconcileAuditsAdoptionAsAnUpdate(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()
	if err := db.CreateTeam(ctx, &models.Team{Name: "team1", Description: "old"}); err != nil {
		t.Fatal(err)
	}
	cfg := teamLinkConfig()
	cfg.Teams[0].Description = "new"
	if err := Reconcile(ctx, cfg, db, nil, testRecorder(db), quietLogger(), nil); err != nil {
		t.Fatal(err)
	}
	events := storedEvents(t, db)
	if len(events) != 1 || events[0].Action != string(audit.TeamUpdate) {
		t.Fatalf("events = %+v, want one team.update", events)
	}
	c := changesOf(t, events[0])
	if len(c) != 2 || c["managed"].From != false || c["managed"].To != true || c["description"].From != "old" || c["description"].To != "new" {
		t.Errorf("adoption changes = %+v, want managed false to true and description old to new", c)
	}
}

func savedViewEvents(t *testing.T, db *sqlite.DB, from int) []models.AuditEvent {
	t.Helper()
	var out []models.AuditEvent
	all := storedEvents(t, db)
	for i := from; i < len(all); i++ {
		if strings.HasPrefix(all[i].Action, "saved_view.") {
			out = append(out, all[i])
		}
	}
	return out
}

func actionsOf(events []models.AuditEvent) string {
	names := make([]string, len(events))
	for i := range events {
		names[i] = events[i].Action + " " + events[i].TargetLabel
	}
	return strings.Join(names, ", ")
}

func TestReconcileAuditsSourceViews(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()
	ds := datasourcesFor(db)
	rec, lines := lineRecorder(db)
	const filterText = `secret_marker = "x"`
	access := config.ProvisionView{Name: "Access", Default: true, Filter: new(filterText), TimeRange: new("1h")}
	errorsView := config.ProvisionView{Name: "Errors"}
	run := func(prune, dryRun bool, views ...config.ProvisionView) []models.AuditEvent {
		t.Helper()
		from := len(storedEvents(t, db))
		cfg := &config.ProvisioningConfig{ManageSources: true, Prune: prune, DryRun: dryRun, Sources: []config.ProvisionSource{viewSource("nginx", views...)}}
		if err := Reconcile(t.Context(), cfg, db, ds, rec, quietLogger(), nil); err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		return savedViewEvents(t, db, from)
	}

	// Creation and the declared default, by the run that created the source.
	events := run(false, false, access, errorsView)
	if got := actionsOf(events); got != "saved_view.create Access, saved_view.create Errors, saved_view.set_default Access" {
		t.Fatalf("first run recorded %s", got)
	}
	src, err := db.GetSourceByNameForProvisioning(ctx, "nginx")
	if err != nil {
		t.Fatal(err)
	}
	all := storedEvents(t, db)
	if all[0].Action != string(audit.SourceCreate) {
		t.Fatalf("first event %s, want source.create", all[0].Action)
	}
	for _, e := range events {
		if e.ActorType != models.AuditActorSystem || e.AuthMethod != models.AuditAuthProvisioning || e.CredentialID != all[0].CredentialID {
			t.Errorf("%s actor %s/%s run %q, want system/provisioning with the run ID %q", e.Action, e.ActorType, e.AuthMethod, e.CredentialID, all[0].CredentialID)
		}
		if e.TargetType != "saved_view" || e.SourceID == nil || *e.SourceID != src.ID {
			t.Errorf("%s target %s source %v, want a saved_view of source %d", e.Action, e.TargetType, e.SourceID, src.ID)
		}
	}
	accessID := events[0].TargetID
	if c := changesOf(t, events[0]); c["managed"].To != true || c["scope"].To != "source" || c["filter_language"].To != "logchefql" || c["time_range"].To != "1h" {
		t.Errorf("create changes = %+v", c)
	}

	// An unchanged file records nothing; a dry run of a change records nothing.
	if events := run(false, false, access, errorsView); len(events) != 0 {
		t.Fatalf("an unchanged file recorded %s", actionsOf(events))
	}
	changed := access
	changed.Default = false
	changed.TimeRange = new("6h")
	errorsDefault := errorsView
	errorsDefault.Default = true
	if events := run(false, true, changed, errorsDefault); len(events) != 0 {
		t.Fatalf("a dry run recorded %s", actionsOf(events))
	}

	// An update, and the default moving to another view.
	events = run(false, false, changed, errorsDefault)
	if got := actionsOf(events); got != "saved_view.update Access, saved_view.set_default Errors" {
		t.Fatalf("change run recorded %s", got)
	}
	if c := changesOf(t, events[0]); len(c) != 2 || c["time_range"].From != "1h" || c["time_range"].To != "6h" || c["revision"].To != float64(2) {
		t.Errorf("update changes = %+v, want time_range and revision", c)
	}
	var detail map[string]any
	if err := json.Unmarshal(events[1].Detail, &detail); err != nil {
		t.Fatal(err)
	}
	if id, _ := strconv.Atoi(accessID); detail["scope"] != "source" || detail["cleared_view_id"] != float64(id) {
		t.Errorf("set_default detail = %v, want scope source and cleared_view_id %s", detail, accessID)
	}

	// Adoption of an unmanaged view with the declared name is one update.
	manual := &models.SavedView{SourceID: src.ID, Scope: models.SavedViewScopeSource, Name: "Slow", Definition: models.SavedViewDefinition{Version: 1}}
	if err := db.CreateSavedView(ctx, manual); err != nil {
		t.Fatal(err)
	}
	slow := config.ProvisionView{Name: "Slow", TimeRange: new("15m")}
	events = run(false, false, changed, errorsDefault, slow)
	if got := actionsOf(events); got != "saved_view.update Slow" {
		t.Fatalf("adopt run recorded %s", got)
	}
	if c := changesOf(t, events[0]); c["managed"].From != false || c["managed"].To != true || c["time_range"].To != "15m" {
		t.Errorf("adoption changes = %+v, want managed false to true and the new time_range", c)
	}

	// Without a declared default the managed default is cleared; prune deletes.
	events = run(true, false, changed, slow)
	if got := actionsOf(events); got != "saved_view.delete Errors" {
		t.Fatalf("prune run recorded %s", got)
	}
	events = run(true, false, changed, config.ProvisionView{Name: "Slow", TimeRange: new("15m"), Default: true})
	events = append(events, run(true, false, changed, slow)...)
	if got := actionsOf(events); got != "saved_view.set_default Slow, saved_view.clear_default Slow" {
		t.Fatalf("default runs recorded %s", got)
	}

	stored := lines.String()
	for _, e := range storedEvents(t, db) {
		stored += string(e.Changes) + string(e.Detail)
	}
	if strings.Contains(stored, "secret_marker") {
		t.Error("the filter text reached the audit log")
	}
}
