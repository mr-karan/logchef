package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

// Two manual runs at once, and a manual run against a scheduled run, from
// two connection pools as two replicas would: each time exactly one run is
// running. Postgres runs when LOGCHEF_TEST_POSTGRES_DSN is set.
func TestReportRunsAtOnce(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		path := sqlitetest.Path(t)
		runReportRace(t, func(t *testing.T) store.Store {
			db, err := sqlite.New(context.Background(), sqlite.Options{Logger: discardLogger(), Config: config.SQLiteConfig{Path: path}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			return db
		})
	})
	t.Run("postgres", func(t *testing.T) {
		dsn := postgrestest.DSN(t)
		runReportRace(t, func(t *testing.T) store.Store {
			db, err := postgres.New(context.Background(), postgres.Options{Logger: discardLogger(), Config: config.PostgresConfig{DSN: dsn}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			return db
		})
	})
}

func runReportRace(t *testing.T, open func(t *testing.T) store.Store) {
	ctx := context.Background()
	a, b := open(t), open(t)
	owner := &models.User{Email: "race-owner@example.com", FullName: "Owner", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := a.CreateUser(ctx, owner); err != nil {
		t.Fatal(err)
	}
	src := &models.Source{Name: "race", Connection: models.ConnectionInfo{Host: "ch:9000", Database: "default", TableName: "race"}}
	if err := a.CreateSource(ctx, src); err != nil {
		t.Fatal(err)
	}
	sq, err := a.CreateSavedQuery(ctx, src.ID, nil, "race", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, `{"version":1,"limit":10,"content":"SELECT 1"}`, &owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	newReport := func(i int) *models.Report {
		r := &models.Report{
			Name: fmt.Sprintf("race %d", i), OwnerID: &owner.ID, SourceKind: models.ReportSourceSavedQuery, SavedQueryID: &sq.ID,
			Schedule: models.ReportSchedule{Frequency: models.ReportHourly}, Timezone: "UTC", TimeRange: "1h",
			RangeAnchor: models.ReportAnchorSendTime, Variables: map[string]any{}, MaxRows: 25,
		}
		if err := a.CreateReport(ctx, r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	p := access.SessionPrincipal(owner)
	runners := []*ReportRunner{{DB: a, QueryBudget: 5 * time.Minute}, {DB: b, QueryBudget: 5 * time.Minute}}

	for i := range 10 {
		r := newReport(i)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for j, rr := range runners {
			wg.Go(func() { _, errs[j] = rr.StartManualRun(ctx, p, r.ID) })
		}
		wg.Wait()
		if failed := countErrs(errs); failed != 1 {
			t.Fatalf("two manual runs: errors %v, want exactly one run admitted", errs)
		}
		for _, err := range errs {
			if err != nil && !errors.Is(err, ErrReportCooldown) && !errors.Is(err, models.ErrReportRunInFlight) {
				t.Fatalf("the losing manual run failed with %v", err)
			}
		}
		assertOneRunning(t, a, r.ID)
	}

	for i := range 10 {
		r := newReport(100 + i)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		wg.Go(func() { _, errs[0] = runners[0].StartManualRun(ctx, p, r.ID) })
		wg.Go(func() {
			_, errs[1] = b.CreateReportRun(ctx, models.ReportRunClaim{
				ReportID: r.ID, Trigger: models.ReportTriggerSchedule, ScheduledFor: time.Date(2026, 11, 16, i, 0, 0, 0, time.UTC), HandlerBudget: time.Minute,
			})
		})
		wg.Wait()
		if failed := countErrs(errs); failed != 1 {
			t.Fatalf("a manual and a scheduled run: errors %v, want exactly one admitted", errs)
		}
		for _, err := range errs {
			if err != nil && !errors.Is(err, models.ErrReportRunInFlight) {
				t.Fatalf("the losing run failed with %v, want ErrReportRunInFlight", err)
			}
		}
		assertOneRunning(t, a, r.ID)
	}
}

func countErrs(errs []error) int {
	n := 0
	for _, err := range errs {
		if err != nil {
			n++
		}
	}
	return n
}

func assertOneRunning(t *testing.T, db store.Store, id models.ReportID) {
	t.Helper()
	runs, err := db.ListReportRuns(context.Background(), id, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	running := 0
	for _, run := range runs {
		if run.Status == models.ReportRunRunning {
			running++
		}
	}
	if running != 1 {
		t.Fatalf("report %d has %d running runs, want 1: %+v", id, running, runs)
	}
}
