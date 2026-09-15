package clickhouse

import (
	"errors"
	"fmt"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestHistogramBucketBoundsClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	for _, tc := range []struct {
		name         string
		buckets      int
		groups       int
		grouped      bool
		wantOverflow bool
	}{
		{name: "ungrouped at budget", buckets: models.MaxHistogramBuckets, groups: 1},
		{name: "ungrouped overflow", buckets: models.MaxHistogramBuckets + 1, groups: 1, wantOverflow: true},
		{name: "grouped exact totals at budget", buckets: models.MaxHistogramBuckets, groups: 12, grouped: true},
		{name: "grouped sparse overflow", buckets: models.MaxHistogramBuckets + 1, groups: 1, grouped: true, wantOverflow: true},
		{name: "grouped row overflow", buckets: models.MaxHistogramBuckets + 1, groups: 12, grouped: true, wantOverflow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := HistogramParams{
				Window: TimeWindow1s,
				Query:  fmt.Sprintf("SELECT toDateTime('2026-09-01 23:59:59', 'UTC') + toIntervalSecond(intDiv(number, %d)) AS ts, toString(number %% %d) AS category FROM numbers(%d)", tc.groups, tc.groups, tc.buckets*tc.groups),
			}
			if tc.grouped {
				params.GroupBy = "category"
			}
			result, err := client.GetHistogramData(ctx, "unused", "ts", params)
			if tc.wantOverflow {
				if !errors.Is(err, models.ErrHistogramBudgetExceeded) || result != nil {
					t.Fatalf("expected budget rejection without partial counts, result=%v err=%v", result, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			total := 0
			other := 0
			for _, row := range result.Data {
				total += row.LogCount
				if row.IsOther {
					other += row.LogCount
				}
			}
			if total != tc.buckets*tc.groups {
				t.Fatalf("total=%d, want %d", total, tc.buckets*tc.groups)
			}
			if tc.groups > 10 && other != tc.buckets*(tc.groups-10) {
				t.Fatalf("Other total=%d", other)
			}
		})
	}
}

func TestHistogramDateBoundariesClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	// This inclusive interval spans two local midnights and the spring DST jump.
	result, err := client.GetHistogramData(ctx, "unused", "ts", HistogramParams{
		Window:   TimeWindow24h,
		Timezone: "America/New_York",
		Query:    "SELECT toDateTime('2026-03-07 05:00:00', 'UTC') + toIntervalHour(number) AS ts FROM numbers(48)",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 3 {
		t.Fatalf("buckets=%d, want 3", len(result.Data))
	}
	for i, want := range []int{24, 23, 1} {
		if result.Data[i].LogCount != want {
			t.Fatalf("bucket %d count=%d, want %d", i, result.Data[i].LogCount, want)
		}
	}
}

func TestHistogramByteBoundClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	result, err := client.GetHistogramData(ctx, "unused", "ts", HistogramParams{
		Window:  TimeWindow1m,
		GroupBy: "category",
		Query:   "SELECT now() AS ts, repeat(repeat('x', 1000000), 17) AS category",
	})
	if !errors.Is(err, models.ErrHistogramBudgetExceeded) || result != nil {
		t.Fatalf("expected byte budget rejection without partial counts, err=%v", err)
	}
}
