package logchefql

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// fixtureKey is one `by` key as the shared fixture writes it.
type fixtureKey struct {
	Field  string `json:"field,omitempty"`
	Time   bool   `json:"time,omitempty"`
	Window string `json:"window,omitempty"`
}

type fixtureCase struct {
	Query         string       `json:"query"`
	Valid         bool         `json:"valid"`
	IsAggregation bool         `json:"is_aggregation"`
	Functions     []string     `json:"functions"`
	Keys          []fixtureKey `json:"keys"`
	Columns       []string     `json:"columns"`
	ErrorCode     string       `json:"error_code"`
}

// The fixture is shared with the frontend vitest suite, so a syntax change
// that reaches one side fails the other.
func TestAggregationFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/aggregation_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []fixtureCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Query, func(t *testing.T) {
			result := Validate(tc.Query)
			if result.Valid != tc.Valid {
				t.Fatalf("Validate valid=%v (error %+v), want %v", result.Valid, result.Error, tc.Valid)
			}
			if !tc.Valid {
				if result.Error == nil || result.Error.Code != tc.ErrorCode {
					t.Fatalf("Validate error=%+v, want code %s", result.Error, tc.ErrorCode)
				}
				if tc.ErrorCode != ErrInvalidAggregation {
					return
				}
			}

			pq, err := ParseLogchefQL(tc.Query)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			agg := AggregationOf(ConvertToAST(pq))
			if (agg != nil) != tc.IsAggregation {
				t.Fatalf("is aggregation=%v, want %v", agg != nil, tc.IsAggregation)
			}
			if agg == nil || !tc.Valid {
				return
			}

			functions := make([]string, len(agg.Aggregates))
			for i, a := range agg.Aggregates {
				functions[i] = string(a.Func)
			}
			keys := make([]fixtureKey, len(agg.By))
			for i, k := range agg.By {
				keys[i] = fixtureKey{Time: k.Time, Window: k.Interval}
				if !k.Time {
					keys[i].Field = getFieldName(k.Field)
				}
			}
			var columns []string
			for _, col := range agg.Info("").Columns {
				columns = append(columns, col.Name)
			}
			if !reflect.DeepEqual(functions, tc.Functions) {
				t.Errorf("functions=%q, want %q", functions, tc.Functions)
			}
			if len(keys) != 0 || len(tc.Keys) != 0 {
				if !reflect.DeepEqual(keys, tc.Keys) {
					t.Errorf("keys=%+v, want %+v", keys, tc.Keys)
				}
			}
			if !reflect.DeepEqual(columns, tc.Columns) {
				t.Errorf("columns=%q, want %q", columns, tc.Columns)
			}
		})
	}
}

func TestValidateAggregationMessages(t *testing.T) {
	tests := []struct {
		query   string
		message string
		column  int
	}{
		{"| median(x)", `unknown function "median". Use count, count_distinct, sum, avg, min, max, p50, p75, p90, p95 or p99`, 3},
		{"| sum()", "sum needs a field, as in sum(bytes)", 3},
		{"| count(), sum(x), avg()", "avg needs a field, as in avg(bytes)", 20},
		{"| count(x)", "count takes no field. Write count()", 3},
		{"| a(), b(), c(), d(), e(), f(), g(), h(), i(), j(), k()", "a query can have at most 10 aggregations", 53},
		{"| count() by a, b, c, d, e, f", "a query can group by at most 5 keys", 29},
		{"| count() by time, time(5m)", "a query can have at most one time key", 20},
		{"| count() as 2xx", `invalid name "2xx"`, 14},
		{"| count() by service as my-name", `invalid name "my-name"`, 25},
		{"| count(), count()", `two columns are named "count". Name one with "as"`, 3},
		{"| count() by service, service", `"service" is grouped twice`, 23},
		{"| sum(__lc_x)", `the field "__lc_x" starts with "__lc_", which is reserved`, 3},
		{"| count() as __lc_v0", `the name "__lc_v0" starts with "__lc_", which is reserved`, 3},
		{"| count() by service | sort foo", `cannot sort by "foo". Columns are service, count`, 29},
		{"| count() | limit 0", "limit must be between 1 and 10000", 19},
		{"| count() | limit 10001", "limit must be between 1 and 10000", 19},
		{"| count() by time, service | limit 10", "limit cannot be used with time and a field, because it would cut buckets and not series. Add a filter to reduce the series", 36},
		{"| count() by time(7m)", `invalid interval "7m": use one of 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h, 2h, 3h, 6h, 12h, 24h`, 14},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			result := Validate(tt.query)
			if result.Valid || result.Error == nil {
				t.Fatalf("Validate(%q) is valid, want an error", tt.query)
			}
			if result.Error.Code != ErrInvalidAggregation || result.Error.Message != tt.message {
				t.Fatalf("error=%s %q, want %s %q", result.Error.Code, result.Error.Message, ErrInvalidAggregation, tt.message)
			}
			if result.Error.Position == nil || result.Error.Position.Column != tt.column {
				t.Fatalf("position=%+v, want column %d", result.Error.Position, tt.column)
			}
		})
	}
}

func TestAggregationInfo(t *testing.T) {
	tests := []struct {
		query    string
		interval string
		want     AggregationInfo
	}{
		{
			query: "level=error | count()",
			want: AggregationInfo{Shape: ShapeScalar, Columns: []ResultColumn{
				{Name: "count", Role: RoleValue, Type: "UInt64", Source: "count"},
			}},
		},
		{
			query: "| count() as errors, count_distinct(user_id), sum(bytes) by service | limit 10",
			want: AggregationInfo{Shape: ShapeGrouped, Limit: 10, Columns: []ResultColumn{
				{Name: "service", Role: RoleGroup, Type: "String", Source: "service"},
				{Name: "errors", Role: RoleValue, Type: "UInt64", Source: "errors"},
				{Name: "count_distinct_user_id", Role: RoleValue, Type: "UInt64", Source: "count_distinct_user_id"},
				{Name: "sum_bytes", Role: RoleValue, Type: "Float64", Source: "sum_bytes"},
			}},
		},
		{
			query:    "| avg(d) by time",
			interval: "1m",
			want: AggregationInfo{Shape: ShapeTimeseries, Interval: "1m", Columns: []ResultColumn{
				{Name: "time", Role: RoleTime, Type: "DateTime", Source: "time"},
				{Name: "avg_d", Role: RoleValue, Type: "Float64", Source: "avg_d"},
			}},
		},
		{
			query:    "| p95(d) by service, time(5m) as bucket",
			interval: "1m",
			want: AggregationInfo{Shape: ShapeGroupedTimeseries, Interval: "5m", Columns: []ResultColumn{
				{Name: "bucket", Role: RoleTime, Type: "DateTime", Source: "bucket"},
				{Name: "service", Role: RoleGroup, Type: "String", Source: "service"},
				{Name: "p95_d", Role: RoleValue, Type: "Float64", Source: "p95_d"},
			}},
		},
		{
			query:    "| count() by service",
			interval: "1m",
			want: AggregationInfo{Shape: ShapeGrouped, Columns: []ResultColumn{
				{Name: "service", Role: RoleGroup, Type: "String", Source: "service"},
				{Name: "count", Role: RoleValue, Type: "UInt64", Source: "count"},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			ast, perr := parseQuery(tt.query)
			if perr != nil {
				t.Fatal(perr)
			}
			got := AggregationOf(ast).Info(tt.interval)
			if !reflect.DeepEqual(*got, tt.want) {
				t.Fatalf("Info=%+v\nwant %+v", *got, tt.want)
			}
		})
	}
}

func TestAggregationJSONOmitsSource(t *testing.T) {
	info := AggregationInfo{Shape: ShapeScalar, Columns: []ResultColumn{{Name: "count", Role: RoleValue, Type: "UInt64", Source: "__lc_v0"}}}
	raw, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"shape":"scalar","columns":[{"name":"count","role":"value","type":"UInt64"}],"interval":"","limit":0}`
	if string(raw) != want {
		t.Fatalf("json=%s, want %s", raw, want)
	}
}

// A caller that did not opt in reads AGGREGATION_NOT_ALLOWED. One that did
// compiles on both ClickHouse and LogsQL.
func TestTranslateAggregationDefaultDeny(t *testing.T) {
	const query = "x=1 | count()"
	const deny = "this feature needs log rows, and the query aggregates. Remove the pipe after the filter"
	translators := []struct {
		name      string
		translate func(allow bool) *ParseError
	}{
		{"clickhouse", func(allow bool) *ParseError {
			return TranslateWithOptions(query, nil, TranslateOptions{AllowAggregation: allow}).Error
		}},
		{"logsql", func(allow bool) *ParseError {
			return TranslateToLogsQL(query, &LogsQLTranslateOptions{AllowAggregation: allow}).Error
		}},
	}
	for _, tr := range translators {
		t.Run(tr.name, func(t *testing.T) {
			denied := tr.translate(false)
			if denied == nil || denied.Code != ErrAggregationNotAllowed || denied.Message != deny {
				t.Fatalf("default error=%+v, want %s %q", denied, ErrAggregationNotAllowed, deny)
			}
			if allowed := tr.translate(true); allowed != nil {
				t.Fatalf("allowed error=%+v, want a compiled query", allowed)
			}
		})
	}

	if result := Translate(query, nil); result.Valid || result.Error == nil || result.Error.Code != ErrAggregationNotAllowed {
		t.Fatalf("Translate=%+v, want %s", result, ErrAggregationNotAllowed)
	}
	if result := TranslateToLogsQL(query, nil); result.Valid || result.Error == nil || result.Error.Code != ErrAggregationNotAllowed {
		t.Fatalf("TranslateToLogsQL(nil options)=%+v, want %s", result, ErrAggregationNotAllowed)
	}
	// An invalid aggregation reports the rule it breaks, not the deny.
	if result := Translate("x=1 | median(y)", nil); result.Error == nil || result.Error.Code != ErrInvalidAggregation {
		t.Fatalf("Translate(invalid)=%+v, want %s", result.Error, ErrInvalidAggregation)
	}
}

// The select pipe keeps its meaning: a bare name is still a field.
func TestSelectPipeUnchangedByAggregation(t *testing.T) {
	for query, want := range map[string]string{
		"| count":     "`count` AS `count`",
		"x=1 | limit": "`limit` AS `limit`",
		"x=1 | sort":  "`sort` AS `sort`",
	} {
		result := Translate(query, nil)
		if !result.Valid || result.SelectClause != want {
			t.Errorf("Translate(%q) select=%q valid=%v error=%+v, want %q", query, result.SelectClause, result.Valid, result.Error, want)
		}
	}
}

func TestPinTimeWindow(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{"| count() by time", "| count() by time(5m)"},
		{"level=error |\n  count() by TIME as t, service", "level=error |\n  count() by TIME(5m) as t, service"},
		{`msg="by time" | count() by service, time`, `msg="by time" | count() by service, time(5m)`},
		{"| count() by time(1h)", "| count() by time(1h)"},
		{`| count() by "time"`, `| count() by "time"`},
		{"| count() by time.zone", "| count() by time.zone"},
		{"level=error | time msg", "level=error | time msg"},
		{"| count() by", "| count() by"},
	} {
		if got := PinTimeWindow(tc.query, "5m"); got != tc.want {
			t.Errorf("PinTimeWindow(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}
}
