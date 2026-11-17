package logchefql

import (
	"strings"
	"testing"
)

// An alert condition on ClickHouse is the aggregated SELECT with the time
// filter the caller names, so the alert renderer applies its own window.
func TestBuildAggregationQuery(t *testing.T) {
	for _, tt := range []struct{ name, query, want string }{
		{
			name:  "scalar",
			query: `level="error" | count() as value`,
			want:  "SELECT count() AS `value`\nFROM default.logs\nWHERE {{__time_filter}}\n  AND (`level` = 'error')",
		},
		{
			name:  "no filter",
			query: `| sum(bytes)`,
			want:  "SELECT sumOrNull(`bytes`) AS `sum_bytes`\nFROM default.logs\nWHERE {{__time_filter}}",
		},
		{
			name:  "grouped",
			query: `level="error" | count() by service`,
			want:  "SELECT `service` AS `service`, count() AS `count`\nFROM default.logs\nWHERE {{__time_filter}}\n  AND (`level` = 'error')\nGROUP BY `service`\nORDER BY `count` DESC, `service` ASC",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := TranslateWithOptions(tt.query, aggregationSchema, TranslateOptions{AllowAggregation: true})
			if !result.Valid {
				t.Fatal(result.Error)
			}
			got, err := BuildAggregationQuery(result, "default.logs", "timestamp", "{{__time_filter}}")
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("query =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}

	for _, tt := range []struct{ name, query, want string }{
		{"no aggregation", `level="error"`, "the query has no aggregation"},
		{"time key", `| count() by time(5m)`, "a time key needs a time range"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := TranslateWithOptions(tt.query, aggregationSchema, TranslateOptions{AllowAggregation: true})
			if !result.Valid {
				t.Fatal(result.Error)
			}
			if _, err := BuildAggregationQuery(result, "default.logs", "timestamp", "{{__time_filter}}"); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

// The metric queries are the ones the alert form wrote for a Loki condition
// before the server compiled conditions (RFC 0006 unit Q1).
func TestTranslateToLogQLMetric(t *testing.T) {
	options := LogQLTranslateOptions{Selector: []LogQLMatcher{{Name: "app", Op: "=", Value: "api"}}, Parser: "logfmt"}
	filter := `{app="api"} | logfmt | drop __error__, __error_details__ | level="error"`
	for _, tt := range []struct{ name, query, want string }{
		{"count", `level="error" | count()`, `sum(count_over_time(` + filter + ` [300s]))`},
		{"sum", `level="error" | sum(dur)`, `sum(sum_over_time(` + filter + ` | unwrap dur | __error__="" [300s]))`},
		{"avg", `level="error" | avg(dur) as value`, `avg_over_time(` + filter + ` | unwrap dur | __error__="" [300s]) by ()`},
		{"min", `level="error" | min(dur)`, `min(min_over_time(` + filter + ` | unwrap dur | __error__="" [300s]))`},
		{"max", `level="error" | max(dur)`, `max(max_over_time(` + filter + ` | unwrap dur | __error__="" [300s]))`},
		{"no filter", `| count()`, `sum(count_over_time({app="api"} | logfmt | drop __error__, __error_details__ [300s]))`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := TranslateToLogQLMetric(tt.query, options, 300)
			if result.Error != nil {
				t.Fatal(result.Error)
			}
			if result.Query != tt.want {
				t.Errorf("query =\n%s\nwant\n%s", result.Query, tt.want)
			}
			if result.Aggregation == nil || result.Aggregation.Shape != ShapeScalar {
				t.Errorf("aggregation = %+v, want scalar", result.Aggregation)
			}
		})
	}

	t.Run("a filter alone is a log query without aggregation", func(t *testing.T) {
		result := TranslateToLogQLMetric(`level="error"`, options, 300)
		if result.Error != nil || result.Aggregation != nil || result.Query != filter {
			t.Fatalf("result = %+v", result)
		}
	})

	for _, tt := range []struct{ name, query, want string }{
		{"by key", `| count() by service`, "Write a native LogQL query to group"},
		{"two aggregates", `| count(), sum(dur)`, "one count, sum, avg, min or max"},
		{"limit", `| count() | limit 5`, "no by, sort or limit"},
		{"other function", `| p95(dur)`, "cannot compute p95()"},
		{"nested field", `| sum(log.dur)`, "not log.dur"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := TranslateToLogQLMetric(tt.query, options, 300)
			if result.Error == nil || result.Error.Code != ErrAggregationNotSupported || !strings.Contains(result.Error.Message, tt.want) {
				t.Fatalf("error = %+v, want %s with %q", result.Error, ErrAggregationNotSupported, tt.want)
			}
		})
	}
}
