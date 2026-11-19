package jobs

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func mustCron(t *testing.T, expr, timezone string) Schedule {
	t.Helper()
	s, err := Cron(expr, timezone)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCronRejects(t *testing.T) {
	cases := map[string][2]string{
		"six fields":        {"0 0 9 * * *", "UTC"},
		"four fields":       {"0 9 * *", "UTC"},
		"timezone prefix":   {"TZ=Asia/Kolkata 0 9 * *", "UTC"},
		"descriptor":        {"@daily", "UTC"},
		"out of range":      {"0 24 * * *", "UTC"},
		"never fires":       {"0 0 30 2 *", "UTC"},
		"no timezone":       {"0 9 * * *", ""},
		"machine timezone":  {"0 9 * * *", "Local"},
		"unknown timezone":  {"0 9 * * *", "Mars/Olympus"},
		"malformed minutes": {"x 9 * * *", "UTC"},
	}
	for name, c := range cases {
		if s, err := Cron(c[0], c[1]); err == nil {
			t.Errorf("%s: Cron(%q, %q) = %v, want an error", name, c[0], c[1], s)
		}
	}
}

func TestCronStoresNormalizedFields(t *testing.T) {
	kind, expr, timezone, err := mustCron(t, "  30  9 * *   1-5 ", "Asia/Kolkata").stored()
	if err != nil || kind != "cron" || expr != "30 9 * * 1-5" || timezone != "Asia/Kolkata" {
		t.Errorf("stored() = %q %q %q %v", kind, expr, timezone, err)
	}
	back, err := parseSchedule(kind, expr, timezone)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) // a Friday
	if want := time.Date(2026, 11, 16, 4, 0, 0, 0, time.UTC); !back.Next(from).Equal(want) {
		t.Errorf("parsed schedule Next = %v, want Monday 09:30 IST %v", back.Next(from), want)
	}
}

// TestCronDaylightSaving walks schedules across the 2026 changes in
// Europe/Berlin. On 29 March clocks jump from 02:00 CET to 03:00 CEST; on 25
// October they fall back from 03:00 CEST to 02:00 CET. Each wall-clock time
// fires once: a removed time at 03:00 CEST, a repeated time at its first
// instance.
func TestCronDaylightSaving(t *testing.T) {
	cet := time.FixedZone("CET", 3600)
	cest := time.FixedZone("CEST", 2*3600)
	cases := []struct {
		name string
		expr string
		from time.Time
		want []time.Time
	}{
		{
			name: "a removed time fires once at the jump",
			expr: "30 2 * * *",
			from: time.Date(2026, 3, 27, 12, 0, 0, 0, cet),
			want: []time.Time{
				time.Date(2026, 3, 28, 2, 30, 0, 0, cet),
				time.Date(2026, 3, 29, 3, 0, 0, 0, cest),
				time.Date(2026, 3, 30, 2, 30, 0, 0, cest),
			},
		},
		{
			name: "several removed times fire once together",
			expr: "*/15 2 * * *",
			from: time.Date(2026, 3, 29, 0, 0, 0, 0, cet),
			want: []time.Time{
				time.Date(2026, 3, 29, 3, 0, 0, 0, cest),
				time.Date(2026, 3, 30, 2, 0, 0, 0, cest),
			},
		},
		{
			name: "a removed time and the jump instant fire once",
			expr: "0,30 2,3 * * *",
			from: time.Date(2026, 3, 29, 1, 0, 0, 0, cet),
			want: []time.Time{
				time.Date(2026, 3, 29, 3, 0, 0, 0, cest),
				time.Date(2026, 3, 29, 3, 30, 0, 0, cest),
				time.Date(2026, 3, 30, 2, 0, 0, 0, cest),
			},
		},
		{
			name: "the hour after the jump is unaffected",
			expr: "0 3 * * *",
			from: time.Date(2026, 3, 28, 12, 0, 0, 0, cet),
			want: []time.Time{
				time.Date(2026, 3, 29, 3, 0, 0, 0, cest),
				time.Date(2026, 3, 30, 3, 0, 0, 0, cest),
			},
		},
		{
			name: "a repeated time fires at its first instance only",
			expr: "30 2 * * *",
			from: time.Date(2026, 10, 24, 12, 0, 0, 0, cest),
			want: []time.Time{
				time.Date(2026, 10, 25, 2, 30, 0, 0, cest),
				time.Date(2026, 10, 26, 2, 30, 0, 0, cet),
			},
		},
		{
			name: "an hourly schedule does not repeat the fallback hour",
			expr: "0 * * * *",
			from: time.Date(2026, 10, 25, 0, 30, 0, 0, cest),
			want: []time.Time{
				time.Date(2026, 10, 25, 1, 0, 0, 0, cest),
				time.Date(2026, 10, 25, 2, 0, 0, 0, cest),
				time.Date(2026, 10, 25, 3, 0, 0, 0, cet),
				time.Date(2026, 10, 25, 4, 0, 0, 0, cet),
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := mustCron(t, c.expr, "Europe/Berlin")
			at := c.from
			for i, want := range c.want {
				at = s.Next(at)
				if !at.Equal(want) {
					t.Fatalf("occurrence %d = %v, want %v", i+1, at.In(want.Location()), want)
				}
			}
		})
	}
}

// TestCronHalfHourChanges: Australia/Lord_Howe moves its clocks by 30
// minutes. On 5 April 2026 they fall back from 02:00 (+11) to 01:30 (+10:30);
// on 4 October 2026 they jump from 02:00 (+10:30) to 02:30 (+11). No day is
// skipped and each wall-clock time fires once.
func TestCronHalfHourChanges(t *testing.T) {
	summer := time.FixedZone("+11", 11*3600)
	winter := time.FixedZone("+1030", 10*3600+1800)
	cases := []struct {
		expr       string
		from, want time.Time
	}{
		{"0 3 * * *", time.Date(2026, 4, 5, 0, 0, 0, 0, summer), time.Date(2026, 4, 5, 3, 0, 0, 0, winter)},
		{"45 1 * * *", time.Date(2026, 4, 5, 0, 0, 0, 0, summer), time.Date(2026, 4, 5, 1, 45, 0, 0, summer)},
		{"45 1 * * *", time.Date(2026, 4, 5, 1, 45, 0, 0, summer), time.Date(2026, 4, 6, 1, 45, 0, 0, winter)},
		{"0 3 * * *", time.Date(2026, 10, 4, 0, 0, 0, 0, winter), time.Date(2026, 10, 4, 3, 0, 0, 0, summer)},
		{"15 2 * * *", time.Date(2026, 10, 4, 0, 0, 0, 0, winter), time.Date(2026, 10, 4, 2, 30, 0, 0, summer)},
	}
	for _, c := range cases {
		if got := mustCron(t, c.expr, "Australia/Lord_Howe").Next(c.from); !got.Equal(c.want) {
			t.Errorf("%q from %v = %v, want %v", c.expr, c.from, got, c.want)
		}
	}
}

// TestCronFarFuture covers dates past the zone's explicit transitions, where
// Go derives changes from a rule, and a schedule whose next occurrence is
// more than five years away, which the cron library alone reports as none.
func TestCronFarFuture(t *testing.T) {
	newYear := mustCron(t, "0 0 1 1 *", "Europe/Berlin")
	if got, want := newYear.Next(time.Date(2040, 6, 1, 0, 0, 0, 0, time.UTC)), time.Date(2040, 12, 31, 23, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("new year 2041 = %v, want %v", got, want)
	}
	leapDay := mustCron(t, "0 0 29 2 *", "UTC")
	if got, want := leapDay.Next(time.Date(2096, 2, 29, 0, 0, 0, 0, time.UTC)), time.Date(2104, 2, 29, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("leap day after 2096 = %v, want %v", got, want)
	}
}

// counting counts the occurrences a search visits.
type counting struct {
	Schedule
	calls *int
}

func (c counting) Next(t time.Time) time.Time {
	*c.calls++
	return c.Schedule.Next(t)
}

// TestRunnerCronDowntime: a per-minute cron job 60 days behind, more than
// maxOccurrences occurrences, is claimed once and runs once with At equal to
// the latest due minute, and the search walks only the seven-day window.
func TestRunnerCronDowntime(t *testing.T) {
	db, raw := openSQLite(t)
	ctx := context.Background()
	perMinute := mustCron(t, "* * * * *", "Europe/Berlin")
	upsert(t, db, Spec{Kind: "downtime", Key: "a", Schedule: perMinute, Misfire: RunOnce, Enabled: true})
	stored := setNextRun(t, raw, "downtime", "a", time.Now().UTC().Add(-60*24*time.Hour).Truncate(time.Minute))
	if behind := time.Since(stored) / time.Minute; behind <= maxOccurrences {
		t.Fatalf("next_run_at is %d occurrences behind, want more than %d", behind, maxOccurrences)
	}

	rec := &recorder{}
	r := newRunner(db)
	r.Register(KindSpec{Kind: "downtime", Handler: rec.handle, Timeout: time.Minute, Concurrency: 1})
	r.Start(ctx)
	waitFor(t, func() bool { return rec.count() == 1 && leaseCleared(t, db, "downtime", "a") })
	stop(t, r)

	job := rec.jobs()[0]
	if want := job.ClaimedAt.Truncate(time.Minute); !job.At.Equal(want) {
		t.Errorf("At = %v, want the latest due minute %v", job.At, want)
	}
	got := getJob(t, db, "downtime", "a")
	if want := job.At.Add(time.Minute); !got.NextRunAt.Equal(want) || got.LastStatus != "ok" || got.LeaseToken != 1 {
		t.Errorf("after the run: next_run_at %v (want %v), status %q, lease token %d (want one claim)",
			got.NextRunAt, want, got.LastStatus, got.LeaseToken)
	}
	if rec.count() != 1 {
		t.Errorf("handler ran %d times, want once", rec.count())
	}

	// The same search, counted: the window holds 10,080 minutes, plus one
	// call that finds the window's first occurrence.
	calls := 0
	at, err := latestDue(counting{perMinute, &calls}, stored, job.ClaimedAt, maxOccurrences)
	if err != nil || !at.Equal(job.At) {
		t.Errorf("latestDue = %v, %v, want %v", at, err, job.At)
	}
	if calls > 10_081 {
		t.Errorf("the search called Next %d times, want at most 10,081", calls)
	}
}

// TestRunnerDefersAtTheSearchCap: with the cap lowered below the distance to
// the latest due occurrence, the runner releases the claim, keeps next_run_at,
// logs and never calls the handler.
func TestRunnerDefersAtTheSearchCap(t *testing.T) {
	db, raw := openSQLite(t)
	ctx := context.Background()
	upsert(t, db, Spec{Kind: "capped", Key: "a", Schedule: mustCron(t, "* * * * *", "UTC"), Misfire: RunOnce, Enabled: true})
	stored := setNextRun(t, raw, "capped", "a", time.Now().UTC().Add(-3*time.Hour).Truncate(time.Minute))

	logs := &syncBuffer{}
	rec := &recorder{}
	r := New(Options{Store: db, Logger: slog.New(slog.NewTextHandler(logs, nil)), PollInterval: time.Hour})
	r.searchCap = 100
	r.Register(KindSpec{Kind: "capped", Handler: rec.handle, Timeout: time.Minute, Concurrency: 1})
	r.Start(ctx)
	waitFor(t, func() bool {
		job := getJob(t, db, "capped", "a")
		return job.LeaseToken == 1 && job.LeaseOwner == ""
	})
	stop(t, r)

	if rec.count() != 0 {
		t.Errorf("handler ran %d times, want never", rec.count())
	}
	got := getJob(t, db, "capped", "a")
	if !got.NextRunAt.Equal(stored) || got.LastStatus != "" || got.LastFinishedAt != nil {
		t.Errorf("after the deferral: next_run_at %v (want %v), status %q, finished %v", got.NextRunAt, stored, got.LastStatus, got.LastFinishedAt)
	}
	if line := logs.String(); !strings.Contains(line, "level=ERROR") || !strings.Contains(line, "deferred a job") || !strings.Contains(line, "job_id=") {
		t.Errorf("log = %q, want an error naming the job", line)
	}
}

// TestRunnerCronSkipPolicy: two hourly cron jobs missed three older
// occurrences each, and their latest due occurrence is about 30 minutes old.
// The job whose grace covers it runs with At set to it; the other is skipped.
// Both are next due at the occurrence after At.
func TestRunnerCronSkipPolicy(t *testing.T) {
	db, raw := openSQLite(t)
	ctx := context.Background()
	now := time.Now().UTC()
	minute := (now.Minute() + 30) % 60
	latest := now.Truncate(time.Hour).Add(time.Duration(minute) * time.Minute)
	if latest.After(now) {
		latest = latest.Add(-time.Hour)
	}
	hourly := mustCron(t, fmt.Sprintf("%d * * * *", minute), "UTC")
	upsert(t, db, Spec{Kind: "cron-skip", Key: "late", Schedule: hourly, Misfire: Skip, Grace: 10 * time.Minute, Enabled: true})
	upsert(t, db, Spec{Kind: "cron-skip", Key: "in-grace", Schedule: hourly, Misfire: Skip, Grace: time.Hour, Enabled: true})
	setNextRun(t, raw, "cron-skip", "late", latest.Add(-3*time.Hour))
	setNextRun(t, raw, "cron-skip", "in-grace", latest.Add(-3*time.Hour))

	rec := &recorder{}
	r := newRunner(db)
	r.Register(KindSpec{Kind: "cron-skip", Handler: rec.handle, Timeout: time.Minute, Concurrency: 2})
	r.Start(ctx)
	waitFor(t, func() bool {
		return rec.count() == 1 && leaseCleared(t, db, "cron-skip", "late") && leaseCleared(t, db, "cron-skip", "in-grace")
	})
	stop(t, r)

	job := rec.jobs()[0]
	if job.Key != "in-grace" || !job.At.Equal(latest) {
		t.Errorf("handler ran for %q with At %v, want in-grace with %v", job.Key, job.At, latest)
	}
	for key, status := range map[string]string{"late": "skipped", "in-grace": "ok"} {
		got := getJob(t, db, "cron-skip", key)
		if got.LastStatus != status || !got.NextRunAt.Equal(latest.Add(time.Hour)) {
			t.Errorf("%s: status %q, next_run_at %v; want %q, %v", key, got.LastStatus, got.NextRunAt, status, latest.Add(time.Hour))
		}
	}
}

// TestUpsertCronDueAtFirstOccurrence: a new cron job waits for its first
// occurrence instead of running at once with an At that is no occurrence.
func TestUpsertCronDueAtFirstOccurrence(t *testing.T) {
	db, _ := openSQLite(t)
	daily := mustCron(t, "0 9 * * *", "Europe/Berlin")
	before := time.Now()
	upsert(t, db, Spec{Kind: "report", Key: "1", Schedule: daily, Misfire: Skip, Grace: time.Hour, Enabled: true})
	if got, want := getJob(t, db, "report", "1").NextRunAt, daily.Next(before); !got.Equal(want) {
		t.Errorf("next_run_at = %v, want the first occurrence %v", got, want)
	}
}

// setNextRun stores at as the job's next run and returns it.
func setNextRun(t *testing.T, raw *sql.DB, kind, key string, at time.Time) time.Time {
	t.Helper()
	if _, err := raw.Exec(`UPDATE jobs SET next_run_at = ? WHERE kind = ? AND key = ?`, at.UTC().Format("2006-01-02T15:04:05.000Z"), kind, key); err != nil {
		t.Fatal(err)
	}
	return at
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
