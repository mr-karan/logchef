package core

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/reports"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
	"github.com/mr-karan/logchef/pkg/models"
)

// pgReportEnv is an owner in a team with a source and a saved query, on a
// Postgres schema of its own.
type pgReportEnv struct {
	t      *testing.T
	dsn    string
	db     store.Store
	owner  *models.User
	sq     *models.SavedQuery
	mailer *fakeMailer
}

func newPGReportEnv(t *testing.T) *pgReportEnv {
	t.Helper()
	ctx := context.Background()
	e := &pgReportEnv{t: t, dsn: postgrestest.DSN(t), mailer: &fakeMailer{}}
	e.db = e.open()
	e.owner = &models.User{Email: "schedule-owner@example.com", FullName: "Owner", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := e.db.CreateUser(ctx, e.owner); err != nil {
		t.Fatal(err)
	}
	src := &models.Source{Name: "scheduled", Connection: models.ConnectionInfo{Host: "ch:9000", Username: "default", Database: "default", TableName: "scheduled"}}
	if err := e.db.CreateSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	team := &models.Team{Name: "payments"}
	if err := e.db.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddTeamMember(ctx, team.ID, e.owner.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	if err := e.db.AddTeamSource(ctx, team.ID, src.ID); err != nil {
		t.Fatal(err)
	}
	sq, err := e.db.CreateSavedQuery(ctx, src.ID, nil, "5xx", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, `{"version":1,"limit":10,"content":"SELECT status FROM logs"}`, &e.owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.sq = sq
	return e
}

// open returns a new connection pool, as another replica would hold.
func (e *pgReportEnv) open() store.Store {
	e.t.Helper()
	db, err := postgres.New(context.Background(), postgres.Options{Logger: discardLogger(), Config: config.PostgresConfig{DSN: e.dsn}})
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = db.Close() })
	return db
}

func (e *pgReportEnv) runner(db store.Store) *ReportRunner {
	provider := &fakeProvider{queryLogsFn: func(context.Context, *models.Source, datasource.QueryRequest) (*models.QueryResult, error) {
		return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "status", Type: "UInt16"}}, Logs: []map[string]any{{"status": 503}}}, nil
	}}
	return &ReportRunner{
		DB: db,
		DS: newFakeDatasourceService(db, discardLogger(), provider),
		Query: config.QueryConfig{
			DefaultPreviewLimit: 1000, MaxPreviewLimit: 10000, MaxResponseBytes: 64 << 20,
			DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 300,
		},
		Mailer:      e.mailer,
		Audit:       audit.New(audit.Options{Store: db, Logger: discardLogger()}),
		Log:         discardLogger(),
		QueryBudget: 5 * time.Minute,
	}
}

// createDaily creates a report whose latest daily tick was 30 minutes ago
// and makes its job due at the tick before, as after downtime.
func (e *pgReportEnv) createDaily(i int) (*models.Report, time.Time) {
	e.t.Helper()
	ctx := context.Background()
	timeRange := "24h"
	schedule, tick := dailyTick(30 * time.Minute)
	saved, err := CreateReport(ctx, e.db, access.SessionPrincipal(e.owner), &ReportInput{
		Name:     fmt.Sprintf("daily %d", i),
		Source:   ReportSourceRef{Kind: models.ReportSourceSavedQuery, SavedQueryID: &e.sq.ID},
		Schedule: schedule, Timezone: "UTC", TimeRange: &timeRange,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	r := saved.Report
	conn, err := pgx.Connect(ctx, e.dsn)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := conn.Exec(ctx, `UPDATE jobs SET next_run_at = $1 WHERE kind = $2 AND key = $3`,
		tick.Add(-24*time.Hour), string(reports.JobKind), strconv.FormatInt(int64(r.ID), 10)); err != nil {
		e.t.Fatal(err)
	}
	return r, tick
}

// TestScheduledReportTwoRunners: two job runners on one database, each
// with its own connection pool and report runner, as two replicas. Every
// tick has one run row and one email.
func TestScheduledReportTwoRunners(t *testing.T) {
	e := newPGReportEnv(t)
	ctx := context.Background()
	const n = 6
	ids := make([]models.ReportID, n)
	ticks := make([]time.Time, n)
	for i := range n {
		r, tick := e.createDaily(i)
		ids[i], ticks[i] = r.ID, tick
	}

	var runners []*jobs.Runner
	for range 2 {
		db := e.open()
		r := jobs.New(jobs.Options{Store: db, Logger: discardLogger(), PollInterval: 10 * time.Millisecond})
		r.Register(e.runner(db).JobKind(2))
		runners = append(runners, r)
	}
	for _, r := range runners {
		r.Start(ctx)
	}
	waitUntil(t, func() bool {
		for _, id := range ids {
			job, err := e.db.GetJob(ctx, string(reports.JobKind), strconv.FormatInt(int64(id), 10))
			if err != nil {
				t.Fatal(err)
			}
			if job.LastFinishedAt == nil || job.LeaseOwner != "" {
				return false
			}
		}
		return true
	})
	for _, r := range runners {
		stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if err := r.Stop(stopCtx); err != nil {
			t.Fatal(err)
		}
		cancel()
	}

	for i, id := range ids {
		runs, err := e.db.ListReportRuns(ctx, id, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) != 1 || runs[0].Status != models.ReportRunSent || !runs[0].ScheduledFor.Equal(ticks[i]) {
			t.Errorf("report %d runs = %+v, want one sent run for %v", id, runs, ticks[i])
		}
	}
	if got := len(e.mailer.messages()); got != n {
		t.Errorf("messages = %d, want %d", got, n)
	}
}

// TestReportEditWaitsForRunCommit: a scheduled run's write holds the job
// row and then updates the report row. A pause that arrives in between
// waits for that write and then succeeds; with the opposite lock order
// Postgres aborts one of the two as a deadlock.
func TestReportEditWaitsForRunCommit(t *testing.T) {
	e := newPGReportEnv(t)
	ctx := context.Background()
	r, tick := e.createDaily(0)
	claimed, err := e.db.ClaimJobs(ctx, string(reports.JobKind), "replica-a", time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	job := jobs.Job{ID: claimed[0].ID, Kind: reports.JobKind, Key: claimed[0].Key, At: tick, LeaseToken: claimed[0].LeaseToken}
	run, err := e.db.CreateReportRun(ctx, models.ReportRunClaim{ReportID: r.ID, Trigger: models.ReportTriggerSchedule, ScheduledFor: tick, HandlerBudget: time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	held, release := make(chan struct{}), make(chan struct{})
	finish := make(chan error, 1)
	go func() {
		finish <- e.db.WithTx(ctx, func(tx store.StoreOps) error {
			if err := jobs.Hold(ctx, tx, job); err != nil {
				return err
			}
			close(held)
			<-release
			_, err := tx.FinishReportRun(ctx, run.ID, models.ReportRunResult{Status: models.ReportRunFailed, Reason: models.ReportReasonAllPanelsFailed})
			return err
		})
	}()
	<-held
	edit := e.open()
	paused := make(chan error, 1)
	go func() {
		_, err := PauseReport(ctx, edit, access.SessionPrincipal(e.owner), r.ID)
		paused <- err
	}()
	select {
	case err := <-paused:
		t.Fatalf("the pause returned %v while the run held the job", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	for name, ch := range map[string]chan error{"run": finish, "pause": paused} {
		select {
		case err := <-ch:
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s did not finish", name)
		}
	}
	got, err := e.db.GetReport(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != models.ReportStatePaused || got.ConsecutiveFailures != 1 {
		t.Errorf("report = %s, %d failures; want paused with the run's failure counted", got.State, got.ConsecutiveFailures)
	}
	if j, err := e.db.GetJob(ctx, string(reports.JobKind), job.Key); err != nil || j.Enabled {
		t.Errorf("job enabled = %v, %v; want disabled", j.Enabled, err)
	}
}
