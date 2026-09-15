package clickhouse

import (
	"context"
	"errors"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/mr-karan/logchef/internal/metrics"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestMetadataLateErrorsClickHouse(t *testing.T) {
	for _, failure := range []string{"scan", "iteration", "close", "cancellation"} {
		t.Run(failure, func(t *testing.T) {
			client, ctx := integrationClient(t)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			source := &models.Source{Name: t.Name()}
			client.metrics = metrics.NewClickHouseMetrics(source)
			successes := queryCounter(source, "select", "success")
			failures := queryCounter(source, "select", "failure")
			beforeSuccess, beforeFailure := successes.Get(), failures.Get()
			query := "SELECT number FROM numbers(10) SETTINGS max_block_size=1, max_threads=1"
			if failure == "iteration" || failure == "close" {
				query = "SELECT number, throwIf(number = 5) FROM numbers(10) SETTINGS max_block_size=1, max_threads=1"
			}
			if failure == "cancellation" {
				query = "SELECT number, sleepEachRow(0.01) FROM numbers(10000) SETTINGS max_block_size=1, max_threads=1"
			}
			consumed := false
			start := time.Now()
			err := client.queryMetadata(ctx, query, func(rows driver.Rows) error {
				if successes.Get() != beforeSuccess || failures.Get() != beforeFailure {
					t.Fatal("completion recorded before row consumption")
				}
				if !rows.Next() {
					t.Fatalf("expected a row before the late failure: %v", rows.Err())
				}
				consumed = true
				if failure == "cancellation" {
					cancel()
					return ctx.Err()
				}
				if failure == "scan" {
					var invalid time.Time
					return rows.Scan(&invalid)
				}
				var number uint64
				var thrown uint8
				if err := rows.Scan(&number, &thrown); err != nil {
					t.Fatalf("first row must scan successfully: %v", err)
				}
				if failure == "iteration" {
					for rows.Next() {
					}
					return rows.Err()
				}
				// Single-row metadata readers leave subsequent packets to Close.
				return nil
			})
			if !consumed || err == nil {
				t.Fatalf("consumed=%v error=%v", consumed, err)
			}
			if failure == "iteration" || failure == "close" {
				var exception *ch.Exception
				if !errors.As(err, &exception) || exception.Code != 395 {
					t.Fatalf("late server exception was lost: %v", err)
				}
			}
			if failure == "cancellation" && (!errors.Is(err, context.Canceled) || time.Since(start) > 3*time.Second) {
				t.Fatalf("cancellation did not stop the reader promptly: duration=%s error=%v", time.Since(start), err)
			}
			if successes.Get() != beforeSuccess || failures.Get()-beforeFailure != 1 {
				t.Fatalf("success count=%d failure count=%d", successes.Get()-beforeSuccess, failures.Get()-beforeFailure)
			}
			if _, err := client.Query(context.Background(), "SELECT 1"); err != nil {
				t.Fatalf("connection unusable after metadata failure: %v", err)
			}
		})
	}
}

func TestMetadataMetricsOnceClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	source := &models.Source{Name: t.Name()}
	client.metrics = metrics.NewClickHouseMetrics(source)
	successes := queryCounter(source, "select", "success")
	failures := queryCounter(source, "select", "failure")
	beforeSuccess, beforeFailure := successes.Get(), failures.Get()
	if _, _, _, err := client.getTableEngine(ctx, "system", "tables"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.getExtendedColumns(ctx, "system", "tables"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.getColumns(ctx, "system", "tables"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.getSortKeys(ctx, "system", "tables"); err != nil {
		t.Fatal(err)
	}
	if successes.Get()-beforeSuccess != 4 || failures.Get() != beforeFailure {
		t.Fatalf("success count=%d failure count=%d", successes.Get()-beforeSuccess, failures.Get()-beforeFailure)
	}
}
