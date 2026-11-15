package victorialogs

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestFormatTimezoneOffset asserts formatTimezoneOffset returns a
// VictoriaLogs-parseable duration string (not a clock-offset string like
// "+05:30") for the `offset` param on /select/logsql/hits, including
// half-hour and negative zones. Empirically verified against a running
// VictoriaLogs instance: offset=19800s (Asia/Kolkata, UTC+5:30) aligns
// buckets to IST midnight (00:00 IST == 18:30 UTC), and offset=-9000s
// (America/St_Johns, UTC-2:30 in July/NDT) aligns buckets to NDT midnight
// (00:00 NDT == 02:30 UTC).
func TestFormatTimezoneOffset(t *testing.T) {
	t.Parallel()

	reference := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		timezone string
		want     string
	}{
		{name: "UTC returns empty", timezone: "UTC", want: ""},
		{name: "empty timezone returns empty", timezone: "", want: ""},
		{name: "unknown timezone returns empty", timezone: "Not/AZone", want: ""},
		{name: "positive offset Asia/Kolkata (+05:30)", timezone: "Asia/Kolkata", want: "19800s"},
		{name: "half-hour negative offset America/St_Johns (-02:30 NDT in July)", timezone: "America/St_Johns", want: "-9000s"},
		{name: "quarter-hour offset Asia/Kathmandu (+05:45)", timezone: "Asia/Kathmandu", want: "20700s"},
		{name: "negative whole-hour offset America/New_York (-04:00 EDT in July)", timezone: "America/New_York", want: "-14400s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatTimezoneOffset(tt.timezone, &reference, nil)
			if got != tt.want {
				t.Fatalf("formatTimezoneOffset(%q) = %q, want %q", tt.timezone, got, tt.want)
			}
			if got != "" {
				if _, err := time.ParseDuration(got); err != nil {
					t.Fatalf("formatTimezoneOffset(%q) = %q is not a valid Go/VL duration: %v", tt.timezone, got, err)
				}
			}
		})
	}
}

// Field values and live tail need log rows, so they reject an aggregation; a
// compile that opts in returns the query, its filter and the result columns.
func TestLogchefQLAggregationOptIn(t *testing.T) {
	source := &models.Source{SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time"}
	_, err := compileQueryForVictoriaLogs("x=1 | count()", models.QueryLanguageLogchefQL, source)
	if parseErr, ok := errors.AsType[*logchefql.ParseError](err); !ok || parseErr.Code != logchefql.ErrAggregationNotAllowed {
		t.Fatalf("field values error=%v, want %s", err, logchefql.ErrAggregationNotAllowed)
	}

	provider := &Provider{}
	denied, err := provider.CompileLogchefQL(context.Background(), source, datasource.LogchefQLCompileRequest{Query: "x=1 | count()"})
	if err == nil || denied.Valid || denied.Error == nil || denied.Error.Code != logchefql.ErrAggregationNotAllowed {
		t.Fatalf("default compile=%+v err=%v, want %s", denied, err, logchefql.ErrAggregationNotAllowed)
	}

	compiled, err := provider.CompileLogchefQL(context.Background(), source, datasource.LogchefQLCompileRequest{
		Query: "x=1 | p95(d) by time", AllowAggregation: true, Interval: "1h",
		StartTime: "2026-11-10 00:00:00", Timezone: "Asia/Kolkata",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "x:=1 | stats by (_time:1h offset 19800s) quantile(0.95, d) if (d:*) as __lc_v0 | sort by (_time)"; compiled.Query != want {
		t.Fatalf("query=%q, want %q", compiled.Query, want)
	}
	if compiled.FilterOnly != "x:=1" {
		t.Fatalf("filter=%q, want x:=1", compiled.FilterOnly)
	}
	wantInfo := &logchefql.AggregationInfo{Shape: logchefql.ShapeTimeseries, Interval: "1h", Columns: []logchefql.ResultColumn{
		{Name: "time", Role: logchefql.RoleTime, Type: "DateTime", Source: "_time"},
		{Name: "p95_d", Role: logchefql.RoleValue, Type: "Float64", Source: "__lc_v0"},
	}}
	if !reflect.DeepEqual(compiled.Aggregation, wantInfo) {
		t.Fatalf("aggregation=%+v, want %+v", compiled.Aggregation, wantInfo)
	}
}

// The bucket offset is the request timezone's offset at the start of the
// range, so a range that starts before a daylight-saving change keeps the
// offset of its start.
func TestBucketOffset(t *testing.T) {
	tests := []struct {
		start, timezone, want string
	}{
		{"2026-11-10T00:00:00Z", "UTC", ""},
		{"", "", ""},
		{"2026-11-10T00:00:00Z", "Asia/Kolkata", "19800s"},
		{"2026-11-10 00:00:00", "Asia/Kolkata", "19800s"},
		{"2026-07-01T00:00:00Z", "America/New_York", "-14400s"},
		{"2026-11-01T07:00:00Z", "America/New_York", "-18000s"},
	}
	for _, tt := range tests {
		got, err := bucketOffset(tt.start, tt.timezone)
		if err != nil || got != tt.want {
			t.Errorf("bucketOffset(%q, %q)=%q, %v, want %q", tt.start, tt.timezone, got, err, tt.want)
		}
	}
	if _, err := bucketOffset("yesterday", "Asia/Kolkata"); err == nil || !strings.Contains(err.Error(), "invalid start_time") {
		t.Fatalf("bucketOffset(yesterday) err=%v, want invalid start_time", err)
	}
}

// A `by time` aggregation over too wide a range fails at compile with the
// error that ClickHouse gives for the same query and range.
func TestLogchefQLAggregationBucketLimit(t *testing.T) {
	source := &models.Source{SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time"}
	provider := &Provider{}
	compile := func(query, start, end, timezone string) (*datasource.CompiledLogchefQL, error) {
		return provider.CompileLogchefQL(context.Background(), source, datasource.LogchefQLCompileRequest{
			Query: query, AllowAggregation: true, Interval: "5m",
			StartTime: start, EndTime: end, Timezone: timezone,
		})
	}

	// 90 days at 1h gives 2160 buckets.
	const query = "x=1 | count() by time(1h)"
	start, end := "2026-08-17 00:00:00", "2026-11-15 00:00:00"
	clickhouse := logchefql.TranslateWithOptions(query, nil, logchefql.TranslateOptions{AllowAggregation: true, Interval: "5m"})
	_, chErr := logchefql.BuildFullQueryFromTranslation(logchefql.QueryBuildParams{
		TableName: "default.logs", TimestampField: "timestamp", StartTime: start, EndTime: end, Timezone: "Asia/Kolkata",
	}, clickhouse)
	if chErr == nil {
		t.Fatal("ClickHouse accepted 2160 buckets")
	}

	for _, bounds := range [][2]string{{start, end}, {"2026-08-16T18:30:00Z", "2026-11-14T18:30:00Z"}} {
		compiled, err := compile(query, bounds[0], bounds[1], "Asia/Kolkata")
		parseErr, ok := errors.AsType[*logchefql.ParseError](err)
		if !ok || parseErr.Code != logchefql.ErrInvalidAggregation || err.Error() != chErr.Error() {
			t.Fatalf("range %v: err=%v, want ClickHouse's %v", bounds, err, chErr)
		}
		if compiled == nil || !compiled.Valid {
			t.Fatalf("range %v: compiled=%+v, want the valid compile beside the range error", bounds, compiled)
		}
	}

	// The request interval is the window of a bare `by time`.
	if _, err := compile("x=1 | count() by time", "2026-11-14 00:00:00", "2026-11-18 00:00:00", "UTC"); err == nil ||
		err.Error() != "5m over this range gives 1152 buckets. The maximum is 1000. Use 10m or longer" {
		t.Fatalf("request interval: err=%v", err)
	}
	// A range that fits, no time key, or no complete window passes.
	for _, tt := range [][4]string{
		{query, "2026-11-14 00:00:00", "2026-11-15 00:00:00", "UTC"},
		{"x=1 | count() by service", start, end, "UTC"},
		{query, start, "", "UTC"},
	} {
		if _, err := compile(tt[0], tt[1], tt[2], tt[3]); err != nil {
			t.Fatalf("%v: %v", tt, err)
		}
	}
}
