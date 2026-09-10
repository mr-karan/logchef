package clickhouse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"reflect"
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
	conn := client.connection()
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
		client, err := NewClient(ClientOptions{Host: base.opts.Addr[0], QuerySettings: map[string]any{"readonly": readonly}}, base.logger)
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
			_ = base.connection().Exec(ctx, "DROP TABLE "+table)
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

func TestConcurrentReconnectClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		group.Go(func() {
			for range 5 {
				// An in-flight query may fail when its pool closes; it must not race.
				_, _ = client.Query(ctx, "SELECT 1")
			}
		})
	}
	for range 3 {
		if err := client.Reconnect(ctx); err != nil {
			t.Fatal(err)
		}
	}
	group.Wait()
	result, err := client.Query(ctx, "SELECT 1 AS n")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result.Logs)
	if err != nil || string(encoded) != `[{"n":1}]` {
		t.Fatalf("replacement pool failed: %s %v", encoded, err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if err := client.Reconnect(ctx); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed client reopened: %v", err)
	}
}

func TestNativeJSONCapabilityRetryClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	timeout := 5
	client.contextWithNativeJSON(cancelled, QueryOptions{TimeoutSeconds: &timeout})
	if client.flattenedJSON != nil {
		t.Fatal("failed capability probe was cached")
	}
	if _, err := client.Query(ctx, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if client.flattenedJSON == nil {
		t.Fatal("successful query did not retry capability detection")
	}
	if err := client.Reconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if client.flattenedJSON != nil {
		t.Fatal("reconnect retained capability from the previous pool")
	}
	if _, err := client.Query(ctx, "SELECT 1"); err != nil {
		t.Fatal(err)
	}
	if client.flattenedJSON == nil {
		t.Fatal("replacement pool did not detect capabilities")
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
	group.Go(func() { _ = client.Reconnect(ctx) })
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	group.Wait()
	if _, err := client.Query(ctx, "SELECT 1"); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("query on closed client: %v", err)
	}
	if err := client.Ping(ctx, "", ""); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("ping on closed client: %v", err)
	}
}
