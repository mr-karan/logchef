package logchefql

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

var aggregationSchema = &Schema{Columns: []ColumnInfo{
	{Name: "timestamp", Type: "DateTime64(3)"},
	{Name: "level", Type: "LowCardinality(String)"},
	{Name: "service", Type: "String"},
	{Name: "code", Type: "FixedString(3)"},
	{Name: "bytes", Type: "UInt64"},
	{Name: "small", Type: "Int8"},
	{Name: "ratio", Type: "Nullable(Float32)"},
	{Name: "price", Type: "Decimal(18, 4)"},
	{Name: "ok", Type: "Bool"},
	{Name: "day", Type: "Date"},
	{Name: "seen", Type: "DateTime"},
	{Name: "tags", Type: "Array(String)"},
	{Name: "pair", Type: "Tuple(String, UInt8)"},
	{Name: "attrs", Type: "Map(LowCardinality(String), String)"},
	{Name: "counts", Type: "Map(String, UInt64)"},
	{Name: "body", Type: "String"},
	{Name: "j", Type: "JSON(n UInt32)"},
}}

func aggregationParams(timezone string) QueryBuildParams {
	return QueryBuildParams{
		TableName:      "default.logs",
		TimestampField: "timestamp",
		StartTime:      "2026-11-15 00:00:00",
		EndTime:        "2026-11-16 00:00:00",
		Timezone:       timezone,
	}
}

func compileAggregation(t *testing.T, query, interval string, params QueryBuildParams) (string, *TranslateResult, error) {
	t.Helper()
	result := TranslateWithOptions(query, aggregationSchema, TranslateOptions{AllowAggregation: true, Interval: interval})
	if !result.Valid {
		return "", result, result.Error
	}
	sql, err := BuildFullQueryFromTranslation(params, result)
	return sql, result, err
}

const aggregationRange = "`timestamp` BETWEEN toDateTime('2026-11-15 00:00:00', 'UTC') AND toDateTime('2026-11-16 00:00:00', 'UTC')"

// One golden query per shape, with the default order of each.
func TestSQLAggregationShapes(t *testing.T) {
	for _, tt := range []struct {
		name, query, interval, want string
	}{
		{
			name:  "scalar",
			query: "level=error | count()",
			want:  "SELECT count() AS `count`\nFROM default.logs\nWHERE " + aggregationRange + "\n  AND (`level` = 'error')",
		},
		{
			name:  "grouped",
			query: "level=error | count() as errors, count_distinct(body) as bodies by service",
			want: "SELECT `service` AS `service`, count() AS `errors`, uniqExactIf(`body`, `body` != '') AS `bodies`\nFROM default.logs\nWHERE " + aggregationRange +
				"\n  AND (`level` = 'error')\nGROUP BY `service`\nORDER BY `errors` DESC, `service` ASC",
		},
		{
			name:     "timeseries",
			query:    "| p95(bytes), avg(bytes) by time",
			interval: "5m",
			want: "SELECT toStartOfInterval(`timestamp`, INTERVAL 5 MINUTE, 'UTC') AS `time`, quantileOrNull(0.95)(`bytes`) AS `p95_bytes`, avgOrNull(`bytes`) AS `avg_bytes`\nFROM default.logs\nWHERE " + aggregationRange +
				"\nGROUP BY `time`\nORDER BY `time` ASC",
		},
		{
			name:  "grouped timeseries",
			query: "| count() by time(1h) as bucket, attrs.host",
			want: "SELECT toStartOfInterval(`timestamp`, INTERVAL 1 HOUR, 'UTC') AS `bucket`, `attrs`['host'] AS `attrs.host`, count() AS `count`\nFROM default.logs\nWHERE " + aggregationRange +
				"\nGROUP BY `bucket`, `attrs`['host']\nORDER BY `bucket` ASC, `attrs`['host'] ASC",
		},
		{
			name:  "sort and limit",
			query: "| count() by service, level | sort service desc | limit 10",
			want: "SELECT `service` AS `service`, `level` AS `level`, count() AS `count`\nFROM default.logs\nWHERE " + aggregationRange +
				"\nGROUP BY `service`, `level`\nORDER BY `service` DESC, `level` ASC\nLIMIT 10",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := compileAggregation(t, tt.query, tt.interval, aggregationParams("UTC"))
			if err != nil {
				t.Fatal(err)
			}
			if sql != tt.want {
				t.Fatalf("sql=\n%s\nwant\n%s", sql, tt.want)
			}
		})
	}
}

// One case per row of the RFC's construct table: the expression that sum,
// avg, min, max and the percentiles read from each resolved type.
func TestSQLAggregationNumberExpression(t *testing.T) {
	for _, tt := range []struct {
		field, want string
	}{
		{"bytes", "sumOrNull(`bytes`)"},
		{"small", "sumOrNull(`small`)"},
		{"ratio", "sumOrNull(`ratio`)"},
		{"price", "sumOrNull(`price`)"},
		{"service", "sumOrNull(toFloat64OrNull(`service`))"},
		{"code", "sumOrNull(toFloat64OrNull(`code`))"},
		{"attrs.duration", "sumOrNull(toFloat64OrNull(`attrs`['duration']))"},
		{"body.duration", "sumOrNull(toFloat64OrNull(JSONExtractString(`body`, 'duration')))"},
		{"missing", "sumOrNull(toFloat64OrNull(`missing`))"},
		{"j.d", "sumOrNull(toFloat64OrNull(toString(`j`.`d`)))"},
		{"j.n", "sumOrNull(`j`.`n`)"},
		{"counts.hits", "sumOrNull(`counts`['hits'])"},
	} {
		t.Run(tt.field, func(t *testing.T) {
			_, result, err := compileAggregation(t, "| sum("+tt.field+") as s", "", aggregationParams("UTC"))
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Aggregation.values[0]; got != tt.want+" AS `s`" {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}

	for _, field := range []string{"ok", "day", "seen", "tags", "pair", "attrs", "j"} {
		t.Run("rejects "+field, func(t *testing.T) {
			_, _, err := compileAggregation(t, "| p95("+field+")", "", aggregationParams("UTC"))
			if err == nil || !strings.Contains(err.Error(), "p95 needs a number field. \""+field+"\" is") {
				t.Fatalf("err=%v, want a number field error", err)
			}
		})
	}
}

func TestSQLAggregationFunctions(t *testing.T) {
	for query, want := range map[string]string{
		"| count()":               "count()",
		"| count_distinct(bytes)": "uniqExact(`bytes`)",
		"| count_distinct(level)": "uniqExactIf(`level`, `level` != '')",
		"| count_distinct(j.u)":   "uniqExactIf(`j`.`u`, toString(`j`.`u`) != '')",
		"| count_distinct(j.n)":   "uniqExact(`j`.`n`)",
		"| avg(bytes)":            "avgOrNull(`bytes`)",
		"| min(bytes)":            "minOrNull(`bytes`)",
		"| max(bytes)":            "maxOrNull(`bytes`)",
		"| p50(bytes)":            "quantileOrNull(0.5)(`bytes`)",
		"| p75(bytes)":            "quantileOrNull(0.75)(`bytes`)",
		"| p90(bytes)":            "quantileOrNull(0.9)(`bytes`)",
		"| p99(bytes)":            "quantileOrNull(0.99)(`bytes`)",
	} {
		_, result, err := compileAggregation(t, query, "", aggregationParams("UTC"))
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if got := result.Aggregation.values[0]; !strings.HasPrefix(got, want+" AS ") {
			t.Fatalf("%s: got %s, want %s", query, got, want)
		}
	}
}

// ClickHouse reads an alias inside WHERE, so an output name may not be a
// column, other than the field that a group key names.
func TestSQLAggregationOutputNames(t *testing.T) {
	for query, wantErr := range map[string]string{
		"| count() by service":             "",
		"| count() by attrs.host":          "",
		"| count() as level":               `the name "level" is a column of this source`,
		"| count() by service as level":    `the name "level" is a column of this source`,
		"| count() by time(5m) as service": `the name "service" is a column of this source`,
		"| count() as timestamp":           `the name "timestamp" is a column of this source`,
	} {
		_, _, err := compileAggregation(t, query, "", aggregationParams("UTC"))
		if (err == nil) != (wantErr == "") || err != nil && !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("%s: err=%v, want %q", query, err, wantErr)
		}
	}

	timeSchema := &Schema{Columns: []ColumnInfo{{Name: "time", Type: "String"}}}
	result := TranslateWithOptions("| count() by time(5m)", timeSchema, TranslateOptions{AllowAggregation: true})
	if result.Error == nil || !strings.Contains(result.Error.Message, `Name it with "as"`) {
		t.Fatalf("time column: err=%v, want the time bucket error", result.Error)
	}
	if result := TranslateWithOptions("| count() by time(5m) as bucket", timeSchema, TranslateOptions{AllowAggregation: true}); !result.Valid {
		t.Fatalf("aliased time bucket: %v", result.Error)
	}

	// With no schema, the build still rejects a name that is the timestamp.
	result = TranslateWithOptions("| count() as ts", nil, TranslateOptions{AllowAggregation: true})
	params := aggregationParams("UTC")
	params.TimestampField = "ts"
	if _, err := BuildFullQueryFromTranslation(params, result); err == nil || !strings.Contains(err.Error(), `the name "ts"`) {
		t.Fatalf("timestamp name: err=%v", err)
	}
}

func TestSQLAggregationInterval(t *testing.T) {
	params := aggregationParams("Asia/Kolkata")
	if _, _, err := compileAggregation(t, "| count() by time", "", params); err == nil || !strings.Contains(err.Error(), "needs an interval") {
		t.Fatalf("no interval: err=%v", err)
	}
	if _, _, err := compileAggregation(t, "| count() by time", "7m", params); err == nil || !strings.Contains(err.Error(), `invalid interval "7m": use one of 1s, 5s`) {
		t.Fatalf("bad interval: err=%v", err)
	}
	// The key's own window wins over the request's.
	sql, result, err := compileAggregation(t, "| count() by time(1h)", "5m", params)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "toStartOfInterval(`timestamp`, INTERVAL 1 HOUR, 'Asia/Kolkata') AS `time`") || result.Aggregation.Info().Interval != "1h" {
		t.Fatalf("sql=%s info=%+v", sql, result.Aggregation.Info())
	}

	// 90 days at 1h gives 2160 buckets.
	params.StartTime, params.EndTime = "2026-08-17 00:00:00", "2026-11-15 00:00:00"
	if _, _, err := compileAggregation(t, "| count() by time(1h)", "", params); err == nil ||
		err.Error() != "1h over this range gives 2160 buckets. The maximum is 1000. Use 3h or longer" {
		t.Fatalf("buckets: err=%v", err)
	}
	if _, _, err := compileAggregation(t, "| count() by time(3h)", "", params); err != nil {
		t.Fatalf("3h: %v", err)
	}
	params.StartTime = "2020-01-01 00:00:00"
	if _, _, err := compileAggregation(t, "| count() by time(24h)", "", params); err == nil || !strings.HasSuffix(err.Error(), "Shorten the range") {
		t.Fatalf("no window fits: err=%v", err)
	}
}

// The smaller of the limit stage and the caller's limit wins.
func TestSQLAggregationLimit(t *testing.T) {
	for _, tt := range []struct {
		query string
		limit int
		want  string
	}{
		{"| count() by service | limit 10", 0, "LIMIT 10"},
		{"| count() by service | limit 10", 5, "LIMIT 5"},
		{"| count() by service | limit 10", 50, "LIMIT 10"},
		{"| count() by service", 7, "LIMIT 7"},
	} {
		params := aggregationParams("UTC")
		params.Limit = tt.limit
		sql, _, err := compileAggregation(t, tt.query, "", params)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(sql, "\n"+tt.want) {
			t.Fatalf("%s limit=%d: sql=%s, want %s", tt.query, tt.limit, sql, tt.want)
		}
	}
	sql, _, err := compileAggregation(t, "| count() by service", "", aggregationParams("UTC"))
	if err != nil || strings.Contains(sql, "LIMIT") {
		t.Fatalf("no limit: sql=%s err=%v", sql, err)
	}
}

// The four shapes run on ClickHouse, return their columns under the output
// names in order, and bucket in the request timezone.
func TestSQLAggregationShapesClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	conn, err := ch.Open(&ch.Options{Addr: []string{addr}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	table := fmt.Sprintf("default.logchefql_agg_%d", time.Now().UnixNano())
	if err := conn.Exec(ctx, "CREATE TABLE "+table+" (timestamp DateTime64(3), level LowCardinality(String), service String, duration String, attrs Map(LowCardinality(String), String)) ENGINE = Memory"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })
	// 20:00 UTC is 01:30 the next day in Asia/Kolkata.
	if err := conn.Exec(ctx, "INSERT INTO "+table+` VALUES
		('2026-11-15 10:00:00', 'error', 'api', '10', {'host': 'h1'}),
		('2026-11-15 10:01:00', 'error', 'api', '30', {'host': 'h2'}),
		('2026-11-15 10:07:00', 'error', 'web', 'n/a', {'host': 'h1'}),
		('2026-11-15 20:00:00', 'info', 'api', '50', {'host': 'h1'})`); err != nil {
		t.Fatal(err)
	}
	schema := &Schema{Columns: []ColumnInfo{
		{Name: "timestamp", Type: "DateTime64(3)"}, {Name: "level", Type: "LowCardinality(String)"},
		{Name: "service", Type: "String"}, {Name: "duration", Type: "String"}, {Name: "attrs", Type: "Map(LowCardinality(String), String)"},
	}}
	params := QueryBuildParams{TableName: table, TimestampField: "timestamp", StartTime: "2026-11-15 00:00:00", EndTime: "2026-11-16 06:00:00", Timezone: "UTC"}

	for _, tt := range []struct {
		name, query, interval, timezone string
		columns                         []string
		rows                            []string
	}{
		{"scalar", "level=error | count(), sum(duration) as total", "", "UTC", []string{"count", "total"}, []string{"3 40"}},
		{"grouped", "| count() by attrs.host", "", "UTC", []string{"attrs.host", "count"}, []string{"h1 3", "h2 1"}},
		{"timeseries", "level=error | count() by time", "5m", "UTC", []string{"time", "count"}, []string{"2026-11-15 10:00:00 2", "2026-11-15 10:05:00 1"}},
		{"grouped timeseries", "| max(duration) as slowest by time(24h), service", "", "Asia/Kolkata", []string{"time", "service", "slowest"},
			[]string{"2026-11-15 00:00:00 api 30", "2026-11-15 00:00:00 web <nil>", "2026-11-16 00:00:00 api 50"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := TranslateWithOptions(tt.query, schema, TranslateOptions{AllowAggregation: true, Interval: tt.interval})
			if !result.Valid {
				t.Fatal(result.Error)
			}
			p := params
			p.Timezone = tt.timezone
			sql, err := BuildFullQueryFromTranslation(p, result)
			if err != nil {
				t.Fatal(err)
			}
			rows, err := conn.Query(ctx, sql)
			if err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
			defer rows.Close()
			if got := rows.Columns(); strings.Join(got, ",") != strings.Join(tt.columns, ",") {
				t.Fatalf("columns=%v, want %v", got, tt.columns)
			}
			var got []string
			for rows.Next() {
				values := make([]any, len(tt.columns))
				for i, ct := range rows.ColumnTypes() {
					values[i] = newScanValue(ct.ScanType())
				}
				if err := rows.Scan(values...); err != nil {
					t.Fatal(err)
				}
				got = append(got, formatRow(values, tt.timezone))
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "|") != strings.Join(tt.rows, "|") {
				t.Fatalf("rows=%q, want %q", got, tt.rows)
			}
		})
	}
}

func newScanValue(scanType reflect.Type) any {
	return reflect.New(scanType).Interface()
}

// formatRow prints the scanned values of one row, times in timezone.
func formatRow(values []any, timezone string) string {
	location, _ := time.LoadLocation(timezone)
	parts := make([]string, len(values))
	for i, value := range values {
		v := reflect.ValueOf(value).Elem()
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				parts[i] = "<nil>"
				continue
			}
			v = v.Elem()
		}
		if ts, ok := reflect.TypeAssert[time.Time](v); ok {
			parts[i] = ts.In(location).Format(time.DateTime)
			continue
		}
		parts[i] = fmt.Sprint(v.Interface())
	}
	return strings.Join(parts, " ")
}

// A native JSON path groups as text, a missing path in the "" group, and
// count_distinct skips empty and missing values.
func TestSQLAggregationNativeJSONClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	conn, err := ch.Open(&ch.Options{Addr: []string{addr}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	version, err := conn.ServerVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version.Version.Major < 25 || version.Version.Major == 25 && version.Version.Minor < 3 {
		t.Skipf("native JSON requires ClickHouse 25.3 or newer, got %s", version.Version)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	table := fmt.Sprintf("default.logchefql_agg_json_%d", time.Now().UnixNano())
	if err := conn.Exec(ctx, "CREATE TABLE "+table+" (timestamp DateTime, j JSON) ENGINE = Memory"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })
	if err := conn.Exec(ctx, "INSERT INTO "+table+` VALUES
		('2026-11-15 10:00:00', '{"host":"h1","user":""}'),
		('2026-11-15 10:00:00', '{"host":"h1","user":"a"}'),
		('2026-11-15 10:00:00', '{"host":"h2","user":5}'),
		('2026-11-15 10:00:00', '{}')`); err != nil {
		t.Fatal(err)
	}
	schema := &Schema{Columns: []ColumnInfo{{Name: "timestamp", Type: "DateTime"}, {Name: "j", Type: "JSON"}}}
	result := TranslateWithOptions("| count(), count_distinct(j.user) as users by j.host", schema, TranslateOptions{AllowAggregation: true})
	if !result.Valid {
		t.Fatal(result.Error)
	}
	sql, err := BuildFullQueryFromTranslation(QueryBuildParams{TableName: table, TimestampField: "timestamp", StartTime: "2026-11-15 00:00:00", EndTime: "2026-11-16 00:00:00", Timezone: "UTC"}, result)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var host string
		var count, users uint64
		if err := rows.Scan(&host, &count, &users); err != nil {
			t.Fatal(err)
		}
		got = append(got, fmt.Sprintf("%q %d %d", host, count, users))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	want := []string{`"h1" 2 1`, `"" 1 0`, `"h2" 1 1`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows=%q, want %q", got, want)
	}
}
