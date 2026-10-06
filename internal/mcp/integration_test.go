package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

// T-MCP-8: real ClickHouse and VictoriaLogs queries through the HTTP handler
// that NewServer returns. The dev compose environment provides the data
// (`just dev-setup`): default.http in ClickHouse and the local-plugin rows in
// VictoriaLogs, all on 2026-10-01 between 09:43 and 11:44 UTC.
func TestRealBackendsThroughHTTPHandler(t *testing.T) {
	chAddr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	vlURL := os.Getenv("LOGCHEF_TEST_VICTORIALOGS_URL")
	if chAddr == "" || vlURL == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR and LOGCHEF_TEST_VICTORIALOGS_URL to run against the dev backends")
	}
	ctx := context.Background()
	w := newWorld(t)
	log := discardLogger()

	chSource := &models.Source{
		Name: "dev-http", SourceType: models.SourceTypeClickHouse, MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{Host: chAddr, Username: "default", Database: "default", TableName: "http"},
	}
	if err := w.db.CreateSource(ctx, chSource); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	w.link(t, w.team, chSource.ID)
	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.AddSource(ctx, chSource); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	w.ds = datasource.NewService(w.db, log)
	w.ds.Register(datasource.NewClickHouseProvider(manager, log))
	w.ds.Register(victorialogs.NewProvider(log))
	vlSource := w.victoriaLogsSource(t, vlURL)

	handler := NewServer(w.deps())
	member := access.APITokenPrincipal(w.member, &models.APIToken{Scopes: []models.TokenScope{
		models.TokenScopeTeamsRead, models.TokenScopeSourcesRead, models.TokenScopeLogsRead,
	}})
	postResult := func(t *testing.T, name string, args map[string]any) toolResult {
		t.Helper()
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequestWithContext(WithPrincipal(t.Context(), member), http.MethodPost, "/mcp", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("%s: status %d, content type %q: %s", name, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
		var envelope struct {
			Result toolResult      `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("%s: %v: %s", name, err, rec.Body.String())
		}
		if len(envelope.Error) != 0 {
			t.Fatalf("%s: JSON-RPC error %s", name, envelope.Error)
		}
		return envelope.Result
	}
	post := func(t *testing.T, name string, args map[string]any) toolResult {
		t.Helper()
		result := postResult(t, name, args)
		if result.IsError {
			t.Fatalf("%s: %s", name, result.text())
		}
		return result
	}

	get := httptest.NewRequestWithContext(WithPrincipal(t.Context(), member), http.MethodGet, "/mcp", http.NoBody)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, get)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /mcp = %d, want 405", rec.Code)
	}

	// The investigation panel sends Date.toISOString() values.
	const start, mid, end = "2026-10-01T09:00:00.000Z", "2026-10-01T11:00:00.000Z", "2026-10-01T12:00:00.000Z"
	for _, backend := range []struct {
		name     string
		source   *models.Source
		language string
		filter   string
	}{
		{"clickhouse", chSource, string(models.QueryLanguageClickHouseSQL), "method=GET"},
		{"victorialogs", vlSource, string(models.QueryLanguageLogsQL), "level=error"},
	} {
		t.Run(backend.name, func(t *testing.T) {
			ids := map[string]any{"team_id": w.team, "source_id": backend.source.ID}
			with := func(extra map[string]any) map[string]any {
				args := maps.Clone(ids)
				maps.Copy(args, extra)
				return args
			}

			// Ported from logchef-mcp TestQueryLogchefQLStructuredEvidence.
			result := post(t, "query_logchefql", with(map[string]any{"query": backend.filter, "start_time": start, "end_time": end, "limit": 5}))
			var evidence QueryResult
			if err := json.Unmarshal(result.StructuredContent, &evidence); err != nil {
				t.Fatal(err)
			}
			if evidence.GeneratedQueryLanguage != backend.language || evidence.TeamID != int(w.team) || evidence.RowCount == 0 ||
				evidence.RowCount != len(evidence.Logs) || evidence.QueryID != "q-test" {
				t.Fatalf("lost evidence: %+v", evidence)
			}
			if result.text() == "" {
				t.Fatal("missing text fallback")
			}

			// The investigation panel's histogram flow: translate, then histogram.
			result = post(t, "translate_logchefql", with(map[string]any{"query": backend.filter, "start_time": start, "end_time": end, "timezone": "UTC"}))
			var translated TranslateResult
			if err := json.Unmarshal(result.StructuredContent, &translated); err != nil || !translated.Valid {
				t.Fatalf("translate = %+v, %v", translated, err)
			}
			native := translated.GeneratedQuery
			if backend.language == string(models.QueryLanguageClickHouseSQL) {
				native = translated.FullSQL
			}
			if native == "" {
				t.Fatalf("translate returned no executable query: %+v", translated)
			}
			result = post(t, "get_log_histogram", with(map[string]any{"raw_sql": native, "start_time": start, "end_time": end, "window": "10m"}))
			var histogram HistogramResult
			if err := json.Unmarshal(result.StructuredContent, &histogram); err != nil || len(histogram.Data) == 0 {
				t.Fatalf("histogram = %+v, %v", histogram, err)
			}

			// Ported from logchef-mcp TestCompareWindowsDoesNotReportSampleAsRate.
			result = post(t, "compare_windows", with(map[string]any{"query": "", "window1_start": start, "window1_end": mid,
				"window2_start": mid, "window2_end": end, "limit": 1}))
			var comparison CompareWindowsResult
			if err := json.Unmarshal(result.StructuredContent, &comparison); err != nil {
				t.Fatal(err)
			}
			if comparison.Delta.RowCountPercent != nil || !comparison.Window1.Truncated {
				t.Fatalf("sample reported as complete comparison: %+v", comparison)
			}

			result = post(t, "get_source_schema", ids)
			var schema []SchemaColumnResult
			if err := json.Unmarshal(result.StructuredContent, &schema); err != nil || len(schema) == 0 {
				t.Fatalf("schema = %+v, %v", schema, err)
			}

			nativeQuery := "* | limit 3"
			if backend.language == string(models.QueryLanguageClickHouseSQL) {
				nativeQuery = "SELECT * FROM default.http ORDER BY timestamp DESC LIMIT 3"
			}
			result = post(t, "query_logs", with(map[string]any{"raw_sql": nativeQuery, "start_time": start, "end_time": end}))
			if err := json.Unmarshal(result.StructuredContent, &evidence); err != nil || evidence.RowCount == 0 {
				t.Fatalf("query_logs = %+v, %v", evidence, err)
			}

			result = post(t, "get_all_field_dimensions", with(map[string]any{"start_time": start, "end_time": end}))
			if !strings.Contains(result.text(), "values") {
				t.Fatalf("field dimensions = %s", result.text())
			}
		})
	}

	// Backend diagnostics stay visible so the model can fix its query.
	diagnostic := postResult(t, "query_logs", map[string]any{"team_id": w.team, "source_id": chSource.ID,
		"raw_sql": "SELECT no_such_column FROM default.http LIMIT 1"})
	if !diagnostic.IsError || !strings.Contains(diagnostic.text(), "no_such_column") || strings.Contains(diagnostic.text(), "internal error") {
		t.Fatalf("query_logs with a bad column = %q, want the ClickHouse diagnostic", diagnostic.text())
	}

	result := post(t, "get_sources", map[string]any{})
	var sources SourcesAggregateResult
	if err := json.Unmarshal(result.StructuredContent, &sources); err != nil {
		t.Fatal(err)
	}
	connected := 0
	for _, s := range sources.Sources {
		if s.IsConnected {
			connected++
		}
	}
	if connected != 2 {
		t.Fatalf("connected sources = %d, want 2: %+v", connected, sources.Sources)
	}

	result = post(t, "top_values", map[string]any{"team_id": w.team, "source_id": chSource.ID, "fields": []string{"method", "missing_field"},
		"start_time": start, "end_time": end, "limit": 3})
	var top TopValuesResult
	if err := json.Unmarshal(result.StructuredContent, &top); err != nil {
		t.Fatal(err)
	}
	if len(top.Fields) != 2 || len(top.Fields[0].Values) == 0 || top.Fields[1].Values != nil {
		t.Fatalf("top values = %+v", top)
	}

	var first QueryResult
	result = post(t, "query_logchefql", map[string]any{"team_id": w.team, "source_id": chSource.ID, "query": "", "start_time": start, "end_time": end, "limit": 1})
	if err := json.Unmarshal(result.StructuredContent, &first); err != nil || len(first.Logs) != 1 {
		t.Fatalf("first row = %+v, %v", first, err)
	}
	result = post(t, "get_log_context", map[string]any{"team_id": w.team, "source_id": chSource.ID,
		"timestamp": timestampMillis(t, first.Logs[0]["timestamp"]), "before_limit": 2, "after_limit": 2})
	if !strings.Contains(result.text(), "target_logs") {
		t.Fatalf("log context = %s", result.text())
	}

	if active, admitted := w.admits.counts(); active != 0 || admitted == 0 {
		t.Fatalf("admission active=%d admitted=%d", active, admitted)
	}
}

func timestampMillis(t *testing.T, value any) int64 {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("timestamp %v is not a string", value)
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.UnixMilli()
		}
	}
	t.Fatalf("unparsable timestamp %q", text)
	return 0
}
