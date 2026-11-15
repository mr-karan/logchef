package storetest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// RunJobs exercises the JobStore contract and the job runner's guarantees
// against one database. open must return a new store on the same database
// each time, like a separate replica. rowLocks says the backend locks rows
// (Postgres), so a claim skips a held job instead of waiting for the writer.
func RunJobs(t *testing.T, open Opener, rowLocks bool) {
	t.Run("Definitions", func(t *testing.T) { testJobDefinitions(t, open(t)) })
	t.Run("KickAndDelete", func(t *testing.T) { testJobKickAndDelete(t, open(t)) })
	t.Run("TwoRunnersClaimDisjoint", func(t *testing.T) { testTwoRunnersClaimDisjoint(t, open(t), open(t)) })
	t.Run("TakeoverAfterNewerClaimCommits", func(t *testing.T) { testTakeoverClaimFirst(t, open(t), open(t)) })
	t.Run("TakeoverWhileHoldIsOpen", func(t *testing.T) { testTakeoverHoldFirst(t, open(t), open(t), rowLocks) })
	t.Run("ScheduleChangeDuringRun", func(t *testing.T) { testScheduleChangeDuringRun(t, open(t)) })
}

func testJobDefinitions(t *testing.T, s store.Store) {
	ctx := context.Background()
	def := models.JobDefinition{Kind: "defs", Key: "a", ScheduleKind: "interval", ScheduleExpr: "60", Timezone: "UTC", Misfire: "run_once", Enabled: true}
	if err := s.UpsertJob(ctx, def); err != nil {
		t.Fatal(err)
	}
	created := mustGetJob(t, s, "defs", "a")
	if created.JobDefinition != def || created.LeaseToken != 0 || created.LeaseOwner != "" || created.LastStatus != "" {
		t.Fatalf("created job = %+v", created)
	}
	if time.Since(created.NextRunAt).Abs() > time.Minute {
		t.Errorf("new job next_run_at = %v, want due now", created.NextRunAt)
	}

	claimed := claimOne(t, s, "defs", "owner-a", time.Minute)
	later := created.NextRunAt.Add(time.Hour).Truncate(time.Millisecond)
	if ok, err := s.FinishJob(ctx, claimed.ID, claimed.LeaseToken, finished(claimed, later)); err != nil || !ok {
		t.Fatalf("FinishJob = %v, %v", ok, err)
	}

	def.Misfire, def.GraceSeconds, def.Enabled = "skip", 30, false
	if err := s.UpsertJob(ctx, def); err != nil {
		t.Fatal(err)
	}
	kept := mustGetJob(t, s, "defs", "a")
	if kept.JobDefinition != def || !kept.NextRunAt.Equal(later) || kept.ID != created.ID || kept.LeaseToken != 1 {
		t.Errorf("an update without a schedule change = %+v, want the definition and next_run_at %v kept", kept, later)
	}
	if got, err := s.ClaimJobs(ctx, "defs", "owner-a", time.Minute, 10); err != nil || len(got) != 0 {
		t.Errorf("claimed a disabled job: %v, %v", got, err)
	}

	def.ScheduleExpr = "120"
	if err := s.UpsertJob(ctx, def); err != nil {
		t.Fatal(err)
	}
	if moved := mustGetJob(t, s, "defs", "a"); !moved.NextRunAt.Before(later) {
		t.Errorf("a schedule change kept next_run_at %v, want due now", moved.NextRunAt)
	}

}

func testJobKickAndDelete(t *testing.T, s store.Store) {
	ctx := context.Background()
	upsertJob(t, s, jobs.Spec{Kind: "kick", Key: "a", Schedule: jobs.Every(time.Hour), Misfire: jobs.RunOnce, Enabled: true})
	claimed := claimOne(t, s, "kick", "owner-a", time.Minute)
	later := claimed.NextRunAt.Add(time.Hour).Truncate(time.Millisecond)
	if ok, err := s.FinishJob(ctx, claimed.ID, claimed.LeaseToken, finished(claimed, later)); err != nil || !ok {
		t.Fatalf("FinishJob = %v, %v", ok, err)
	}
	if err := s.KickJob(ctx, "kick", "a"); err != nil {
		t.Fatal(err)
	}
	if kicked := claimOne(t, s, "kick", "owner-a", time.Minute); kicked.ID != claimed.ID || !kicked.NextRunAt.Before(later) {
		t.Errorf("kicked job = %+v, want it due now", kicked)
	}
	if err := s.KickJob(ctx, "kick", "missing"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("KickJob(missing) = %v, want ErrNotFound", err)
	}
	if err := s.DeleteJob(ctx, "kick", "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteJob(ctx, "kick", "a"); err != nil {
		t.Errorf("deleting a missing job: %v", err)
	}
	if _, err := s.GetJob(ctx, "kick", "a"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetJob after delete = %v, want ErrNotFound", err)
	}
}

// testTwoRunnersClaimDisjoint runs two runners with different owners, each
// on its own connection, over 200 due jobs. No job may be handed to both.
func testTwoRunnersClaimDisjoint(t *testing.T, a, b store.Store) {
	ctx := context.Background()
	const total = 200
	for i := range total {
		upsertJob(t, a, jobs.Spec{Kind: "disjoint", Key: strconv.Itoa(i), Schedule: jobs.Every(time.Hour), Misfire: jobs.RunOnce, Enabled: true})
	}

	var mu sync.Mutex
	runsByJob := map[int64][]string{}
	all := make(chan struct{})
	newRunner := func(s store.Store, name string) *jobs.Runner {
		r := jobs.New(jobs.Options{Store: s, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), PollInterval: 5 * time.Millisecond})
		r.Register(jobs.KindSpec{Kind: "disjoint", Timeout: time.Minute, Concurrency: 4, Handler: func(_ context.Context, job jobs.Job) error {
			mu.Lock()
			defer mu.Unlock()
			runsByJob[job.ID] = append(runsByJob[job.ID], name)
			if len(runsByJob) == total {
				close(all)
			}
			return nil
		}})
		return r
	}
	runnerA, runnerB := newRunner(a, "a"), newRunner(b, "b")
	runnerA.Start(ctx)
	runnerB.Start(ctx)
	select {
	case <-all:
	case <-time.After(60 * time.Second):
		t.Error("not every job ran within 60s")
	}
	// Further polls must not run any job again.
	time.Sleep(50 * time.Millisecond)
	for _, r := range []*jobs.Runner{runnerA, runnerB} {
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := r.Stop(stopCtx); err != nil {
			t.Errorf("Stop: %v", err)
		}
		cancel()
	}

	mu.Lock()
	defer mu.Unlock()
	perOwner := map[string]int{}
	for id, owners := range runsByJob {
		if len(owners) != 1 {
			t.Errorf("job %d ran %d times, by %v", id, len(owners), owners)
		}
		perOwner[owners[0]]++
	}
	if len(runsByJob) != total {
		t.Errorf("%d jobs ran, want %d", len(runsByJob), total)
	}
	t.Logf("runs per owner: %v", perOwner)
	for i := range total {
		if got := mustGetJob(t, a, "disjoint", strconv.Itoa(i)); got.LeaseToken != 1 {
			t.Errorf("job %d was claimed %d times", got.ID, got.LeaseToken)
		}
	}
}

// testTakeoverClaimFirst: run A holds a job past its lease, run B claims it
// and commits its state, then A's write transaction fails at Hold.
func testTakeoverClaimFirst(t *testing.T, a, b store.Store) {
	ctx := context.Background()
	upsertJob(t, a, jobs.Spec{Kind: "takeover-1", Key: "k", Schedule: jobs.Every(time.Minute), Misfire: jobs.RunOnce, Enabled: true})
	runA := claimOne(t, a, "takeover-1", "owner-a", 50*time.Millisecond)
	waitLeaseExpiry()

	runB := claimOne(t, b, "takeover-1", "owner-b", time.Minute)
	if runB.ID != runA.ID || runB.LeaseToken != runA.LeaseToken+1 {
		t.Fatalf("B claimed %d/%d after A's %d/%d", runB.ID, runB.LeaseToken, runA.ID, runA.LeaseToken)
	}
	if err := writeHeldState(ctx, b, runB, "b"); err != nil {
		t.Fatalf("B's write: %v", err)
	}
	if err := writeHeldState(ctx, a, runA, "a"); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatalf("A's write after B's claim = %v, want ErrLeaseLost", err)
	}
	assertHeldState(t, a, "takeover-1", "b")
	if ok, err := a.FinishJob(ctx, runA.ID, runA.LeaseToken, finished(runA, time.Now())); err != nil || ok {
		t.Errorf("A's completion after B's claim = %v, %v, want no row", ok, err)
	}
	if got := mustGetJob(t, a, "takeover-1", "k"); got.LeaseOwner != "owner-b" {
		t.Errorf("lease owner = %q, want owner-b", got.LeaseOwner)
	}
}

// testTakeoverHoldFirst: run A holds a job past its lease and is inside its
// write transaction after Hold when B tries to claim. B does not get the job
// until A commits; once B holds it, A's next transaction fails at Hold. With
// row locks (Postgres) B's claim returns at once with the other due job; on
// SQLite it waits for A's write lock.
func testTakeoverHoldFirst(t *testing.T, a, b store.Store, rowLocks bool) {
	ctx := context.Background()
	upsertJob(t, a, jobs.Spec{Kind: "takeover-2", Key: "held", Schedule: jobs.Every(time.Minute), Misfire: jobs.RunOnce, Enabled: true})
	runA := claimOne(t, a, "takeover-2", "owner-a", 50*time.Millisecond)
	upsertJob(t, a, jobs.Spec{Kind: "takeover-2", Key: "free", Schedule: jobs.Every(time.Minute), Misfire: jobs.RunOnce, Enabled: true})
	waitLeaseExpiry()

	claimed := make(chan claimResult, 1)
	var runB *models.ScheduledJob
	err := a.WithTx(ctx, func(tx store.StoreOps) error {
		if err := jobs.Hold(ctx, tx, jobs.Job{ID: runA.ID, LeaseToken: runA.LeaseToken}); err != nil {
			return err
		}
		go func() {
			got, err := b.ClaimJobs(ctx, "takeover-2", "owner-b", time.Minute, 2)
			claimed <- claimResult{got, err}
		}()
		if err := claimDuringHold(claimed, rowLocks); err != nil {
			return err
		}
		return putHeldState(ctx, tx, "takeover-2", "a")
	})
	if err != nil {
		t.Fatalf("A's transaction: %v", err)
	}
	assertHeldState(t, a, "takeover-2", "a")

	if rowLocks {
		runB = claimOne(t, b, "takeover-2", "owner-b", time.Minute)
	} else {
		res := <-claimed
		if res.err != nil || len(res.jobs) != 2 {
			t.Fatalf("the claim that waited for A's commit returned %v, %v; want both jobs", res.jobs, res.err)
		}
		for _, job := range res.jobs {
			if job.Key == "held" {
				runB = job
			}
		}
	}
	if runB == nil || runB.ID != runA.ID || runB.LeaseToken != runA.LeaseToken+1 {
		t.Fatalf("B's claim = %+v, want job %d with token %d", runB, runA.ID, runA.LeaseToken+1)
	}
	if err := writeHeldState(ctx, b, runB, "b"); err != nil {
		t.Fatalf("B's write: %v", err)
	}
	if err := writeHeldState(ctx, a, runA, "a2"); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatalf("A's second write = %v, want ErrLeaseLost", err)
	}
	assertHeldState(t, a, "takeover-2", "b")
}

type claimResult struct {
	jobs []*models.ScheduledJob
	err  error
}

// claimDuringHold checks B's claim while A's Hold is open. With row locks it
// must return at once with only the free job; on SQLite it must still wait.
func claimDuringHold(claimed <-chan claimResult, rowLocks bool) error {
	if rowLocks {
		select {
		case res := <-claimed:
			if res.err != nil {
				return fmt.Errorf("a claim during A's transaction: %w", res.err)
			}
			if len(res.jobs) != 1 || res.jobs[0].Key != "free" {
				return fmt.Errorf("a claim during A's transaction returned %v; want only the free job", res.jobs)
			}
			return nil
		case <-time.After(5 * time.Second):
			return errors.New("a claim during A's transaction waited for A's row lock")
		}
	}
	select {
	case res := <-claimed:
		return fmt.Errorf("a claim returned %v (error %v) while A held the write lock", res.jobs, res.err != nil)
	case <-time.After(300 * time.Millisecond):
		return nil
	}
}

// testScheduleChangeDuringRun: a run that finishes after its job's schedule
// changed records its outcome and releases the lease, and keeps the due time
// the change set.
func testScheduleChangeDuringRun(t *testing.T, s store.Store) {
	ctx := context.Background()
	upsertJob(t, s, jobs.Spec{Kind: "edit", Key: "a", Schedule: jobs.Every(time.Hour), Misfire: jobs.RunOnce, Enabled: true})
	run := claimOne(t, s, "edit", "owner-a", time.Minute)
	upsertJob(t, s, jobs.Spec{Kind: "edit", Key: "a", Schedule: jobs.Every(10 * time.Second), Misfire: jobs.RunOnce, Enabled: true})
	edited := mustGetJob(t, s, "edit", "a")

	ok, err := s.FinishJob(ctx, run.ID, run.LeaseToken, models.JobRunResult{
		ScheduleKind: run.ScheduleKind, ScheduleExpr: run.ScheduleExpr, Timezone: run.Timezone,
		NextRunAt: run.NextRunAt.Add(time.Hour).Truncate(time.Millisecond), Status: "ok",
	})
	if err != nil || !ok {
		t.Fatalf("FinishJob = %v, %v", ok, err)
	}
	got := mustGetJob(t, s, "edit", "a")
	if !got.NextRunAt.Equal(edited.NextRunAt) {
		t.Errorf("the old run moved next_run_at to %v, want the edit's %v", got.NextRunAt, edited.NextRunAt)
	}
	if got.LeaseOwner != "" || got.LastStatus != "ok" || got.ScheduleExpr != "10" {
		t.Errorf("after the old run: owner %q, status %q, schedule %q", got.LeaseOwner, got.LastStatus, got.ScheduleExpr)
	}
}

// --- helpers ---

// finished is an ok outcome under the job's current schedule.
func finished(job *models.ScheduledJob, next time.Time) models.JobRunResult {
	return models.JobRunResult{ScheduleKind: job.ScheduleKind, ScheduleExpr: job.ScheduleExpr, Timezone: job.Timezone, NextRunAt: next, Status: "ok"}
}

func upsertJob(t *testing.T, s store.Store, spec jobs.Spec) {
	t.Helper()
	ctx := context.Background()
	if err := s.WithTx(ctx, func(tx store.StoreOps) error { return jobs.Upsert(ctx, tx, spec) }); err != nil {
		t.Fatal(err)
	}
}

func mustGetJob(t *testing.T, s store.JobStore, kind, key string) *models.ScheduledJob {
	t.Helper()
	job, err := s.GetJob(context.Background(), kind, key)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func claimOne(t *testing.T, s store.JobStore, kind, owner string, lease time.Duration) *models.ScheduledJob {
	t.Helper()
	got, err := s.ClaimJobs(context.Background(), kind, owner, lease, 10)
	if err != nil || len(got) != 1 {
		t.Fatalf("ClaimJobs(%s, %s) = %v, %v; want one job", kind, owner, got, err)
	}
	if got[0].LeaseOwner != owner || got[0].LastStartedAt == nil {
		t.Fatalf("claimed job = %+v", got[0])
	}
	return got[0]
}

// waitLeaseExpiry outlasts a 50ms lease on the database clock.
func waitLeaseExpiry() { time.Sleep(150 * time.Millisecond) }

// writeHeldState is a handler's write transaction: Hold, then state.
func writeHeldState(ctx context.Context, s store.Store, run *models.ScheduledJob, value string) error {
	return s.WithTx(ctx, func(tx store.StoreOps) error {
		if err := jobs.Hold(ctx, tx, jobs.Job{ID: run.ID, LeaseToken: run.LeaseToken}); err != nil {
			return err
		}
		return putHeldState(ctx, tx, run.Kind, value)
	})
}

func putHeldState(ctx context.Context, tx store.StoreOps, kind, value string) error {
	return tx.UpsertSetting(ctx, "jobs.test."+kind, value, "string", "server", "takeover test state", false)
}

func assertHeldState(t *testing.T, s store.Store, kind, want string) {
	t.Helper()
	got, err := s.GetSetting(context.Background(), "jobs.test."+kind)
	if err != nil {
		t.Fatal(fmt.Errorf("read state: %w", err))
	}
	if got != want {
		t.Errorf("state = %q, want %q", got, want)
	}
}
