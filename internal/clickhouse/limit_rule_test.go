package clickhouse

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/ClickHouse/clickhouse-go/v2"
)

func TestTruncationState(t *testing.T) {
	breakRows := func(rows int) queryEnd {
		return queryEnd{settings: clickhouse.Settings{"max_result_rows": rows, "result_overflow_mode": "break"}}
	}
	probed := QueryOptions{MaxRows: 100, ResultRows: 101, LimitSource: models.LimitSourceRequest}
	for _, tc := range []struct {
		name   string
		opts   QueryOptions
		rows   int
		reason string
		end    queryEnd
		want   models.Truncation
	}{
		{name: "a probe row proves the cut", opts: probed, rows: 100, reason: "row_limit", end: breakRows(101), want: models.TruncationRows},
		{name: "the byte budget is a proven cut", opts: probed, rows: 7, reason: "byte_limit", end: breakRows(101), want: models.TruncationBytes},
		{name: "a probed cap filled without the extra row is complete", opts: probed, rows: 100, end: breakRows(101), want: models.TruncationNone},
		{name: "fewer rows than the cap is complete", opts: QueryOptions{MaxRows: 100, ResultRows: 100, LimitSource: models.LimitSourceRequest}, rows: 99, end: breakRows(100), want: models.TruncationNone},
		{name: "an unprobed cap filled may be incomplete", opts: QueryOptions{MaxRows: 100, ResultRows: 100, LimitSource: models.LimitSourceRequest}, rows: 100, end: breakRows(100), want: models.TruncationUnknown},
		{name: "an unprobed cap filled in throw mode may be incomplete", opts: QueryOptions{MaxRows: 100, ResultRows: 100, LimitSource: models.LimitSourceRequest}, rows: 100,
			end: queryEnd{settings: clickhouse.Settings{"max_result_rows": 100, "result_overflow_mode": "throw"}}, want: models.TruncationUnknown},
		{name: "the statement's own limit filled is what the author asked", opts: QueryOptions{MaxRows: 20, ResultRows: 20, LimitSource: models.LimitSourceStatement}, rows: 20, end: breakRows(20), want: models.TruncationNone},
		{name: "a reader cap without a server bound probes itself", opts: QueryOptions{MaxRows: 100}, rows: 100, end: breakRows(0), want: models.TruncationNone},
		{name: "a source row limit below the reader cap that the result filled", opts: QueryOptions{MaxRows: 100}, rows: 50, end: breakRows(50), want: models.TruncationUnknown},
		{name: "a source row limit below the reader cap in throw mode", opts: QueryOptions{MaxRows: 100}, rows: 50,
			end: queryEnd{settings: clickhouse.Settings{"max_result_rows": int64(50), "result_overflow_mode": "throw"}}, want: models.TruncationNone},
		{name: "a byte limit the result profile stayed under", opts: probed, rows: 30,
			end: queryEnd{settings: clickhouse.Settings{"max_result_bytes": int64(1_000_000), "result_overflow_mode": "break"}, profiled: true, resultBytes: 4104}, want: models.TruncationNone},
		{name: "a byte limit the result profile reached", opts: probed, rows: 30,
			end: queryEnd{settings: clickhouse.Settings{"max_result_bytes": int64(1_000_000), "result_overflow_mode": "break"}, profiled: true, resultBytes: 1_000_000}, want: models.TruncationUnknown},
		{name: "a byte limit without a result profile", opts: probed, rows: 30,
			end: queryEnd{settings: clickhouse.Settings{"max_result_bytes": int64(1_000_000), "result_overflow_mode": "break"}}, want: models.TruncationUnknown},
		{name: "a byte limit that throws", opts: probed, rows: 30,
			end: queryEnd{settings: clickhouse.Settings{"max_result_bytes": int64(100), "result_overflow_mode": "throw"}}, want: models.TruncationNone},
		{name: "the query's own SETTINGS may cut", opts: probed, rows: 5, end: queryEnd{settings: breakRows(101).settings, ownSettingsMayCut: true}, want: models.TruncationUnknown},
		{name: "a proven cut outranks the query's own SETTINGS", opts: probed, rows: 100, reason: "row_limit", end: queryEnd{ownSettingsMayCut: true}, want: models.TruncationRows},
		{name: "an overflow mode the server knows went unsent", opts: probed, rows: 5, end: queryEnd{settings: breakRows(101).settings, unsentModes: true}, want: models.TruncationUnknown},
		{name: "no cap is never a cut", opts: QueryOptions{}, rows: 5000, end: breakRows(0), want: models.TruncationNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.truncation(tc.rows, tc.reason, tc.end); got != tc.want {
				t.Fatalf("truncation(%d, %q) = %q, want %q", tc.rows, tc.reason, got, tc.want)
			}
		})
	}
}

func TestSettingsMayCut(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  bool
	}{
		{query: "SELECT 1", want: false},
		{query: "SELECT name FROM system.settings", want: false},
		{query: "SELECT 1 SETTINGS max_threads = 1, max_block_size = 10", want: false},
		{query: "SELECT 1 SETTINGS max_rows_to_read = 10", want: false},
		{query: "SELECT 1 SETTINGS `limit` = 5", want: true},
		{query: `SELECT 1 SETTINGS "offset" = 5`, want: true},
		{query: "SELECT 1 SETTINGS `LIMIT` = 5", want: true},
		{query: "SELECT 1 SETTINGS `limit` = 0, `offset` = 0", want: false},
		{query: "SELECT 1 SETTINGS limit = 5", want: true},
		{query: "SELECT 1 SETTINGS `li\\x6dit` = 5", want: true},
		{query: "SELECT 1 SETTINGS read_overflow_mode_leaf = 'break'", want: true},
		{query: "SELECT 1 SETTINGS timeout_overflow_mode_leaf = 'break'", want: true},
		{query: "SELECT 1 SETTINGS `read_overflow_mode_leaf` = 'break'", want: true},
		{query: `SELECT 1 SETTINGS "timeout_overflow_mode_leaf" = 'break'`, want: true},
		{query: "SELECT 1 SETTINGS READ_OVERFLOW_MODE_LEAF = 'break'", want: true},
		{query: "SELECT 1 SETTINGS `read\\x5foverflow_mode_leaf` = 'break'", want: true},
		{query: "SELECT 1 SETTINGS a_future_overflow_mode_variant = 'break'", want: true},
		{query: "SELECT 1 SETTINGS read_overflow_mode_leaf = 1", want: true},
		{query: "SELECT 1 SETTINGS read_overflow_mode_leaf = 'throw', timeout_overflow_mode_leaf = 'throw'", want: false},
		{query: "SELECT 1 SETTINGS skip_unavailable_shards = 1", want: true},
		{query: "SELECT 1 SETTINGS skip_unavailable_shards = true", want: true},
		{query: "SELECT 1 SETTINGS skip_unavailable_shards = 0", want: false},
		{query: "SELECT 1 SETTINGS skip_unavailable_shards = false", want: false},
		{query: "SELECT 1 SETTINGS max_result_rows = 0", want: false},
		{query: "SELECT 1 SETTINGS `limit` = 5; SELECT 2 SETTINGS max_threads = 1", want: true},
		{query: "SELECT 1 SETTINGS sort_overflow_mode = 'throw'", want: false},
		{query: "SELECT 1 SETTINGS max_result_rows = 5, result_overflow_mode = 'throw'", want: false},
		{query: "SELECT 1 SETTINGS max_result_rows = 5", want: true},
		{query: "SELECT 1 SETTINGS max_result_bytes = 100", want: true},
		{query: "SELECT 1 SETTINGS MAX_RESULT_ROWS = 5", want: true},
		{query: "SELECT 1 SETTINGS max_rows_to_sort = 10, sort_overflow_mode = 'break'", want: true},
		{query: "SELECT 1 SETTINGS group_by_overflow_mode = 'any'", want: true},
		{query: "SELECT 1 SETTINGS result_overflow_mode = 'break'", want: true},
		{query: "SELECT 1 SETTINGS read_overflow_mode = concat('br', 'eak')", want: true},
		{query: "SELECT x FROM (SELECT 1 AS x SETTINGS read_overflow_mode = 'break')", want: true},
		{query: "WITH c AS (SELECT 1 AS x SETTINGS timeout_overflow_mode = 'break') SELECT x FROM c", want: true},
		{query: "SELECT 1 UNION ALL SELECT 2 SETTINGS join_overflow_mode = 'break'", want: true},
		{query: "SELECT 1 SETTINGS max_result_rows = 5 SETTINGS", want: true},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := SettingsMayCut(tc.query); got != tc.want {
				t.Fatalf("SettingsMayCut() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResultQuerySettings checks what a result query sends: Logchef's own
// result limits under the source's caps, and throw modes and a timeout that
// nothing overrides.
func TestResultQuerySettings(t *testing.T) {
	timeout := 30
	for _, tc := range []struct {
		name   string
		source clickhouse.Settings
		opts   QueryOptions
		want   clickhouse.Settings
	}{
		{name: "no source settings", opts: QueryOptions{TimeoutSeconds: &timeout, ResultRows: 101},
			want: clickhouse.Settings{"max_result_rows": 101, "max_result_bytes": 0, "result_overflow_mode": "break", "limit": 0, "offset": 0, "max_execution_time": 30, "sort_overflow_mode": "throw"}},
		{name: "the source's result caps", source: clickhouse.Settings{"max_result_rows": int64(50), "max_result_bytes": int64(100), "result_overflow_mode": "throw", "readonly": 2},
			opts: QueryOptions{TimeoutSeconds: &timeout, ResultRows: 101},
			want: clickhouse.Settings{"max_result_rows": int64(50), "max_result_bytes": int64(100), "result_overflow_mode": "throw", "readonly": 2, "max_execution_time": 30}},
		{name: "a source timeout caps the request", source: clickhouse.Settings{"max_execution_time": 7}, opts: QueryOptions{TimeoutSeconds: &timeout},
			want: clickhouse.Settings{"max_execution_time": 7}},
		{name: "a source timeout above the request", source: clickhouse.Settings{"max_execution_time": 120}, opts: QueryOptions{TimeoutSeconds: &timeout},
			want: clickhouse.Settings{"max_execution_time": 30}},
		{name: "a source timeout of zero", source: clickhouse.Settings{"max_execution_time": 0}, opts: QueryOptions{},
			want: clickhouse.Settings{"max_execution_time": DefaultQueryTimeout}},
		{name: "an overflow mode is never the source's", source: clickhouse.Settings{"sort_overflow_mode": "break"}, opts: QueryOptions{TimeoutSeconds: &timeout},
			want: clickhouse.Settings{"sort_overflow_mode": "throw"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientWithModes(tc.source, clickhouse.Settings{"sort_overflow_mode": "throw"})
			got, err := client.resultQuerySettings(t.Context(), tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range tc.want {
				if got[name] != value {
					t.Errorf("%s = %#v, want %#v (all: %v)", name, got[name], value, got)
				}
			}
		})
	}
}

// clientWithModes returns a client whose server knows modes, with source as
// its source settings.
func clientWithModes(source, modes clickhouse.Settings) *Client {
	client := &Client{querySettings: source}
	client.throwModes = serverFact[clickhouse.Settings]{dials: &client.dials, read: func(context.Context) (clickhouse.Settings, error) { return modes, nil }}
	client.flattenedJSON = serverFact[bool]{dials: &client.dials, read: func(context.Context) (bool, error) { return false, nil }}
	return client
}

// TestThrowModesClickHouse sets every overflow mode the server knows to break
// in the connection settings and reads back what a Logchef query got: throw
// for every mode but the result mode, whose break Logchef owns.
func TestThrowModesClickHouse(t *testing.T) {
	addr := testClickHouseAddr(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	admin, err := NewClient(ClientOptions{Host: addr}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	rows, err := admin.conn.Query(ctx, "SELECT name FROM system.settings WHERE name LIKE '%overflow\\_mode%'")
	if err != nil {
		t.Fatal(err)
	}
	defaults := clickhouse.Settings{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		defaults[name] = "break"
	}
	if err := rows.Close(); err != nil || len(defaults) < 9 {
		t.Fatalf("found overflow modes %v (%v)", defaults, err)
	}

	client, err := NewClient(ClientOptions{Host: addr, Settings: defaults}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := client.QueryWithOptions(ctx, "SELECT name, value FROM system.settings WHERE name LIKE '%overflow\\_mode%'", QueryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Logs) != len(defaults) {
		t.Fatalf("read %d overflow modes, want %d", len(result.Logs), len(defaults))
	}
	for _, row := range result.Logs {
		want := "throw"
		if row["name"] == "result_overflow_mode" {
			want = "break"
		}
		if row["value"] != want {
			t.Errorf("%s = %v, want %s", row["name"], row["value"], want)
		}
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

// TestCapabilityProbeIsItsOwnQueryClickHouse runs a client's first query,
// which also probes for native JSON support. A profile callback in the
// caller's context does not hear the probe: Logchef owns the result query's
// profile, and the probe is its own query.
func TestCapabilityProbeIsItsOwnQueryClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	var profiles atomic.Int64
	ctx = clickhouse.Context(ctx, clickhouse.WithProfileInfo(func(*clickhouse.ProfileInfo) { profiles.Add(1) }))
	if _, err := client.QueryWithOptions(ctx, "SELECT 1", QueryOptions{}); err != nil {
		t.Fatal(err)
	}
	if !client.flattenedJSON.cached() {
		t.Fatal("the first query did not probe for native JSON")
	}
	if n := profiles.Load(); n != 0 {
		t.Fatalf("the caller's profile callback ran %d times, want 0", n)
	}
}
