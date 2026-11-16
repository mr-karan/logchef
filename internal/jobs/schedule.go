package jobs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Schedule says when a job is due.
type Schedule interface {
	// Next returns the first occurrence after t.
	Next(t time.Time) time.Time
	// stored returns the schedule's columns, or an error when it cannot be
	// stored.
	stored() (kind, expr, timezone string, err error)
}

const scheduleInterval = "interval"

// Every returns a schedule that fires every d. d must be a positive whole
// number of seconds up to 100 years; Upsert rejects any other value.
func Every(d time.Duration) Schedule { return every{d} }

type every struct{ d time.Duration }

func (e every) Next(t time.Time) time.Time { return t.Add(e.d) }

func (e every) stored() (kind, expr, timezone string, err error) {
	if e.d < time.Second || e.d > maxInterval || e.d%time.Second != 0 {
		return "", "", "", fmt.Errorf("jobs: interval %s is not a positive whole number of seconds", e.d)
	}
	return scheduleInterval, strconv.FormatInt(int64(e.d/time.Second), 10), "UTC", nil
}

func parseSchedule(kind, expr, timezone string) (Schedule, error) {
	switch kind {
	case scheduleInterval:
		seconds, err := strconv.ParseInt(expr, 10, 64)
		if err != nil || seconds < 1 || seconds > int64(maxInterval/time.Second) {
			return nil, fmt.Errorf("jobs: invalid interval %q", expr)
		}
		return every{time.Duration(seconds) * time.Second}, nil
	case scheduleCron:
		return Cron(expr, timezone)
	default:
		return nil, fmt.Errorf("jobs: unknown schedule kind %q", kind)
	}
}

const scheduleCron = "cron"

// cronParser reads exactly five fields: minute, hour, day of month, month
// and day of week. It accepts no descriptors such as @daily.
var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Cron returns a schedule of five cron fields (minute, hour, day of month,
// month, day of week) read as wall-clock times in timezone, an IANA name.
//
// Each wall-clock time fires once across daylight-saving changes. A time
// that a forward change removes fires at the instant the clocks jump, once
// however many removed times the schedule names. A time that a backward
// change repeats fires at its first instance only. The cron library alone
// does neither, and with a half-hour change it skips whole days, so it is
// used only to match fields against wall-clock times in UTC, which has no
// changes.
func Cron(expr, timezone string) (Schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("jobs: cron expression %q must have five fields", expr)
	}
	if timezone == "" || timezone == "Local" {
		return nil, fmt.Errorf("jobs: cron timezone %q is not an IANA name", timezone)
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("jobs: cron timezone %q: %w", timezone, err)
	}
	normalized := strings.Join(fields, " ")
	parsed, err := cronParser.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("jobs: cron expression %q: %w", expr, err)
	}
	spec, ok := parsed.(*cron.SpecSchedule)
	if !ok {
		return nil, fmt.Errorf("jobs: cron expression %q is not a field schedule", expr)
	}
	wall := *spec
	wall.Location = time.UTC
	c := cronSchedule{expr: normalized, timezone: timezone, loc: loc, wall: &wall}
	if c.wallNext(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)).IsZero() {
		return nil, fmt.Errorf("jobs: cron expression %q never fires", expr)
	}
	return c, nil
}

type cronSchedule struct {
	expr     string
	timezone string
	loc      *time.Location
	// wall matches the fields in UTC, so it tests wall-clock readings: a
	// UTC time stands for the reading with the same fields.
	wall *cron.SpecSchedule
}

func (c cronSchedule) stored() (kind, expr, timezone string, err error) {
	return scheduleCron, c.expr, c.timezone, nil
}

// Next returns the first occurrence after t: the instant of the next
// wall-clock time the fields name whose instant is after t. A repeated time
// whose first instance is not after t is passed over.
func (c cronSchedule) Next(t time.Time) time.Time {
	w := wallReading(t.In(c.loc))
	for {
		w = c.wallNext(w)
		if w.IsZero() {
			return w
		}
		if at := c.instant(w); at.After(t) {
			return at
		}
	}
}

// maxWallSearches bounds wallNext to about 500 years. Every schedule that
// fires at all fires within eight years.
const maxWallSearches = 100

// wallNext returns the first wall-clock time after w that the fields name.
// The library gives up after five years, which a schedule on 29 February
// can need (2096 to 2104), so the search resumes where the library stopped.
func (c cronSchedule) wallNext(w time.Time) time.Time {
	for range maxWallSearches {
		if next := c.wall.Next(w); !next.IsZero() {
			return next
		}
		// The library searched at least to the end of w's year plus four.
		w = time.Date(w.Year()+5, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Second)
	}
	return time.Time{}
}

// instant returns the first instant at which the clocks of c.loc read w. A
// reading that a forward change removes returns the instant of that change.
func (c cronSchedule) instant(w time.Time) time.Time {
	// An instant that reads w lies within a day of w taken as UTC, so the
	// offsets in effect a day before and after cover every candidate.
	const day = 24 * time.Hour
	var first time.Time
	minOffset, maxOffset := 0, 0
	for i, probe := range []time.Duration{-day, 0, day} {
		offset := zoneOffset(w.Add(probe), c.loc)
		if i == 0 || offset < minOffset {
			minOffset = offset
		}
		if i == 0 || offset > maxOffset {
			maxOffset = offset
		}
		at := w.Add(-time.Duration(offset) * time.Second)
		if zoneOffset(at, c.loc) == offset && (first.IsZero() || at.Before(first)) {
			first = at
		}
	}
	if !first.IsZero() {
		return first
	}
	// w falls in a forward change. Before the change the offset is the
	// smaller one, so w minus the larger offset is before it and w minus the
	// smaller one is after it. The change is on a whole second.
	before := w.Add(-time.Duration(maxOffset) * time.Second)
	after := w.Add(-time.Duration(minOffset) * time.Second)
	old := zoneOffset(before, c.loc)
	for after.Sub(before) > time.Second {
		mid := before.Add(after.Sub(before) / 2).Truncate(time.Second)
		if zoneOffset(mid, c.loc) == old {
			before = mid
		} else {
			after = mid
		}
	}
	return after
}

func zoneOffset(t time.Time, loc *time.Location) int {
	_, offset := t.In(loc).Zone()
	return offset
}

// wallReading returns the wall-clock reading of t in its location, as a UTC
// time with the same fields.
func wallReading(t time.Time) time.Time {
	_, offset := t.Zone()
	return t.UTC().Add(time.Duration(offset) * time.Second)
}

// maxInterval keeps interval arithmetic far from time.Duration overflow.
const maxInterval = 100 * 365 * 24 * time.Hour

const (
	// searchWindow bounds how far back the search for the latest due
	// occurrence starts. The finest cron schedule fires once a minute, so
	// the window holds at most 10,080 occurrences.
	searchWindow = 7 * 24 * time.Hour
	// maxOccurrences is the hard cap on the occurrences one search visits.
	maxOccurrences = 50_000
)

var errSearchCap = errors.New("jobs: the search for the latest due occurrence reached its cap")

// latestDue returns the latest occurrence of s that is not after now, given
// that nextRunAt, the stored due time, is an occurrence and is not after now.
// That occurrence is the time a run stands for; older missed occurrences are
// dropped. A search that would visit more than limit occurrences returns
// errSearchCap and no time, so a caller never runs an intermediate one.
func latestDue(s Schedule, nextRunAt, now time.Time, limit int) (time.Time, error) {
	if e, ok := s.(every); ok {
		// Interval occurrences are nextRunAt + k*d, so the latest one is
		// computed directly. A 10-second interval holds 60,480 occurrences in
		// the search window, more than the cap, so it must not be walked.
		if !now.After(nextRunAt) {
			return nextRunAt, nil
		}
		elapsed := now.Sub(nextRunAt)
		return nextRunAt.Add(elapsed - elapsed%e.d), nil
	}
	// An occurrence older than the window cannot be the latest due one for a
	// schedule that fires at least once in the window, so the walk starts at
	// the window's first occurrence when there is one. A schedule that fires
	// less often walks from nextRunAt, which takes few steps.
	start := nextRunAt
	if from := now.Add(-searchWindow); from.After(nextRunAt) {
		if first := s.Next(from.Add(-time.Nanosecond)); !first.After(now) {
			start = first
		}
	}
	at := start
	for visited := 1; ; visited++ {
		next := s.Next(at)
		if next.After(now) {
			return at, nil
		}
		if visited >= limit {
			return time.Time{}, errSearchCap
		}
		at = next
	}
}
