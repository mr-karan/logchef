package clickhouse

import (
	"strings"
	"testing"
	"time"
)

func TestIngestionActivityQueryIsBoundedToOneDay(t *testing.T) {
	query, err := ingestionActivityQuery("logs", "events", "event_time")
	if err != nil {
		t.Fatal(err)
	}
	upper := strings.ToUpper(query)
	if strings.Count(upper, "WHERE") != 1 || !strings.Contains(upper, "INTERVAL 24 HOUR") {
		t.Fatalf("expected exactly one 24 hour WHERE clause, got: %s", query)
	}
	if strings.Count(upper, "<= ANCHOR") != 2 {
		t.Fatalf("expected future rows excluded from the window and 1h count, got: %s", query)
	}
	for _, forbidden := range []string{"INTERVAL 7 DAY", "INTERVAL 30 DAY", "ROWS_7D", "DAILY"} {
		if strings.Contains(upper, forbidden) {
			t.Fatalf("unexpected unbounded summary %q in query: %s", forbidden, query)
		}
	}
	if !strings.Contains(query, "now64(3)") {
		t.Fatalf("expected millisecond anchor, got: %s", query)
	}
}

func TestAccumulateIngestionActivity(t *testing.T) {
	first := time.Date(2025, time.January, 2, 9, 0, 0, 0, time.UTC)
	latest := first.Add(90 * time.Minute)
	stats := accumulateIngestionActivity([]ingestionActivityRow{
		{bucket: first, rows: 4, rows1h: 1, latest: &first},
		{bucket: first.Add(time.Hour), rows: 6, rows1h: 3, latest: &latest},
	})
	if stats.Rows1h != 4 || stats.Rows24h != 10 {
		t.Fatalf("unexpected totals: %#v", stats)
	}
	if stats.LatestTS == nil || !stats.LatestTS.Equal(latest) {
		t.Fatalf("unexpected latest timestamp: %#v", stats.LatestTS)
	}
	if len(stats.HourlyBuckets) != 2 || stats.HourlyBuckets[0].Rows != 4 || !stats.HourlyBuckets[1].Bucket.Equal(first.Add(time.Hour)) {
		t.Fatalf("unexpected hourly aggregation: %#v", stats.HourlyBuckets)
	}
}
