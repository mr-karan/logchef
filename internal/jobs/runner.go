// Package jobs runs background jobs on every replica so that one replica
// starts a given tick (RFC 0006). A job is a row in the jobs table. A process
// claims due jobs under a lease, runs the handler registered for the job's
// kind and writes the next run. A handler that writes state calls Hold as the
// first statement of each write transaction, so a run whose claim was taken
// over cannot commit.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// Kind names a handler, for example "alert.evaluate".
type Kind string

// Misfire is the missed-tick policy, applied to the latest due occurrence.
type Misfire string

const (
	// RunOnce runs the latest due occurrence however late the claim is.
	RunOnce Misfire = "run_once"
	// Skip runs the latest due occurrence only when the claim is at most the
	// job's grace after it, and records the run as skipped otherwise.
	Skip Misfire = "skip"
)

// Job is one claimed run.
type Job struct {
	ID         int64 // stable for the life of the job
	Kind       Kind
	Key        string
	At         time.Time // the latest due occurrence, the time this run stands for
	ClaimedAt  time.Time // the database clock at the claim
	LeaseToken int64     // the claim's identity, checked by Hold
}

var (
	// ErrGone is returned by a handler whose object no longer exists. The
	// runner then deletes the job.
	ErrGone = errors.New("jobs: the object of this job no longer exists")
	// ErrLeaseLost is returned by Hold when a later claim owns the job.
	ErrLeaseLost = errors.New("jobs: a later claim owns this job")
)

// Hold proves, inside the handler's write transaction, that this run still
// owns the job. Call it as the first statement of every transaction that
// changes state, with the transaction's handle. It returns ErrLeaseLost when
// a later claim exists; otherwise the job row stays locked until the
// transaction ends, so no claim can take the job over before it commits.
func Hold(ctx context.Context, tx store.JobStore, job Job) error {
	held, err := tx.HoldJob(ctx, job.ID, job.LeaseToken)
	if err != nil {
		return fmt.Errorf("jobs: hold job %d: %w", job.ID, err)
	}
	if !held {
		return ErrLeaseLost
	}
	return nil
}

// HandlerFunc runs one job. Its context ends at the kind's Timeout or when
// the runner stops.
type HandlerFunc func(ctx context.Context, job Job) error

// KindSpec is registered once per process, at startup.
type KindSpec struct {
	Kind        Kind
	Handler     HandlerFunc
	Timeout     time.Duration // the handler's deadline
	Concurrency int           // runs of this kind at once, per process
}

// Spec is the stored definition of one job.
type Spec struct {
	Kind     Kind
	Key      string
	Schedule Schedule
	Misfire  Misfire
	Grace    time.Duration // for Skip, a whole number of seconds
	Enabled  bool
}

// Upsert creates or updates a job in the caller's transaction, so an object
// and its job change together. A new job, or one whose schedule changes, is
// due at once.
func Upsert(ctx context.Context, tx store.JobStore, spec Spec) error {
	if spec.Kind == "" || spec.Key == "" {
		return errors.New("jobs: a job needs a kind and a key")
	}
	if spec.Schedule == nil {
		return fmt.Errorf("jobs: job %s/%s has no schedule", spec.Kind, spec.Key)
	}
	scheduleKind, expr, timezone, err := spec.Schedule.stored()
	if err != nil {
		return err
	}
	if spec.Misfire != RunOnce && spec.Misfire != Skip {
		return fmt.Errorf("jobs: unknown misfire policy %q", spec.Misfire)
	}
	if spec.Grace < 0 || spec.Grace%time.Second != 0 {
		return fmt.Errorf("jobs: grace %s is not a whole number of seconds", spec.Grace)
	}
	return tx.UpsertJob(ctx, models.JobDefinition{
		Kind:         string(spec.Kind),
		Key:          spec.Key,
		ScheduleKind: scheduleKind,
		ScheduleExpr: expr,
		Timezone:     timezone,
		Misfire:      string(spec.Misfire),
		GraceSeconds: int64(spec.Grace / time.Second),
		Enabled:      spec.Enabled,
	})
}

// Delete removes a job in the caller's transaction. A missing job is not an
// error.
func Delete(ctx context.Context, tx store.JobStore, kind Kind, key string) error {
	return tx.DeleteJob(ctx, string(kind), key)
}

const (
	// leaseMargin is how long a lease outlives its handler's deadline.
	leaseMargin = 30 * time.Second
	// storeTimeout bounds each claim and each write the runner makes.
	storeTimeout = 10 * time.Second
	// maxErrorBytes bounds the stored error text of a failed run.
	maxErrorBytes = 512
)

// Options configures a Runner.
type Options struct {
	Store        store.JobStore
	Logger       *slog.Logger
	PollInterval time.Duration
}

// Runner claims and runs the jobs of the kinds registered with it.
type Runner struct {
	db    store.JobStore
	log   *slog.Logger
	owner string
	poll  time.Duration
	wake  chan struct{}

	mu       sync.Mutex
	kinds    []*kindRunner // fixed once Start runs
	started  bool
	stopped  bool
	stopLoop context.CancelFunc
	stopRuns context.CancelFunc
	loopDone chan struct{}

	runs sync.WaitGroup
	// writes guards abandoned. Every database write the runner makes holds
	// it for reading; Stop takes it to set abandoned when it gives up, so no
	// write starts or is in flight after Stop returns.
	writes sync.RWMutex
	// abandoned is set when Stop gives up on running handlers. A handler
	// that returns later writes nothing, and its lease ends by time.
	abandoned bool
}

type kindRunner struct {
	spec  KindSpec
	slots chan struct{}
}

// New returns a runner with a random owner id, made fresh for each process.
func New(opts Options) *Runner {
	owner := uuid.NewString()
	return &Runner{
		db:    opts.Store,
		log:   opts.Logger.With("component", "jobs", "owner", owner),
		owner: owner,
		poll:  opts.PollInterval,
		wake:  make(chan struct{}, 1),
	}
}

// Register adds a kind. It panics on an invalid spec, a duplicate kind or a
// call after Start, which are programming errors.
func (r *Runner) Register(spec KindSpec) {
	if spec.Kind == "" || spec.Handler == nil || spec.Timeout <= 0 || spec.Concurrency < 1 {
		panic(fmt.Sprintf("jobs: invalid KindSpec for kind %q", spec.Kind))
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		panic("jobs: Register after Start")
	}
	for _, k := range r.kinds {
		if k.spec.Kind == spec.Kind {
			panic(fmt.Sprintf("jobs: kind %q registered twice", spec.Kind))
		}
	}
	r.kinds = append(r.kinds, &kindRunner{spec: spec, slots: make(chan struct{}, spec.Concurrency)})
}

// Start begins claiming. Handlers run under ctx. Start after Start or Stop
// does nothing.
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started || r.stopped {
		return
	}
	if r.poll <= 0 {
		panic("jobs: PollInterval must be positive")
	}
	r.started = true
	runCtx, stopRuns := context.WithCancel(ctx)
	loopCtx, stopLoop := context.WithCancel(runCtx)
	r.stopRuns, r.stopLoop = stopRuns, stopLoop
	r.loopDone = make(chan struct{})
	go r.loop(loopCtx, runCtx)
}

// Stop claims nothing more, cancels the handlers' contexts and waits for
// them within ctx. It clears a job's lease only after its handler has
// returned. A handler still running when ctx ends keeps its lease, which then
// ends by time, and Stop returns ctx's error. The runner writes nothing after
// Stop returns; Stop may wait up to storeTimeout past ctx for a write that is
// already in flight.
func (r *Runner) Stop(ctx context.Context) error {
	r.mu.Lock()
	started, stopped := r.started, r.stopped
	r.stopped = true
	r.mu.Unlock()
	if !started || stopped {
		return nil
	}

	r.stopLoop()
	select {
	case <-r.loopDone:
	case <-ctx.Done():
		r.abandon()
		r.stopRuns()
		return fmt.Errorf("jobs: stop: the claim loop did not end: %w", ctx.Err())
	}
	r.stopRuns()

	done := make(chan struct{})
	go func() {
		r.runs.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		r.abandon()
		return fmt.Errorf("jobs: stop: handlers still running keep their leases: %w", ctx.Err())
	}
}

// abandon waits for a write in flight, then stops all further writes.
func (r *Runner) abandon() {
	r.writes.Lock()
	r.abandoned = true
	r.writes.Unlock()
}

// write runs one of the runner's database writes with its own deadline,
// unless Stop has given up. The write outlives a cancelled run context,
// because Stop clears the lease of a handler that has returned.
func (r *Runner) write(runCtx context.Context, fn func(ctx context.Context)) bool {
	r.writes.RLock()
	defer r.writes.RUnlock()
	if r.abandoned {
		return false
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(runCtx), storeTimeout)
	defer cancel()
	fn(ctx)
	return true
}

// Kick makes a job due now and wakes the local loop. It returns
// models.ErrNotFound when the job does not exist. A run in progress
// overwrites the kick when it records its next run.
func (r *Runner) Kick(ctx context.Context, kind Kind, key string) error {
	if err := r.db.KickJob(ctx, string(kind), key); err != nil {
		return err
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
	return nil
}

func (r *Runner) loop(loopCtx, runCtx context.Context) {
	defer close(r.loopDone)
	ticker := time.NewTicker(r.poll)
	defer ticker.Stop()
	for {
		for _, k := range r.kinds {
			if loopCtx.Err() != nil {
				return
			}
			r.claim(loopCtx, runCtx, k)
		}
		select {
		case <-loopCtx.Done():
			return
		case <-ticker.C:
		case <-r.wake:
		}
	}
}

func (r *Runner) claim(loopCtx, runCtx context.Context, k *kindRunner) {
	free := cap(k.slots) - len(k.slots)
	if free == 0 {
		return
	}
	// The claim is not cancelled by Stop: a claim that commits after its
	// caller gave up would leave leases that no handler owns.
	var claimed []*models.ScheduledJob
	var err error
	if !r.write(loopCtx, func(ctx context.Context) {
		claimed, err = r.db.ClaimJobs(ctx, string(k.spec.Kind), r.owner, k.spec.Timeout+leaseMargin, free)
	}) {
		return
	}
	if err != nil {
		r.log.Error("failed to claim jobs", "kind", k.spec.Kind, "error", err)
		return
	}
	for _, row := range claimed {
		if loopCtx.Err() != nil {
			// Stop began during the claim: hand the job back unrun.
			r.release(runCtx, row)
			continue
		}
		k.slots <- struct{}{}
		r.runs.Add(1)
		go r.run(runCtx, k, row)
	}
}

func (r *Runner) run(runCtx context.Context, k *kindRunner, row *models.ScheduledJob) {
	defer func() {
		<-k.slots
		r.runs.Done()
	}()
	kind := string(k.spec.Kind)
	log := r.log.With("kind", kind, "job_id", row.ID)
	claimedAt := *row.LastStartedAt
	metrics.ObserveJobScheduleLag(kind, claimedAt.Sub(row.NextRunAt))

	sched, err := parseSchedule(row.ScheduleKind, row.ScheduleExpr)
	if err != nil {
		// Upsert stores only valid schedules. The lease ends by time and the
		// job is tried again then.
		log.Error("cannot run a job with an invalid schedule", "error", err)
		metrics.RecordJobRun(kind, "error")
		return
	}
	at, err := latestDue(sched, row.NextRunAt, claimedAt, maxOccurrences)
	if err != nil {
		log.Error("deferred a job whose latest due occurrence is too far away", "next_run_at", row.NextRunAt, "error", err)
		metrics.RecordJobRun(kind, "deferred")
		r.release(runCtx, row)
		return
	}
	job := Job{ID: row.ID, Kind: k.spec.Kind, Key: row.Key, At: at, ClaimedAt: claimedAt, LeaseToken: row.LeaseToken}
	result := models.JobRunResult{
		ScheduleKind: row.ScheduleKind,
		ScheduleExpr: row.ScheduleExpr,
		Timezone:     row.Timezone,
		NextRunAt:    sched.Next(at),
	}

	if Misfire(row.Misfire) == Skip && claimedAt.Sub(at) > time.Duration(row.GraceSeconds)*time.Second {
		result.Status = "skipped"
		r.finish(runCtx, log, job, result)
		return
	}

	started := time.Now()
	err = call(runCtx, k.spec, job)
	metrics.ObserveJobRunDuration(kind, time.Since(started))
	switch {
	case errors.Is(err, ErrGone):
		r.deleteGone(runCtx, log, job)
	case err != nil && runCtx.Err() != nil:
		// Shutdown ended the run, so the tick is handed to another process.
		log.Info("job stopped by shutdown", "error", err)
		metrics.RecordJobRun(kind, "error")
		r.release(runCtx, row)
	case err != nil:
		log.Error("job failed", "error", err)
		result.Status, result.Error = "error", truncateError(err.Error())
		r.finish(runCtx, log, job, result)
	default:
		result.Status = "ok"
		r.finish(runCtx, log, job, result)
	}
}

func call(runCtx context.Context, spec KindSpec, job Job) (err error) {
	ctx, cancel := context.WithTimeout(runCtx, spec.Timeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("jobs: handler panicked: %v\n%s", p, debug.Stack())
		}
	}()
	return spec.Handler(ctx, job)
}

func (r *Runner) finish(runCtx context.Context, log *slog.Logger, job Job, result models.JobRunResult) {
	var finished bool
	var err error
	if !r.write(runCtx, func(ctx context.Context) {
		finished, err = r.db.FinishJob(ctx, job.ID, job.LeaseToken, result)
	}) {
		log.Warn("job returned after shutdown gave up; its lease ends by time", "status", result.Status)
		return
	}
	switch {
	case err != nil:
		log.Error("failed to record a job run; its lease ends by time", "status", result.Status, "error", err)
		metrics.RecordJobRun(string(job.Kind), "error")
	case !finished:
		log.Warn("a later claim owns this job; the run's outcome was not recorded", "status", result.Status)
		metrics.RecordJobRun(string(job.Kind), "lost")
	default:
		metrics.RecordJobRun(string(job.Kind), result.Status)
	}
}

func (r *Runner) release(runCtx context.Context, row *models.ScheduledJob) {
	var err error
	if !r.write(runCtx, func(ctx context.Context) {
		_, err = r.db.ReleaseJob(ctx, row.ID, row.LeaseToken)
	}) {
		return
	}
	if err != nil {
		r.log.Error("failed to release a job; its lease ends by time", "kind", row.Kind, "job_id", row.ID, "error", err)
	}
}

func (r *Runner) deleteGone(runCtx context.Context, log *slog.Logger, job Job) {
	var deleted bool
	var err error
	if !r.write(runCtx, func(ctx context.Context) {
		deleted, err = r.db.DeleteClaimedJob(ctx, job.ID, job.LeaseToken)
	}) {
		return
	}
	switch {
	case err != nil:
		log.Error("failed to delete a job whose object is gone", "error", err)
		metrics.RecordJobRun(string(job.Kind), "error")
	case !deleted:
		metrics.RecordJobRun(string(job.Kind), "lost")
	default:
		log.Info("deleted a job whose object is gone")
		metrics.RecordJobRun(string(job.Kind), "ok")
	}
}

// truncateError cuts s to maxErrorBytes on a rune boundary and marks the cut.
func truncateError(s string) string {
	if len(s) <= maxErrorBytes {
		return s
	}
	const mark = " [truncated]"
	cut := maxErrorBytes - len(mark)
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + mark
}
