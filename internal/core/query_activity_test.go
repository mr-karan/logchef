package core

import (
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestQueryActivityWindows(t *testing.T) {
	for _, tc := range []struct {
		name         string
		now          time.Time
		days         int
		current      DateWindow
		prior        DateWindow
		shapeSince   string
		shapePrior   string
		shapePriorOK bool
	}{
		{
			// 23:30 in New York is already the next day in UTC.
			name:    "late evening west of UTC",
			now:     time.Date(2026, 3, 1, 23, 30, 0, 0, time.FixedZone("EST", -5*3600)),
			days:    7,
			current: DateWindow{Since: "2026-02-24", Until: "2026-03-02", Days: 7},
			prior:   DateWindow{Since: "2026-02-17", Until: "2026-02-23", Days: 7},
			// The shape rollup keeps both windows.
			shapeSince: "2026-02-24", shapePrior: "2026-02-17", shapePriorOK: true,
		},
		{
			name:       "one day",
			now:        time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
			days:       1,
			current:    DateWindow{Since: "2026-03-02", Until: "2026-03-02", Days: 1},
			prior:      DateWindow{Since: "2026-03-01", Until: "2026-03-01", Days: 1},
			shapeSince: "2026-03-02", shapePrior: "2026-03-01", shapePriorOK: true,
		},
		{
			name:       "across a leap day",
			now:        time.Date(2028, 3, 1, 12, 0, 0, 0, time.UTC),
			days:       30,
			current:    DateWindow{Since: "2028-02-01", Until: "2028-03-01", Days: 30},
			prior:      DateWindow{Since: "2028-01-02", Until: "2028-01-31", Days: 30},
			shapeSince: "2028-02-01", shapePrior: "2028-01-02", shapePriorOK: true,
		},
		{
			name:    "90 days keeps the window, not the prior one",
			now:     time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC),
			days:    90,
			current: DateWindow{Since: "2026-08-23", Until: "2026-11-20", Days: 90},
			prior:   DateWindow{Since: "2026-05-25", Until: "2026-08-22", Days: 90},
			// No prior rows are read: PriorSince equals Since.
			shapeSince: "2026-08-23", shapePrior: "2026-08-23", shapePriorOK: false,
		},
		{
			name:       "a year clamps the shapes to 90 days",
			now:        time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC),
			days:       365,
			current:    DateWindow{Since: "2025-11-21", Until: "2026-11-20", Days: 365},
			prior:      DateWindow{Since: "2024-11-21", Until: "2025-11-20", Days: 365},
			shapeSince: "2026-08-23", shapePrior: "2026-08-23", shapePriorOK: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, prior := QueryActivityWindows(tc.now, tc.days)
			if current != tc.current || prior != tc.prior {
				t.Fatalf("windows = %+v, %+v, want %+v, %+v", current, prior, tc.current, tc.prior)
			}
			f, priorOK := shapeWindow(tc.now, current, prior, 0)
			if f.Since != tc.shapeSince || f.PriorSince != tc.shapePrior || f.Until != tc.current.Until || priorOK != tc.shapePriorOK {
				t.Fatalf("shape window = %+v (prior kept %v), want since %s, prior since %s, until %s (prior kept %v)",
					f, priorOK, tc.shapeSince, tc.shapePrior, tc.current.Until, tc.shapePriorOK)
			}
		})
	}
}

func TestRunHealthWithoutCost(t *testing.T) {
	if h := runHealth(models.RunCost{}, 0); h.Cost != nil || h.FailureCount != 0 || h.FailureRate != nil {
		t.Errorf("an idle window = %+v, want no cost and no rate", h)
	}
	// Runs before cost collection began recorded no cost: only failures since
	// then are known, and all of those runs failed.
	if h := runHealth(models.RunCost{}, 2); h.Cost != nil || h.FailureRate == nil || *h.FailureRate != 1 {
		t.Errorf("failures alone = %+v, want no cost and a rate of 1", h)
	}
	h := runHealth(models.RunCost{Latency: models.LatencyHistogram{9: 2}, RowsRead: 10, BytesRead: 30}, 0)
	want := RunCostSummary{Runs: 2, P50: models.Percentile{Ms: 60_000, Open: true}, P95: models.Percentile{Ms: 60_000, Open: true}, AvgRowsRead: 5, AvgBytesRead: 15}
	if h.Cost == nil || *h.Cost != want || h.FailureRate == nil || *h.FailureRate != 0 {
		t.Errorf("slow runs = %+v, want %+v and a rate of 0", h.Cost, want)
	}
}
