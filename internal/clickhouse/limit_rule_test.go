package clickhouse

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestTruncationState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opts   QueryOptions
		rows   int
		reason string
		want   models.Truncation
	}{
		{name: "a probe row proves the cut", opts: QueryOptions{MaxRows: 100, Probed: true, LimitSource: models.LimitSourceRequest}, rows: 100, reason: "row_limit", want: models.TruncationRows},
		{name: "the byte budget is a proven cut", opts: QueryOptions{MaxRows: 100, Probed: true, LimitSource: models.LimitSourceRequest}, rows: 7, reason: "byte_limit", want: models.TruncationBytes},
		{name: "a probed cap filled without the extra row is complete", opts: QueryOptions{MaxRows: 100, Probed: true, LimitSource: models.LimitSourceRequest}, rows: 100, want: models.TruncationNone},
		{name: "fewer rows than the cap is complete", opts: QueryOptions{MaxRows: 100, LimitSource: models.LimitSourceRequest}, rows: 99, want: models.TruncationNone},
		{name: "an unprobed cap filled may be incomplete", opts: QueryOptions{MaxRows: 100, LimitSource: models.LimitSourceRequest}, rows: 100, want: models.TruncationUnknown},
		{name: "the statement's own limit filled is what the author asked", opts: QueryOptions{MaxRows: 20, LimitSource: models.LimitSourceStatement}, rows: 20, want: models.TruncationNone},
		{name: "no cap is never a cut", opts: QueryOptions{}, rows: 5000, want: models.TruncationNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.truncation(tc.rows, tc.reason); got != tc.want {
				t.Fatalf("truncation(%d, %q) = %q, want %q", tc.rows, tc.reason, got, tc.want)
			}
		})
	}
}

// TestLimitRuleSourceMaxResultRowsClickHouse runs the review's reproduction:
// source settings that end a result at the request limit, in blocks of that
// size, so no probe row can arrive. A filled result may be incomplete.
func TestLimitRuleSourceMaxResultRowsClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, tc := range []struct {
		maxResultRows int64
		wantRows      int
		wantSource    models.LimitSource
	}{
		{maxResultRows: 100, wantRows: 100, wantSource: models.LimitSourceRequest},
		{maxResultRows: 50, wantRows: 50, wantSource: models.LimitSourceMax},
	} {
		t.Run(fmt.Sprintf("max_result_rows %d", tc.maxResultRows), func(t *testing.T) {
			settings := map[string]any{"max_result_rows": tc.maxResultRows, "max_block_size": 100, "max_threads": 1}
			client, err := NewClient(ClientOptions{Host: addr, QuerySettings: settings}, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			built, err := client.QueryBuilder("", 100000).BuildRawQueryWithLimitPolicy("SELECT number FROM numbers(10000)", 100, 1000, 100000)
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.QueryWithOptions(ctx, built.SQL, built.QueryOptions(30))
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Stats; len(result.Logs) != tc.wantRows || got.LimitSource != tc.wantSource || got.Truncation != models.TruncationUnknown {
				t.Fatalf("%d rows, source %q, truncation %q, want %d rows, source %q, truncation unknown", len(result.Logs), got.LimitSource, got.Truncation, tc.wantRows, tc.wantSource)
			}
		})
	}
}

func TestLimitRuleSilentBreakClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	for _, bound := range []struct{ name, mode string }{
		{"max_result_bytes", "result_overflow_mode"},
		{"max_rows_to_read", "read_overflow_mode"},
		{"max_bytes_to_read", "read_overflow_mode"},
	} {
		for _, sourceSetting := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/source=%v", bound.name, sourceSetting), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				settings := map[string]any{bound.name: int64(100), bound.mode: "break", "max_block_size": 10, "max_threads": 1}
				clientOptions := ClientOptions{Host: addr}
				if sourceSetting {
					clientOptions.QuerySettings = settings
				}
				client, err := NewClient(clientOptions, slog.New(slog.NewTextHandler(io.Discard, nil)))
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				built, err := client.QueryBuilder("", 100000).BuildRawQueryWithLimitPolicy("SELECT number FROM numbers(10000)", 1000, 1000, 100000)
				if err != nil {
					t.Fatal(err)
				}
				opts := built.QueryOptions(10)
				if !sourceSetting {
					maps.Copy(opts.Settings, settings)
				}
				t.Run("buffered", func(t *testing.T) {
					result, err := client.QueryWithOptions(ctx, built.SQL, opts)
					if err != nil {
						t.Fatal(err)
					}
					if result.Stats.Truncation != models.TruncationUnknown {
						t.Fatalf("%d rows, truncation %q, want unknown", len(result.Logs), result.Stats.Truncation)
					}
				})
				t.Run("stream", func(t *testing.T) {
					writer := &statsRowWriter{}
					stats, err := client.QueryStream(ctx, built.SQL, opts, writer)
					if err != nil {
						t.Fatal(err)
					}
					if stats.Truncation != models.TruncationUnknown || writer.stats.Truncation != models.TruncationUnknown {
						t.Fatalf("%d rows, truncation %q, Finish %q, want unknown", stats.RowsReturned, stats.Truncation, writer.stats.Truncation)
					}
				})
			})
		}
	}
}
