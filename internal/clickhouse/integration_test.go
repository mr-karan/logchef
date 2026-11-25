package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

func integrationClient(t *testing.T) (*Client, context.Context) {
	t.Helper()
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	client, err := NewClient(ClientOptions{Host: addr}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return client, ctx
}

func TestFieldValuesAndSchemaClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	table := fmt.Sprintf("logchef_driver_test_%d", time.Now().UnixNano())
	conn := client.conn
	if err := conn.Exec(ctx, "CREATE TABLE "+table+" (ts DateTime64(9, 'Asia/Kolkata'), msg String, n Nullable(Int16), attributes Map(String,String), category LowCardinality(Nullable(String)) COMMENT 'category description') ENGINE=MergeTree ORDER BY (ts, msg)"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP TABLE "+table)
	for i, fraction := range []string{"100000000", "500000000", "900000000", "999999999"} {
		if err := conn.Exec(ctx, "INSERT INTO "+table+" SELECT toDateTime64('2026-09-09 12:00:00."+fraction+"',9,'UTC'), 'message', ?, map('service','target'), 'test'", int16(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := conn.Exec(ctx, "INSERT INTO "+table+" SELECT toDateTime64('2026-09-09 12:00:00.5',9,'UTC'), 'message', NULL, map('service','target'), NULL"); err != nil {
		t.Fatal(err)
	}
	params := FieldValuesParams{
		FieldName: "n", FieldType: "Nullable(Int16)", TimestampField: "ts",
		StartTime: time.Date(2026, 9, 9, 12, 0, 0, 200000000, time.UTC),
		EndTime:   time.Date(2026, 9, 9, 12, 0, 0, 900000000, time.UTC),
		LogchefQL: `attributes.service="target"`,
	}
	for _, timezone := range []string{"UTC", "Asia/Kolkata"} {
		params.Timezone = timezone
		result, err := client.GetFieldDistinctValues(ctx, "default", table, params)
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]int64{}
		for _, item := range result.Values {
			values[item.Value] = item.Count
		}
		if !reflect.DeepEqual(values, map[string]int64{"1": 1, "2": 1}) || result.TotalDistinct != 2 {
			t.Errorf("timezone %s: %+v", timezone, result)
		}
	}
	params.LogchefQL = "invalid filter"
	if _, err := client.GetFieldDistinctValues(ctx, "default", table, params); err == nil {
		t.Error("invalid filter was silently ignored")
	}
	info, err := client.GetTableInfo(ctx, "default", table)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ExtColumns[4].IsNullable || info.ExtColumns[4].Comment != "category description" || !reflect.DeepEqual(info.SortKeys, []string{"ts", "msg"}) {
		t.Fatalf("incorrect metadata: %+v", info)
	}
	distributed := table + "_distributed"
	if err := conn.Exec(ctx, "CREATE TABLE "+distributed+" (category String) ENGINE=Distributed(default, default, "+table+")"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP TABLE "+distributed)
	info, err = client.GetTableInfo(ctx, "default", distributed)
	if err != nil || len(info.Columns) != 1 || info.Columns[0].Name != "category" {
		t.Fatalf("Distributed schema replaced by local schema: info=%+v err=%v", info, err)
	}
	cyclic := table + "_cyclic"
	if err := conn.Exec(ctx, "CREATE TABLE "+cyclic+" (category String) ENGINE=Distributed(default, default, "+cyclic+"_other)"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP TABLE "+cyclic)
	if err := conn.Exec(ctx, "CREATE TABLE "+cyclic+"_other (category String) ENGINE=Distributed(default, default, "+cyclic+")"); err != nil {
		// Newer servers reject cycles at DDL time; older servers permit them.
		var exception *ch.Exception
		if !errors.As(err, &exception) || exception.Code != 269 {
			t.Fatal(err)
		}
		t.Logf("server rejected cyclic schema: %v", err)
	} else {
		defer conn.Exec(ctx, "DROP TABLE "+cyclic+"_other")
		if _, err := client.GetTableInfo(ctx, "default", cyclic); err != nil {
			t.Fatalf("cycle should preserve base schema: %v", err)
		}
	}
	interval, err := windowToIntervalFunc(TimeWindow24h, "ts", "Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	query, err := client.buildHistogramQuery("SELECT msg FROM "+table, "ts", interval, "category")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Query(ctx, query)
	if err != nil || len(result.Logs) != 2 {
		t.Fatalf("daily grouped histogram failed: result=%+v err=%v", result, err)
	}
	var nullCount int64
	for _, row := range result.Logs {
		if parseHistogramFlag(row, "is_null") {
			nullCount, _ = extractInt64FromRow(row, "log_count")
		}
	}
	if nullCount != 1 {
		t.Fatalf("histogram lost null group: %+v", result.Logs)
	}
}

func TestReadonlyDDLClickHouse(t *testing.T) {
	base, ctx := integrationClient(t)
	for _, readonly := range []int{1, 2} {
		client, err := NewClient(ClientOptions{Host: testClickHouseAddr(t), QuerySettings: map[string]any{"readonly": readonly}}, base.logger)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = client.Close() })
		if _, err := client.Query(ctx, "SELECT 1"); err != nil {
			t.Fatalf("readonly=%d blocks SELECT: %v", readonly, err)
		}
		table := fmt.Sprintf("logchef_readonly_test_%d", time.Now().UnixNano())
		_, err = client.Query(ctx, "CREATE TABLE "+table+" (n UInt8) ENGINE=Memory")
		if err == nil {
			_ = base.conn.Exec(ctx, "DROP TABLE "+table)
			t.Fatalf("source readonly=%d did not reject DDL", readonly)
		}
	}
}

func TestDailyHistogramDSTClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	interval, err := windowToIntervalFunc(TimeWindow24h, "@timestamp", "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Query(ctx, "SELECT "+interval+" AS bucket FROM (SELECT arrayJoin([toDateTime('2026-03-08 12:00:00','UTC'),toDateTime('2026-03-09 12:00:00','UTC')]) AS `@timestamp`)")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Logs) != 2 {
		t.Fatalf("expected 2 buckets: %+v", result.Logs)
	}
	first, firstOK := result.Logs[0]["bucket"].(time.Time)
	second, secondOK := result.Logs[1]["bucket"].(time.Time)
	if !firstOK || !secondOK || second.Sub(first) != 23*time.Hour {
		t.Fatalf("daily buckets ignore DST: %+v", result.Logs)
	}
}

func TestNativeJSONCapabilityRetryClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	client.flattensJSON(cancelled)
	if client.flattenedJSON.cached() {
		t.Fatal("failed capability probe was cached")
	}
	if _, err := client.Query(ctx, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if !client.flattenedJSON.cached() {
		t.Fatal("successful query did not retry capability detection")
	}
}

func TestConcurrentCloseClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	var group sync.WaitGroup
	for range 4 {
		group.Go(func() {
			for range 5 {
				_, _ = client.Query(ctx, "SELECT 1")
				_ = client.Ping(ctx, "", "")
			}
		})
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	group.Wait()
	if _, err := client.Query(ctx, "SELECT 1"); !errors.Is(err, ch.ErrConnectionClosed) {
		t.Fatalf("query on closed client: %v", err)
	}
	if err := client.Ping(ctx, "", ""); !errors.Is(err, ch.ErrConnectionClosed) {
		t.Fatalf("ping on closed client: %v", err)
	}
}

// A listing of every field's top values leaves out the log-body columns: their
// values are whole log lines, which can carry personal data, and counting
// them reads every body in the window. A column asked for by name is read.
func TestAllFieldValuesSkipsLogBodiesClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	table := fmt.Sprintf("logchef_body_test_%d", time.Now().UnixNano())
	conn := client.conn
	if err := conn.Exec(ctx, "CREATE TABLE "+table+" (ts DateTime64(3), job_name String, msg String, Message String, body String) ENGINE=MergeTree ORDER BY ts"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP TABLE "+table)
	if err := conn.Exec(ctx, "INSERT INTO "+table+" VALUES (toDateTime64('2026-09-09 12:00:00', 3, 'UTC'), 'payments', 'client 4711 paid', 'x', 'y')"); err != nil {
		t.Fatal(err)
	}
	result, err := client.GetAllFilterableFieldValues(ctx, "default", table, AllFieldValuesParams{
		TimestampField: "ts", Timezone: "UTC", Limit: 10,
		StartTime: time.Date(2026, 9, 9, 11, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result["job_name"]; !ok || len(result) != 1 {
		t.Fatalf("fields %v, want job_name only", slices.Sorted(maps.Keys(result)))
	}
	named, err := client.GetFieldDistinctValues(ctx, "default", table, FieldValuesParams{
		FieldName: "msg", FieldType: "String", TimestampField: "ts", Timezone: "UTC", Limit: 10,
		StartTime: time.Date(2026, 9, 9, 11, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC),
	})
	if err != nil || len(named.Values) != 1 {
		t.Fatalf("msg by name: %+v %v, want its one value", named, err)
	}
}

// A field whose query times out is listed with the error and no values, so a
// caller does not take it for a field that does not exist. The other fields
// keep their values.
func TestAllFieldValuesListsATimedOutFieldClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	table := fmt.Sprintf("logchef_slow_field_test_%d", time.Now().UnixNano())
	conn := client.conn
	// Reading slow sleeps a second a row, so its query outlasts a 1 s timeout.
	if err := conn.Exec(ctx, "CREATE TABLE "+table+" (ts DateTime64(3), job_name String, slow String ALIAS toString(sleepEachRow(1))) ENGINE=MergeTree ORDER BY ts"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP TABLE "+table)
	if err := conn.Exec(ctx, "INSERT INTO "+table+" SELECT toDateTime64('2026-09-09 12:00:00', 3, 'UTC'), 'payments' FROM numbers(3)"); err != nil {
		t.Fatal(err)
	}
	timeout := 1
	result, err := client.GetAllFilterableFieldValues(ctx, "default", table, AllFieldValuesParams{
		TimestampField: "ts", Timezone: "UTC", Limit: 10, Timeout: &timeout,
		StartTime: time.Date(2026, 9, 9, 11, 0, 0, 0, time.UTC), EndTime: time.Date(2026, 9, 9, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if job := result["job_name"]; job == nil || job.Error != "" || len(job.Values) != 1 {
		t.Fatalf("job_name = %+v, want its one value", job)
	}
	slow := result["slow"]
	if slow == nil || slow.Error != "timed out after 1s" || slow.Values == nil || len(slow.Values) != 0 {
		t.Fatalf("slow = %+v, want the timeout and an empty list of values", slow)
	}
}
