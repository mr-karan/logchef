package jobs

import (
	"errors"
	"fmt"
	"strconv"
	"time"
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

func parseSchedule(kind, expr string) (Schedule, error) {
	if kind != scheduleInterval {
		return nil, fmt.Errorf("jobs: unknown schedule kind %q", kind)
	}
	seconds, err := strconv.ParseInt(expr, 10, 64)
	if err != nil || seconds < 1 || seconds > int64(maxInterval/time.Second) {
		return nil, fmt.Errorf("jobs: invalid interval %q", expr)
	}
	return every{time.Duration(seconds) * time.Second}, nil
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
