package clickhouse

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mr-karan/logchef/pkg/models"
)

// An aggregation sends its group count and the lowest of its memory limit,
// the source's and the ClickHouse user's.
func TestAggregationQuerySettings(t *testing.T) {
	const maxMemory = int64(2 << 30)
	for _, tc := range []struct {
		name       string
		source     clickhouse.Settings
		userCap    int64
		opts       QueryOptions
		wantGroups any
		wantMemory any
	}{
		{name: "not an aggregation", opts: QueryOptions{}, wantGroups: nil, wantMemory: nil},
		{name: "no other limit", opts: QueryOptions{AggregationMaxMemoryBytes: maxMemory}, wantGroups: int64(AggregationMaxGroups), wantMemory: maxMemory},
		{name: "a lower source limit", source: clickhouse.Settings{"max_memory_usage": int64(1 << 30), "max_rows_to_group_by": int64(500)},
			opts: QueryOptions{AggregationMaxMemoryBytes: maxMemory}, wantGroups: int64(500), wantMemory: int64(1 << 30)},
		{name: "a higher source limit", source: clickhouse.Settings{"max_memory_usage": int64(8 << 30), "max_rows_to_group_by": int64(1 << 20)},
			opts: QueryOptions{AggregationMaxMemoryBytes: maxMemory}, wantGroups: int64(AggregationMaxGroups), wantMemory: maxMemory},
		{name: "a lower user limit", userCap: 30000000, opts: QueryOptions{AggregationMaxMemoryBytes: maxMemory},
			wantGroups: int64(AggregationMaxGroups), wantMemory: int64(30000000)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientWithModes(tc.source, clickhouse.Settings{"group_by_overflow_mode": "throw"})
			client.memoryCap = serverFact[int64]{dials: &client.dials, read: func(context.Context) (int64, error) { return tc.userCap, nil }}
			got, err := client.resultQuerySettings(t.Context(), tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got["max_rows_to_group_by"] != tc.wantGroups || got["max_memory_usage"] != tc.wantMemory || got["group_by_overflow_mode"] != "throw" {
				t.Fatalf("settings %v, want groups %v and memory %v", got, tc.wantGroups, tc.wantMemory)
			}
		})
	}
}

// RFC 0005 claim 3: the limit rule reads the SQL of every aggregation shape
// and adds the probe limit, or keeps the statement's own.
func TestLimitRuleAggregationShapes(t *testing.T) {
	const where = "WHERE `timestamp` BETWEEN toDateTime('2026-11-14 00:00:00', 'UTC') AND toDateTime('2026-11-16 00:00:00', 'UTC')"
	shapes := map[string]string{
		"scalar": "SELECT count() AS `count`, uniqExactIf(`body`, `body` != '') AS `count_distinct_body` FROM default.logs " + where + " AND (`lvl` = 'error')",
		"grouped": "SELECT `log_attributes`['host'] AS `log_attributes.host`, count() AS `count`, sumOrNull(toFloat64OrNull(`log_attributes`['duration'])) AS `sum_d` FROM default.logs " +
			where + " GROUP BY `log_attributes`['host'] ORDER BY `count` DESC, `log_attributes`['host'] ASC",
		"timeseries": "SELECT toStartOfInterval(`timestamp`, INTERVAL 5 MINUTE, 'Asia/Kolkata') AS `time`, quantileOrNull(0.95)(toFloat64OrNull(`log_attributes`['duration'])) AS `p95_d` FROM default.logs " +
			where + " GROUP BY `time` ORDER BY `time` ASC",
		"grouped timeseries": "SELECT toStartOfInterval(`timestamp`, INTERVAL 1 HOUR, 'Asia/Kolkata') AS `time`, toString(`j`.`host`) AS `j.host`, `service` AS `service`, count() AS `count`, avgOrNull(`d`) AS `avg_d`, uniqExact(`lvl`) AS `u` FROM default.logs " +
			where + " GROUP BY `time`, toString(`j`.`host`), `service` ORDER BY `time` ASC, toString(`j`.`host`) ASC, `service` ASC",
	}
	for name, sql := range shapes {
		t.Run(name, func(t *testing.T) {
			qb := NewQueryBuilder("default.logs", 100000)
			for _, tc := range []struct {
				request, applied, fetch int
				source                  models.LimitSource
			}{
				{0, 1000, 1001, models.LimitSourceDefault},
				{5, 5, 6, models.LimitSourceRequest},
			} {
				got, err := qb.BuildRawQueryWithLimitPolicy(sql, tc.request, 1000, 100000)
				if err != nil {
					t.Fatal(err)
				}
				if got.AppliedLimit != tc.applied || got.FetchLimit != tc.fetch || got.LimitSource != tc.source || !strings.HasSuffix(got.SQL, " LIMIT "+strconv.Itoa(tc.fetch)) {
					t.Fatalf("request %d: %+v", tc.request, got)
				}
			}
		})
	}

	// The limit stage is the statement's LIMIT, and a smaller request wins.
	qb := NewQueryBuilder("default.logs", 100000)
	sql := shapes["grouped"] + " LIMIT 10"
	got, err := qb.BuildRawQueryWithLimitPolicy(sql, 0, 1000, 100000)
	if err != nil || got.AppliedLimit != 10 || got.LimitSource != models.LimitSourceStatement {
		t.Fatalf("statement limit: %+v err=%v", got, err)
	}
	got, err = qb.BuildRawQueryWithLimitPolicy(sql, 5, 1000, 100000)
	if err != nil || got.AppliedLimit != 5 || got.LimitSource != models.LimitSourceRequest {
		t.Fatalf("request below the statement: %+v err=%v", got, err)
	}
}

func TestAggregationError(t *testing.T) {
	groups := &clickhouse.Exception{Code: codeTooManyRows, Message: "Limit for rows to GROUP BY exceeded: has 16384 rows, maximum: 10"}
	memory := &clickhouse.Exception{Code: codeMemoryLimitExceeded, Message: "Query memory limit exceeded"}
	read := &clickhouse.Exception{Code: codeTooManyRows, Message: "Limit for rows or bytes to read exceeded"}
	for _, tc := range []struct {
		err  error
		want string
	}{
		{groups, "the query produced more than 10 groups. Add a filter or group by fewer fields"},
		{&QueryError{Message: "wrapped", Err: groups}, "the query produced more than 10 groups. Add a filter or group by fewer fields"},
		{&clickhouse.Exception{Code: codeTooManyRows, Message: "Limit for rows to GROUP BY exceeded"}, "the query produced more than 100000 groups. Add a filter or group by fewer fields"},
		{memory, "the query needs more memory than the limit allows. Add a filter or shorten the range"},
		{read, read.Error()},
		{context.Canceled, context.Canceled.Error()},
	} {
		got := AggregationError(tc.err)
		if got.Error() != tc.want {
			t.Fatalf("AggregationError(%v) = %v, want %s", tc.err, got, tc.want)
		}
		if !errors.Is(got, tc.err) {
			t.Fatalf("AggregationError(%v) dropped the cause", tc.err)
		}
	}
}

// The bounds fail an aggregation on ClickHouse with the errors that
// AggregationError explains, and the user's memory limit reads back.
func TestAggregationBoundsClickHouse(t *testing.T) {
	addr := testClickHouseAddr(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	client, err := NewClient(ClientOptions{Host: addr, QuerySettings: map[string]any{"max_rows_to_group_by": int64(10)}}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.memoryCap.get(ctx); err != nil {
		t.Fatalf("reading the memory limit: %v", err)
	}
	timeout := 20
	opts := QueryOptions{TimeoutSeconds: &timeout, MaxRows: 100, ResultRows: 101, AggregationMaxMemoryBytes: 2 << 30}
	_, err = client.QueryWithOptions(ctx, "SELECT number % 1000 AS n, count() AS c FROM numbers(100000) GROUP BY n LIMIT 101", opts)
	if got := AggregationError(err); got == nil || !strings.Contains(got.Error(), "more than 10 groups") {
		t.Fatalf("group limit: err=%v", err)
	}

	opts.AggregationMaxMemoryBytes = 20 << 20
	_, err = client.QueryWithOptions(ctx, "SELECT uniqExact(toString(number)) AS u FROM numbers(5000000) LIMIT 101", opts)
	if got := AggregationError(err); got == nil || !strings.Contains(got.Error(), "needs more memory") {
		t.Fatalf("memory limit: err=%v", err)
	}
}
