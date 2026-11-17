package reports

import (
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestCronFields(t *testing.T) {
	cases := map[string]struct {
		schedule models.ReportSchedule
		want     string
	}{
		"hourly":  {models.ReportSchedule{Frequency: models.ReportHourly, Minute: 15}, "15 * * * *"},
		"daily":   {models.ReportSchedule{Frequency: models.ReportDaily, Minute: 0, Hour: new(9)}, "0 9 * * *"},
		"weekly":  {models.ReportSchedule{Frequency: models.ReportWeekly, Minute: 30, Hour: new(8), Weekdays: []int{5, 1, 3}}, "30 8 * * 1,3,5"},
		"sunday":  {models.ReportSchedule{Frequency: models.ReportWeekly, Minute: 0, Hour: new(23), Weekdays: []int{0}}, "0 23 * * 0"},
		"monthly": {models.ReportSchedule{Frequency: models.ReportMonthly, Minute: 45, Hour: new(6), DayOfMonth: new(28)}, "45 6 28 * *"},
	}
	for name, c := range cases {
		if got := cronFields(c.schedule); got != c.want {
			t.Errorf("%s: cronFields = %q, want %q", name, got, c.want)
		}
	}
}

func TestJobSpec(t *testing.T) {
	owner := models.UserID(1)
	r := &models.Report{
		ID: 42, OwnerID: &owner, State: models.ReportStateActive, Timezone: "Asia/Kolkata",
		Schedule: models.ReportSchedule{Frequency: models.ReportDaily, Minute: 0, Hour: new(9)},
	}
	spec, err := Job(r)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Kind != "report" || spec.Key != "42" || spec.Misfire != jobs.Skip || spec.Grace != time.Hour || !spec.Enabled {
		t.Errorf("spec = %+v, want report/42, skip with one hour of grace, enabled", spec)
	}
	from := time.Date(2026, 11, 17, 4, 0, 0, 0, time.UTC) // 09:30 IST
	if want := time.Date(2026, 11, 18, 3, 30, 0, 0, time.UTC); !spec.Schedule.Next(from).Equal(want) {
		t.Errorf("next = %v, want 09:00 IST tomorrow %v", spec.Schedule.Next(from), want)
	}
	if next, err := NextRunAt(r, from); err != nil || next == nil || !next.Equal(time.Date(2026, 11, 18, 3, 30, 0, 0, time.UTC)) {
		t.Errorf("NextRunAt = %v, %v", next, err)
	}

	r.State, r.PauseReason = models.ReportStatePaused, models.ReportPauseUser
	if spec, err := Job(r); err != nil || spec.Enabled {
		t.Errorf("a paused report's job: enabled %v, %v; want disabled", spec.Enabled, err)
	}
	if next, err := NextRunAt(r, from); err != nil || next != nil {
		t.Errorf("NextRunAt of a paused report = %v, %v; want nil", next, err)
	}
	r.State, r.OwnerID = models.ReportStateActive, nil
	if next, err := NextRunAt(r, from); err != nil || next != nil {
		t.Errorf("NextRunAt of a report without an owner = %v, %v; want nil", next, err)
	}

	r.Schedule = models.ReportSchedule{Frequency: models.ReportDaily, Minute: 0}
	if _, err := Job(r); err == nil {
		t.Error("Job accepted a daily schedule without an hour")
	}
	if id, err := ParseJobKey("42"); err != nil || id != 42 {
		t.Errorf("ParseJobKey(42) = %d, %v", id, err)
	}
	for _, key := range []string{"", "0", "-3", "x"} {
		if _, err := ParseJobKey(key); err == nil {
			t.Errorf("ParseJobKey(%q) accepted", key)
		}
	}
}

// TestDailyReportAcrossDaylightSaving walks the job schedule of a daily 02:30
// report in Europe/Berlin, as the runner does (Next of the tick it ran). On
// 29 March 2026 the clocks jump from 02:00 CET to 03:00 CEST, so 02:30 does
// not exist; on 25 October they fall back from 03:00 CEST to 02:00 CET, so
// 02:30 happens twice. Each day fires exactly once: the missing time at the
// jump, the repeated time at its first instance.
func TestDailyReportAcrossDaylightSaving(t *testing.T) {
	owner := models.UserID(1)
	r := &models.Report{
		ID: 1, OwnerID: &owner, State: models.ReportStateActive, Timezone: "Europe/Berlin",
		Schedule: models.ReportSchedule{Frequency: models.ReportDaily, Minute: 30, Hour: new(2)},
	}
	spec, err := Job(r)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		from time.Time
		want []time.Time
	}{
		{
			name: "spring forward",
			from: time.Date(2026, 3, 27, 12, 0, 0, 0, time.UTC),
			want: []time.Time{
				time.Date(2026, 3, 28, 1, 30, 0, 0, time.UTC), // 02:30 CET
				time.Date(2026, 3, 29, 1, 0, 0, 0, time.UTC),  // 03:00 CEST, the jump
				time.Date(2026, 3, 30, 0, 30, 0, 0, time.UTC), // 02:30 CEST
			},
		},
		{
			name: "fall back",
			from: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC),
			want: []time.Time{
				time.Date(2026, 10, 24, 0, 30, 0, 0, time.UTC), // 02:30 CEST
				time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC), // 02:30 CEST, the first of two
				time.Date(2026, 10, 26, 1, 30, 0, 0, time.UTC), // 02:30 CET
			},
		},
	}
	for _, c := range cases {
		at := c.from
		for i, want := range c.want {
			at = spec.Schedule.Next(at)
			if !at.Equal(want) {
				t.Errorf("%s: tick %d = %v, want %v", c.name, i, at, want)
			}
		}
	}
}
