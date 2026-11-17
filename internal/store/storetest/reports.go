package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// RunReports exercises the ReportStore contract (RFC 0008 unit R1). open
// must return a new store on the same database each time, like a separate
// replica.
func RunReports(t *testing.T, open Opener) {
	ctx := context.Background()
	s := open(t)
	t.Run("RoundTrip", func(t *testing.T) { testReportRoundTrip(t, ctx, s) })
	t.Run("Update", func(t *testing.T) { testReportUpdate(t, ctx, s) })
	t.Run("OneSource", func(t *testing.T) { testReportOneSource(t, ctx, s) })
	t.Run("DeletedUser", func(t *testing.T) { testReportDeletedUser(t, ctx, s) })
	t.Run("DeletedSource", func(t *testing.T) { testReportDeletedSource(t, ctx, s) })
	t.Run("StateAndClaim", func(t *testing.T) { testReportStateAndClaim(t, ctx, s) })
	t.Run("Recipients", func(t *testing.T) { testReportRecipients(t, ctx, s) })
	t.Run("TickConflict", func(t *testing.T) { testReportTickConflict(t, ctx, s) })
	t.Run("OneRunningRow", func(t *testing.T) { testReportOneRunningRow(t, ctx, s) })
	t.Run("CooldownOnce", func(t *testing.T) { testReportCooldownOnce(t, ctx, open(t), open(t)) })
	t.Run("ConcurrentRecipientReplace", func(t *testing.T) { testReportConcurrentReplace(t, ctx, open(t), open(t)) })
	t.Run("Finish", func(t *testing.T) { testReportFinish(t, ctx, s) })
	t.Run("FailureStreak", func(t *testing.T) { testReportFailureStreak(t, ctx, s) })
	t.Run("Prune", func(t *testing.T) { testReportPrune(t, ctx, s) })
	t.Run("InterruptStale", func(t *testing.T) { testReportInterruptStale(t, ctx, s) })
	t.Run("LatestRuns", func(t *testing.T) { testReportLatestRuns(t, ctx, s) })
}

var reportDaily = models.ReportSchedule{Frequency: models.ReportDaily, Minute: 0, Hour: new(9)}

// mkSavedQueryReport creates an owner, a source, a saved query and an
// active daily report on it.
func mkSavedQueryReport(t *testing.T, ctx context.Context, s store.Store, prefix string) (*models.Report, *models.User, *models.SavedQuery) {
	t.Helper()
	owner := mkUser(t, ctx, s, prefix+"-owner@test.dev")
	src := mkSource(t, ctx, s, prefix)
	sq, err := s.CreateSavedQuery(ctx, src.ID, nil, prefix, "", models.QueryLanguageLogchefQL, models.SavedQueryEditorModeBuilder, `{"content":"status>=500"}`, &owner.ID)
	if err != nil {
		t.Fatalf("CreateSavedQuery: %v", err)
	}
	r := &models.Report{
		Name:         prefix + " report",
		OwnerID:      &owner.ID,
		SourceKind:   models.ReportSourceSavedQuery,
		SavedQueryID: &sq.ID,
		Schedule:     reportDaily,
		Timezone:     "Asia/Kolkata",
		TimeRange:    "24h",
		RangeAnchor:  models.ReportAnchorSendTime,
		MaxRows:      25,
	}
	if err := s.CreateReport(ctx, r); err != nil {
		t.Fatalf("CreateReport: %v", err)
	}
	return r, owner, sq
}

func mkReportDashboard(t *testing.T, ctx context.Context, s store.Store, owner *models.User) *models.Dashboard {
	t.Helper()
	d := &models.Dashboard{Name: "Reports", PanelsJSON: json.RawMessage(`{"version":1,"layout":[],"panels":[]}`), CreatedBy: &owner.ID}
	if err := s.CreateDashboard(ctx, d); err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}
	return d
}

func testReportRoundTrip(t *testing.T, ctx context.Context, s store.Store) {
	r, owner, _ := mkSavedQueryReport(t, ctx, s, "roundtrip")
	if r.ID == 0 || r.State != models.ReportStateActive || r.PauseReason != models.ReportPauseNone || r.CreatedAt.IsZero() {
		t.Fatalf("created report = %+v", r)
	}
	if r.Variables == nil || len(r.Variables) != 0 {
		t.Errorf("created report variables = %#v, want an empty map", r.Variables)
	}
	got, err := s.GetReport(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reportEdits(got) != reportEdits(r) || got.TabID != "" || got.DashboardID != nil {
		t.Errorf("stored report = %+v, want %+v", got, r)
	}
	if mine, err := s.ListReportsByOwner(ctx, owner.ID); err != nil || len(mine) != 1 || mine[0].ID != r.ID {
		t.Errorf("ListReportsByOwner = %v, %v", mine, err)
	}
	if all, err := s.ListReports(ctx); err != nil || !containsReport(all, r.ID) {
		t.Errorf("ListReports = %v, %v", all, err)
	}
	if _, err := s.GetReport(ctx, 1<<40); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetReport(missing) = %v, want ErrNotFound", err)
	}
	if err := s.DeleteReport(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteReport(ctx, r.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("second DeleteReport = %v, want ErrNotFound", err)
	}
}

func testReportUpdate(t *testing.T, ctx context.Context, s store.Store) {
	r, owner, _ := mkSavedQueryReport(t, ctx, s, "update")
	dash := mkReportDashboard(t, ctx, s, owner)
	edit := *r
	edit.SourceKind, edit.DashboardID, edit.SavedQueryID, edit.TabID = models.ReportSourceDashboard, &dash.ID, nil, "main"
	edit.Schedule = models.ReportSchedule{Frequency: models.ReportWeekly, Minute: 30, Hour: new(8), Weekdays: []int{1, 5}}
	edit.TimeRange, edit.RangeAnchor = "", models.ReportAnchorDayStart
	edit.Variables = map[string]any{"service": "api"}
	edit.MaxRows, edit.SkipIfEmpty, edit.AttachCSV = 50, true, true
	if err := s.UpdateReport(ctx, &edit); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetReport(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reportEdits(got) != reportEdits(&edit) {
		t.Errorf("updated report = %s, want %s", reportEdits(got), reportEdits(&edit))
	}

	missing := edit
	missing.ID = 1 << 40
	if err := s.UpdateReport(ctx, &missing); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("UpdateReport(missing) = %v, want ErrNotFound", err)
	}
}

// reportEdits renders the fields a user sets, plus the owner, for comparison.
func reportEdits(r *models.Report) string {
	c := *r
	c.ID, c.State, c.PauseReason, c.ConsecutiveFailures = 0, "", "", 0
	c.LastManualRunAt, c.LastPreviewAt = nil, nil
	c.CreatedAt, c.UpdatedAt = time.Time{}, time.Time{}
	b, err := json.Marshal(c)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func testReportOneSource(t *testing.T, ctx context.Context, s store.Store) {
	r, owner, sq := mkSavedQueryReport(t, ctx, s, "onesource")
	dash := mkReportDashboard(t, ctx, s, owner)
	cases := map[string]*models.Report{
		"both sources":            {SourceKind: models.ReportSourceSavedQuery, SavedQueryID: &sq.ID, DashboardID: &dash.ID},
		"no source":               {SourceKind: models.ReportSourceDashboard},
		"kind names the other id": {SourceKind: models.ReportSourceDashboard, SavedQueryID: &sq.ID},
	}
	for name, bad := range cases {
		bad.Name, bad.OwnerID, bad.Schedule, bad.Timezone, bad.RangeAnchor, bad.MaxRows = name, &owner.ID, reportDaily, "UTC", models.ReportAnchorSendTime, 25
		if err := s.CreateReport(ctx, bad); err == nil {
			t.Errorf("CreateReport with %s succeeded", name)
		}
	}
	r.DashboardID = &dash.ID
	if err := s.UpdateReport(ctx, r); err == nil {
		t.Error("UpdateReport to two sources succeeded")
	}
}

func testReportDeletedUser(t *testing.T, ctx context.Context, s store.Store) {
	r, owner, _ := mkSavedQueryReport(t, ctx, s, "deluser")
	other := mkUser(t, ctx, s, "deluser-other@test.dev")
	if err := s.ReplaceReportRecipients(ctx, r.ID, []models.ReportRecipient{
		{Kind: models.ReportRecipientUser, UserID: &owner.ID, AddedBy: &owner.ID},
		{Kind: models.ReportRecipientUser, UserID: &other.ID, AddedBy: &owner.ID},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, owner.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetReport(ctx, r.ID)
	if err != nil {
		t.Fatalf("report went with its owner: %v", err)
	}
	if got.OwnerID != nil {
		t.Errorf("owner_id = %v, want NULL", *got.OwnerID)
	}
	recipients, err := s.ListReportRecipients(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(recipients) != 1 || *recipients[0].UserID != other.ID || recipients[0].AddedBy != nil {
		t.Errorf("recipients after deleting the owner = %+v, want only the other user with added_by NULL", recipients)
	}
}

func testReportDeletedSource(t *testing.T, ctx context.Context, s store.Store) {
	r, owner, sq := mkSavedQueryReport(t, ctx, s, "delsource")
	dash := mkReportDashboard(t, ctx, s, owner)
	onDash := &models.Report{Name: "dash", OwnerID: &owner.ID, SourceKind: models.ReportSourceDashboard, DashboardID: &dash.ID, Schedule: reportDaily, Timezone: "UTC", RangeAnchor: models.ReportAnchorSendTime, MaxRows: 25}
	if err := s.CreateReport(ctx, onDash); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSavedQuery(ctx, sq.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetReport(ctx, r.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("report on a deleted saved query: %v, want ErrNotFound", err)
	}
	if err := s.DeleteDashboard(ctx, dash.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetReport(ctx, onDash.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("report on a deleted dashboard: %v, want ErrNotFound", err)
	}
}

func testReportStateAndClaim(t *testing.T, ctx context.Context, s store.Store) {
	r, _, _ := mkSavedQueryReport(t, ctx, s, "state")
	claimer := mkUser(t, ctx, s, "state-claimer@test.dev")
	check := func(step string, state models.ReportState, reason models.ReportPauseReason, failures int) {
		t.Helper()
		got, err := s.GetReport(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != state || got.PauseReason != reason || got.ConsecutiveFailures != failures {
			t.Errorf("%s: state %s/%q failures %d, want %s/%q %d", step, got.State, got.PauseReason, got.ConsecutiveFailures, state, reason, failures)
		}
	}

	if err := s.PauseReport(ctx, r.ID, models.ReportPauseNone); err == nil {
		t.Error("a pause with no reason succeeded")
	}
	finishRun(t, ctx, s, r.ID, models.ReportRunFailed)
	finishRun(t, ctx, s, r.ID, models.ReportRunFailed)
	if err := s.PauseReport(ctx, r.ID, models.ReportPauseFailures); err != nil {
		t.Fatal(err)
	}
	check("paused for failures", models.ReportStatePaused, models.ReportPauseFailures, 2)
	if err := s.ResumeReport(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	check("resumed", models.ReportStateActive, models.ReportPauseNone, 0)

	if err := s.PauseReport(ctx, r.ID, models.ReportPauseUser); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimReport(ctx, r.ID, claimer.ID); !errors.Is(err, models.ErrReportOwned) {
		t.Fatalf("claiming a report with an active owner = %v, want ErrReportOwned", err)
	}
	check("a refused claim changes nothing", models.ReportStatePaused, models.ReportPauseUser, 0)

	if err := s.PauseReport(ctx, r.ID, models.ReportPauseOwnerInactive); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimReport(ctx, r.ID, claimer.ID); err != nil {
		t.Fatal(err)
	}
	check("claim lifts an owner_inactive pause", models.ReportStateActive, models.ReportPauseNone, 0)
	if got, _ := s.GetReport(ctx, r.ID); got.OwnerID == nil || *got.OwnerID != claimer.ID {
		t.Errorf("owner after claim = %v, want %d", got.OwnerID, claimer.ID)
	}
	late := mkUser(t, ctx, s, "state-late@test.dev")
	if err := s.ClaimReport(ctx, r.ID, late.ID); !errors.Is(err, models.ErrReportOwned) {
		t.Errorf("a second claim after the first = %v, want ErrReportOwned", err)
	}

	if err := s.PauseReport(ctx, r.ID, models.ReportPauseUser); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, claimer.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ClaimReport(ctx, r.ID, late.ID); err != nil {
		t.Fatalf("claiming a report with no owner = %v", err)
	}
	check("claim keeps a user pause", models.ReportStatePaused, models.ReportPauseUser, 0)
	if err := s.ClaimReport(ctx, 1<<40, claimer.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("ClaimReport(missing) = %v, want ErrNotFound", err)
	}
}

func testReportRecipients(t *testing.T, ctx context.Context, s store.Store) {
	r, owner, _ := mkSavedQueryReport(t, ctx, s, "recipients")
	direct := mkUser(t, ctx, s, "recipients-direct@test.dev")
	member := mkUser(t, ctx, s, "recipients-member@test.dev")
	byMail := mkUser(t, ctx, s, "recipients-mail@test.dev")
	outsider := mkUser(t, ctx, s, "recipients-outsider@test.dev")
	team := &models.Team{Name: "recipients-team"}
	if err := s.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	if err := s.AddTeamMember(ctx, team.ID, member.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	want := []models.ReportRecipient{
		{Kind: models.ReportRecipientUser, UserID: &direct.ID, AddedBy: &owner.ID},
		{Kind: models.ReportRecipientTeam, TeamID: &team.ID, AddedBy: &owner.ID},
		{Kind: models.ReportRecipientEmail, Email: "Recipients-MAIL@test.dev", AddedBy: &owner.ID},
		{Kind: models.ReportRecipientEmail, Email: "oncall@example.com", AddedBy: &owner.ID},
	}
	if err := s.ReplaceReportRecipients(ctx, r.ID, want); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListReportRecipients(ctx, r.ID)
	if err != nil || len(got) != len(want) {
		t.Fatalf("ListReportRecipients = %+v, %v", got, err)
	}
	if *got[0].UserID != direct.ID || *got[1].TeamID != team.ID || got[2].Email != "Recipients-MAIL@test.dev" || got[1].UserID != nil {
		t.Errorf("recipients = %+v", got)
	}

	for _, u := range []*models.User{direct, member, byMail} {
		if reports, err := s.ListReportsForRecipient(ctx, u.ID); err != nil || len(reports) != 1 || reports[0].ID != r.ID {
			t.Errorf("ListReportsForRecipient(%s) = %v, %v", u.Email, reports, err)
		}
	}
	if reports, err := s.ListReportsForRecipient(ctx, outsider.ID); err != nil || len(reports) != 0 {
		t.Errorf("ListReportsForRecipient(outsider) = %v, %v", reports, err)
	}

	testReportRecipientEdits(t, ctx, s, r.ID, want, direct, member, byMail, team)
}

func testReportRecipientEdits(t *testing.T, ctx context.Context, s store.Store, id models.ReportID, want []models.ReportRecipient, direct, member, byMail *models.User, team *models.Team) {
	dup := []models.ReportRecipient{want[0], {Kind: models.ReportRecipientUser, UserID: &direct.ID}}
	if err := s.ReplaceReportRecipients(ctx, id, dup); !errors.Is(err, models.ErrConflict) {
		t.Errorf("a duplicate recipient = %v, want ErrConflict", err)
	}
	if kept, _ := s.ListReportRecipients(ctx, id); len(kept) != len(want) {
		t.Errorf("a failed replace left %d recipients, want the %d before it", len(kept), len(want))
	}
	if err := s.ReplaceReportRecipients(ctx, id, []models.ReportRecipient{{Kind: models.ReportRecipientUser, TeamID: &team.ID}}); err == nil {
		t.Error("a user row that names a team was stored")
	}

	if err := s.RemoveReportUserRecipient(ctx, id, direct.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveReportUserRecipient(ctx, id, direct.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("second RemoveReportUserRecipient = %v, want ErrNotFound", err)
	}
	if err := s.RemoveReportUserRecipient(ctx, id, member.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("removing a team member as a direct recipient = %v, want ErrNotFound", err)
	}
	// An email row with the user's address, in another case, names them too.
	if err := s.RemoveReportUserRecipient(ctx, id, byMail.ID); err != nil {
		t.Errorf("removing the user an email row names = %v", err)
	}
	if left, _ := s.ListReportRecipients(ctx, id); len(left) != len(want)-2 || slices.ContainsFunc(left, func(r models.ReportRecipient) bool { return strings.EqualFold(r.Email, byMail.Email) }) {
		t.Errorf("recipients after removing the email row = %+v, want only the team and the other address", left)
	}
	if err := s.ReplaceReportRecipients(ctx, id, nil); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.ListReportRecipients(ctx, id); len(left) != 0 {
		t.Errorf("recipients after clearing = %+v", left)
	}
}

func testReportTickConflict(t *testing.T, ctx context.Context, s store.Store) {
	r, _, _ := mkSavedQueryReport(t, ctx, s, "tick")
	tick := time.Date(2026, 11, 16, 3, 30, 0, 0, time.UTC)
	claim := models.ReportRunClaim{ReportID: r.ID, Trigger: models.ReportTriggerSchedule, ScheduledFor: tick, HandlerBudget: 10 * time.Minute}

	run := mustClaimRun(t, ctx, s, claim)
	if _, err := s.CreateReportRun(ctx, claim); !errors.Is(err, models.ErrReportTickTaken) {
		t.Errorf("the same tick while running = %v, want ErrReportTickTaken", err)
	}
	if _, err := s.FinishReportRun(ctx, run.ID, models.ReportRunResult{Status: models.ReportRunSent}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateReportRun(ctx, claim); !errors.Is(err, models.ErrReportTickTaken) {
		t.Errorf("the same tick after it finished = %v, want ErrReportTickTaken", err)
	}
	inIST := tick.In(time.FixedZone("IST", 5*3600+1800))
	if _, err := s.SkipReportTick(ctx, r.ID, inIST, models.ReportReasonBusy); !errors.Is(err, models.ErrReportTickTaken) {
		t.Errorf("skipping a taken tick given in another zone = %v, want ErrReportTickTaken", err)
	}

	next := tick.Add(24 * time.Hour)
	skipped, err := s.SkipReportTick(ctx, r.ID, next, models.ReportReasonBusy)
	if err != nil {
		t.Fatal(err)
	}
	if skipped.Status != models.ReportRunSkipped || skipped.Reason != models.ReportReasonBusy || skipped.FinishedAt == nil || skipped.DeadlineAt != nil || !skipped.ScheduledFor.Equal(next) {
		t.Errorf("skipped tick = %+v", skipped)
	}
	if _, err := s.CreateReportRun(ctx, models.ReportRunClaim{ReportID: r.ID, Trigger: models.ReportTriggerSchedule, ScheduledFor: next, HandlerBudget: time.Minute}); !errors.Is(err, models.ErrReportTickTaken) {
		t.Errorf("running a skipped tick = %v, want ErrReportTickTaken", err)
	}
}

func testReportOneRunningRow(t *testing.T, ctx context.Context, s store.Store) {
	triggers := []models.ReportRunTrigger{models.ReportTriggerSchedule, models.ReportTriggerManual}
	base := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	for _, first := range triggers {
		for _, second := range triggers {
			t.Run(string(first)+"_then_"+string(second), func(t *testing.T) {
				r, owner, _ := mkSavedQueryReport(t, ctx, s, "running-"+string(first)+"-"+string(second))
				running := mustClaimRun(t, ctx, s, models.ReportRunClaim{ReportID: r.ID, Trigger: first, ScheduledFor: base, ActorID: &owner.ID, HandlerBudget: 10 * time.Minute})
				if running.Status != models.ReportRunRunning || running.DeadlineAt == nil || running.FinishedAt != nil || !running.DeadlineAt.After(running.StartedAt) {
					t.Errorf("claimed run = %+v", running)
				}
				_, err := s.CreateReportRun(ctx, models.ReportRunClaim{ReportID: r.ID, Trigger: second, ScheduledFor: base.Add(time.Hour), HandlerBudget: 10 * time.Minute})
				if !errors.Is(err, models.ErrReportRunInFlight) {
					t.Errorf("a second run = %v, want ErrReportRunInFlight", err)
				}
				if _, err := s.FinishReportRun(ctx, running.ID, models.ReportRunResult{Status: models.ReportRunSent}); err != nil {
					t.Fatal(err)
				}
				mustClaimRun(t, ctx, s, models.ReportRunClaim{ReportID: r.ID, Trigger: second, ScheduledFor: base.Add(time.Hour), HandlerBudget: 10 * time.Minute})
			})
		}
	}
}

// testReportCooldownOnce sends two cooldown updates for one report at the
// same instant through two stores; exactly one may take it.
func testReportCooldownOnce(t *testing.T, ctx context.Context, a, b store.Store) {
	r, _, _ := mkSavedQueryReport(t, ctx, a, "cooldown")
	for _, kind := range []models.ReportCooldownKind{models.ReportCooldownManualRun, models.ReportCooldownPreview} {
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make([]bool, 2)
		errs := make([]error, 2)
		for i, s := range []store.Store{a, b} {
			wg.Go(func() {
				<-start
				results[i], errs[i] = s.TakeReportCooldown(ctx, r.ID, kind)
			})
		}
		close(start)
		wg.Wait()
		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("%s cooldown errors: %v", kind, errs)
		}
		if results[0] == results[1] {
			t.Errorf("%s cooldown taken = %v, want exactly one", kind, results)
		}
		if again, err := a.TakeReportCooldown(ctx, r.ID, kind); err != nil || again {
			t.Errorf("%s cooldown inside the window = %v, %v, want false", kind, again, err)
		}
	}
	got, err := a.GetReport(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastManualRunAt == nil || got.LastPreviewAt == nil || time.Since(*got.LastManualRunAt).Abs() > time.Minute {
		t.Errorf("cooldown times = %v / %v", got.LastManualRunAt, got.LastPreviewAt)
	}
}

// testReportConcurrentReplace replaces one report's recipients from two
// stores at once. The last replace must win whole: a union of both lists
// would keep an address that an edit removed.
func testReportConcurrentReplace(t *testing.T, ctx context.Context, a, b store.Store) {
	r, _, _ := mkSavedQueryReport(t, ctx, a, "replace-race")
	for round := range 20 {
		seed := []models.ReportRecipient{{Kind: models.ReportRecipientEmail, Email: fmt.Sprintf("seed-%d@example.com", round)}}
		if err := a.ReplaceReportRecipients(ctx, r.ID, seed); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, 2)
		for i, s := range []store.Store{a, b} {
			wg.Go(func() {
				<-start
				errs[i] = s.ReplaceReportRecipients(ctx, r.ID, []models.ReportRecipient{{Kind: models.ReportRecipientEmail, Email: fmt.Sprintf("r%d-s%d@example.com", round, i)}})
			})
		}
		close(start)
		wg.Wait()
		if errs[0] != nil || errs[1] != nil {
			t.Fatalf("round %d: %v", round, errs)
		}
		got, err := a.ListReportRecipients(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("round %d: %d recipients after two concurrent replaces, want 1: %+v", round, len(got), got)
		}
	}
	if err := a.ReplaceReportRecipients(ctx, 1<<40, nil); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("replacing the recipients of a missing report = %v, want ErrNotFound", err)
	}
}

func testReportFinish(t *testing.T, ctx context.Context, s store.Store) {
	r, owner, _ := mkSavedQueryReport(t, ctx, s, "finish")
	run := mustClaimRun(t, ctx, s, models.ReportRunClaim{ReportID: r.ID, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now(), ActorID: &owner.ID, HandlerBudget: time.Minute})
	from := time.Date(2026, 11, 15, 3, 30, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	res := models.ReportRunResult{
		Status:      models.ReportRunPartial,
		Reason:      models.ReportReasonPanelFailed,
		WindowStart: &from,
		WindowEnd:   &to,
		Panels: []models.ReportPanelOutcome{
			{PanelID: "query", Title: "5xx", Status: models.ReportPanelOK, Rows: 25, Truncation: models.TruncationRows},
			{PanelID: "p2", Title: "slow", Status: models.ReportPanelFailed, Error: "timeout"},
		},
		Deliveries: []models.ReportDelivery{{Class: models.ReportDeliveryUser, Target: "7", Via: "direct", Status: models.ReportDeliverySent, Attempts: 1}},
	}
	if failures, err := s.FinishReportRun(ctx, run.ID, res); err != nil || failures != 0 {
		t.Fatalf("FinishReportRun = %d, %v", failures, err)
	}
	if _, err := s.FinishReportRun(ctx, run.ID, res); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("finishing a finished run = %v, want ErrNotFound", err)
	}
	if _, err := s.FinishReportRun(ctx, run.ID, models.ReportRunResult{Status: models.ReportRunRunning}); err == nil {
		t.Error("finishing with status running succeeded")
	}
	runs, err := s.ListReportRuns(ctx, r.ID, 0, 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("ListReportRuns = %v, %v", runs, err)
	}
	got := runs[0]
	if got.Status != models.ReportRunPartial || got.Reason != models.ReportReasonPanelFailed || got.DeadlineAt != nil || got.FinishedAt == nil ||
		!got.WindowStart.Equal(from) || !got.WindowEnd.Equal(to) || *got.ActorID != owner.ID || got.Trigger != models.ReportTriggerManual {
		t.Errorf("finished run = %+v", got)
	}
	if len(got.Panels) != 2 || got.Panels[0] != res.Panels[0] || got.Panels[1] != res.Panels[1] || len(got.Deliveries) != 1 || got.Deliveries[0].Target != "7" {
		t.Errorf("finished run panels %+v deliveries %+v", got.Panels, got.Deliveries)
	}

}

func testReportFailureStreak(t *testing.T, ctx context.Context, s store.Store) {
	r, _, _ := mkSavedQueryReport(t, ctx, s, "streak")
	for i, want := range []int{1, 2, 3, 0, 0, 1, 1} {
		status := []models.ReportRunStatus{models.ReportRunFailed, models.ReportRunFailed, models.ReportRunFailed, models.ReportRunSent, models.ReportRunSkipped, models.ReportRunFailed, models.ReportRunSkipped}[i]
		if got := finishRun(t, ctx, s, r.ID, status); got != want {
			t.Errorf("failure streak after run %d (%s) = %d, want %d", i, status, got, want)
		}
	}

}

func testReportPrune(t *testing.T, ctx context.Context, s store.Store) {
	r, _, _ := mkSavedQueryReport(t, ctx, s, "prune")
	for range models.ReportRunHistoryLimit {
		finishRun(t, ctx, s, r.ID, models.ReportRunSent)
	}
	all, err := s.ListReportRuns(ctx, r.ID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != models.ReportRunHistoryLimit {
		t.Fatalf("runs kept = %d, want %d", len(all), models.ReportRunHistoryLimit)
	}
	for i := 1; i < len(all); i++ {
		if all[i].ID >= all[i-1].ID {
			t.Fatalf("runs are not newest first at %d", i)
		}
	}
	page, err := s.ListReportRuns(ctx, r.ID, all[9].ID, 5)
	if err != nil || len(page) != 5 || page[0].ID != all[10].ID {
		t.Errorf("a page before run %d = %v, %v", all[9].ID, page, err)
	}
}

func testReportInterruptStale(t *testing.T, ctx context.Context, s store.Store) {
	stale, _, _ := mkSavedQueryReport(t, ctx, s, "stale")
	live, _, _ := mkSavedQueryReport(t, ctx, s, "live")
	// A negative budget puts the deadline in the past, as if the run had
	// claimed long ago.
	old := mustClaimRun(t, ctx, s, models.ReportRunClaim{ReportID: stale.ID, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now(), HandlerBudget: -3 * time.Minute})
	recent := mustClaimRun(t, ctx, s, models.ReportRunClaim{ReportID: live.ID, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now(), HandlerBudget: -time.Minute})
	n, err := s.InterruptStaleReportRuns(ctx, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("interrupted %d runs, want 1", n)
	}
	runs, _ := s.ListReportRuns(ctx, stale.ID, 0, 1)
	if len(runs) != 1 || runs[0].ID != old.ID || runs[0].Status != models.ReportRunInterrupted || runs[0].Reason != models.ReportReasonProcessStopped || runs[0].DeadlineAt != nil || runs[0].FinishedAt == nil {
		t.Errorf("stale run = %+v", runs)
	}
	if _, err := s.FinishReportRun(ctx, old.ID, models.ReportRunResult{Status: models.ReportRunSent}); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("finishing an interrupted run = %v, want ErrNotFound", err)
	}
	mustClaimRun(t, ctx, s, models.ReportRunClaim{ReportID: stale.ID, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now(), HandlerBudget: time.Minute})
	if _, err := s.CreateReportRun(ctx, models.ReportRunClaim{ReportID: live.ID, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now(), HandlerBudget: time.Minute}); !errors.Is(err, models.ErrReportRunInFlight) {
		t.Errorf("a run within the grace = %v, want it still in flight (run %d)", err, recent.ID)
	}
}

func testReportLatestRuns(t *testing.T, ctx context.Context, s store.Store) {
	twice, _, _ := mkSavedQueryReport(t, ctx, s, "latest-twice")
	once, _, _ := mkSavedQueryReport(t, ctx, s, "latest-once")
	never, _, _ := mkSavedQueryReport(t, ctx, s, "latest-never")
	finishRun(t, ctx, s, twice.ID, models.ReportRunFailed)
	newest := mustClaimRun(t, ctx, s, models.ReportRunClaim{ReportID: twice.ID, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now(), HandlerBudget: time.Minute})
	finishRun(t, ctx, s, once.ID, models.ReportRunSent)

	runs, err := s.ListLatestReportRuns(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[models.ReportID]*models.ReportRun{}
	for _, run := range runs {
		got[run.ReportID] = run
	}
	if got[twice.ID] == nil || got[twice.ID].ID != newest.ID || got[twice.ID].Status != models.ReportRunRunning {
		t.Errorf("latest runs = %+v, want the running run %d of report %d", runs, newest.ID, twice.ID)
	}
	if got[once.ID] == nil || got[once.ID].Status != models.ReportRunSent {
		t.Errorf("latest run of report %d = %+v, want sent", once.ID, got[once.ID])
	}
	if got[never.ID] != nil {
		t.Errorf("report %d has no run, got %+v", never.ID, got[never.ID])
	}
}

func mustClaimRun(t *testing.T, ctx context.Context, s store.Store, claim models.ReportRunClaim) *models.ReportRun {
	t.Helper()
	run, err := s.CreateReportRun(ctx, claim)
	if err != nil {
		t.Fatalf("CreateReportRun(%+v): %v", claim, err)
	}
	return run
}

// finishRun claims a manual run and finishes it with status, returning the
// failure streak.
func finishRun(t *testing.T, ctx context.Context, s store.Store, id models.ReportID, status models.ReportRunStatus) int {
	t.Helper()
	run := mustClaimRun(t, ctx, s, models.ReportRunClaim{ReportID: id, Trigger: models.ReportTriggerManual, ScheduledFor: time.Now(), HandlerBudget: time.Minute})
	failures, err := s.FinishReportRun(ctx, run.ID, models.ReportRunResult{Status: status})
	if err != nil {
		t.Fatalf("FinishReportRun: %v", err)
	}
	return failures
}

func containsReport(reports []*models.Report, id models.ReportID) bool {
	for _, r := range reports {
		if r.ID == id {
			return true
		}
	}
	return false
}
