package reports

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// JobKind is the job that sends one report on its schedule. Its key is the
// report id.
const JobKind jobs.Kind = "report"

// MisfireGrace is how late a tick may start. After downtime the runner
// takes the latest due tick and skips it when it is older than this.
const MisfireGrace = time.Hour

// cronFields returns the five cron fields of a schedule that passed
// Validate, read in the report's timezone.
func cronFields(s models.ReportSchedule) string {
	minute := strconv.Itoa(s.Minute)
	switch s.Frequency {
	case models.ReportHourly:
		return minute + " * * * *"
	case models.ReportDaily:
		return fmt.Sprintf("%s %d * * *", minute, *s.Hour)
	case models.ReportWeekly:
		days := slices.Sorted(slices.Values(s.Weekdays))
		names := make([]string, len(days))
		for i, d := range days {
			names[i] = strconv.Itoa(d)
		}
		return fmt.Sprintf("%s %d * * %s", minute, *s.Hour, strings.Join(names, ","))
	case models.ReportMonthly:
		return fmt.Sprintf("%s %d %d * *", minute, *s.Hour, *s.DayOfMonth)
	}
	panic(fmt.Sprintf("reports: schedule frequency %q passed Validate", s.Frequency))
}

// Schedule is the report's schedule as the job runner reads it.
func Schedule(r *models.Report) (jobs.Schedule, error) {
	if err := r.Schedule.Validate(); err != nil {
		return nil, err
	}
	return jobs.Cron(cronFields(r.Schedule), r.Timezone)
}

// Job is the report's job definition. A paused report has a disabled job.
func Job(r *models.Report) (jobs.Spec, error) {
	sched, err := Schedule(r)
	if err != nil {
		return jobs.Spec{}, fmt.Errorf("report %d: %w", r.ID, err)
	}
	return jobs.Spec{
		Kind:     JobKind,
		Key:      strconv.FormatInt(int64(r.ID), 10),
		Schedule: sched,
		Misfire:  jobs.Skip,
		Grace:    MisfireGrace,
		Enabled:  r.State == models.ReportStateActive,
	}, nil
}

// UpsertJob writes the report's job in the caller's transaction.
func UpsertJob(ctx context.Context, tx store.JobStore, r *models.Report) error {
	spec, err := Job(r)
	if err != nil {
		return err
	}
	return jobs.Upsert(ctx, tx, spec)
}

// DeleteJob removes the report's job in the caller's transaction.
func DeleteJob(ctx context.Context, tx store.JobStore, id models.ReportID) error {
	return jobs.Delete(ctx, tx, JobKind, strconv.FormatInt(int64(id), 10))
}

// ParseJobKey returns the report id that a job key names.
func ParseJobKey(key string) (models.ReportID, error) {
	id, err := strconv.ParseInt(key, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("report job key %q is not a report id", key)
	}
	return models.ReportID(id), nil
}

// NextRunAt is the first tick after now of a report that runs, and nil for
// a paused report or one without an owner.
func NextRunAt(r *models.Report, now time.Time) (*time.Time, error) {
	if r.State != models.ReportStateActive || r.OwnerID == nil {
		return nil, nil
	}
	sched, err := Schedule(r)
	if err != nil {
		return nil, fmt.Errorf("report %d: %w", r.ID, err)
	}
	next := sched.Next(now).UTC()
	return &next, nil
}
