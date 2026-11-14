package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// The tests in this file hold Logchef to its completeness rule against a
// live ClickHouse: a result that reports truncation none holds every row the
// query asked for.

func requireNoneMeansComplete(t *testing.T, path string, rows, complete int, truncation models.Truncation) {
	t.Helper()
	if truncation == models.TruncationNone && rows != complete {
		t.Fatalf("%s: %d of %d rows reported complete", path, rows, complete)
	}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// createProfileUser creates a ClickHouse user whose settings profile holds
// clauses, and skips when the server does not allow creating users.
func createProfileUser(t *testing.T, addr string, clauses []string) string {
	t.Helper()
	admin, err := NewClient(ClientOptions{Host: addr}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("logchef_test_%d", time.Now().UnixNano())
	if err := admin.conn.Exec(t.Context(), fmt.Sprintf("CREATE USER %s IDENTIFIED WITH no_password SETTINGS %s", name, strings.Join(clauses, ", "))); err != nil {
		t.Skipf("this ClickHouse server does not allow creating users: %v", err)
	}
	t.Cleanup(func() { _ = admin.conn.Exec(context.Background(), "DROP USER IF EXISTS "+name) })
	return name
}

// createTestTable creates a table from columns, fills it from a SELECT and
// drops it when the test ends.
func createTestTable(t *testing.T, addr, columns, engine, fill string) string {
	t.Helper()
	admin, err := NewClient(ClientOptions{Host: addr}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("logchef_test_%d", time.Now().UnixNano())
	if err := admin.conn.Exec(t.Context(), fmt.Sprintf("CREATE TABLE default.%s (%s) ENGINE = %s", name, columns, engine)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.conn.Exec(context.Background(), "DROP TABLE IF EXISTS default."+name) })
	if err := admin.conn.Exec(t.Context(), fmt.Sprintf("INSERT INTO default.%s %s", name, fill)); err != nil {
		t.Fatal(err)
	}
	return name
}

func settingClauses(settings clickhouse.Settings) []string {
	clauses := make([]string, 0, len(settings))
	for _, name := range slices.Sorted(maps.Keys(settings)) {
		value := settings[name]
		if text, ok := value.(string); ok {
			value = "'" + text + "'"
		}
		clauses = append(clauses, fmt.Sprintf("%s = %v", name, value))
	}
	return clauses
}

// defaultsClients returns clients that get settings as a ClickHouse user's
// settings profile and as connection settings, which servers that do not
// allow creating users still take.
func defaultsClients(t *testing.T, settings clickhouse.Settings) map[string]func(*testing.T) *Client {
	addr := testClickHouseAddr(t)
	open := func(t *testing.T, options ClientOptions) *Client {
		client, err := NewClient(options, discardLogger())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		return client
	}
	return map[string]func(*testing.T) *Client{
		"profile": func(t *testing.T) *Client {
			return open(t, ClientOptions{Host: addr, Username: createProfileUser(t, addr, settingClauses(settings))})
		},
		"connection": func(t *testing.T) *Client {
			return open(t, ClientOptions{Host: addr, Settings: settings})
		},
	}
}

type collectedStream struct {
	rows  int
	stats models.QueryStats
}

func (w *collectedStream) Begin([]models.ColumnInfo) error { return nil }
func (w *collectedStream) WriteRow(map[string]any) error   { w.rows++; return nil }
func (w *collectedStream) Finish(stats models.QueryStats) error {
	w.stats = stats
	return nil
}

// TestSQLSettingsMayCutClickHouse runs queries whose own SETTINGS override
// what Logchef sends and let ClickHouse end the result early without an
// error. Such a result may be incomplete, and SETTINGS that cannot end it
// early leave it complete.
func TestSQLSettingsMayCutClickHouse(t *testing.T) {
	addr := testClickHouseAddr(t)
	client, err := NewClient(ClientOptions{Host: addr}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	leaf := "remote('127.0.0.1', default, " + createTestTable(t, addr, "n UInt64", "MergeTree ORDER BY n SETTINGS index_granularity = 10", "SELECT number FROM numbers(10000)") + ")"
	for _, tc := range []struct {
		query    string
		complete int
		want     models.Truncation
	}{
		{query: "SELECT n FROM " + leaf + " SETTINGS max_rows_to_read_leaf = 50, read_overflow_mode_leaf = 'break', max_block_size = 10, max_threads = 1", complete: 100, want: models.TruncationUnknown},
		{query: "SELECT n FROM " + leaf + " SETTINGS max_rows_to_read_leaf = 50, `read_overflow_mode_leaf` = 'break', max_block_size = 10, max_threads = 1", complete: 100, want: models.TruncationUnknown},
		{query: "SELECT n FROM " + leaf + " SETTINGS max_rows_to_read_leaf = 50, \"read_overflow_mode_leaf\" = 'break', max_block_size = 10, max_threads = 1", complete: 100, want: models.TruncationUnknown},
		{query: "SELECT number FROM numbers(1000) SETTINGS `limit` = 5", complete: 100, want: models.TruncationUnknown},
		{query: "SELECT number FROM numbers(1000) SETTINGS \"offset\" = 995", complete: 100, want: models.TruncationUnknown},
		{query: "SELECT count() AS shards FROM remote('127.0.0.1:9000,127.0.0.2:1', system.one) SETTINGS skip_unavailable_shards = 1", complete: 1, want: models.TruncationUnknown},
		{query: "SELECT number FROM numbers(10000) SETTINGS max_result_rows = 5, max_block_size = 10", complete: 100, want: models.TruncationUnknown},
		{query: "SELECT number FROM numbers(10000) SETTINGS max_result_bytes = 100, max_block_size = 10", complete: 100, want: models.TruncationUnknown},
		{query: "SELECT number FROM numbers(10000) ORDER BY number DESC SETTINGS max_rows_to_sort = 10, sort_overflow_mode = 'break', max_block_size = 10", complete: 100, want: models.TruncationUnknown},
		{query: "SELECT number FROM (SELECT number FROM numbers(10000) SETTINGS max_rows_to_read = 50, read_overflow_mode = 'break', max_block_size = 10)", complete: 100, want: models.TruncationUnknown},
		{query: "SELECT number FROM numbers(30) SETTINGS max_threads = 1", complete: 30, want: models.TruncationNone},
		{query: "SELECT number FROM numbers(3) SETTINGS max_result_rows = 5, result_overflow_mode = 'throw'", complete: 3, want: models.TruncationNone},
	} {
		t.Run(tc.query, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			built, err := client.QueryBuilder("", 100000).BuildRawQueryWithLimitPolicy(tc.query, 100, 1000, 100000)
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.QueryWithOptions(ctx, built.SQL, built.QueryOptions(10))
			if err != nil {
				t.Fatal(err)
			}
			requireNoneMeansComplete(t, "buffered", len(result.Logs), tc.complete, result.Stats.Truncation)
			if result.Stats.Truncation != tc.want {
				t.Fatalf("buffered: %d rows, truncation %q, want %q", len(result.Logs), result.Stats.Truncation, tc.want)
			}
			writer := &collectedStream{}
			stats, err := client.QueryStream(ctx, built.SQL, built.QueryOptions(10), writer)
			if err != nil {
				t.Fatal(err)
			}
			requireNoneMeansComplete(t, "stream", writer.rows, tc.complete, stats.Truncation)
			if stats.Truncation != tc.want || writer.stats.Truncation != tc.want {
				t.Fatalf("stream: %d rows, truncation %q, Finish %q, want %q", writer.rows, stats.Truncation, writer.stats.Truncation, tc.want)
			}
		})
	}
}

// TestProfileLimitsClickHouse sets limits where Logchef does not see them: in
// the ClickHouse user's settings profile and in the connection settings.
// Logchef sends its own result limits and makes every other limit throw, so a
// profile limit can no longer cut a result that reports complete.
func TestProfileLimitsClickHouse(t *testing.T) {
	const blocksOfTen = " SETTINGS max_block_size = 10, max_threads = 1"
	for _, tc := range []struct {
		name     string
		settings clickhouse.Settings
		query    string
		// plain runs the query through QueryWithOptions with a reader cap, as
		// the histogram and alerts do, instead of the LIMIT rule.
		plain    bool
		complete int
		want     models.Truncation
		wantErr  string
	}{
		{name: "byte cap in break mode, plain query", settings: clickhouse.Settings{"max_result_bytes": 100, "result_overflow_mode": "break"},
			query: "SELECT number FROM numbers(10000)" + blocksOfTen, plain: true, complete: 1000, want: models.TruncationRows},
		{name: "byte cap in break mode, LIMIT rule", settings: clickhouse.Settings{"max_result_bytes": 100, "result_overflow_mode": "break"},
			query: "SELECT number FROM numbers(10000)" + blocksOfTen, complete: 100, want: models.TruncationRows},
		{name: "byte cap, complete result", settings: clickhouse.Settings{"max_result_bytes": 100},
			query: "SELECT number FROM numbers(30)" + blocksOfTen, complete: 30, want: models.TruncationNone},
		{name: "row cap in break mode, plain query", settings: clickhouse.Settings{"max_result_rows": 50, "result_overflow_mode": "break"},
			query: "SELECT number FROM numbers(10000)" + blocksOfTen, plain: true, complete: 1000, want: models.TruncationRows},
		{name: "sort cap in break mode", settings: clickhouse.Settings{"max_rows_to_sort": 10, "sort_overflow_mode": "break", "max_block_size": 10},
			query: "SELECT number FROM numbers(10000) ORDER BY number DESC", complete: 100, wantErr: "max_rows_to_sort"},
		{name: "read cap in break mode", settings: clickhouse.Settings{"max_rows_to_read": 50, "read_overflow_mode": "break"},
			query: "SELECT number FROM numbers(10000)" + blocksOfTen, complete: 100, wantErr: "max_rows_to_read"},
		{name: "read cap in break mode, plain query", settings: clickhouse.Settings{"max_rows_to_read": 50, "read_overflow_mode": "break"},
			query: "SELECT count() FROM numbers(10000)" + blocksOfTen, plain: true, complete: 1, wantErr: "max_rows_to_read"},
		{name: "limit setting", settings: clickhouse.Settings{"limit": 5},
			query: "SELECT number FROM numbers(10000)", complete: 100, want: models.TruncationRows},
	} {
		for where, open := range defaultsClients(t, tc.settings) {
			t.Run(tc.name+"/"+where, func(t *testing.T) {
				client := open(t)
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				query, opts := tc.query, QueryOptions{MaxRows: 1000}
				if !tc.plain {
					built, err := client.QueryBuilder("", 100000).BuildRawQueryWithLimitPolicy(tc.query, 100, 1000, 100000)
					if err != nil {
						t.Fatal(err)
					}
					query, opts = built.SQL, built.QueryOptions(10)
				}
				result, err := client.QueryWithOptions(ctx, query, opts)
				if tc.wantErr != "" {
					if queryErr, ok := errors.AsType[*QueryError](err); !ok || !strings.Contains(queryErr.Message, tc.wantErr) {
						t.Fatalf("buffered: error %v, result %+v, want a query error naming %s", err, result, tc.wantErr)
					}
					writer := &collectedStream{}
					stats, err := client.QueryStream(ctx, query, opts, writer)
					if _, ok := errors.AsType[*QueryError](err); !ok {
						t.Fatalf("stream: error %v, %d rows with truncation %q, want a query error", err, writer.rows, stats.Truncation)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				requireNoneMeansComplete(t, "buffered", len(result.Logs), tc.complete, result.Stats.Truncation)
				if len(result.Logs) != tc.complete || result.Stats.Truncation != tc.want {
					t.Fatalf("buffered: %d rows, truncation %q, want %d rows, %q", len(result.Logs), result.Stats.Truncation, tc.complete, tc.want)
				}
				writer := &collectedStream{}
				stats, err := client.QueryStream(ctx, query, opts, writer)
				if err != nil {
					t.Fatal(err)
				}
				requireNoneMeansComplete(t, "stream", writer.rows, tc.complete, stats.Truncation)
				if writer.rows != tc.complete || stats.Truncation != tc.want {
					t.Fatalf("stream: %d rows, truncation %q, want %d rows, %q", writer.rows, stats.Truncation, tc.complete, tc.want)
				}
			})
		}
	}
}

// TestProfileConstantByteCapClickHouse fixes a byte cap in the ClickHouse
// user's profile so Logchef cannot override it: the query fails rather than
// returning a cut result.
func TestProfileConstantByteCapClickHouse(t *testing.T) {
	addr := testClickHouseAddr(t)
	client, err := NewClient(ClientOptions{Host: addr, Username: createProfileUser(t, addr, []string{"max_result_bytes = 100 CONST", "max_block_size = 10"})}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	built, err := client.QueryBuilder("", 100000).BuildRawQueryWithLimitPolicy("SELECT number FROM numbers(10000)", 100, 1000, 100000)
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.QueryWithOptions(ctx, built.SQL, built.QueryOptions(10))
	if err == nil {
		requireNoneMeansComplete(t, "buffered", len(result.Logs), 100, result.Stats.Truncation)
		if result.Stats.Truncation != models.TruncationBytes {
			t.Fatalf("%d rows with truncation %q, want an error or a bytes cut", len(result.Logs), result.Stats.Truncation)
		}
	}
}

// TestHistogramCompletenessClickHouse counts 100000 rows in hourly buckets.
// A histogram is complete or fails: a limit that can end its query early,
// from the query's SETTINGS or a settings profile, is an error, never a lower
// count.
func TestHistogramCompletenessClickHouse(t *testing.T) {
	addr := testClickHouseAddr(t)
	const rows = "SELECT toDateTime('2026-01-01 00:00:00', 'UTC') + number AS ts FROM numbers(100000)"
	histogram := func(t *testing.T, client *Client, query string) (int, error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := client.GetHistogramData(ctx, "", "ts", HistogramParams{Window: TimeWindow1h, Query: query, Timezone: "UTC"})
		if err != nil {
			return 0, err
		}
		total := 0
		for _, bucket := range result.Data {
			total += bucket.LogCount
		}
		return total, nil
	}

	client, err := NewClient(ClientOptions{Host: addr}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if total, err := histogram(t, client, rows); err != nil || total != 100000 {
		t.Fatalf("histogram counted %d rows (%v), want 100000", total, err)
	}
	if total, err := histogram(t, client, rows+" SETTINGS max_rows_to_read = 1000, read_overflow_mode = 'break', max_threads = 1, max_block_size = 100"); err == nil {
		t.Fatalf("histogram counted %d rows under SETTINGS that end the query early, want an error", total)
	}
	settings := clickhouse.Settings{"max_rows_to_read": 1000, "read_overflow_mode": "break", "max_threads": 1, "max_block_size": 100}
	for where, open := range defaultsClients(t, settings) {
		t.Run(where, func(t *testing.T) {
			if total, err := histogram(t, open(t), rows); err == nil {
				t.Fatalf("histogram counted %d rows under a read cap in break mode, want an error", total)
			}
		})
	}
}

// TestSourceTimeoutReachesServerClickHouse reads back the max_execution_time
// that ClickHouse applied to a query: the request's timeout, capped by the
// source's, as sent, and a source timeout ends a query on the server.
func TestSourceTimeoutReachesServerClickHouse(t *testing.T) {
	addr := testClickHouseAddr(t)
	for _, tc := range []struct {
		source, request int
		want            uint64
	}{
		{source: 7, request: 30, want: 7},
		{source: 30, request: 5, want: 5},
		{request: 12, want: 12},
	} {
		t.Run(fmt.Sprintf("source %d request %d", tc.source, tc.request), func(t *testing.T) {
			options := ClientOptions{Host: addr}
			if tc.source > 0 {
				options.QuerySettings = map[string]any{"max_execution_time": tc.source}
			}
			client, err := NewClient(options, discardLogger())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			result, err := client.QueryWithOptions(ctx, "SELECT toUInt64(getSetting('max_execution_time')) AS timeout", QueryOptions{TimeoutSeconds: &tc.request})
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Logs[0]["timeout"]; got != tc.want {
				t.Fatalf("server max_execution_time = %v, want %d", got, tc.want)
			}
		})
	}

	client, err := NewClient(ClientOptions{Host: addr, QuerySettings: map[string]any{"max_execution_time": 1}}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	timeout := 30
	start := time.Now()
	_, err = client.QueryWithOptions(context.Background(), "SELECT sleepEachRow(1) FROM numbers(3) SETTINGS max_block_size = 1", QueryOptions{TimeoutSeconds: &timeout})
	if exception, ok := errors.AsType[*clickhouse.Exception](err); !ok || exception.Code != chExceptionTimeoutExceeded || time.Since(start) >= 3*time.Second {
		t.Fatalf("source max_execution_time 1 ended the query after %v with %v, want TIMEOUT_EXCEEDED from the server", time.Since(start), err)
	}
}
