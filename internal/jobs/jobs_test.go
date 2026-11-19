package jobs

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

// minutely fires on every minute boundary, like the finest cron schedule. It
// counts calls to Next so a test can bound the search.
type minutely struct{ calls *int }

func (m minutely) Next(t time.Time) time.Time {
	*m.calls++
	return t.Truncate(time.Minute).Add(time.Minute)
}

func (minutely) stored() (kind, expr, timezone string, err error) {
	return "", "", "", errors.New("test schedule")
}

// monthly fires every 30 days from the Unix epoch, less often than the
// search window.
type monthly struct{}

const thirtyDays = 30 * 24 * time.Hour

func (monthly) Next(t time.Time) time.Time {
	sinceEpoch := t.Sub(time.Unix(0, 0))
	return time.Unix(0, 0).UTC().Add(sinceEpoch - sinceEpoch%thirtyDays + thirtyDays)
}

func (monthly) stored() (kind, expr, timezone string, err error) {
	return "", "", "", errors.New("test schedule")
}

func TestLatestDue(t *testing.T) {
	next := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

	t.Run("interval after a gap of 100 intervals", func(t *testing.T) {
		got, err := latestDue(Every(time.Minute), next, next.Add(100*time.Minute+30*time.Second), 1)
		if err != nil {
			t.Fatal(err)
		}
		if want := next.Add(100 * time.Minute); !got.Equal(want) {
			t.Errorf("At = %v, want %v", got, want)
		}
	})
	t.Run("interval exactly on an occurrence", func(t *testing.T) {
		got, err := latestDue(Every(time.Minute), next, next.Add(3*time.Minute), 1)
		if err != nil || !got.Equal(next.Add(3*time.Minute)) {
			t.Errorf("At = %v, %v, want %v", got, err, next.Add(3*time.Minute))
		}
	})
	t.Run("interval of 10 seconds a month behind is not capped", func(t *testing.T) {
		got, err := latestDue(Every(10*time.Second), next, next.Add(thirtyDays+5*time.Second), maxOccurrences)
		if err != nil || !got.Equal(next.Add(thirtyDays)) {
			t.Errorf("At = %v, %v, want %v", got, err, next.Add(thirtyDays))
		}
	})
	t.Run("walk from next_run_at", func(t *testing.T) {
		calls := 0
		got, err := latestDue(minutely{&calls}, next, next.Add(3*time.Minute+10*time.Second), maxOccurrences)
		if err != nil || !got.Equal(next.Add(3*time.Minute)) {
			t.Errorf("At = %v, %v, want %v", got, err, next.Add(3*time.Minute))
		}
	})
	t.Run("walk 60 days behind starts seven days back", func(t *testing.T) {
		calls := 0
		now := next.Add(60*24*time.Hour + 17*time.Second)
		got, err := latestDue(minutely{&calls}, next, now, maxOccurrences)
		if err != nil || !got.Equal(now.Truncate(time.Minute)) {
			t.Errorf("At = %v, %v, want %v", got, err, now.Truncate(time.Minute))
		}
		// The window holds 10,080 minutes; one more call finds the first
		// window occurrence and one passes now.
		if calls > 10_080+2 {
			t.Errorf("Next called %d times, want at most %d", calls, 10_080+2)
		}
	})
	t.Run("sparse schedule older than the window walks from next_run_at", func(t *testing.T) {
		occurrence := monthly{}.Next(next)
		got, err := latestDue(monthly{}, occurrence, occurrence.Add(8*24*time.Hour), maxOccurrences)
		if err != nil || !got.Equal(occurrence) {
			t.Errorf("At = %v, %v, want %v", got, err, occurrence)
		}
	})
	t.Run("cap", func(t *testing.T) {
		calls := 0
		_, err := latestDue(minutely{&calls}, next, next.Add(3*time.Hour), 100)
		if !errors.Is(err, errSearchCap) {
			t.Fatalf("err = %v, want errSearchCap", err)
		}
		if calls > 100 {
			t.Errorf("Next called %d times past a cap of 100", calls)
		}
	})
}

func TestTruncateError(t *testing.T) {
	if got := truncateError("short"); got != "short" {
		t.Errorf("short error changed to %q", got)
	}
	long := strings.Repeat("é", 400) // 800 bytes
	got := truncateError(long)
	if len(got) > maxErrorBytes || !strings.HasSuffix(got, " [truncated]") {
		t.Errorf("truncated to %d bytes, suffix %q", len(got), got[len(got)-12:])
	}
	if !strings.HasPrefix(long, strings.TrimSuffix(got, " [truncated]")) || !utf8Valid(got) {
		t.Errorf("truncation broke a rune: %q", got)
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "�") == s }

func TestUpsertRejectsInvalidSpecs(t *testing.T) {
	db, _ := openSQLite(t)
	ctx := context.Background()
	valid := Spec{Kind: "test", Key: "k", Schedule: Every(time.Minute), Misfire: RunOnce, Enabled: true}
	cases := map[string]func(*Spec){
		"no kind":           func(s *Spec) { s.Kind = "" },
		"no key":            func(s *Spec) { s.Key = "" },
		"no schedule":       func(s *Spec) { s.Schedule = nil },
		"zero interval":     func(s *Spec) { s.Schedule = Every(0) },
		"fractional second": func(s *Spec) { s.Schedule = Every(1500 * time.Millisecond) },
		"unknown misfire":   func(s *Spec) { s.Misfire = "later" },
		"negative grace":    func(s *Spec) { s.Grace = -time.Second },
		"fractional grace":  func(s *Spec) { s.Grace = time.Millisecond },
	}
	for name, mutate := range cases {
		spec := valid
		mutate(&spec)
		if err := Upsert(ctx, db, spec); err == nil {
			t.Errorf("%s: Upsert accepted %+v", name, spec)
		}
	}
	if err := Upsert(ctx, db, valid); err != nil {
		t.Fatalf("valid spec: %v", err)
	}
}

// TestRunnerCatchUp is the time test: after a gap of 100 intervals the
// handler runs once with At set to the latest due occurrence, and the next
// run is the occurrence after At.
func TestRunnerCatchUp(t *testing.T) {
	db, raw := openSQLite(t)
	ctx := context.Background()
	upsert(t, db, Spec{Kind: "catchup", Key: "a", Schedule: Every(time.Minute), Misfire: RunOnce, Enabled: true})
	stored := backdate(t, raw, "catchup", "a", 100*time.Minute+30*time.Second)

	rec := &recorder{}
	r := newRunner(db)
	r.Register(KindSpec{Kind: "catchup", Handler: rec.handle, Timeout: time.Minute, Concurrency: 1})
	r.Start(ctx)
	waitFor(t, func() bool { return rec.count() == 1 && leaseCleared(t, db, "catchup", "a") })
	stop(t, r)

	job := rec.jobs()[0]
	if want := stored.Add(100 * time.Minute); !job.At.Equal(want) {
		t.Errorf("At = %v, want %v (next_run_at %v)", job.At, want, stored)
	}
	if lag := job.ClaimedAt.Sub(job.At); lag < 0 || lag >= time.Minute {
		t.Errorf("ClaimedAt - At = %v, want within one interval", lag)
	}
	got := getJob(t, db, "catchup", "a")
	if want := job.At.Add(time.Minute); !got.NextRunAt.Equal(want) {
		t.Errorf("next_run_at = %v, want %v", got.NextRunAt, want)
	}
	if got.LastStatus != "ok" || got.LastError != "" || got.LastFinishedAt == nil {
		t.Errorf("last run = %q %q %v, want ok", got.LastStatus, got.LastError, got.LastFinishedAt)
	}
	if rec.count() != 1 {
		t.Errorf("handler ran %d times, want once", rec.count())
	}
}

func TestRunnerSkipPolicy(t *testing.T) {
	db, raw := openSQLite(t)
	ctx := context.Background()
	// Each job is 3 intervals and 30 seconds behind, so its latest due
	// occurrence is 30 seconds old.
	upsert(t, db, Spec{Kind: "skip", Key: "late", Schedule: Every(time.Minute), Misfire: Skip, Grace: 10 * time.Second, Enabled: true})
	upsert(t, db, Spec{Kind: "skip", Key: "in-grace", Schedule: Every(time.Minute), Misfire: Skip, Grace: time.Minute, Enabled: true})
	late := backdate(t, raw, "skip", "late", 3*time.Minute+30*time.Second)
	backdate(t, raw, "skip", "in-grace", 3*time.Minute+30*time.Second)

	rec := &recorder{}
	r := newRunner(db)
	r.Register(KindSpec{Kind: "skip", Handler: rec.handle, Timeout: time.Minute, Concurrency: 2})
	r.Start(ctx)
	waitFor(t, func() bool {
		return rec.count() == 1 && leaseCleared(t, db, "skip", "late") && leaseCleared(t, db, "skip", "in-grace")
	})
	stop(t, r)

	if key := rec.jobs()[0].Key; key != "in-grace" {
		t.Errorf("handler ran for %q, want only in-grace", key)
	}
	got := getJob(t, db, "skip", "late")
	if got.LastStatus != "skipped" {
		t.Errorf("late job status = %q, want skipped", got.LastStatus)
	}
	if want := late.Add(4 * time.Minute); !got.NextRunAt.Equal(want) {
		t.Errorf("skipped job next_run_at = %v, want the occurrence after At, %v", got.NextRunAt, want)
	}
}

func TestRunnerHandlerOutcomes(t *testing.T) {
	db, _ := openSQLite(t)
	ctx := context.Background()
	for _, key := range []string{"gone", "fails", "panics"} {
		upsert(t, db, Spec{Kind: "outcome", Key: key, Schedule: Every(time.Hour), Misfire: RunOnce, Enabled: true})
	}
	ran := make(chan Job, 3)
	r := newRunner(db)
	r.Register(KindSpec{Kind: "outcome", Timeout: time.Minute, Concurrency: 3, Handler: func(_ context.Context, job Job) error {
		ran <- job
		switch job.Key {
		case "gone":
			return ErrGone
		case "fails":
			return errors.New(strings.Repeat("x", 600))
		default:
			panic("boom")
		}
	}})
	r.Start(ctx)
	ats := map[string]time.Time{}
	for range 3 {
		job := <-ran
		ats[job.Key] = job.At
	}
	waitFor(t, func() bool {
		return leaseCleared(t, db, "outcome", "fails") && leaseCleared(t, db, "outcome", "panics")
	})
	waitFor(t, func() bool {
		_, err := db.GetJob(ctx, "outcome", "gone")
		return errors.Is(err, models.ErrNotFound)
	})
	stop(t, r)

	failed := getJob(t, db, "outcome", "fails")
	if failed.LastStatus != "error" || len(failed.LastError) > maxErrorBytes || !strings.HasSuffix(failed.LastError, "[truncated]") {
		t.Errorf("failed job = %q, %d bytes of error %q", failed.LastStatus, len(failed.LastError), failed.LastError)
	}
	if want := ats["fails"].Add(time.Hour); !failed.NextRunAt.Equal(want) {
		t.Errorf("a handler error changed the schedule: next_run_at = %v, want %v", failed.NextRunAt, want)
	}
	panicked := getJob(t, db, "outcome", "panics")
	if panicked.LastStatus != "error" || !strings.Contains(panicked.LastError, "panicked: boom") {
		t.Errorf("panicking job = %q %q", panicked.LastStatus, panicked.LastError)
	}
}

// TestStopLeases is the shutdown test: Stop clears the lease of a handler
// that returned and keeps the lease of one that did not.
func TestStopLeases(t *testing.T) {
	db, _ := openSQLite(t)
	ctx := context.Background()
	upsert(t, db, Spec{Kind: "shutdown", Key: "returns", Schedule: Every(time.Hour), Misfire: RunOnce, Enabled: true})
	upsert(t, db, Spec{Kind: "shutdown", Key: "stuck", Schedule: Every(time.Hour), Misfire: RunOnce, Enabled: true})
	before := getJob(t, db, "shutdown", "returns").NextRunAt

	started := make(chan string, 2)
	unstick := make(chan struct{})
	r := newRunner(db)
	r.Register(KindSpec{Kind: "shutdown", Timeout: time.Minute, Concurrency: 2, Handler: func(ctx context.Context, job Job) error {
		started <- job.Key
		if job.Key == "stuck" {
			<-unstick
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}})
	r.Start(ctx)
	<-started
	<-started

	stopCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	if err := r.Stop(stopCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v, want the deadline error for the stuck handler", err)
	}

	returned := getJob(t, db, "shutdown", "returns")
	if returned.LeaseOwner != "" || returned.LeaseExpiresAt != nil {
		t.Errorf("returned handler kept its lease: owner %q", returned.LeaseOwner)
	}
	if !returned.NextRunAt.Equal(before) || returned.LastStatus != "" {
		t.Errorf("a run ended by shutdown was recorded: next_run_at %v (was %v), status %q", returned.NextRunAt, before, returned.LastStatus)
	}
	if stuck := getJob(t, db, "shutdown", "stuck"); stuck.LeaseOwner != r.owner {
		t.Errorf("running handler lost its lease: owner %q, want %q", stuck.LeaseOwner, r.owner)
	}

	close(unstick)
	r.runs.Wait()
	if stuck := getJob(t, db, "shutdown", "stuck"); stuck.LeaseOwner != r.owner || stuck.LastStatus != "" {
		t.Errorf("a handler that returned after Stop gave up wrote its outcome: owner %q, status %q", stuck.LeaseOwner, stuck.LastStatus)
	}
}

// TestNoWriteAfterStop races a handler's return against Stop's deadline:
// whichever wins, the job row must not change after Stop returns.
func TestNoWriteAfterStop(t *testing.T) {
	db, _ := openSQLite(t)
	ctx := context.Background()
	for i := range 20 {
		key := strconv.Itoa(i)
		upsert(t, db, Spec{Kind: "boundary", Key: key, Schedule: Every(time.Hour), Misfire: RunOnce, Enabled: true})
		stopCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
		started := make(chan struct{})
		r := newRunner(db)
		r.Register(KindSpec{Kind: "boundary", Timeout: time.Minute, Concurrency: 1, Handler: func(_ context.Context, _ Job) error {
			close(started)
			<-stopCtx.Done()
			return nil
		}})
		r.Start(ctx)
		<-started
		_ = r.Stop(stopCtx)
		atStop := getJob(t, db, "boundary", key)
		r.runs.Wait()
		cancel()
		if after := getJob(t, db, "boundary", key); !after.UpdatedAt.Equal(atStop.UpdatedAt) || after.LastStatus != atStop.LastStatus {
			t.Fatalf("run %d wrote after Stop returned: status %q -> %q", i, atStop.LastStatus, after.LastStatus)
		}
	}
}

func TestStopWaitsForHandlers(t *testing.T) {
	db, _ := openSQLite(t)
	ctx := context.Background()
	upsert(t, db, Spec{Kind: "graceful", Key: "a", Schedule: Every(time.Hour), Misfire: RunOnce, Enabled: true})
	started := make(chan struct{})
	r := newRunner(db)
	r.Register(KindSpec{Kind: "graceful", Timeout: time.Minute, Concurrency: 1, Handler: func(ctx context.Context, _ Job) error {
		close(started)
		<-ctx.Done()
		return nil // finished its work despite the cancellation
	}})
	r.Start(ctx)
	<-started
	stop(t, r)
	got := getJob(t, db, "graceful", "a")
	if got.LeaseOwner != "" || got.LastStatus != "ok" {
		t.Errorf("after Stop: owner %q, status %q, want no lease and ok", got.LeaseOwner, got.LastStatus)
	}
}

func TestKickWakesTheLoop(t *testing.T) {
	db, _ := openSQLite(t)
	ctx := context.Background()
	upsert(t, db, Spec{Kind: "kick", Key: "a", Schedule: Every(time.Hour), Misfire: RunOnce, Enabled: true})
	rec := &recorder{}
	r := newRunner(db)
	r.Register(KindSpec{Kind: "kick", Handler: rec.handle, Timeout: time.Minute, Concurrency: 1})
	r.Start(ctx)
	waitFor(t, func() bool { return rec.count() == 1 && leaseCleared(t, db, "kick", "a") })

	if err := r.Kick(ctx, "kick", "a"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return rec.count() == 2 })
	if err := r.Kick(ctx, "kick", "missing"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("Kick(missing) = %v, want ErrNotFound", err)
	}
	stop(t, r)
	if first, second := rec.jobs()[0].At, rec.jobs()[1].At; !second.After(first) || second.Sub(first) >= time.Hour {
		t.Errorf("kicked run At = %v, first At = %v; want the kick time", second, first)
	}
}

// A kick that lands after a run records its finish but before the run frees
// its slot still runs the job before the next poll.
func TestKickWhileSlotIsBusyRunsAfterTheSlotFrees(t *testing.T) {
	db, _ := openSQLite(t)
	ctx := context.Background()
	upsert(t, db, Spec{Kind: "busy", Key: "a", Schedule: Every(time.Hour), Misfire: RunOnce, Enabled: true})
	rec := &recorder{}
	hooked := &kickAfterFinish{JobStore: db}
	r := newRunner(hooked)
	hooked.kick = func() {
		if err := r.Kick(ctx, "busy", "a"); err != nil {
			t.Error(err)
		}
		// Hold the slot until the loop has taken the wake and tried to claim.
		waitFor(t, func() bool { return len(r.wake) == 0 })
		time.Sleep(50 * time.Millisecond)
	}
	r.Register(KindSpec{Kind: "busy", Handler: rec.handle, Timeout: time.Minute, Concurrency: 1})
	r.Start(ctx)
	defer stop(t, r)
	waitFor(t, func() bool { return rec.count() == 2 })
}

// kickAfterFinish runs kick once, right after the first FinishJob commits,
// while the finishing run still holds its slot.
type kickAfterFinish struct {
	store.JobStore
	once sync.Once
	kick func()
}

func (k *kickAfterFinish) FinishJob(ctx context.Context, id, token int64, res models.JobRunResult) (bool, error) {
	finished, err := k.JobStore.FinishJob(ctx, id, token, res)
	k.once.Do(k.kick)
	return finished, err
}

// --- helpers ---

type recorder struct {
	mu  sync.Mutex
	got []Job
}

func (r *recorder) handle(_ context.Context, job Job) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, job)
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *recorder) jobs() []Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Job(nil), r.got...)
}

func openSQLite(t *testing.T) (store.Store, *sql.DB) {
	t.Helper()
	path := sqlitetest.Path(t)
	db, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: path},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return db, raw
}

// newRunner polls once an hour, so during a test it claims only at Start and
// on Kick.
func newRunner(db store.JobStore) *Runner {
	return New(Options{Store: db, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), PollInterval: time.Hour})
}

func upsert(t *testing.T, db store.Store, spec Spec) {
	t.Helper()
	if err := db.WithTx(context.Background(), func(tx store.StoreOps) error { return Upsert(context.Background(), tx, spec) }); err != nil {
		t.Fatal(err)
	}
}

// backdate moves the job's next run into the past and returns it as stored.
func backdate(t *testing.T, raw *sql.DB, kind, key string, by time.Duration) time.Time {
	t.Helper()
	at := time.Now().UTC().Add(-by).Truncate(time.Millisecond)
	if _, err := raw.Exec(`UPDATE jobs SET next_run_at = ? WHERE kind = ? AND key = ?`, at.Format("2006-01-02T15:04:05.000Z"), kind, key); err != nil {
		t.Fatal(err)
	}
	return at
}

func getJob(t *testing.T, db store.JobStore, kind, key string) *models.ScheduledJob {
	t.Helper()
	job, err := db.GetJob(context.Background(), kind, key)
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func leaseCleared(t *testing.T, db store.JobStore, kind, key string) bool {
	job := getJob(t, db, kind, key)
	return job.LeaseOwner == "" && job.LastFinishedAt != nil
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 10s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func stop(t *testing.T, r *Runner) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}
