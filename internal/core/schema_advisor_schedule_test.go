package core

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/advisor"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
	"github.com/mr-karan/logchef/pkg/models"
)

// recordingAdvisor is a ClickHouse provider whose analysis records the
// evaluation time and usage it was given.
type recordingAdvisor struct {
	*fakeProvider
	at    []time.Time
	usage []advisor.Usage
}

func (r *recordingAdvisor) AdviseSchema(_ context.Context, _ *models.Source, usage advisor.Usage, at time.Time) (*models.SchemaAdvisorReport, error) {
	r.at = append(r.at, at)
	r.usage = append(r.usage, usage)
	return &models.SchemaAdvisorReport{Status: models.AdvisorStatusOK, Inputs: []models.AdvisorInput{}, Findings: []models.SchemaFinding{}}, nil
}

// TestScheduledSchemaAdvice turns the weekly refresh on for one of two
// sources and runs its job as the runner would. The run analyzes as of the
// tick job.At, not the wall clock: the usage window ends on the tick's day
// and the report is stamped with the tick.
func TestScheduledSchemaAdvice(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) { testScheduledSchemaAdvice(t, newTestDB(t)) })
	t.Run("postgres", func(t *testing.T) {
		db, err := postgres.New(context.Background(), postgres.Options{Logger: discardLogger(), Config: config.PostgresConfig{DSN: postgrestest.DSN(t)}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		testScheduledSchemaAdvice(t, db)
	})
}

func testScheduledSchemaAdvice(t *testing.T, db store.Store) {
	ctx := context.Background()
	log := discardLogger()
	provider := &recordingAdvisor{fakeProvider: &fakeProvider{}}
	ds := newFakeDatasourceService(db, log, provider.fakeProvider)
	ds.Register(provider)
	admin := &models.User{Email: "schedule-admin@example.com", FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, admin); err != nil {
		t.Fatal(err)
	}
	p := access.SessionPrincipal(admin)
	var on, off models.SourceID
	for i, id := range []*models.SourceID{&on, &off} {
		source := &models.Source{Name: "advisor-" + strconv.Itoa(i), SourceType: models.SourceTypeClickHouse, Connection: models.ConnectionInfo{Host: "ch:9000", Database: "default", TableName: "logs_" + strconv.Itoa(i)}}
		if err := db.CreateSource(ctx, source); err != nil {
			t.Fatal(err)
		}
		*id = source.ID
	}

	// The tick is in the past, so a window that ended on the wall clock's
	// day would hold none of the usage recorded the day before the tick, and
	// a window with no end would also count the usage after it, as a claim
	// that runs late after downtime finds.
	at := time.Date(2025, time.March, 10, 3, 0, 0, 0, time.UTC)
	for day, count := range map[time.Time]int64{at.AddDate(0, 0, -1): 7, at.AddDate(0, 0, 2): 1000} {
		if err := db.IncrementQueryShapeStats(ctx, models.QueryShapeIncrement{
			BucketDate: day.Format(time.DateOnly), SourceID: on,
			Usage: models.QueryShapeUsage{
				QueryLanguage: models.QueryLanguageLogchefQL, ShapeHash: "1111111111111111",
				Shape: models.FilterShape{{Column: "request_id", Kind: models.MatchEq}}, QueryCount: count, TotalDurationMs: 10 * count,
			},
		}); err != nil {
			t.Fatal(err)
		}
	}

	member := &models.User{Email: "schedule-member@example.com", FullName: "Member", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, member); err != nil {
		t.Fatal(err)
	}
	if _, err := SetSchemaAdviceSchedule(ctx, db, ds, access.SessionPrincipal(member), on, true); !errors.Is(err, access.ErrGlobalAdminRequired) {
		t.Fatalf("member turns the schedule on: %v, want ErrGlobalAdminRequired", err)
	}
	for _, want := range []bool{false, true} {
		before, err := SetSchemaAdviceSchedule(ctx, db, ds, p, on, true)
		if err != nil || before != want {
			t.Fatalf("turn on: before %v, err %v; want before %v", before, err, want)
		}
	}
	advice, err := GetSchemaAdvice(ctx, db, ds, p, on)
	if err != nil || !advice.ScheduledRefresh {
		t.Fatalf("advice of the scheduled source = %+v, %v; want scheduled_refresh", advice, err)
	}
	if advice, err := GetSchemaAdvice(ctx, db, ds, p, off); err != nil || advice.ScheduledRefresh {
		t.Fatalf("advice of the other source = %+v, %v; want no schedule", advice, err)
	}
	job, err := db.GetJob(ctx, string(SchemaAdviceRefreshKind), strconv.FormatInt(int64(on), 10))
	if err != nil {
		t.Fatal(err)
	}
	if job.ScheduleKind != "cron" || job.ScheduleExpr != "0 3 * * 1" || job.Timezone != "UTC" || !job.Enabled {
		t.Fatalf("job = %+v, want an enabled weekly cron on Monday 03:00 UTC", job)
	}
	if next := job.NextRunAt.UTC(); next.Weekday() != time.Monday || next.Hour() != 3 || next.Minute() != 0 || !next.After(time.Now()) {
		t.Fatalf("first run at %s, want the next Monday 03:00 UTC", next)
	}

	// Run the tick as the runner does: claim the due job, then call the
	// registered handler with the claim.
	if err := db.KickJob(ctx, string(SchemaAdviceRefreshKind), job.Key); err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimJobs(ctx, string(SchemaAdviceRefreshKind), "replica-a", time.Minute, 10)
	if err != nil || len(claimed) != 1 || claimed[0].Key != job.Key {
		t.Fatalf("claimed %+v, %v; want the scheduled source's job alone", claimed, err)
	}
	kind := SchemaAdviceJobKind(db, ds)
	claim := jobs.Job{ID: claimed[0].ID, Kind: kind.Kind, Key: claimed[0].Key, At: at, ClaimedAt: time.Now(), LeaseToken: claimed[0].LeaseToken}

	// A run whose claim was taken over writes nothing.
	stale := claim
	stale.LeaseToken++
	if err := kind.Handler(ctx, stale); err != nil {
		t.Fatalf("run with a lost claim: %v, want nil", err)
	}
	if _, err := db.GetSchemaAdvisorReport(ctx, on); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("report after a lost claim: %v, want none", err)
	}

	if err := kind.Handler(ctx, claim); err != nil {
		t.Fatal(err)
	}
	stored, err := db.GetSchemaAdvisorReport(ctx, on)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.GeneratedAt.Equal(at) || stored.GeneratedBy != nil || stored.WindowDays != DefaultSchemaAdviceWindowDays {
		t.Errorf("stored report at %s by %v over %d days, want at the tick %s by nobody over %d days", stored.GeneratedAt, stored.GeneratedBy, stored.WindowDays, at, DefaultSchemaAdviceWindowDays)
	}
	last := len(provider.at) - 1
	if !provider.at[last].Equal(at) {
		t.Errorf("analysis evaluated at %s, want the tick %s", provider.at[last], at)
	}
	if got := provider.usage[last].Queries; got != 7 {
		t.Errorf("usage in the window ending on the tick's day = %d queries, want the 7 recorded the day before and none after", got)
	}
	if _, err := db.GetSchemaAdvisorReport(ctx, off); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("source without a schedule has a report: %v", err)
	}

	gone := claim
	gone.Key = strconv.FormatInt(int64(off+1000), 10)
	if err := kind.Handler(ctx, gone); !errors.Is(err, jobs.ErrGone) {
		t.Errorf("run for a deleted source: %v, want jobs.ErrGone", err)
	}

	if before, err := SetSchemaAdviceSchedule(ctx, db, ds, p, on, false); err != nil || !before {
		t.Fatalf("turn off: before %v, err %v; want before true", before, err)
	}
	if _, err := db.GetJob(ctx, string(SchemaAdviceRefreshKind), job.Key); !errors.Is(err, models.ErrNotFound) {
		t.Fatalf("job after turning the schedule off: %v, want none", err)
	}
}
