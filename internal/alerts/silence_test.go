package alerts

import (
	"slices"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestSilenceActiveAt(t *testing.T) {
	utc := time.UTC
	kolkata := mustLocation(t, "Asia/Kolkata")
	berlin := mustLocation(t, "Europe/Berlin")
	at := func(loc *time.Location, year int, month time.Month, day, hour, minute int) time.Time {
		return time.Date(year, month, day, hour, minute, 0, 0, loc)
	}
	ptr := func(t time.Time) *time.Time { return &t }

	once := &models.AlertSilence{
		ScheduleKind: models.SilenceOnce,
		StartsAt:     ptr(at(utc, 2026, 11, 16, 10, 0)),
		EndsAt:       ptr(at(utc, 2026, 11, 16, 11, 0)),
	}
	// Saturday 23:00 for two hours: the window crosses into Sunday.
	midnight := &models.AlertSilence{
		ScheduleKind: models.SilenceWeekly,
		Weekly:       &models.SilenceWeeklyWindow{Weekdays: []int{int(time.Saturday)}, Start: "23:00", DurationMinutes: 120, Timezone: "UTC"},
	}
	// Monday 02:00 to 03:00 in Kolkata is Sunday 20:30 to 21:30 UTC.
	india := &models.AlertSilence{
		ScheduleKind: models.SilenceWeekly,
		Weekly:       &models.SilenceWeeklyWindow{Weekdays: []int{int(time.Monday)}, Start: "02:00", DurationMinutes: 60, Timezone: "Asia/Kolkata"},
	}
	ending := &models.AlertSilence{
		ScheduleKind: models.SilenceWeekly,
		EndsAt:       ptr(at(utc, 2026, 11, 20, 0, 0)),
		Weekly:       &models.SilenceWeeklyWindow{Weekdays: []int{0, 1, 2, 3, 4, 5, 6}, Start: "00:00", DurationMinutes: 1440, Timezone: "UTC"},
	}
	// 02:30 does not exist in Berlin on 2026-03-29; the window still opens.
	springGap := &models.AlertSilence{
		ScheduleKind: models.SilenceWeekly,
		Weekly:       &models.SilenceWeeklyWindow{Weekdays: []int{int(time.Sunday)}, Start: "02:30", DurationMinutes: 60, Timezone: "Europe/Berlin"},
	}

	santiago := mustLocation(t, "America/Santiago")
	santiagoSunday := &models.AlertSilence{
		ScheduleKind: models.SilenceWeekly,
		Weekly:       &models.SilenceWeeklyWindow{Weekdays: []int{int(time.Sunday)}, Start: "23:00", DurationMinutes: 120, Timezone: "America/Santiago"},
	}

	for _, tc := range []struct {
		name    string
		silence *models.AlertSilence
		t       time.Time
		want    bool
	}{
		{"once: before the start", once, at(utc, 2026, 11, 16, 9, 59), false},
		{"once: at the start", once, at(utc, 2026, 11, 16, 10, 0), true},
		{"once: inside", once, at(utc, 2026, 11, 16, 10, 30), true},
		{"once: at the end", once, at(utc, 2026, 11, 16, 11, 0), false},
		{"weekly: before the window", midnight, at(utc, 2026, 11, 21, 22, 59), false},
		{"weekly: Saturday inside", midnight, at(utc, 2026, 11, 21, 23, 30), true},
		{"weekly: past midnight, Sunday inside", midnight, at(utc, 2026, 11, 22, 0, 30), true},
		{"weekly: at the window end", midnight, at(utc, 2026, 11, 22, 1, 0), false},
		{"weekly: Friday at the same hour", midnight, at(utc, 2026, 11, 20, 23, 30), false},
		{"timezone: Monday 02:30 in Kolkata", india, at(kolkata, 2026, 11, 23, 2, 30), true},
		{"timezone: the same instant read in UTC", india, at(utc, 2026, 11, 22, 21, 0), true},
		{"timezone: Monday 02:30 UTC is not the window", india, at(utc, 2026, 11, 23, 2, 30), false},
		{"weekly: before its last day", ending, at(utc, 2026, 11, 19, 23, 0), true},
		{"weekly: after its last day", ending, at(utc, 2026, 11, 20, 0, 0), false},
		{"daylight saving: a start in the gap still opens", springGap, at(berlin, 2026, 3, 29, 3, 40), true},
		// Santiago skips Sunday 00:00-01:00 on 2026-09-06; Monday 00:30 is
		// still inside the window that opened Sunday 23:00.
		{"daylight saving: the day before has no midnight", santiagoSunday, at(santiago, 2026, 9, 7, 0, 30), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ActiveAt(tc.silence, tc.t)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("ActiveAt(%s) = %v, want %v", tc.t, got, tc.want)
			}
		})
	}

	broken := &models.AlertSilence{ScheduleKind: models.SilenceWeekly, Weekly: &models.SilenceWeeklyWindow{Weekdays: []int{1}, Start: "02:00", DurationMinutes: 60, Timezone: "Mars/Olympus"}}
	if _, err := ActiveAt(broken, time.Now()); err == nil {
		t.Error("ActiveAt with an unknown timezone err = nil, want an error")
	}
}

func TestSilenceMatches(t *testing.T) {
	alert := &models.Alert{ID: 7, SourceID: 3, Name: "errors", Severity: models.AlertSeverityCritical, Labels: map[string]string{"team": "payments"}}
	other := models.AlertID(8)
	rule := alert.ID
	labels := SilenceLabels(alert, map[string]string{"service": "checkout"})
	for _, tc := range []struct {
		name    string
		silence models.AlertSilence
		want    bool
	}{
		{"every rule of the source", models.AlertSilence{SourceID: 3}, true},
		{"another source", models.AlertSilence{SourceID: 4}, false},
		{"this rule", models.AlertSilence{SourceID: 3, AlertID: &rule}, true},
		{"another rule", models.AlertSilence{SourceID: 3, AlertID: &other}, false},
		{"a group value", models.AlertSilence{SourceID: 3, Matchers: map[string]string{"service": "checkout"}}, true},
		{"another group value", models.AlertSilence{SourceID: 3, Matchers: map[string]string{"service": "search"}}, false},
		{"a rule label and alertname", models.AlertSilence{SourceID: 3, Matchers: map[string]string{"team": "payments", "alertname": "errors"}}, true},
		{"severity", models.AlertSilence{SourceID: 3, Matchers: map[string]string{"severity": "critical"}}, true},
		{"one matcher of two misses", models.AlertSilence{SourceID: 3, Matchers: map[string]string{"service": "checkout", "region": "eu"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Matches(&tc.silence, alert, labels); got != tc.want {
				t.Errorf("Matches = %v, want %v", got, tc.want)
			}
		})
	}
}

// evalAt evaluates the stored rule at at against one result and returns the
// notifications it sent as "status service".
func (f *groupedFixture) evalAt(t *testing.T, at time.Time, result *models.QueryResult) []string {
	t.Helper()
	alert, err := f.db.GetAlert(t.Context(), f.alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.provider.results = append(f.provider.results, result)
	if err := f.manager.evaluateAlert(t.Context(), alert, at); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	f.sent = f.queue.take()
	got := []string{}
	for i := range f.sent {
		got = append(got, string(f.sent[i].Status)+" "+f.sent[i].Group["service"])
	}
	slices.Sort(got)
	return got
}

// silence stores a ten-minute once silence of the fixture's source from start.
func (f *groupedFixture) silence(t *testing.T, start time.Time, matchers map[string]string) *models.AlertSilence {
	t.Helper()
	end := start.Add(10 * time.Minute)
	s := &models.AlertSilence{SourceID: f.alert.SourceID, Matchers: matchers, Comment: "deploy", ScheduleKind: models.SilenceOnce, StartsAt: &start, EndsAt: &end}
	if err := f.db.CreateAlertSilence(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	return s
}

func wantSent(t *testing.T, step string, got []string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("%s sent %v, want %v", step, got, want)
	}
}

// TestSilenceEndsWithGroupStillFiring: a group that fires inside a silence
// writes its episode and sends nothing; the first evaluation after the
// silence ends sends the firing notification, once.
func TestSilenceEndsWithGroupStillFiring(t *testing.T) {
	f := newGroupedFixture(t)
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	s := f.silence(t, at.Add(-time.Minute), map[string]string{"service": "checkout"})

	wantSent(t, "fire inside the silence", f.evalAt(t, at, groupedResult(row("checkout", 12), row("search", 30))), "triggered search")
	history, err := f.db.ListOpenAlertHistory(t.Context(), f.alert.ID)
	if err != nil || len(history) != 2 {
		t.Fatalf("open episodes = %d, %v; want both groups", len(history), err)
	}
	for _, entry := range history {
		ids, _ := entry.Payload["silenced_by"].([]any)
		if entry.Group["service"] == "checkout" && (len(ids) != 1 || ids[0] != float64(s.ID)) {
			t.Errorf("checkout payload silenced_by = %v, want [%d]", entry.Payload["silenced_by"], s.ID)
		}
		if entry.Group["service"] == "search" && entry.Payload["silenced_by"] != nil {
			t.Errorf("search payload silenced_by = %v, want none", entry.Payload["silenced_by"])
		}
	}
	wantSent(t, "still inside the silence", f.evalAt(t, at.Add(5*time.Minute), groupedResult(row("checkout", 12), row("search", 30))))
	wantSent(t, "after the silence", f.evalAt(t, at.Add(10*time.Minute), groupedResult(row("checkout", 12), row("search", 30))), "triggered checkout")
	wantSent(t, "the tick after", f.evalAt(t, at.Add(11*time.Minute), groupedResult(row("checkout", 12), row("search", 30))))
	wantSent(t, "resolve after the silence", f.evalAt(t, at.Add(12*time.Minute), groupedResult(row("checkout", 1), row("search", 30))), "resolved checkout")
}

// TestSilenceHidesWholeEpisode: a group that fires and resolves inside a
// silence sends nothing at all, and its history still records the episode.
func TestSilenceHidesWholeEpisode(t *testing.T) {
	f := newGroupedFixture(t)
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	f.silence(t, at, nil)

	wantSent(t, "fire", f.evalAt(t, at, groupedResult(row("checkout", 12))))
	wantSent(t, "resolve", f.evalAt(t, at.Add(time.Minute), groupedResult(row("checkout", 1))))
	history, err := f.db.ListAlertHistory(t.Context(), f.alert.ID, 10)
	if err != nil || len(history) != 1 || history[0].Status != models.AlertStatusResolved {
		t.Fatalf("history = %+v, %v; want one resolved episode", history, err)
	}
	wantSent(t, "after the silence", f.evalAt(t, at.Add(20*time.Minute), groupedResult(row("checkout", 1))))
}

// TestSilenceMutesResolve: a group announced before a silence resolves
// inside it without a notification.
func TestSilenceMutesResolve(t *testing.T) {
	f := newGroupedFixture(t)
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	wantSent(t, "fire", f.evalAt(t, at, groupedResult(row("checkout", 12))), "triggered checkout")
	f.silence(t, at.Add(time.Minute), nil)
	wantSent(t, "resolve inside the silence", f.evalAt(t, at.Add(2*time.Minute), groupedResult(row("checkout", 1))))
}

// TestManualResolveOfSilencedEpisode: a manual resolve of an episode that a
// silence kept from being announced sends nothing.
func TestManualResolveOfSilencedEpisode(t *testing.T) {
	f := newGroupedFixture(t)
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	f.silence(t, at, map[string]string{"service": "checkout"})
	wantSent(t, "fire", f.evalAt(t, at, groupedResult(row("checkout", 12), row("search", 30))), "triggered search")
	f.queue.take()
	if err := f.manager.ManualResolve(t.Context(), f.alert.ID, nil, "fixed"); err != nil {
		t.Fatal(err)
	}
	f.sent = f.queue.take()
	if len(f.sent) != 1 || f.sent[0].Group["service"] != "search" {
		t.Fatalf("manual resolve sent %+v, want only search's resolved", f.sent)
	}
}

// TestSilencedEpisodeResolvesAfterSilence: a group that fired only inside a
// silence and resolves after it ends was never announced, so it sends no
// resolved notification.
func TestSilencedEpisodeResolvesAfterSilence(t *testing.T) {
	f := newGroupedFixture(t)
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	f.silence(t, at, nil)
	wantSent(t, "fire", f.evalAt(t, at, groupedResult(row("checkout", 12))))
	wantSent(t, "resolve after the silence", f.evalAt(t, at.Add(10*time.Minute), groupedResult(row("checkout", 1))))
}

// TestManualResolveWhileSilenced: an announced group resolved by hand while a
// silence mutes it sends nothing.
func TestManualResolveWhileSilenced(t *testing.T) {
	f := newGroupedFixture(t)
	wantSent(t, "fire", f.evalAt(t, time.Now(), groupedResult(row("checkout", 12))), "triggered checkout")
	f.silence(t, time.Now().Add(-time.Minute), nil)
	f.queue.take()
	if err := f.manager.ManualResolve(t.Context(), f.alert.ID, nil, "fixed"); err != nil {
		t.Fatal(err)
	}
	f.sent = f.queue.take()
	if len(f.sent) != 0 {
		t.Fatalf("manual resolve inside a silence sent %+v, want nothing", f.sent)
	}
}

// TestSilenceHoldsBackNoData: under the no_data policy, a group that loses
// its data inside a silence sends nothing; the first evaluation after the
// silence ends sends the no_data notification it owes.
func TestSilenceHoldsBackNoData(t *testing.T) {
	f := newGroupedFixture(t)
	f.withPolicy(t, models.AlertNoDataNoData, true)
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	f.silence(t, at, nil)

	wantSent(t, "no data inside the silence", f.evalAt(t, at, ungroupedResult()))
	wantSent(t, "still inside", f.evalAt(t, at.Add(5*time.Minute), ungroupedResult()))
	wantSent(t, "after the silence", f.evalAt(t, at.Add(10*time.Minute), ungroupedResult()), "no_data ")
	wantSent(t, "the tick after", f.evalAt(t, at.Add(11*time.Minute), ungroupedResult()))
}

// TestSilenceHoldsBackMovedEpisode: an announced firing group that loses its
// data inside a silence moves to no_data without a notification. After the
// silence it owes the no_data notification, and enqueues it once.
func TestSilenceHoldsBackMovedEpisode(t *testing.T) {
	f := newGroupedFixture(t)
	f.withPolicy(t, models.AlertNoDataNoData, true, "service")
	at := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	wantSent(t, "fire", f.evalAt(t, at, groupedResult(row("checkout", 12))), "triggered checkout")

	f.silence(t, at.Add(time.Minute), nil)
	wantSent(t, "no data inside the silence", f.evalAt(t, at.Add(2*time.Minute), groupedResult()))
	open, err := f.db.ListOpenAlertHistory(t.Context(), f.alert.ID)
	if err != nil || len(open) != 1 || open[0].Status != models.AlertStatusNoData {
		t.Fatalf("open episodes = %+v, %v; want the episode moved to no_data", open, err)
	}

	wantSent(t, "after the silence", f.evalAt(t, at.Add(11*time.Minute), groupedResult()), "no_data checkout")
	wantSent(t, "the tick after", f.evalAt(t, at.Add(12*time.Minute), groupedResult()))
}
