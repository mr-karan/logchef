package reports

import (
	"testing"
	"time"
)

func TestResolveWindow(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	utc := func(s string) time.Time {
		t.Helper()
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// New York springs forward on 2026-03-08 (EST -05:00 to EDT -04:00) and
	// falls back on 2026-11-01. Expected instants follow RFC 0008 "The
	// range": a rolling range ends at T and subtracts exact time; day_start
	// ends at local midnight and subtracts calendar days for a whole-day
	// range, so "24h" from day start is yesterday's midnight to today's.
	afterSpring := utc("2026-03-09T13:00:00Z") // 09:00 EDT
	afterFall := utc("2026-11-02T14:00:00Z")   // 09:00 EST
	cases := []struct {
		name       string
		spec       string
		dayStart   bool
		at         time.Time
		start, end string
	}{
		{"24h at send time, spring", "24h", false, afterSpring, "2026-03-08T13:00:00Z", "2026-03-09T13:00:00Z"},
		{"24h at day start, spring: a 23-hour day", "24h", true, afterSpring, "2026-03-08T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"7d at send time, spring: exact hours", "7d", false, afterSpring, "2026-03-02T13:00:00Z", "2026-03-09T13:00:00Z"},
		{"7d at day start, spring: midnight to midnight", "7d", true, afterSpring, "2026-03-02T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"24h at send time, fall", "24h", false, afterFall, "2026-11-01T14:00:00Z", "2026-11-02T14:00:00Z"},
		{"24h at day start, fall: a 25-hour day", "24h", true, afterFall, "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z"},
		{"7d at send time, fall", "7d", false, afterFall, "2026-10-26T14:00:00Z", "2026-11-02T14:00:00Z"},
		{"7d at day start, fall", "7d", true, afterFall, "2026-10-26T04:00:00Z", "2026-11-02T05:00:00Z"},
		{"1w at day start equals 7d", "1w", true, afterFall, "2026-10-26T04:00:00Z", "2026-11-02T05:00:00Z"},
		{"12h at day start subtracts hours", "12h", true, afterSpring, "2026-03-08T16:00:00Z", "2026-03-09T04:00:00Z"},
		{"48h at day start is two calendar days", "48h", true, afterSpring, "2026-03-07T05:00:00Z", "2026-03-09T04:00:00Z"},
		{"36h at day start subtracts hours", "36h", true, afterSpring, "2026-03-07T16:00:00Z", "2026-03-09T04:00:00Z"},
		{"30m at send time", "30m", false, afterSpring, "2026-03-09T12:30:00Z", "2026-03-09T13:00:00Z"},
		{"today-7d across spring", "today-7d", false, afterSpring, "2026-03-02T05:00:00Z", "2026-03-09T13:00:00Z"},
		{"today", "today", false, afterFall, "2026-11-02T05:00:00Z", "2026-11-02T14:00:00Z"},
		{"yesterday across fall: 25 hours", "yesterday", false, afterFall, "2026-11-01T04:00:00Z", "2026-11-02T05:00:00Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, err := ResolveWindow(tc.spec, tc.dayStart, ny, tc.at)
			if err != nil {
				t.Fatal(err)
			}
			if !w.Start.Equal(utc(tc.start)) || !w.End.Equal(utc(tc.end)) {
				t.Errorf("window = %s to %s, want %s to %s", w.Start.UTC().Format(time.RFC3339), w.End.UTC().Format(time.RFC3339), tc.start, tc.end)
			}
		})
	}

	// Santiago skips midnight on 2026-09-06: clocks go from 23:59:59 -04 to
	// 01:00 -03. That day starts at 01:00 -03, as the client resolves it.
	santiago, err := time.LoadLocation("America/Santiago")
	if err != nil {
		t.Fatal(err)
	}
	nineAM := utc("2026-09-06T12:00:00Z")
	for _, tc := range []struct {
		spec       string
		dayStart   bool
		start, end string
	}{
		{"24h", true, "2026-09-05T04:00:00Z", "2026-09-06T04:00:00Z"},
		{"today", false, "2026-09-06T04:00:00Z", "2026-09-06T12:00:00Z"},
		{"yesterday", false, "2026-09-05T04:00:00Z", "2026-09-06T04:00:00Z"},
	} {
		w, err := ResolveWindow(tc.spec, tc.dayStart, santiago, nineAM)
		if err != nil {
			t.Fatal(err)
		}
		if !w.Start.Equal(utc(tc.start)) || !w.End.Equal(utc(tc.end)) {
			t.Errorf("Santiago %s dayStart=%v = %s to %s, want %s to %s", tc.spec, tc.dayStart, w.Start.UTC().Format(time.RFC3339), w.End.UTC().Format(time.RFC3339), tc.start, tc.end)
		}
	}

	for _, bad := range []struct {
		spec     string
		dayStart bool
	}{
		{"", false}, {"0h", false}, {"24", false}, {"2y", false}, {"today-0d", false}, {"now-1h", false},
		{"today", true}, {"yesterday", true}, {"today-7d", true},
	} {
		if _, err := ResolveWindow(bad.spec, bad.dayStart, ny, afterSpring); err == nil {
			t.Errorf("ResolveWindow(%q, dayStart=%v) passed", bad.spec, bad.dayStart)
		}
	}
}
