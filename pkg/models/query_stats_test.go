package models

import "testing"

func TestLatencyHistogramOfPlacesEachBound(t *testing.T) {
	for _, tc := range []struct {
		ms     int64
		bucket int
	}{
		{0, 0}, {100, 0}, {101, 1}, {250, 1}, {999, 3}, {1_000, 3}, {2_500, 4},
		{60_000, 8}, {60_001, 9}, {3_600_000, 9},
	} {
		h := LatencyHistogramOf(tc.ms)
		if h[tc.bucket] != 1 || h.Count() != 1 {
			t.Errorf("LatencyHistogramOf(%d) = %v, want one run in bucket %d", tc.ms, h, tc.bucket)
		}
	}
}

func TestLatencyHistogramQuantile(t *testing.T) {
	var spread LatencyHistogram
	spread[0], spread[1], spread[3] = 50, 40, 10 // 100 runs
	for _, tc := range []struct {
		name string
		h    LatencyHistogram
		q    float64
		want Percentile
		ok   bool
	}{
		{"empty", LatencyHistogram{}, 0.5, Percentile{}, false},
		{"one run in the first bucket", LatencyHistogramOf(40), 0.5, Percentile{Ms: 50}, true},
		{"one run in the first bucket, p95", LatencyHistogramOf(40), 0.95, Percentile{Ms: 95}, true},
		{"single middle bucket", LatencyHistogram{0, 0, 0, 0, 8}, 0.5, Percentile{Ms: 1_750}, true},
		{"median at a bucket edge", spread, 0.5, Percentile{Ms: 100}, true},
		{"p95 interpolates in a later bucket", spread, 0.95, Percentile{Ms: 750}, true},
		{"skips empty buckets", spread, 0.91, Percentile{Ms: 550}, true},
		{"all runs in the open bucket", LatencyHistogram{9: 3}, 0.5, Percentile{Ms: 60_000, Open: true}, true},
		{"p95 in the open bucket", LatencyHistogram{0: 90, 9: 10}, 0.95, Percentile{Ms: 60_000, Open: true}, true},
		{"p50 below the open bucket", LatencyHistogram{0: 90, 9: 10}, 0.5, Percentile{Ms: 56}, true},
	} {
		got, ok := tc.h.Quantile(tc.q)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%s: Quantile(%v) = %+v, %v; want %+v, %v", tc.name, tc.q, got, ok, tc.want, tc.ok)
		}
	}
}
