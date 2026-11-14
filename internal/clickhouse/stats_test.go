package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
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

func TestBuildIngestionStatsSettings(t *testing.T) {
	source := clickhouse.Settings{"max_execution_time": 120, "read_overflow_mode": "break", "max_rows_to_read": int64(500_000_000)}
	if got := buildIngestionStatsSettings(source, IngestionStatsOptions{}); len(got) != 0 {
		t.Fatalf("non-view overrides = %v, want none", got)
	}
	got := buildIngestionStatsSettings(source, IngestionStatsOptions{EnforceScanLimits: true})
	if len(got) != 2 || got["max_rows_to_read"] != DefaultViewActivityMaxRowsToRead || got["max_bytes_to_read"] != DefaultViewActivityMaxBytesToRead {
		t.Fatalf("view caps = %v", got)
	}
	rows, bytes := int64(250_000), int64(64<<20)
	got = buildIngestionStatsSettings(clickhouse.Settings{"max_rows_to_read": int64(100_000)}, IngestionStatsOptions{EnforceScanLimits: true, MaxRowsToRead: &rows, MaxBytesToRead: &bytes})
	if got["max_rows_to_read"] != int64(100_000) || got["max_bytes_to_read"] != bytes {
		t.Fatalf("stricter caps = %v", got)
	}
}

func TestIsReadLimitExceededError(t *testing.T) {
	t.Parallel()

	if IsReadLimitExceededError(nil) {
		t.Fatal("nil error reported as read limit exceeded")
	}
	for _, code := range []int32{chExceptionTooManyRows, chExceptionTooManyBytes} {
		err := fmt.Errorf("activity failed: %w", publicQueryError(&clickhouse.Exception{Code: code, Message: "limit exceeded"}))
		if !IsReadLimitExceededError(err) {
			t.Fatalf("code %d not classified as read limit exceeded", code)
		}
	}
	timeoutErr := &clickhouse.Exception{Code: chExceptionTimeoutExceeded, Message: "timeout"}
	if IsReadLimitExceededError(timeoutErr) {
		t.Fatal("timeout exception classified as read limit exceeded")
	}
}

func TestActivityErrorTimeout(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, &clickhouse.Exception{Code: 159, Message: "timeout"}} {
		if got := activityError(t.Context(), "activity", err); !errors.Is(got, context.DeadlineExceeded) {
			t.Fatalf("activityError(%v) = %v, want deadline", err, got)
		}
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if got := activityError(ctx, "activity", context.Canceled); !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("expired activity = %v, want deadline", got)
	}
}
