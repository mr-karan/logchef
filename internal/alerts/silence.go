package alerts

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// ActiveAt reports whether s applies at t. A once silence applies from
// StartsAt until EndsAt. A weekly silence applies inside a window that starts
// at Weekly.Start, local time in Weekly.Timezone, on one of Weekly.Weekdays,
// and before EndsAt when set. A window may cross midnight, so the window that
// started the day before is checked too. An error means the stored window
// cannot be read.
func ActiveAt(s *models.AlertSilence, t time.Time) (bool, error) {
	if s.EndsAt != nil && !t.Before(*s.EndsAt) {
		return false, nil
	}
	switch s.ScheduleKind {
	case models.SilenceOnce:
		return s.StartsAt != nil && s.EndsAt != nil && !t.Before(*s.StartsAt), nil
	case models.SilenceWeekly:
		return weeklyActiveAt(s.Weekly, t)
	default:
		return false, fmt.Errorf("silence %d has schedule kind %q", s.ID, s.ScheduleKind)
	}
}

func weeklyActiveAt(w *models.SilenceWeeklyWindow, t time.Time) (bool, error) {
	if w == nil {
		return false, fmt.Errorf("weekly silence has no window")
	}
	loc, err := time.LoadLocation(w.Timezone)
	if err != nil {
		return false, fmt.Errorf("weekly silence timezone: %w", err)
	}
	hour, minute, err := ParseClock(w.Start)
	if err != nil {
		return false, err
	}
	local := t.In(loc)
	duration := time.Duration(w.DurationMinutes) * time.Minute
	for _, back := range []int{0, 1} {
		// Noon of the calendar day exists on every day, so a clock change
		// near midnight cannot move the day.
		day := time.Date(local.Year(), local.Month(), local.Day()-back, 12, 0, 0, 0, loc)
		if !slices.Contains(w.Weekdays, int(day.Weekday())) {
			continue
		}
		// time.Date resolves a start inside a daylight-saving gap to a real
		// instant, so the window still opens on that day.
		start := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, loc)
		if !t.Before(start) && t.Before(start.Add(duration)) {
			return true, nil
		}
	}
	return false, nil
}

// ParseClock reads an "HH:MM" local time of day.
func ParseClock(clock string) (hour, minute int, err error) {
	h, m, ok := strings.Cut(clock, ":")
	if ok && len(h) == 2 && len(m) == 2 {
		hour, errH := strconv.Atoi(h)
		minute, errM := strconv.Atoi(m)
		if errH == nil && errM == nil && hour >= 0 && hour < 24 && minute >= 0 && minute < 60 {
			return hour, minute, nil
		}
	}
	return 0, 0, fmt.Errorf("start %q is not a time of day in HH:MM form", clock)
}

// Matches reports whether s mutes an instance of alert with the given
// labels: s belongs to the alert's source, names no rule or this rule, and
// every matcher equals a label. It does not check the schedule.
func Matches(s *models.AlertSilence, alert *models.Alert, labels map[string]string) bool {
	if s.SourceID != alert.SourceID || (s.AlertID != nil && *s.AlertID != alert.ID) {
		return false
	}
	for key, value := range s.Matchers {
		if got, ok := labels[key]; !ok || got != value {
			return false
		}
	}
	return true
}

// SilenceLabels are the labels a silence matches against: the rule's labels,
// the group values, and alertname and severity, which win a name clash.
func SilenceLabels(alert *models.Alert, group map[string]string) map[string]string {
	labels := make(map[string]string, len(alert.Labels)+len(group)+2)
	maps.Copy(labels, alert.Labels)
	maps.Copy(labels, group)
	labels["alertname"] = alert.Name
	labels["severity"] = string(alert.Severity)
	return labels
}
