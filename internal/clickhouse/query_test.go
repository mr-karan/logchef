package clickhouse

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/chcol"

	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestScanRowMapJSON(t *testing.T) {
	object := chcol.NewJSON()
	object.SetValueAtPath("component", "badger")
	object.SetValueAtPath("nested.count", int64(42))
	object.SetValueAtPath("timestamp1", []int64{0, 1, 2})
	ptrs := []reflect.Value{reflect.ValueOf(object)}
	columns := []models.ColumnInfo{{Name: "p", Type: "JSON"}}
	first := scanRowMap(ptrs, columns)

	// Scan targets are reused. A later row must not replace an earlier result.
	*object = *chcol.NewJSON()
	object.SetValueAtPath("component", "other")
	second := scanRowMap(ptrs, columns)

	encoded, err := json.Marshal([]map[string]any{first, second})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"p":{"component":"badger","nested":{"count":42},"timestamp1":[0,1,2]}},{"p":{"component":"other"}}]`
	if string(encoded) != want {
		t.Fatalf("JSON response = %s, want %s", encoded, want)
	}
}

func TestResponseSizeAccountsForEscapes(t *testing.T) {
	for _, value := range []string{"simple", "\"\\\n\x00<>&", "\u2028\u2029", "a\xffb", "é日志"} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if size := jsonStringSize(value); size < len(encoded) {
			t.Errorf("size of %q = %d, encoded size = %d", value, size, len(encoded))
		}
	}
	row := map[string]any{"\"\u2028": time.Date(2026, 9, 9, 17, 8, 36, 237280221, time.FixedZone("offset", 19800))}
	encoded, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if approxJSONSize(row) < len(encoded) {
		t.Errorf("response budget undercounts %s", encoded)
	}
}

func TestQueryCancellationClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	query := "SELECT number, sleepEachRow(0.01) FROM numbers(10000) SETTINGS max_block_size=1"
	start := time.Now()
	result, err := client.QueryWithOptions(ctx, query, QueryOptions{MaxRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Stats.Truncated || len(result.Logs) != 1 || time.Since(start) > 2*time.Second {
		t.Fatalf("query did not truncate promptly: %+v, duration=%v", result.Stats, time.Since(start))
	}
	wantErr := errors.New("writer disconnected")
	writer := &testRowWriter{err: wantErr}
	start = time.Now()
	_, err = client.QueryStream(ctx, query, QueryOptions{}, writer)
	if !errors.Is(err, wantErr) || writer.finished || time.Since(start) > 2*time.Second {
		t.Fatalf("writer failure did not cancel promptly: err=%v finished=%v duration=%v", err, writer.finished, time.Since(start))
	}
	if _, err := client.Query(ctx, "SELECT 1"); err != nil {
		t.Fatalf("connection unusable after cancellation: %v", err)
	}
}

type testRowWriter struct {
	rows     []map[string]any
	err      error
	finished bool
}

func (w *testRowWriter) Begin([]models.ColumnInfo) error { return nil }
func (w *testRowWriter) WriteRow(row map[string]any) error {
	w.rows = append(w.rows, row)
	return w.err
}
func (w *testRowWriter) Finish(models.QueryStats) error {
	w.finished = true
	return nil
}

func BenchmarkScanRowMap(b *testing.B) {
	columns := []models.ColumnInfo{{Name: "ts"}, {Name: "msg"}, {Name: "level"}, {Name: "host"}, {Name: "count"}}
	values := []any{time.Now(), "handling message", "INFO", "test-host", uint64(42)}
	ptrs := make([]reflect.Value, len(values))
	for i, value := range values {
		ptrs[i] = reflect.New(reflect.TypeOf(value))
		ptrs[i].Elem().Set(reflect.ValueOf(value))
	}
	b.Run("previous_scalar_path", func(b *testing.B) {
		retained := make([]map[string]any, 1)
		b.ReportAllocs()
		for b.Loop() {
			row := make(map[string]any, len(columns))
			for i, col := range columns {
				row[col.Name] = ptrs[i].Elem().Interface()
			}
			retained[0] = row
			if len(row) != len(columns) {
				b.Fatal("missing columns")
			}
		}
	})
	b.Run("normalized_scalar_path", func(b *testing.B) {
		retained := make([]map[string]any, 1)
		b.ReportAllocs()
		for b.Loop() {
			resetNullableScanTargets(ptrs)
			row := scanRowMap(ptrs, columns)
			retained[0] = row
			if len(row) != len(columns) {
				b.Fatal("missing columns")
			}
		}
	})
}

func TestScanRowMapJSONClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	conn := client.connection()
	version, err := conn.ServerVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version.Version.Major < 25 || version.Version.Major == 25 && version.Version.Minor < 3 {
		t.Skip("native JSON requires ClickHouse 25.3 or newer")
	}
	translation := logchefql.Translate(`p.tcode=23506 and p.component~bad | p.tcode`, &logchefql.Schema{Columns: []logchefql.ColumnInfo{{Name: "p", Type: "JSON"}}})
	if !translation.Valid {
		t.Fatal(translation.Error)
	}
	filtered, err := client.Query(ctx, "SELECT "+translation.SelectClause+" FROM (SELECT CAST('{\"component\":\"badger\",\"tcode\":23506}', 'JSON') AS p) WHERE "+translation.SQL)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(filtered.Logs)
	if err != nil || string(encoded) != `[{"p_tcode":23506}]` {
		t.Fatalf("native JSON filter/projection failed: %s %v", encoded, err)
	}
	query := `SELECT CAST(raw, 'JSON(max_dynamic_paths=1)') AS p, toJSONString(p) AS expected
		FROM (SELECT arrayJoin(['{"component":"badger","nested":{"count":42},"timestamp1":[0,1,2]}', '{"component":"other"}', '{}', '{"items":[{"name":"first"},{"name":"second"}],"enabled":true}', '{"mixed":[1,"two",null],"empty":[],"missing":null}']) AS raw)`
	for _, stringify := range []bool{false, true} {
		options := QueryOptions{Settings: map[string]any{"output_format_native_write_json_as_string": stringify}}
		result, err := client.QueryWithOptions(ctx, query, options)
		if err != nil {
			t.Fatal(err)
		}
		results := result.Logs
		if len(results) != 5 {
			t.Fatalf("got %d rows, want 5", len(results))
		}
		for _, result := range results {
			encoded, err := json.Marshal(result["p"])
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != result["expected"] {
				t.Errorf("serialized p = %s, ClickHouse JSON = %s", encoded, result["expected"])
			}
		}
		writer := &testRowWriter{}
		if _, err := client.QueryStream(ctx, query, options, writer); err != nil {
			t.Fatal(err)
		}
		if !writer.finished || !reflect.DeepEqual(result.Logs, writer.rows) {
			t.Fatalf("JSON stream differs from buffered response: %+v", writer)
		}
		nullable, err := client.QueryWithOptions(ctx, `SELECT arrayJoin([CAST('{"component":"badger"}', 'Nullable(JSON)'), CAST(NULL, 'Nullable(JSON)')]) AS p`, options)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(nullable.Logs)
		if err != nil || string(encoded) != `[{"p":{"component":"badger"}},{"p":null}]` {
			t.Fatalf("nullable JSON mode %t: %s %v", stringify, encoded, err)
		}
	}
}

func TestScanRowMapCompatibility(t *testing.T) {
	client, ctx := integrationClient(t)
	queries := []struct{ sql, want string }{
		{`SELECT toNullable('hello') AS value`, `{"value":"hello"}`},
		{`SELECT CAST(NULL, 'Nullable(String)') AS value`, `{"value":null}`},
		{`SELECT CAST('{"component":"badger"}', 'Nullable(String)') AS value`, `{"value":"{\"component\":\"badger\"}"}`},
		{`SELECT toLowCardinality('INFO') AS value`, `{"value":"INFO"}`},
		{`SELECT CAST(['one', NULL, 'two'], 'Array(Nullable(String))') AS value`, `{"value":["one",null,"two"]}`},
		{`SELECT map('one', 1, 'two', 2) AS value`, `{"value":{"one":1,"two":2}}`},
		{`SELECT CAST(('name', 42), 'Tuple(name String, count UInt32)') AS value`, `{"value":{"count":42,"name":"name"}}`},
		{`SELECT toDecimal64('123.45', 2) AS value`, `{"value":"123.45"}`},
		{`SELECT toUInt64(18446744073709551615) AS value`, `{"value":18446744073709551615}`},
		{`SELECT toDateTime64('2026-09-09 17:08:36.237280221',9,'Asia/Kolkata') AS value`, `{"value":"2026-09-09T17:08:36.237280221+05:30"}`},
		{`SELECT arrayJoin(CAST([true,NULL,false,NULL], 'Array(Nullable(Bool))')) AS value`, `[{"value":true},{"value":null},{"value":false},{"value":null}]`},
		{`SELECT arrayJoin([toNullable(toUUID('00000000-0000-0000-0000-000000000001')),NULL]) AS value`, `[{"value":"00000000-0000-0000-0000-000000000001"},{"value":null}]`},
		{`SELECT arrayJoin([toNullable(toDecimal64('12.30',2)),NULL]) AS value`, `[{"value":"12.3"},{"value":null}]`},
		{`SELECT arrayJoin([toNullable(toUInt128(123)),NULL]) AS value`, `[{"value":123},{"value":null}]`},
		{`SELECT CAST([1,2,3], 'Array(UInt8)') AS value`, `{"value":[1,2,3]}`},
		{`SELECT CAST([[1,2],[3]], 'Array(Array(UInt8))') AS value`, `{"value":[[1,2],[3]]}`},
		{`SELECT [nan, inf, -inf, 1.] AS value`, `{"value":[null,null,null,1]}`},
		{`SELECT toInt128('170141183460469231731687303715884105727') AS value`, `{"value":170141183460469231731687303715884105727}`},
	}
	for _, query := range queries {
		t.Run(query.sql, func(t *testing.T) {
			result, err := client.Query(ctx, query.sql)
			if err != nil {
				t.Fatal(err)
			}
			var value any = result.Logs
			if len(result.Logs) == 1 {
				value = result.Logs[0]
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != query.want {
				t.Errorf("response = %s, want %s", encoded, query.want)
			}
			writer := &testRowWriter{}
			if _, err := client.QueryStream(ctx, query.sql, QueryOptions{}, writer); err != nil {
				t.Fatal(err)
			}
			if !writer.finished || !reflect.DeepEqual(result.Logs, writer.rows) {
				t.Fatalf("stream differs from buffered response: %+v", writer)
			}
		})
	}
}
