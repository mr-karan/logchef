package logchefql

import (
	"strings"
	"testing"
)

// A nested path on a String column is read with JSONExtractString, which
// returns "" for a JSON number. Using such a path as a number translates, so
// the translation says what it will quietly miss.
func TestJSONNumberInStringWarnings(t *testing.T) {
	schema := &Schema{Columns: []ColumnInfo{
		{Name: "msg", Type: "String"},
		{Name: "body", Type: "LowCardinality(Nullable(String))"},
		{Name: "attrs", Type: "Map(LowCardinality(String), String)"},
		{Name: "duration_ms", Type: "UInt64"},
	}}
	for _, tc := range []struct {
		query string
		want  []string // substrings of the single warning; nil means no warning
	}{
		{`msg.latency_ms > "500"`, []string{"msg.latency_ms", "String column msg", "orders text", "JSONExtractFloat(`msg`, 'latency_ms')"}},
		{`body.latency_ms <= "5"`, []string{"body.latency_ms", "String column body", "JSONExtractFloat(`body`, 'latency_ms')"}},
		{`msg.level = "error" | p95(msg.latency_ms) by msg.service`, []string{"p95 skips every numeric value"}},
		{`| avg(msg.latency_ms)`, []string{"avg skips every numeric value"}},
		{`msg.level = "error"`, nil},
		{`msg.level ~ "err"`, nil},
		{`| count_distinct(msg.user) by msg.service`, nil},
		{`duration_ms > 500 | p95(duration_ms)`, nil},
		{`attrs.latency > "5"`, nil},
	} {
		t.Run(tc.query, func(t *testing.T) {
			result := TranslateWithOptions(tc.query, schema, TranslateOptions{AllowAggregation: true})
			if !result.Valid {
				t.Fatalf("translate failed: %v", result.Error)
			}
			if tc.want == nil {
				if len(result.Warnings) != 0 {
					t.Fatalf("warnings = %q, want none", result.Warnings)
				}
				return
			}
			if len(result.Warnings) != 1 {
				t.Fatalf("warnings = %q, want one", result.Warnings)
			}
			for _, part := range tc.want {
				if !strings.Contains(result.Warnings[0], part) {
					t.Errorf("warning %q does not contain %q", result.Warnings[0], part)
				}
			}
		})
	}

	// The same path used twice warns once.
	result := TranslateWithOptions(`msg.latency_ms > "5" and msg.latency_ms < "9"`, schema, TranslateOptions{})
	if len(result.Warnings) != 1 {
		t.Fatalf("repeated path: warnings = %q, want one", result.Warnings)
	}

	// A numeric literal against the path is an error that names both fixes:
	// quoting for a JSON string, native SQL for a JSON number.
	result = Translate(`msg.latency_ms > 500`, schema)
	if result.Valid || result.Error == nil || !strings.Contains(result.Error.Message, "JSONExtractFloat(`msg`, 'latency_ms')") || !strings.Contains(result.Error.Message, "quote the value") {
		t.Fatalf("numeric literal: valid=%v error=%v, want an error naming quoting and JSONExtractFloat", result.Valid, result.Error)
	}
}
