package logchefql

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// One golden per row of the RFC 0005 construct table, LogsQL column, plus the
// cases that internal names exist for: an alias that equals a key field, and
// two keys that swap names.
func TestLogsQLAggregationGolden(t *testing.T) {
	tests := []struct {
		name, query, interval, offset string
		want                          string
		// sources maps each output column to the field VictoriaLogs returns
		// it under, in result order. Nil skips the check.
		sources [][2]string
	}{
		{
			name:    "count scalar",
			query:   "x=1 | count()",
			want:    "x:=1 | stats count() as __lc_v0",
			sources: [][2]string{{"count", "__lc_v0"}},
		},
		{
			name:  "no filter matches every row",
			query: "| count()",
			want:  "* | stats count() as __lc_v0",
		},
		{
			name:  "count_distinct",
			query: "| count_distinct(user_id)",
			want:  "* | stats count_uniq(user_id) as __lc_v0",
		},
		{
			name:  "sum and avg skip rows without the field",
			query: "| sum(bytes), avg(bytes)",
			want:  "* | stats sum(bytes) if (bytes:*) as __lc_v0, avg(bytes) if (bytes:*) as __lc_v1",
		},
		{
			name:  "min and max",
			query: "| min(bytes), max(bytes)",
			want:  "* | stats min(bytes) if (bytes:*) as __lc_v0, max(bytes) if (bytes:*) as __lc_v1",
		},
		{
			name:  "percentiles",
			query: "| p50(d), p75(d), p90(d), p95(d), p99(d)",
			want: "* | stats quantile(0.5, d) if (d:*) as __lc_v0, quantile(0.75, d) if (d:*) as __lc_v1, " +
				"quantile(0.9, d) if (d:*) as __lc_v2, quantile(0.95, d) if (d:*) as __lc_v3, quantile(0.99, d) if (d:*) as __lc_v4",
			sources: [][2]string{{"p50_d", "__lc_v0"}, {"p75_d", "__lc_v1"}, {"p90_d", "__lc_v2"}, {"p95_d", "__lc_v3"}, {"p99_d", "__lc_v4"}},
		},
		{
			name:    "by field sorts by the first value then the keys",
			query:   "level=error | count() by service",
			want:    `level:="error" | stats by (service) count() as __lc_v0 | sort by (__lc_v0 desc, service)`,
			sources: [][2]string{{"service", "service"}, {"count", "__lc_v0"}},
		},
		{
			name:    "nested field keeps its dotted name",
			query:   "| count() by log_attributes.host",
			want:    "* | stats by (log_attributes.host) count() as __lc_v0 | sort by (__lc_v0 desc, log_attributes.host)",
			sources: [][2]string{{"log_attributes.host", "log_attributes.host"}, {"count", "__lc_v0"}},
		},
		{
			name:     "by time with its own window and the offset",
			query:    "service=api | p95(duration_ms), avg(duration_ms) by time(5m)",
			interval: "1h",
			offset:   "19800s",
			want: `service:="api" | stats by (_time:5m offset 19800s) quantile(0.95, duration_ms) if (duration_ms:*) as __lc_v0, ` +
				`avg(duration_ms) if (duration_ms:*) as __lc_v1 | sort by (_time)`,
			sources: [][2]string{{"time", "_time"}, {"p95_duration_ms", "__lc_v0"}, {"avg_duration_ms", "__lc_v1"}},
		},
		{
			name:     "by time with the request interval",
			query:    "| count() by time",
			interval: "1h",
			want:     "* | stats by (_time:1h) count() as __lc_v0 | sort by (_time)",
		},
		{
			name:     "a negative offset",
			query:    "| count() by time",
			interval: "24h",
			offset:   "-18000s",
			want:     "* | stats by (_time:24h offset -18000s) count() as __lc_v0 | sort by (_time)",
		},
		{
			name:    "time and a field put the bucket first",
			query:   "| count() by service, time(1m)",
			want:    "* | stats by (_time:1m, service) count() as __lc_v0 | sort by (_time, service)",
			sources: [][2]string{{"time", "_time"}, {"service", "service"}, {"count", "__lc_v0"}},
		},
		{
			name:  "sort desc and limit",
			query: "level=error | count() as errors, count_distinct(user_id) as users by service | sort errors desc | limit 10",
			want: `level:="error" | stats by (service) count() as __lc_v0, count_uniq(user_id) as __lc_v1 ` +
				"| sort by (__lc_v0 desc, service) | limit 10",
			sources: [][2]string{{"service", "service"}, {"errors", "__lc_v0"}, {"users", "__lc_v1"}},
		},
		{
			name:  "sort asc by a value",
			query: "| sum(bytes) by service | sort sum_bytes asc",
			want:  "* | stats by (service) sum(bytes) if (bytes:*) as __lc_v0 | sort by (__lc_v0, service)",
		},
		{
			name:  "sort by a key lists it once",
			query: "| count() by service, host | sort host desc",
			want:  "* | stats by (service, host) count() as __lc_v0 | sort by (host desc, service)",
		},
		{
			name:  "sort by time desc",
			query: "| count() by time(5m) | sort time desc",
			want:  "* | stats by (_time:5m) count() as __lc_v0 | sort by (_time desc)",
		},
		{
			name:  "scalar sort and limit",
			query: "| count() | sort count desc | limit 1",
			want:  "* | stats count() as __lc_v0 | sort by (__lc_v0 desc) | limit 1",
		},
		{
			name:    "an alias that equals a key field",
			query:   "| count() as service by service as svc",
			want:    "* | stats by (service) count() as __lc_v0 | sort by (__lc_v0 desc, service)",
			sources: [][2]string{{"svc", "service"}, {"service", "__lc_v0"}},
		},
		{
			name:    "two keys that swap names",
			query:   "| count() by service as host, host as service",
			want:    "* | stats by (service, host) count() as __lc_v0 | sort by (__lc_v0 desc, service, host)",
			sources: [][2]string{{"host", "service"}, {"service", "host"}, {"count", "__lc_v0"}},
		},
		{
			name:    "a field named time next to the bucket",
			query:   `| count() by time(5m) as t, "time"`,
			want:    "* | stats by (_time:5m, time) count() as __lc_v0 | sort by (_time, time)",
			sources: [][2]string{{"t", "_time"}, {"time", "time"}, {"count", "__lc_v0"}},
		},
		{
			name:    "keyword and spaced fields are quoted",
			query:   `| min(count), avg("bytes sent") by "my host"`,
			want:    `* | stats by ("my host") min("count") if ("count":*) as __lc_v0, avg("bytes sent") if ("bytes sent":*) as __lc_v1 | sort by (__lc_v0 desc, "my host")`,
			sources: [][2]string{{"my host", "my host"}, {"min_count", "__lc_v0"}, {"avg_bytes_sent", "__lc_v1"}},
		},
		{
			// A bare name with a colon is a bucket in a stats clause.
			name:    "fields with a colon are quoted",
			query:   `| sum("latency:10") by "http:status"`,
			want:    `* | stats by ("http:status") sum("latency:10") if ("latency:10":*) as __lc_v0 | sort by (__lc_v0 desc, "http:status")`,
			sources: [][2]string{{"http:status", "http:status"}, {"sum_latency_10", "__lc_v0"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TranslateToLogsQL(tt.query, &LogsQLTranslateOptions{AllowAggregation: true, Interval: tt.interval, BucketOffset: tt.offset})
			if !result.Valid {
				t.Fatalf("error=%+v", result.Error)
			}
			if result.Query != tt.want {
				t.Fatalf("query\n got %s\nwant %s", result.Query, tt.want)
			}
			if tt.sources == nil {
				return
			}
			got := make([][2]string, len(result.Aggregation.Columns))
			for i, col := range result.Aggregation.Columns {
				got[i] = [2]string{col.Name, col.Source}
			}
			if !reflect.DeepEqual(got, tt.sources) {
				t.Fatalf("sources=%v, want %v", got, tt.sources)
			}
		})
	}
}

// Filter is the query without its pipes, so the field sidebar and the
// conditions list read the filter of an aggregated query.
func TestLogsQLAggregationFilter(t *testing.T) {
	result := TranslateToLogsQL("level=error and service=api | count() by host", &LogsQLTranslateOptions{AllowAggregation: true})
	if want := `(level:="error") AND (service:="api")`; !result.Valid || result.Filter != want {
		t.Fatalf("filter=%q error=%+v, want %q", result.Filter, result.Error, want)
	}
	if len(result.Conditions) != 2 {
		t.Fatalf("conditions=%v, want the two filter conditions", result.Conditions)
	}
	if plain := TranslateToLogsQL("level=error | msg", nil); plain.Filter != plain.Query || plain.Aggregation != nil {
		t.Fatalf("select query filter=%q aggregation=%v, want the query and no aggregation", plain.Filter, plain.Aggregation)
	}
}

func TestLogsQLAggregationErrors(t *testing.T) {
	tests := []struct {
		name, query, interval, code, message string
	}{
		{
			name:    "time with no interval",
			query:   "| count() by time",
			code:    ErrInvalidAggregation,
			message: "this query buckets by time, so the request needs an interval",
		},
		{
			name:     "the _time field next to the bucket",
			query:    "| count() by time, _time",
			interval: "5m",
			code:     ErrAggregationNotSupported,
			message:  "cannot group by the field _time and by time together on this source, because the time bucket is the _time field",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TranslateToLogsQL(tt.query, &LogsQLTranslateOptions{AllowAggregation: true, Interval: tt.interval})
			if result.Valid || result.Error == nil || result.Error.Code != tt.code || result.Error.Message != tt.message {
				t.Fatalf("result=%+v error=%+v, want %s %q", result, result.Error, tt.code, tt.message)
			}
			if result.Aggregation != nil {
				t.Fatalf("aggregation=%+v on a failed compile", result.Aggregation)
			}
		})
	}

	// The _time field alone groups by the exact timestamp, which VictoriaLogs runs.
	if result := TranslateToLogsQL("| count() by _time", &LogsQLTranslateOptions{AllowAggregation: true}); !result.Valid {
		t.Fatalf("by _time error=%+v", result.Error)
	}
}

// CheckBuckets is the bucket limit that both backends run: ClickHouse from
// the full query build, VictoriaLogs from its provider compile.
func TestCheckBuckets(t *testing.T) {
	day := time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name, window string
		from, to     time.Time
		want         string
	}{
		{"1000 buckets fit", "1m", day, day.Add(1000 * time.Minute), ""},
		{"1001 buckets", "1m", day, day.Add(1001 * time.Minute), "1m over this range gives 1001 buckets. The maximum is 1000. Use 5m or longer"},
		{"a partial bucket counts", "1h", day, day.Add(1000*time.Hour + time.Second), "1h over this range gives 1001 buckets. The maximum is 1000. Use 2h or longer"},
		{"90 days at 1h", "1h", day.AddDate(0, 0, -90), day, "1h over this range gives 2160 buckets. The maximum is 1000. Use 3h or longer"},
		{"no window fits", "24h", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), day, "24h over this range gives 2510 buckets. The maximum is 1000. Shorten the range"},
		{"unknown window", "7m", day, day.Add(time.Hour), `invalid interval "7m": use one of 1s, 5s`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckBuckets(tt.window, tt.from, tt.to)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("err=%v, want nil", err)
				}
				return
			}
			if err == nil || err.Code != ErrInvalidAggregation || !strings.HasPrefix(err.Error(), tt.want) {
				t.Fatalf("err=%v, want %s %q", err, ErrInvalidAggregation, tt.want)
			}
		})
	}
}
