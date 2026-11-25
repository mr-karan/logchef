package logchefql

import (
	"strings"
	"testing"
)

// A nested path on a String column is read with JSONExtractString, which
// returns the text of a JSON number ("400"; checked on ClickHouse 23.7, 24.1
// and 26.8). A number therefore compares as text with = and != and as a
// parsed number in an ordering, an aggregate parses the text, and only an
// ordering against a quoted value compares text, which gets a warning.
func TestJSONNumberInString(t *testing.T) {
	schema := &Schema{Columns: []ColumnInfo{
		{Name: "msg", Type: "String"},
		{Name: "body", Type: "LowCardinality(Nullable(String))"},
		{Name: "attrs", Type: "Map(LowCardinality(String), String)"},
		{Name: "duration_ms", Type: "UInt64"},
	}}
	for _, tc := range []struct {
		query   string
		sql     string // a substring of the translation
		warning string // a substring of the single warning; "" means none
	}{
		{`msg.status = 400`, "JSONExtractString(`msg`, 'status') = '400'", ""},
		{`msg.status != 400`, "JSONExtractString(`msg`, 'status') != '400'", ""},
		{`msg.status ~ 40`, "positionCaseInsensitive(JSONExtractString(`msg`, 'status'), '40') > 0", ""},
		{`msg.latency_ms > 500`, "toFloat64OrNull(JSONExtractString(`msg`, 'latency_ms')) > 500", ""},
		{`body.latency_ms <= 5`, "toFloat64OrNull(JSONExtractString(`body`, 'latency_ms')) <= 5", ""},
		{`msg.status = "400"`, "JSONExtractString(`msg`, 'status') = '400'", ""},
		{`msg.latency_ms > "500"`, "JSONExtractString(`msg`, 'latency_ms') > '500'", `as text, so "1000" sorts before "500". Write the number without quotes to compare numbers: msg.latency_ms > 500.`},
		{`| p95(msg.latency_ms)`, "toFloat64OrNull(JSONExtractString(`msg`, 'latency_ms'))", ""},
		{`duration_ms > 500`, "`duration_ms` > 500", ""},
		{`attrs.latency > "5"`, "`attrs`['latency'] > '5'", ""},
	} {
		t.Run(tc.query, func(t *testing.T) {
			result := TranslateWithOptions(tc.query, schema, TranslateOptions{AllowAggregation: true})
			if !result.Valid {
				t.Fatalf("translate failed: %v", result.Error)
			}
			translated := result.SQL
			if result.Aggregation != nil {
				query, err := BuildAggregationQuery(result, "logs.app", "ts", "{{__time_filter}}")
				if err != nil {
					t.Fatal(err)
				}
				translated = query
			}
			if !strings.Contains(translated, tc.sql) {
				t.Errorf("translation %q does not contain %q", translated, tc.sql)
			}
			switch {
			case tc.warning == "" && len(result.Warnings) != 0:
				t.Errorf("warnings = %q, want none", result.Warnings)
			case tc.warning != "" && (len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], tc.warning)):
				t.Errorf("warnings = %q, want one containing %q", result.Warnings, tc.warning)
			}
		})
	}

	// The same comparison twice warns once.
	result := TranslateWithOptions(`msg.latency_ms > "5" and msg.latency_ms > "5"`, schema, TranslateOptions{})
	if len(result.Warnings) != 1 {
		t.Fatalf("repeated comparison: warnings = %q, want one", result.Warnings)
	}

	// A number against a plain String column is still an error.
	if result := Translate(`msg = 400`, schema); result.Valid {
		t.Fatalf("msg = 400 on a String column translated to %q, want an error", result.SQL)
	}
}
