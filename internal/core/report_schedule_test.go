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
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/reports"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// Scheduled runs go through the real job runner on SQLite. The runner reads
// the database clock, so a test moves a report's job back in time instead of
// moving the clock.

// dailyTick returns a daily UTC schedule whose latest tick was ago before
// now, and that tick.
func dailyTick(ago time.Duration) (json.RawMessage, time.Time) {
	tick := time.Now().UTC().Add(-ago).Truncate(time.Minute)
	return json.RawMessage(fmt.Sprintf(`{"frequency":"daily","hour":%d,"minute":%d}`, tick.Hour(), tick.Minute())), tick
}

// createDaily creates the env's saved-query report on a daily UTC schedule
// whose latest tick was ago before now, and makes its job due at the tick
// before that, as after downtime that spans two ticks.
func (e *reportEnv) createDaily(ago time.Duration) (*models.Report, time.Time) {
	e.t.Helper()
	in := e.input()
	in.Timezone = "UTC"
	var tick time.Time
	in.Schedule, tick = dailyTick(ago)
	saved, err := CreateReport(context.Background(), e.db, access.SessionPrincipal(e.owner), in)
	if err != nil {
		e.t.Fatalf("CreateReport: %v", err)
	}
	r := saved.Report
	e.setJobDue(r.ID, tick.Add(-24*time.Hour))
	return r, tick
}

func (e *reportEnv) setJobDue(id models.ReportID, at time.Time) {
	e.t.Helper()
	raw := openRawSQLite(e.t, e.dbPath)
	if _, err := raw.Exec(`UPDATE jobs SET next_run_at = ? WHERE kind = ? AND key = ?`,
		at.UTC().Format("2006-01-02T15:04:05.000Z"), string(reports.JobKind), strconv.FormatInt(int64(id), 10)); err != nil {
		e.t.Fatal(err)
	}
}

func (e *reportEnv) job(id models.ReportID) *models.ScheduledJob {
	e.t.Helper()
	job, err := e.db.GetJob(context.Background(), string(reports.JobKind), strconv.FormatInt(int64(id), 10))
	if err != nil {
		e.t.Fatalf("GetJob(report %d): %v", id, err)
	}
	return job
}

func (e *reportEnv) runs(id models.ReportID) []*models.ReportRun {
	e.t.Helper()
	runs, err := e.db.ListReportRuns(context.Background(), id, 0, models.ReportRunHistoryLimit)
	if err != nil {
		e.t.Fatal(err)
	}
	return runs
}

// schedule runs a job runner with the report kind until every job in ids
// has finished a run, then stops it. The runner polls once an hour, so it
// claims only at Start.
func (e *reportEnv) schedule(ids ...models.ReportID) {
	e.t.Helper()
	r := jobs.New(jobs.Options{Store: e.db, Logger: discardLogger(), PollInterval: time.Hour})
	r.Register(e.rr.JobKind(4))
	r.Start(context.Background())
	waitUntil(e.t, func() bool {
		for _, id := range ids {
			if job := e.job(id); job.LastFinishedAt == nil || job.LeaseOwner != "" {
				return false
			}
		}
		return true
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Stop(ctx); err != nil {
		e.t.Fatalf("Stop: %v", err)
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 10s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// claimJob claims the report's due job as a replica would, with a lease
// that ends after lease, and returns the claim the handler receives.
func (e *reportEnv) claimJob(ctx context.Context, id models.ReportID, at time.Time, lease time.Duration) jobs.Job {
	e.t.Helper()
	claimed, err := e.db.ClaimJobs(ctx, string(reports.JobKind), "replica", lease, 1)
	if err != nil || len(claimed) != 1 || claimed[0].Key != strconv.FormatInt(int64(id), 10) {
		e.t.Fatalf("ClaimJobs = %v, %v", claimed, err)
	}
	return jobs.Job{ID: claimed[0].ID, Kind: reports.JobKind, Key: claimed[0].Key, At: at, ClaimedAt: *claimed[0].LastStartedAt, LeaseToken: claimed[0].LeaseToken}
}

// TestReportJobFollowsTheReport: every report write keeps the job in step,
// and the response's next_run_at is the job's next tick.
func TestReportJobFollowsTheReport(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	p := access.SessionPrincipal(e.owner)
	r := e.create()

	job := e.job(r.ID)
	if job.ScheduleKind != "cron" || job.ScheduleExpr != "0 9 * * *" || job.Timezone != "Asia/Kolkata" ||
		job.Misfire != string(jobs.Skip) || job.GraceSeconds != 3600 || !job.Enabled {
		t.Fatalf("job of a new report = %+v, want cron 0 9 * * * in Asia/Kolkata, skip with 3600s grace, enabled", job.JobDefinition)
	}
	resp, err := BuildReportResponse(ctx, ReportDeps{DB: e.db}, p, r)
	if err != nil {
		t.Fatal(err)
	}
	if resp.NextRunAt == nil || !resp.NextRunAt.Equal(job.NextRunAt) {
		t.Errorf("next_run_at = %v, want the job's next run %v", resp.NextRunAt, job.NextRunAt)
	}

	in := e.input()
	in.Schedule = json.RawMessage(`{"frequency":"weekly","hour":8,"minute":15,"weekdays":[5,1]}`)
	in.Timezone = "Europe/Berlin"
	if _, _, err := UpdateReport(ctx, e.db, p, r.ID, in); err != nil {
		t.Fatal(err)
	}
	if job := e.job(r.ID); job.ScheduleExpr != "15 8 * * 1,5" || job.Timezone != "Europe/Berlin" || !job.Enabled {
		t.Errorf("job after the edit = %+v, want 15 8 * * 1,5 in Europe/Berlin", job.JobDefinition)
	}

	paused, err := PauseReport(ctx, e.db, p, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e.job(r.ID).Enabled {
		t.Error("the job of a paused report is enabled")
	}
	if resp, err := BuildReportResponse(ctx, ReportDeps{DB: e.db}, p, paused); err != nil || resp.NextRunAt != nil {
		t.Errorf("next_run_at of a paused report = %v, %v; want null", resp.NextRunAt, err)
	}
	// An edit of a paused report keeps its job disabled.
	if _, _, err := UpdateReport(ctx, e.db, p, r.ID, in); err != nil {
		t.Fatal(err)
	}
	if e.job(r.ID).Enabled {
		t.Error("an edit enabled the job of a paused report")
	}
	if _, err := ResumeReport(ctx, e.db, p, r.ID); err != nil {
		t.Fatal(err)
	}
	if !e.job(r.ID).Enabled {
		t.Error("the job of a resumed report is disabled")
	}

	if _, err := DeleteReport(ctx, e.db, p, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.GetJob(ctx, string(reports.JobKind), strconv.FormatInt(int64(r.ID), 10)); !models.IsNotFound(err) {
		t.Errorf("job of a deleted report: %v, want not found", err)
	}
}

// TestScheduledReportAfterDowntime: downtime spans two ticks. Ending 30
// minutes after the second tick runs the second only, with T equal to it;
// ending 2 hours after it runs nothing.
func TestScheduledReportAfterDowntime(t *testing.T) {
	e := newReportEnv(t)
	inGrace, tick := e.createDaily(30 * time.Minute)
	e.schedule(inGrace.ID)

	runs := e.runs(inGrace.ID)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want one", len(runs))
	}
	run := runs[0]
	if run.Trigger != models.ReportTriggerSchedule || !run.ScheduledFor.Equal(tick) || run.Status != models.ReportRunSent {
		t.Errorf("run = %s %v %s/%s, want a sent scheduled run for %v", run.Trigger, run.ScheduledFor, run.Status, run.Reason, tick)
	}
	// The window ends at T, not at the claim 30 minutes later.
	if run.WindowEnd == nil || !run.WindowEnd.Equal(tick) || !run.WindowStart.Equal(tick.Add(-24*time.Hour)) {
		t.Errorf("window = %v to %v, want the 24 hours up to %v", run.WindowStart, run.WindowEnd, tick)
	}
	if sent := e.mailer.messages(); len(sent) != 1 || sent[0].to != e.owner.Email {
		t.Errorf("messages = %+v, want one to the owner", sent)
	}
	if job := e.job(inGrace.ID); !job.NextRunAt.Equal(tick.Add(24*time.Hour)) || job.LastStatus != "ok" {
		t.Errorf("job after the run: next %v, status %q; want %v, ok", job.NextRunAt, job.LastStatus, tick.Add(24*time.Hour))
	}

	late, lateTick := e.createDaily(2 * time.Hour)
	e.schedule(late.ID)
	if runs := e.runs(late.ID); len(runs) != 0 {
		t.Errorf("a tick 2 hours late made runs %+v, want none", runs)
	}
	if n := len(e.mailer.messages()); n != 1 {
		t.Errorf("messages = %d after the late tick, want still 1", n)
	}
	if job := e.job(late.ID); job.LastStatus != "skipped" || !job.NextRunAt.Equal(lateTick.Add(24*time.Hour)) {
		t.Errorf("late job: status %q, next %v; want skipped, %v", job.LastStatus, job.NextRunAt, lateTick.Add(24*time.Hour))
	}
}

// TestScheduledReportPausedDoesNotFire: a paused report's job is never
// claimed. An active report due at the same time shows the runner ran.
func TestScheduledReportPausedDoesNotFire(t *testing.T) {
	e := newReportEnv(t)
	active, _ := e.createDaily(30 * time.Minute)
	paused, _ := e.createDaily(30 * time.Minute)
	if _, err := PauseReport(context.Background(), e.db, access.SessionPrincipal(e.owner), paused.ID); err != nil {
		t.Fatal(err)
	}
	e.schedule(active.ID)
	if runs := e.runs(paused.ID); len(runs) != 0 {
		t.Errorf("paused report runs = %+v, want none", runs)
	}
	if job := e.job(paused.ID); job.LeaseToken != 0 {
		t.Errorf("paused report's job was claimed %d times", job.LeaseToken)
	}
	if runs := e.runs(active.ID); len(runs) != 1 {
		t.Errorf("active report runs = %d, want 1", len(runs))
	}
}

// TestScheduledReportPausedAfterTheClaim: a pause that lands between the
// claim and the run records the tick as skipped with reason paused.
func TestScheduledReportPausedAfterTheClaim(t *testing.T) {
	const skipped = `logchef_report_runs_total{trigger="schedule",status="skipped"}`
	skippedBefore := counter(skipped)
	e := newReportEnv(t)
	ctx := context.Background()
	r, tick := e.createDaily(30 * time.Minute)
	job := e.claimJob(ctx, r.ID, tick, time.Minute)
	if _, err := PauseReport(ctx, e.db, access.SessionPrincipal(e.owner), r.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.rr.runScheduled(ctx, job); err != nil {
		t.Fatal(err)
	}
	runs := e.runs(r.ID)
	if len(runs) != 1 || runs[0].Status != models.ReportRunSkipped || runs[0].Reason != models.ReportReasonPaused || !runs[0].ScheduledFor.Equal(tick) {
		t.Fatalf("runs = %+v, want one skipped/paused tick", runs)
	}
	if n := len(e.mailer.messages()); n != 0 {
		t.Errorf("messages = %d, want none", n)
	}
	if got := counter(skipped); got != skippedBefore+1 {
		t.Errorf("%s = %d, want %d", skipped, got, skippedBefore+1)
	}
}

// TestScheduledReportPauseDuringTheClaim: a pause that has taken the job
// row but not yet committed when the tick starts wins: the tick waits for
// it, sees the pause and sends nothing.
func TestScheduledReportPauseDuringTheClaim(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	r, tick := e.createDaily(30 * time.Minute)
	job := e.claimJob(ctx, r.ID, tick, time.Minute)

	held, release := make(chan struct{}), make(chan struct{})
	paused := make(chan error, 1)
	go func() {
		_, err := writeReport(ctx, e.db, r, nil, func(tx store.StoreOps) error {
			close(held)
			<-release
			return tx.PauseReport(ctx, r.ID, models.ReportPauseUser)
		})
		paused <- err
	}()
	<-held
	ran := make(chan error, 1)
	go func() { ran <- e.rr.runScheduled(ctx, job) }()
	time.Sleep(200 * time.Millisecond)
	close(release)
	if err := <-paused; err != nil {
		t.Fatal(err)
	}
	if err := <-ran; err != nil {
		t.Fatal(err)
	}
	runs := e.runs(r.ID)
	if len(runs) != 1 || runs[0].Status != models.ReportRunSkipped || runs[0].Reason != models.ReportReasonPaused {
		t.Fatalf("runs = %+v, want one skipped/paused tick", runs)
	}
	if n := len(e.mailer.messages()); n != 0 {
		t.Errorf("messages = %d, want none after the pause", n)
	}
}

// TestScheduledReportBusy: a tick that finds a manual run in flight is
// recorded as skipped with reason busy.
func TestScheduledReportBusy(t *testing.T) {
	const skipped = `logchef_report_runs_total{trigger="schedule",status="skipped"}`
	skippedBefore := counter(skipped)
	e := newReportEnv(t)
	ctx := context.Background()
	r, tick := e.createDaily(30 * time.Minute)
	if _, err := e.rr.StartManualRun(ctx, access.SessionPrincipal(e.owner), r.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.rr.runScheduled(ctx, e.claimJob(ctx, r.ID, tick, time.Minute)); err != nil {
		t.Fatal(err)
	}
	var tickRun *models.ReportRun
	for _, run := range e.runs(r.ID) {
		if run.Trigger == models.ReportTriggerSchedule {
			tickRun = run
		}
	}
	if tickRun == nil || tickRun.Status != models.ReportRunSkipped || tickRun.Reason != models.ReportReasonBusy || !tickRun.ScheduledFor.Equal(tick) {
		t.Fatalf("tick run = %+v, want skipped/busy for %v", tickRun, tick)
	}
	if got := counter(skipped); got != skippedBefore+1 {
		t.Errorf("%s = %d, want %d", skipped, got, skippedBefore+1)
	}
}

// TestScheduledReportLeaseTakenOver: a run whose claim another replica took
// over writes nothing; the current claim runs the tick once, and a second
// start of the same tick, as from a stalled process, ends without work.
func TestScheduledReportLeaseTakenOver(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	r, tick := e.createDaily(30 * time.Minute)
	stale := e.claimJob(ctx, r.ID, tick, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	current := e.claimJob(ctx, r.ID, tick, time.Minute)
	if current.LeaseToken == stale.LeaseToken {
		t.Fatalf("the second claim kept lease token %d", stale.LeaseToken)
	}

	if err := e.rr.runScheduled(ctx, stale); err != nil {
		t.Fatal(err)
	}
	if runs := e.runs(r.ID); len(runs) != 0 {
		t.Fatalf("the stale claim wrote runs %+v", runs)
	}
	for range 2 {
		if err := e.rr.runScheduled(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	if runs := e.runs(r.ID); len(runs) != 1 || runs[0].Status != models.ReportRunSent || !runs[0].ScheduledFor.Equal(tick) {
		t.Fatalf("runs = %+v, want one sent run for the tick", runs)
	}
	if n := len(e.mailer.messages()); n != 1 {
		t.Errorf("messages = %d, want 1", n)
	}
}

// TestScheduledReportStaleClaimCannotFinish: a claim taken over during the
// run leaves the run row running, for cleanup to interrupt, and sends no
// pause.
func TestScheduledReportStaleClaimCannotFinish(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	r, tick := e.createDaily(30 * time.Minute)
	job := e.claimJob(ctx, r.ID, tick, 200*time.Millisecond)
	e.query = func(ctx context.Context, _ datasource.QueryRequest) (*models.QueryResult, error) {
		time.Sleep(300 * time.Millisecond)
		e.claimJob(ctx, r.ID, tick, time.Minute)
		return nil, errors.New("query failed")
	}
	if err := e.rr.runScheduled(ctx, job); err != nil {
		t.Fatal(err)
	}
	runs := e.runs(r.ID)
	if len(runs) != 1 || runs[0].Status != models.ReportRunRunning {
		t.Fatalf("runs = %+v, want the claimed run left running", runs)
	}
	if got := e.report(r.ID); got.ConsecutiveFailures != 0 {
		t.Errorf("failure streak = %d, want the stale run to count nothing", got.ConsecutiveFailures)
	}
}

// TestScheduledReportOwnerInactive: a tick whose owner is inactive is
// skipped, pauses the report and disables its job; a claim by another user
// enables it again.
func TestScheduledReportOwnerInactive(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	r, _ := e.createDaily(30 * time.Minute)
	e.owner.Status = models.UserStatusInactive
	if err := e.db.UpdateUser(ctx, e.owner); err != nil {
		t.Fatal(err)
	}
	e.schedule(r.ID)
	runs := e.runs(r.ID)
	if len(runs) != 1 || runs[0].Status != models.ReportRunSkipped || runs[0].Reason != models.ReportReasonOwnerInactive {
		t.Fatalf("runs = %+v, want one skipped/owner_inactive", runs)
	}
	if got := e.report(r.ID); got.State != models.ReportStatePaused || got.PauseReason != models.ReportPauseOwnerInactive {
		t.Errorf("report = %s/%s, want paused/owner_inactive", got.State, got.PauseReason)
	}
	if e.job(r.ID).Enabled {
		t.Error("the job of a report paused for an inactive owner is enabled")
	}

	member := newTestUser(t, e.db, "member@example.com", "Member")
	if err := e.db.AddTeamMember(ctx, e.team.ID, member.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ClaimReport(ctx, e.db, access.SessionPrincipal(member), r.ID); err != nil {
		t.Fatal(err)
	}
	if !e.job(r.ID).Enabled {
		t.Error("the job of a claimed report is disabled")
	}
}

// TestScheduledReportSlowPanel: on a dashboard, a panel still running at
// the query deadline fails with timeout and the run is sent as partial.
func TestScheduledReportSlowPanel(t *testing.T) {
	e := newDashboardReportEnv(t)
	e.rr.QueryBudget = 300 * time.Millisecond
	fast := e.query
	e.query = func(ctx context.Context, req datasource.QueryRequest) (*models.QueryResult, error) {
		if strings.Contains(req.RawQuery, "status, host") {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return fast(ctx, req)
	}
	in := e.input("")
	var tick time.Time
	in.Schedule, tick = dailyTick(30 * time.Minute)
	saved, err := CreateReport(context.Background(), e.db, access.SessionPrincipal(e.owner), in)
	if err != nil {
		t.Fatal(err)
	}
	r := saved.Report
	e.setJobDue(r.ID, tick)
	e.schedule(r.ID)

	runs := e.runs(r.ID)
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	run := runs[0]
	if run.Status != models.ReportRunPartial || run.Reason != models.ReportReasonPanelFailed {
		t.Errorf("run = %s/%s, want partial/panel_failed", run.Status, run.Reason)
	}
	if got := panelStatuses(run); !strings.Contains(got, "table:failed") || strings.Count(got, ":failed") != 1 {
		t.Errorf("panels = %s, want only the slow table failed", got)
	}
	for _, p := range run.Panels {
		if p.PanelID == "table" && !strings.Contains(p.Error, "timeout") {
			t.Errorf("slow panel error = %q, want timeout", p.Error)
		}
	}
	if n := len(e.mailer.messages()); n != 1 {
		t.Errorf("messages = %d, want 1", n)
	}
}

// TestSyncReportJobs gives a report without a job one and leaves an
// existing job as it is.
func TestSyncReportJobs(t *testing.T) {
	e := newReportEnv(t)
	ctx := context.Background()
	withJob, tick := e.createDaily(30 * time.Minute)
	withoutJob := e.create()
	if err := e.db.DeleteJob(ctx, string(reports.JobKind), strconv.FormatInt(int64(withoutJob.ID), 10)); err != nil {
		t.Fatal(err)
	}
	if err := SyncReportJobs(ctx, e.db); err != nil {
		t.Fatal(err)
	}
	if job := e.job(withoutJob.ID); job.ScheduleExpr != "0 9 * * *" || !job.Enabled || !job.NextRunAt.After(time.Now()) {
		t.Errorf("synced job = %+v next %v, want 0 9 * * *, enabled, due at its next tick", job.JobDefinition, job.NextRunAt)
	}
	if job := e.job(withJob.ID); !job.NextRunAt.Equal(tick.Add(-24 * time.Hour)) {
		t.Errorf("existing job next run = %v, want it kept at %v", job.NextRunAt, tick.Add(-24*time.Hour))
	}
}
