package loki

import (
	"slices"
	"testing"
	"time"
)

func TestPlanHistogram(t *testing.T) {
	const minute = int64(60_000)
	tests := []struct {
		name       string
		start, end int64
		want       []histogramQuery
	}{
		{"inside one bucket is one instant query at end", 10_000, 50_000, []histogramQuery{
			{instant: true, at: 50_000, width: 40_000, bucket: 0},
		}},
		{"an end on the bucket's boundary stays one bucket", 10_000, minute, []histogramQuery{
			{instant: true, at: minute, width: 50_000, bucket: 0},
		}},
		{"an aligned range is one range query", minute, 4 * minute, []histogramQuery{
			{start: 2 * minute, end: 4 * minute, width: minute},
		}},
		{"leading partial only", 30_000, 3 * minute, []histogramQuery{
			{instant: true, at: minute, width: 30_000, bucket: 0},
			{start: 2 * minute, end: 3 * minute, width: minute},
		}},
		{"trailing partial only", minute, 3*minute + 5_000, []histogramQuery{
			{start: 2 * minute, end: 3 * minute, width: minute},
			{instant: true, at: 3*minute + 5_000, width: 5_000, bucket: 3 * minute},
		}},
		{"two partials and no full bucket", 30_000, minute + 10_000, []histogramQuery{
			{instant: true, at: minute, width: 30_000, bucket: 0},
			{instant: true, at: minute + 10_000, width: 10_000, bucket: minute},
		}},
		{"both partials around full buckets", 15_000, 3*minute + 20_000, []histogramQuery{
			{instant: true, at: minute, width: 45_000, bucket: 0},
			{start: 2 * minute, end: 3 * minute, width: minute},
			{instant: true, at: 3*minute + 20_000, width: 20_000, bucket: 3 * minute},
		}},
		{"an empty range has no request", minute, minute, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := planHistogram(tt.start, tt.end, minute)
			if !slices.Equal(got, tt.want) {
				t.Fatalf("plan\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestParseHistogramWindow(t *testing.T) {
	for window, want := range map[string]time.Duration{"500ms": 500 * time.Millisecond, "30s": 30 * time.Second, "1h30m": 90 * time.Minute, "1d": 24 * time.Hour, "2w": 14 * 24 * time.Hour} {
		if got, err := parseHistogramWindow(window); err != nil || got != want {
			t.Errorf("%s: got %v, %v", window, got, err)
		}
	}
	for _, window := range []string{"0s", "-1m", "1.5d", "1us", "1500us", "abc"} {
		if _, err := parseHistogramWindow(window); err == nil {
			t.Errorf("%s: want an error", window)
		}
	}
}
