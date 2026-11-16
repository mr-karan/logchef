// Package reports turns a report's source into an email: the time window,
// the rendered panels and the MIME message. It holds no HTTP or SMTP code.
package reports

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Window is the time range a run covers, as instants.
type Window struct {
	Start time.Time
	End   time.Time
}

// rangeDuration and rangeCalendarDays accept the dashboard range grammar of
// RFC 0003 (pkg/models/dashboards.go): a rolling count of minutes, hours,
// days or weeks, today, yesterday, or today-<n>d.
var (
	rangeDuration     = regexp.MustCompile(`^([1-9]\d{0,3})([mhdw])$`)
	rangeCalendarDays = regexp.MustCompile(`^today-([1-9]\d{0,3})d$`)
)

// ResolveWindow resolves a range at the run's effective time at, in loc.
//
// Without dayStart the range ends at at (the send_time anchor). With
// dayStart it ends at the start of at's day in loc, and a range of whole
// days (days, weeks, or hours in multiples of 24) subtracts calendar days, so
// a daylight-saving change keeps both ends on midnight. Calendar forms (today, yesterday, today-<n>d) resolve at at,
// as RFC 0003 defines them for a viewer, and refuse dayStart.
func ResolveWindow(spec string, dayStart bool, loc *time.Location, at time.Time) (Window, error) {
	at = at.In(loc)
	if m := rangeDuration.FindStringSubmatch(spec); m != nil {
		n, _ := strconv.Atoi(m[1])
		return durationWindow(n, m[2], dayStart, at), nil
	}

	calendar := spec == "today" || spec == "yesterday" || rangeCalendarDays.MatchString(spec)
	if !calendar {
		return Window{}, fmt.Errorf("time range %q is not a relative range such as 24h, 7d, today or today-6d", spec)
	}
	if dayStart {
		return Window{}, fmt.Errorf("time range %q is a calendar range and takes the send_time anchor, not day_start", spec)
	}
	today := startOfDay(at, 0)
	switch spec {
	case "today":
		return Window{Start: today, End: at}, nil
	case "yesterday":
		return Window{Start: startOfDay(at, -1), End: today}, nil
	}
	back, _ := strconv.Atoi(rangeCalendarDays.FindStringSubmatch(spec)[1])
	return Window{Start: startOfDay(at, -back), End: at}, nil
}

func durationWindow(n int, unit string, dayStart bool, at time.Time) Window {
	end := at
	if dayStart {
		end = startOfDay(at, 0)
		if days, whole := wholeDays(n, unit); whole {
			return Window{Start: startOfDay(end, -days), End: end}
		}
	}
	step := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[unit]
	return Window{Start: end.Add(-time.Duration(n) * step), End: end}
}

// wholeDays reports a range that spans whole days. 24h counts as one day, so
// the RFC's "24h from day start" is yesterday's midnight to today's on a
// daylight-saving day too.
func wholeDays(n int, unit string) (int, bool) {
	switch {
	case unit == "d":
		return n, true
	case unit == "w":
		return 7 * n, true
	case unit == "h" && n%24 == 0:
		return n / 24, true
	}
	return 0, false
}

// startOfDay is the first instant of the date days after t's date, in t's
// location. Where a daylight-saving change skips midnight, time.Date lands
// on the previous date; the day then starts when the new offset begins, as
// the client resolves it.
func startOfDay(t time.Time, days int) time.Time {
	y, m, d := t.Date()
	start := time.Date(y, m, d+days, 0, 0, 0, 0, t.Location())
	if start.Day() != time.Date(y, m, d+days, 12, 0, 0, 0, t.Location()).Day() {
		_, next := start.ZoneBounds()
		return next
	}
	return start
}
