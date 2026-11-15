package victorialogs

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestIntegrationLogchefQLAggregation compiles LogchefQL aggregations and runs
// them on a live VictoriaLogs. The expected rows follow RFC 0005: output names,
// the default order, a missing key as the "" group, counts as digit strings,
// and every other value as a number or null.
func TestIntegrationLogchefQLAggregation(t *testing.T) {
	baseURL := integrationBaseURL(t)
	runID := newTestRunID(t)

	// The hour before base starts three hours ago, so every row is in the
	// past. With Asia/Kolkata (+05:30) an hour bucket starts at :30 UTC.
	base := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	start, end := base.Add(-time.Hour), base.Add(3*time.Hour)
	at := func(d time.Duration) string { return base.Add(d).Format(time.RFC3339) }
	// d is numeric where present. mixed holds a number, text and nothing.
	records := []map[string]any{
		{"_time": at(5 * time.Minute), "_msg": "r1", "test_run": runID, "service": "api", "d": "10", "mixed": "5"},
		{"_time": at(40 * time.Minute), "_msg": "r2", "test_run": runID, "service": "api", "d": "30", "mixed": "abc"},
		{"_time": at(45 * time.Minute), "_msg": "r3", "test_run": runID, "service": "web", "d": "20"},
		{"_time": at(100 * time.Minute), "_msg": "r4", "test_run": runID, "mixed": "100"},
	}
	ingestRecords(t, baseURL, records)

	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: baseURL, Scope: models.VictoriaLogsScope{Query: fmt.Sprintf(`test_run:=%q`, runID)}})
	provider := newTestProvider(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	waitForFixtures(t, provider, source, queryWindow{start: start, end: end}, len(records))

	run := func(t *testing.T, query, interval string, limit int) *models.QueryResult {
		t.Helper()
		compiled, err := provider.CompileLogchefQL(ctx, source, datasource.LogchefQLCompileRequest{
			Query: query, AllowAggregation: true, Interval: interval,
			StartTime: start.Format(time.RFC3339), EndTime: end.Format(time.RFC3339), Timezone: "Asia/Kolkata",
		})
		if err != nil {
			t.Fatalf("compile %q: %v", query, err)
		}
		t.Logf("LogsQL: %s", compiled.Query)
		result, err := provider.QueryLogs(ctx, source, datasource.QueryRequest{
			RawQuery: compiled.Query, StartTime: &start, EndTime: &end,
			Limit: limit, MaxLimit: 1000, Aggregation: compiled.Aggregation,
		})
		if err != nil {
			t.Fatalf("run %q: %v", compiled.Query, err)
		}
		return result
	}
	check := func(t *testing.T, result *models.QueryResult, columns []models.ColumnInfo, rows []map[string]any) {
		t.Helper()
		if !reflect.DeepEqual(result.Columns, columns) {
			t.Errorf("columns=%v, want %v", result.Columns, columns)
		}
		if len(result.Logs) != len(rows) {
			t.Fatalf("rows=%v, want %v", result.Logs, rows)
		}
		for i := range rows {
			if !reflect.DeepEqual(result.Logs[i], rows[i]) {
				t.Errorf("row %d=%#v, want %#v", i, result.Logs[i], rows[i])
			}
		}
	}
	col := func(name, typ string) models.ColumnInfo { return models.ColumnInfo{Name: name, Type: typ} }

	t.Run("scalar with numeric input", func(t *testing.T) {
		result := run(t, "| count(), sum(d), avg(d), max(d), p95(d), count_distinct(service)", "", 0)
		check(t, result,
			[]models.ColumnInfo{col("count", "UInt64"), col("sum_d", "Float64"), col("avg_d", "Float64"), col("max_d", "Float64"), col("p95_d", "Float64"), col("count_distinct_service", "UInt64")},
			// r4 has no d and no service: the number functions skip it and
			// count_distinct does not count the empty value.
			[]map[string]any{{"count": "4", "sum_d": 60.0, "avg_d": 20.0, "max_d": 30.0, "p95_d": 30.0, "count_distinct_service": "2"}})
	})

	t.Run("no numeric input is null", func(t *testing.T) {
		result := run(t, "service=web | sum(mixed), avg(mixed), min(mixed), max(mixed), p95(mixed)", "", 0)
		check(t, result,
			[]models.ColumnInfo{col("sum_mixed", "Float64"), col("avg_mixed", "Float64"), col("min_mixed", "Float64"), col("max_mixed", "Float64"), col("p95_mixed", "Float64")},
			[]map[string]any{{"sum_mixed": nil, "avg_mixed": nil, "min_mixed": nil, "max_mixed": nil, "p95_mixed": nil}})
	})

	t.Run("non-numeric input", func(t *testing.T) {
		// sum and avg skip text. min, max and quantile compare text as text, and
		// a text result is null.
		result := run(t, "| sum(mixed), avg(mixed), max(mixed)", "", 0)
		check(t, result,
			[]models.ColumnInfo{col("sum_mixed", "Float64"), col("avg_mixed", "Float64"), col("max_mixed", "Float64")},
			[]map[string]any{{"sum_mixed": 105.0, "avg_mixed": 52.5, "max_mixed": nil}})
	})

	t.Run("grouped", func(t *testing.T) {
		result := run(t, "| count(), sum(d) by service", "", 0)
		// The first value descending, then the key ascending. r4 has no
		// service and groups under "".
		check(t, result,
			[]models.ColumnInfo{col("service", "String"), col("count", "UInt64"), col("sum_d", "Float64")},
			[]map[string]any{
				{"service": "api", "count": "2", "sum_d": 40.0},
				{"service": "", "count": "1", "sum_d": nil},
				{"service": "web", "count": "1", "sum_d": 20.0},
			})
		if result.Stats.Truncation != models.TruncationNone {
			t.Errorf("truncation=%q, want none", result.Stats.Truncation)
		}
	})

	t.Run("timeseries aligns buckets to the timezone", func(t *testing.T) {
		result := run(t, "| count() by time", "1h", 0)
		check(t, result,
			[]models.ColumnInfo{col("time", "DateTime"), col("count", "UInt64")},
			[]map[string]any{
				{"time": at(-30 * time.Minute), "count": "1"},
				{"time": at(30 * time.Minute), "count": "2"},
				{"time": at(90 * time.Minute), "count": "1"},
			})
	})

	t.Run("grouped timeseries", func(t *testing.T) {
		result := run(t, "| count() by service, time(1h)", "", 0)
		check(t, result,
			[]models.ColumnInfo{col("time", "DateTime"), col("service", "String"), col("count", "UInt64")},
			[]map[string]any{
				{"time": at(-30 * time.Minute), "service": "api", "count": "1"},
				{"time": at(30 * time.Minute), "service": "api", "count": "1"},
				{"time": at(30 * time.Minute), "service": "web", "count": "1"},
				{"time": at(90 * time.Minute), "service": "", "count": "1"},
			})
	})

	t.Run("an empty grouped result keeps its columns", func(t *testing.T) {
		result := run(t, "service=nope | count() by service", "", 0)
		check(t, result, []models.ColumnInfo{col("service", "String"), col("count", "UInt64")}, []map[string]any{})
	})

	t.Run("an alias that equals a key field", func(t *testing.T) {
		result := run(t, "| count() as service by service as svc | sort service desc | limit 1", "", 0)
		check(t, result, []models.ColumnInfo{col("svc", "String"), col("service", "UInt64")}, []map[string]any{{"svc": "api", "service": "2"}})
	})

	t.Run("the row probe proves a cut", func(t *testing.T) {
		result := run(t, "| count() by service", "", 2)
		if len(result.Logs) != 2 || result.Stats.Truncation != models.TruncationRows {
			t.Fatalf("rows=%v truncation=%q, want 2 rows cut by the row limit", result.Logs, result.Stats.Truncation)
		}
	})
}
