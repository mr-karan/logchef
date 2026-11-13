package clickhouse

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
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
			if got := tc.opts.truncation(tc.rows, tc.reason, queryEnd{}); got != tc.want {
				t.Fatalf("truncation(%d, %q) = %q, want %q", tc.rows, tc.reason, got, tc.want)
			}
		})
	}
}

// TestTruncationBreakLimits holds a short result to the limits that
// ClickHouse ends silently: the query's settings override the server's
// defaults, and a counter below a limit proves that the limit did not cut.
func TestTruncationBreakLimits(t *testing.T) {
	probed := QueryOptions{MaxRows: 100, Probed: true, LimitSource: models.LimitSourceRequest}
	for _, tc := range []struct {
		name     string
		defaults map[string]any
		sent     clickhouse.Settings
		end      queryEnd
		want     models.Truncation
	}{
		{name: "a byte cap the result stayed under", sent: clickhouse.Settings{"max_result_bytes": int64(1_000_000), "result_overflow_mode": "break"},
			end: queryEnd{profiled: true, resultBytes: 4104}, want: models.TruncationNone},
		{name: "a byte cap the result reached", sent: clickhouse.Settings{"max_result_bytes": int64(1_000_000), "result_overflow_mode": "break"},
			end: queryEnd{profiled: true, resultBytes: 1_000_000}, want: models.TruncationUnknown},
		{name: "a byte cap without the result profile", sent: clickhouse.Settings{"max_result_bytes": int64(1_000_000), "result_overflow_mode": "break"},
			end: queryEnd{}, want: models.TruncationUnknown},
		{name: "a byte cap that throws", sent: clickhouse.Settings{"max_result_bytes": int64(100), "result_overflow_mode": "throw"},
			end: queryEnd{profiled: true, resultBytes: 1_000_000}, want: models.TruncationNone},
		{name: "a profile byte cap under the break mode the query sends", defaults: map[string]any{"max_result_bytes": "100"}, sent: clickhouse.Settings{"result_overflow_mode": "break"},
			end: queryEnd{profiled: true, resultBytes: 2704}, want: models.TruncationUnknown},
		{name: "a source byte cap overrides the profile's", defaults: map[string]any{"max_result_bytes": "100"}, sent: clickhouse.Settings{"max_result_bytes": int64(0), "result_overflow_mode": "break"},
			end: queryEnd{profiled: true, resultBytes: 2704}, want: models.TruncationNone},
		{name: "a sent overflow mode overrides the profile's", defaults: map[string]any{"max_result_bytes": "100", "result_overflow_mode": "break"}, sent: clickhouse.Settings{"result_overflow_mode": "throw"},
			end: queryEnd{profiled: true, resultBytes: 2704}, want: models.TruncationNone},
		{name: "a profile row read cap the query reached", defaults: map[string]any{"max_rows_to_read": "100", "read_overflow_mode": "break"},
			end: queryEnd{rowsRead: 100}, want: models.TruncationUnknown},
		{name: "a profile row read cap the query stayed under", defaults: map[string]any{"max_rows_to_read": "100", "read_overflow_mode": "break"},
			end: queryEnd{rowsRead: 99}, want: models.TruncationNone},
		{name: "a byte read cap the query reached", defaults: map[string]any{"read_overflow_mode": "break"}, sent: clickhouse.Settings{"max_bytes_to_read": int64(800)},
			end: queryEnd{bytesRead: 880}, want: models.TruncationUnknown},
		{name: "a byte read cap the query stayed under", defaults: map[string]any{"read_overflow_mode": "break"}, sent: clickhouse.Settings{"max_bytes_to_read": int64(800)},
			end: queryEnd{bytesRead: 400}, want: models.TruncationNone},
		{name: "a read cap that throws", sent: clickhouse.Settings{"max_rows_to_read": int64(100)},
			end: queryEnd{rowsRead: 5000}, want: models.TruncationNone},
		{name: "a profile timeout in break mode the query ran into", defaults: map[string]any{"timeout_overflow_mode": "break"}, sent: clickhouse.Settings{"max_execution_time": 30},
			end: queryEnd{elapsed: 30 * time.Second}, want: models.TruncationUnknown},
		{name: "a profile timeout in break mode the query finished before", defaults: map[string]any{"timeout_overflow_mode": "break"}, sent: clickhouse.Settings{"max_execution_time": 30},
			end: queryEnd{elapsed: 29 * time.Second}, want: models.TruncationNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			end := tc.end
			end.settings = overlaySettings(tc.defaults, tc.sent)
			if got := probed.truncation(5, "", end); got != tc.want {
				t.Fatalf("truncation = %q, want %q", got, tc.want)
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

// TestLimitRuleServerDefaultsClickHouse sets limits where Logchef does not
// send them: in a ClickHouse user's settings profile, and as connection
// settings for servers that do not allow creating users. A limit that the
// query reached reports unknown, and one that it stayed under reports none.
func TestLimitRuleServerDefaultsClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	const blocksOfTen = " SETTINGS max_block_size = 10, max_threads = 1"
	for _, tc := range []struct {
		name     string
		defaults clickhouse.Settings
		query    string
		wantRows int
		want     models.Truncation
	}{
		{name: "byte cap reached", defaults: clickhouse.Settings{"max_result_bytes": 100},
			query: "SELECT number FROM numbers(10000)" + blocksOfTen, wantRows: 20, want: models.TruncationUnknown},
		{name: "byte cap not reached", defaults: clickhouse.Settings{"max_result_bytes": 1_000_000},
			query: "SELECT number FROM numbers(30)", wantRows: 30, want: models.TruncationNone},
		{name: "read cap reached", defaults: clickhouse.Settings{"max_rows_to_read": 50, "read_overflow_mode": "break"},
			query: "SELECT number FROM numbers(10000)" + blocksOfTen, wantRows: 50, want: models.TruncationUnknown},
		{name: "read cap not reached", defaults: clickhouse.Settings{"max_rows_to_read": 50, "read_overflow_mode": "break"},
			query: "SELECT number FROM numbers(30)", wantRows: 30, want: models.TruncationNone},
		{name: "timeout in break mode not reached", defaults: clickhouse.Settings{"timeout_overflow_mode": "break"},
			query: "SELECT number FROM numbers(30)", wantRows: 30, want: models.TruncationNone},
		{name: "row cap below the probe row is overridden", defaults: clickhouse.Settings{"max_result_rows": 50},
			query: "SELECT number FROM numbers(10000)", wantRows: 100, want: models.TruncationRows},
	} {
		for _, where := range []string{"profile", "connection"} {
			t.Run(tc.name+"/"+where, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				options := ClientOptions{Host: addr, Settings: tc.defaults}
				if where == "profile" {
					options = ClientOptions{Host: addr, Username: profileUser(t, addr, tc.defaults)}
				}
				client, err := NewClient(options, slog.New(slog.NewTextHandler(io.Discard, nil)))
				if err != nil {
					t.Fatal(err)
				}
				defer client.Close()
				built, err := client.QueryBuilder("", 100000).BuildRawQueryWithLimitPolicy(tc.query, 100, 1000, 100000)
				if err != nil {
					t.Fatal(err)
				}
				result, err := client.QueryWithOptions(ctx, built.SQL, built.QueryOptions(10))
				if err != nil {
					t.Fatal(err)
				}
				if len(result.Logs) != tc.wantRows || result.Stats.Truncation != tc.want {
					t.Fatalf("buffered: %d rows, truncation %q, want %d rows, %q", len(result.Logs), result.Stats.Truncation, tc.wantRows, tc.want)
				}
				writer := &statsRowWriter{}
				stats, err := client.QueryStream(ctx, built.SQL, built.QueryOptions(10), writer)
				if err != nil {
					t.Fatal(err)
				}
				if stats.RowsReturned != tc.wantRows || stats.Truncation != tc.want || writer.stats.Truncation != tc.want {
					t.Fatalf("stream: %d rows, truncation %q, Finish %q, want %d rows, %q", stats.RowsReturned, stats.Truncation, writer.stats.Truncation, tc.wantRows, tc.want)
				}
			})
		}
	}
}

// profileUser creates a ClickHouse user whose settings profile holds
// settings, and skips when the server does not allow creating users.
func profileUser(t *testing.T, addr string, settings clickhouse.Settings) string {
	t.Helper()
	admin, err := NewClient(ClientOptions{Host: addr}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	clauses := make([]string, 0, len(settings))
	for name, value := range settings {
		if text, ok := value.(string); ok {
			value = "'" + text + "'"
		}
		clauses = append(clauses, fmt.Sprintf("%s = %v", name, value))
	}
	name := fmt.Sprintf("logchef_test_%d", time.Now().UnixNano())
	if err := admin.conn.Exec(t.Context(), fmt.Sprintf("CREATE USER %s IDENTIFIED WITH no_password SETTINGS %s", name, strings.Join(clauses, ", "))); err != nil {
		t.Skipf("this ClickHouse server does not allow creating users: %v", err)
	}
	t.Cleanup(func() { _ = admin.conn.Exec(context.Background(), "DROP USER IF EXISTS "+name) })
	return name
}

// TestLimitRuleServerDefaultsReadFailureClickHouse fails the read of the
// server's defaults: results keep their settled state, the failure is logged
// once, and the next query reads the defaults again.
func TestLimitRuleServerDefaultsReadFailureClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var logs bytes.Buffer
	client, err := NewClient(ClientOptions{
		Host:          addr,
		Settings:      clickhouse.Settings{"max_rows_to_read": 50, "read_overflow_mode": "break"},
		QuerySettings: map[string]any{"max_result_bytes": int64(1_000_000)},
	}, slog.New(slog.NewTextHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	run := func(query string) models.Truncation {
		t.Helper()
		built, err := client.QueryBuilder("", 100000).BuildRawQueryWithLimitPolicy(query, 100, 1000, 100000)
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.QueryWithOptions(ctx, built.SQL, built.QueryOptions(10))
		if err != nil {
			t.Fatal(err)
		}
		return result.Stats.Truncation
	}

	working := serverDefaultsQuery
	serverDefaultsQuery = "SELECT name, value FROM system.logchef_missing_table"
	t.Cleanup(func() { serverDefaultsQuery = working })
	for range 2 {
		if got := run("SELECT number FROM numbers(30)"); got != models.TruncationNone {
			t.Fatalf("with the read failing, a result under every cap reports %q, want none", got)
		}
	}
	if n := strings.Count(logs.String(), "reading the ClickHouse profile limits failed"); n != 1 {
		t.Fatalf("logged the failed read %d times, want once:\n%s", n, logs.String())
	}

	serverDefaultsQuery = working
	if got := run("SELECT number FROM numbers(10000) SETTINGS max_block_size = 10, max_threads = 1"); got != models.TruncationUnknown {
		t.Fatalf("after the read recovers, a result that reached the connection's read cap reports %q, want unknown", got)
	}
}

// TestCapabilityReadsAreTheirOwnQueriesClickHouse runs a client's first
// query, which also probes for native JSON support and reads the server's
// defaults. A profile callback in the caller's context hears none of them:
// Logchef owns the result query's profile, and each read is its own query.
func TestCapabilityReadsAreTheirOwnQueriesClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	var profiles atomic.Int64
	ctx = clickhouse.Context(ctx, clickhouse.WithProfileInfo(func(*clickhouse.ProfileInfo) { profiles.Add(1) }))
	if _, err := client.QueryWithOptions(ctx, "SELECT 1", QueryOptions{}); err != nil {
		t.Fatal(err)
	}
	if client.serverDefaults(ctx) == nil {
		t.Fatal("the first query did not read the server's defaults")
	}
	if n := profiles.Load(); n != 0 {
		t.Fatalf("the caller's profile callback ran %d times, want 0", n)
	}
}
