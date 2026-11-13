package datasource

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

type collectingStreamWriter struct {
	rows     []map[string]any
	warnings []models.QueryWarning
	stats    models.QueryStats
}

func (w *collectingStreamWriter) SetWarnings(warnings []models.QueryWarning) { w.warnings = warnings }
func (w *collectingStreamWriter) Begin([]models.ColumnInfo) error            { return nil }
func (w *collectingStreamWriter) WriteRow(row map[string]any) error {
	w.rows = append(w.rows, row)
	return nil
}
func (w *collectingStreamWriter) Finish(stats models.QueryStats) error {
	w.stats = stats
	return nil
}

// TestQueryLogsLimitRuleClickHouse runs the LIMIT rule against ClickHouse: the
// statement's own limit wins when it is smaller, a clamp keeps OFFSET, a probe
// row proves a cut, and a LIMIT expression fills its cap with an unknown state.
// A source's max_result_rows at or below the probe row leaves a filled result
// unknown.
func TestQueryLogsLimitRuleClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := clickhouse.NewManager(log)
	defer manager.Close()
	source := &models.Source{ID: 1, Connection: models.ConnectionInfo{Host: addr, Database: "system", TableName: "numbers"}}
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	provider := NewClickHouseProvider(manager, log)

	maxResultRows := func(rows int64, overflow string) *models.ClickHouseQuerySettings {
		settings := &models.ClickHouseQuerySettings{MaxResultRows: &rows}
		if overflow != "" {
			settings.ResultOverflowMode = &overflow
		}
		return settings
	}

	for i, tc := range []struct {
		name           string
		query          string
		limit          int
		settings       *models.ClickHouseQuerySettings
		wantRows       int
		wantFirst      uint64
		wantSource     models.LimitSource
		wantTruncation models.Truncation
	}{
		{
			name: "statement limit", query: "SELECT number FROM numbers(1000) ORDER BY number DESC LIMIT 20", limit: 100,
			wantRows: 20, wantFirst: 999, wantSource: models.LimitSourceStatement, wantTruncation: models.TruncationNone,
		},
		{
			name: "capped with offset and more rows", query: "SELECT number FROM numbers(1000) ORDER BY number LIMIT 5000 OFFSET 200", limit: 100,
			wantRows: 100, wantFirst: 200, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationRows,
		},
		{
			name: "capped with offset and no more rows", query: "SELECT number FROM numbers(300) ORDER BY number LIMIT 5000 OFFSET 200", limit: 100,
			wantRows: 100, wantFirst: 200, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationNone,
		},
		{
			name: "request limit with a cut", query: "SELECT number FROM numbers(1000) ORDER BY number", limit: 100,
			wantRows: 100, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationRows,
		},
		{
			name: "request limit filled exactly", query: "SELECT number FROM numbers(100) ORDER BY number", limit: 100,
			wantRows: 100, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationNone,
		},
		{
			name: "default limit with a cut", query: "SELECT number FROM numbers(2000) ORDER BY number", limit: 0,
			wantRows: 1000, wantFirst: 0, wantSource: models.LimitSourceDefault, wantTruncation: models.TruncationRows,
		},
		{
			name: "LIMIT expression filling the cap", query: "SELECT number FROM numbers(1000) ORDER BY number LIMIT 50 * 2", limit: 100,
			wantRows: 100, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationUnknown,
		},
		{
			name: "LIMIT expression below the cap", query: "SELECT number FROM numbers(1000) ORDER BY number LIMIT 5 * 2", limit: 100,
			wantRows: 10, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationNone,
		},
		{
			name: "CTE limit untouched", query: "WITH c AS (SELECT number FROM numbers(1000) ORDER BY number LIMIT 5) SELECT number FROM c", limit: 100,
			wantRows: 5, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationNone,
		},
		{
			name: "UNION branch limits are their own", query: "SELECT number FROM numbers(5) LIMIT 2 UNION ALL SELECT number FROM numbers(5) LIMIT 3", limit: 100,
			wantRows: 5, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationNone,
		},
		{
			name: "parenthesized UNION branch limits are their own", query: "(SELECT number FROM numbers(5) LIMIT 2) UNION ALL (SELECT number FROM numbers(5) LIMIT 3)", limit: 100,
			wantRows: 5, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationNone,
		},
		{
			name: "UNION capped as a whole with a cut", query: "SELECT number FROM numbers(100) UNION ALL SELECT number FROM numbers(100)", limit: 150,
			wantRows: 150, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationRows,
		},
		{
			name: "UNION filling the cap exactly", query: "SELECT number FROM numbers(50) UNION ALL SELECT number FROM numbers(50) SETTINGS max_threads = 1", limit: 100,
			wantRows: 100, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationNone,
		},
		{
			name: "source max_result_rows equal to the request", query: "SELECT number FROM numbers(10000) ORDER BY number", limit: 100,
			settings: maxResultRows(100, ""),
			wantRows: 100, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationUnknown,
		},
		{
			name: "source max_result_rows below the request", query: "SELECT number FROM numbers(10000) ORDER BY number", limit: 100,
			settings: maxResultRows(50, ""),
			wantRows: 50, wantFirst: 0, wantSource: models.LimitSourceMax, wantTruncation: models.TruncationUnknown,
		},
		{
			name: "source max_result_rows that throws leaves the probe out", query: "SELECT number FROM numbers(10000) ORDER BY number", limit: 100,
			settings: maxResultRows(100, "throw"),
			wantRows: 100, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationUnknown,
		},
		{
			name: "source max_result_rows above the probe row", query: "SELECT number FROM numbers(10000) ORDER BY number", limit: 100,
			settings: maxResultRows(1000, ""),
			wantRows: 100, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationRows,
		},
		{
			name: "source max_result_rows with a complete result", query: "SELECT number FROM numbers(30) ORDER BY number", limit: 100,
			settings: maxResultRows(50, ""),
			wantRows: 30, wantFirst: 0, wantSource: models.LimitSourceMax, wantTruncation: models.TruncationNone,
		},
		{
			name: "LIMIT n BY untouched", query: "SELECT number % 3 AS n, number FROM numbers(1000) ORDER BY n, number LIMIT 1 BY n", limit: 100,
			wantRows: 3, wantFirst: 0, wantSource: models.LimitSourceRequest, wantTruncation: models.TruncationNone,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := source
			if tc.settings != nil {
				source = &models.Source{ID: models.SourceID(100 + i), Connection: source.Connection}
				source.Connection.Settings = tc.settings
				if err := manager.AddSource(ctx, source); err != nil {
					t.Fatal(err)
				}
			}
			req := QueryRequest{RawQuery: tc.query, Limit: tc.limit, DefaultLimit: 1000, MaxLimit: 100000}
			result, err := provider.QueryLogs(ctx, source, req)
			if err != nil {
				t.Fatal(err)
			}
			check := func(path string, rows []map[string]any, stats models.QueryStats) {
				t.Helper()
				if len(rows) != tc.wantRows || stats.RowsReturned != tc.wantRows {
					t.Fatalf("%s returned %d rows (stats %d), want %d", path, len(rows), stats.RowsReturned, tc.wantRows)
				}
				if first, _ := rows[0]["number"].(uint64); first != tc.wantFirst {
					t.Fatalf("%s first row = %v, want %d", path, rows[0]["number"], tc.wantFirst)
				}
				if stats.LimitSource != tc.wantSource || stats.Truncation != tc.wantTruncation {
					t.Fatalf("%s stats = source %q truncation %q, want %q %q", path, stats.LimitSource, stats.Truncation, tc.wantSource, tc.wantTruncation)
				}
				if stats.Truncated != (tc.wantTruncation == models.TruncationRows) {
					t.Fatalf("%s truncated = %v for truncation %q", path, stats.Truncated, stats.Truncation)
				}
			}
			check("QueryLogs", result.Logs, result.Stats)

			writer := &collectingStreamWriter{}
			stats, err := provider.QueryLogsStream(ctx, source, req, writer)
			if err != nil {
				t.Fatal(err)
			}
			check("QueryLogsStream", writer.rows, stats)
			if writer.stats.Truncation != stats.Truncation {
				t.Fatalf("Finish stats truncation %q, returned %q", writer.stats.Truncation, stats.Truncation)
			}
		})
	}
}
