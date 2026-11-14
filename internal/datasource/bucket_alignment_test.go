package datasource

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/template"
	"github.com/mr-karan/logchef/pkg/models"
)

// Europe/Berlin leaves summer time on 2026-10-25, so that local day has 25
// hours. The range covers the five local days 2026-10-23 to 2026-10-27, and
// the fixture holds one row every 30 minutes.
var (
	berlinRangeStart = time.Date(2026, 10, 22, 22, 0, 0, 0, time.UTC) // 2026-10-23 00:00 +02:00
	berlinRangeEnd   = time.Date(2026, 10, 27, 23, 0, 0, 0, time.UTC) // 2026-10-28 00:00 +01:00
)

const berlinHalfHourRows = "SELECT toDateTime64('2026-10-22 22:00:00', 3, 'UTC') + toIntervalMinute(number * 30) AS ts FROM system.numbers LIMIT 300"

type alignedBucket struct {
	start time.Time
	count int
}

// Local midnight is 22:00 UTC before the change and 23:00 UTC after it.
var berlinLocalDays = []alignedBucket{
	{time.Date(2026, 10, 22, 22, 0, 0, 0, time.UTC), 48},
	{time.Date(2026, 10, 23, 22, 0, 0, 0, time.UTC), 48},
	{time.Date(2026, 10, 24, 22, 0, 0, 0, time.UTC), 50},
	{time.Date(2026, 10, 25, 23, 0, 0, 0, time.UTC), 48},
	{time.Date(2026, 10, 26, 23, 0, 0, 0, time.UTC), 48},
}

// TestBucketAlignmentAcrossDaylightSavingClickHouse is the ClickHouse half of
// the "Bucket alignment" check of RFC 0003. Count mode and a result query that
// buckets with {{__interval}} and {{__timezone}} both start their day buckets
// at local midnight on both sides of the change.
func TestBucketAlignmentAcrossDaylightSavingClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := clickhouse.NewManager(log)
	defer manager.Close()
	source := &models.Source{ID: 1, MetaTSField: "ts", Connection: models.ConnectionInfo{Host: addr, Database: "system", TableName: "numbers"}}
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	provider := NewClickHouseProvider(manager, log)
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	interval, _ := template.ParseInterval("24h")
	builtins := template.Builtins{From: &berlinRangeStart, To: &berlinRangeEnd, Location: berlin, Interval: interval, TimestampColumn: source.MetaTSField}
	render := func(query string) string {
		t.Helper()
		rendered, err := template.ClickHouseSQL.Substitute(query, nil, builtins)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("rendered SQL: %s", rendered)
		return rendered
	}

	t.Run("count mode", func(t *testing.T) {
		result, err := provider.Histogram(ctx, source, HistogramRequest{
			Query:     render("SELECT ts FROM (" + berlinHalfHourRows + ") WHERE {{__time_filter}}"),
			StartTime: &berlinRangeStart, EndTime: &berlinRangeEnd, Window: "24h", Timezone: "Europe/Berlin",
		})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]alignedBucket, len(result.Data))
		for i, bucket := range result.Data {
			got[i] = alignedBucket{bucket.Bucket.UTC(), bucket.LogCount}
		}
		assertBuckets(t, got, berlinLocalDays)
	})

	t.Run("result mode", func(t *testing.T) {
		result, err := provider.QueryLogs(ctx, source, QueryRequest{
			RawQuery: render("SELECT toStartOfInterval(ts, {{__interval}}, {{__timezone}}) AS t, count() AS n FROM (" + berlinHalfHourRows + ") WHERE {{__time_filter}} GROUP BY t ORDER BY t"),
			Limit:    100, DefaultLimit: 1000, MaxLimit: 1000,
		})
		if err != nil {
			t.Fatal(err)
		}
		got := make([]alignedBucket, len(result.Logs))
		for i, row := range result.Logs {
			start, ok := row["t"].(time.Time)
			count, isCount := row["n"].(uint64)
			if !ok || !isCount {
				t.Fatalf("row %d = %#v, want a time and a count", i, row)
			}
			got[i] = alignedBucket{start.UTC(), int(count)}
		}
		assertBuckets(t, got, berlinLocalDays)
	})
}

func assertBuckets(t *testing.T, got, want []alignedBucket) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d buckets %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if !got[i].start.Equal(want[i].start) || got[i].count != want[i].count {
			t.Errorf("bucket %d = %s with %d rows, want %s with %d", i, got[i].start.Format(time.RFC3339), got[i].count, want[i].start.Format(time.RFC3339), want[i].count)
		}
	}
}
