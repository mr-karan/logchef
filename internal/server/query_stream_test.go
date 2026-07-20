package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func drainWriter(t *testing.T, run func(w *queryStreamWriter)) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	w := newQueryStreamWriter(bw, queryStreamConfig{logsKey: "data"}, "qid-1")
	run(w)
	if err := bw.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("streamed body is not valid JSON: %v\nbody=%s", err, buf.String())
	}
	return got
}

func TestQueryStreamWriter_NativeShapeAllRows(t *testing.T) {
	t.Parallel()

	columns := []models.ColumnInfo{{Name: "ts", Type: "DateTime64"}, {Name: "msg", Type: "String"}}
	rows := []map[string]any{
		{"ts": "2026-07-14T00:00:00Z", "msg": "a"},
		{"ts": "2026-07-14T00:00:01Z", "msg": "b"},
		{"ts": "2026-07-14T00:00:02Z", "msg": "c"},
	}

	got := drainWriter(t, func(w *queryStreamWriter) {
		w.SetWarnings([]models.QueryWarning{{Code: "LIMIT_APPLIED", Message: "showing first 3"}})
		if err := w.Begin(columns); err != nil {
			t.Fatalf("Begin: %v", err)
		}
		for _, r := range rows {
			if err := w.WriteRow(r); err != nil {
				t.Fatalf("WriteRow: %v", err)
			}
		}
		if err := w.Finish(models.QueryStats{RowsReturned: len(rows), LimitApplied: 3, ExecutionTimeMs: 12}); err != nil {
			t.Fatalf("Finish: %v", err)
		}
	})

	if got["status"] != "success" {
		t.Fatalf("status = %v, want success", got["status"])
	}
	data, ok := got["data"].(map[string]any)
	if !ok {
		t.Fatalf("data is not an object: %T", got["data"])
	}
	// Logs live under the "data" key for the native endpoint (byte-compatible
	// with the previous buffered response).
	logs, ok := data["data"].([]any)
	if !ok {
		t.Fatalf("data.data is not an array: %T", data["data"])
	}
	if len(logs) != len(rows) {
		t.Fatalf("streamed %d rows, want %d", len(logs), len(rows))
	}
	cols, ok := data["columns"].([]any)
	if !ok || len(cols) != 2 {
		t.Fatalf("data.columns wrong: %v", data["columns"])
	}
	if _, ok := data["stats"].(map[string]any); !ok {
		t.Fatalf("data.stats missing/wrong: %T", data["stats"])
	}
	if data["query_id"] != "qid-1" {
		t.Fatalf("data.query_id = %v, want qid-1", data["query_id"])
	}
	warnings, ok := data["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("data.warnings wrong: %v", data["warnings"])
	}
	if _, present := data["generated_sql"]; present {
		t.Fatalf("native shape should not include generated_sql")
	}
}

func TestQueryStreamWriter_ZeroRowsEmitsEmptyArray(t *testing.T) {
	t.Parallel()

	got := drainWriter(t, func(w *queryStreamWriter) {
		if err := w.Begin([]models.ColumnInfo{{Name: "msg", Type: "String"}}); err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := w.Finish(models.QueryStats{}); err != nil {
			t.Fatalf("Finish: %v", err)
		}
	})

	data := got["data"].(map[string]any)
	logs, ok := data["data"].([]any)
	if !ok {
		t.Fatalf("data.data is not an array: %T", data["data"])
	}
	if len(logs) != 0 {
		t.Fatalf("expected empty logs array, got %d", len(logs))
	}
	// warnings default to [] even when SetWarnings was never called.
	if _, ok := data["warnings"].([]any); !ok {
		t.Fatalf("warnings should default to an array, got %T", data["warnings"])
	}
}

func TestQueryStreamWriter_LogchefQLShape(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	w := newQueryStreamWriter(bw, queryStreamConfig{
		logsKey:           "logs",
		includeGenerated:  true,
		generatedSQL:      "SELECT 1",
		generatedQuery:    "SELECT 1",
		generatedLanguage: models.QueryLanguageClickHouseSQL,
	}, "qid-2")

	if err := w.Begin([]models.ColumnInfo{{Name: "msg", Type: "String"}}); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := w.WriteRow(map[string]any{"msg": "hi"}); err != nil {
		t.Fatalf("WriteRow: %v", err)
	}
	if err := w.Finish(models.QueryStats{RowsReturned: 1}); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if err := bw.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("not valid JSON: %v\nbody=%s", err, buf.String())
	}
	data := got["data"].(map[string]any)
	if _, ok := data["logs"].([]any); !ok {
		t.Fatalf("logchefql shape must put rows under data.logs, got %T", data["logs"])
	}
	if _, present := data["data"]; present {
		t.Fatalf("logchefql shape should not include a data.data key")
	}
	if data["generated_sql"] != "SELECT 1" || data["generated_query"] != "SELECT 1" {
		t.Fatalf("generated fields missing: %v", data)
	}
	if data["generated_query_language"] != string(models.QueryLanguageClickHouseSQL) {
		t.Fatalf("generated_query_language = %v", data["generated_query_language"])
	}
}

func TestQueryStreamWriter_ErrorBeforeBegin(t *testing.T) {
	t.Parallel()

	got := drainWriter(t, func(w *queryStreamWriter) {
		// Query failed before any columns/rows were produced (bad SQL, timeout).
		if err := w.WriteError(errors.New("boom: syntax error")); err != nil {
			t.Fatalf("WriteError: %v", err)
		}
	})

	if got["status"] != "error" {
		t.Fatalf("status = %v, want error", got["status"])
	}
	if got["message"] != "boom: syntax error" {
		t.Fatalf("message = %v", got["message"])
	}
	if got["error_type"] != string(models.DatabaseErrorType) {
		t.Fatalf("error_type = %v", got["error_type"])
	}
	if _, present := got["data"]; present {
		t.Fatalf("error envelope should not carry a data payload")
	}
}

func TestQueryStreamWriter_ErrorMidStreamStaysValidJSON(t *testing.T) {
	t.Parallel()

	got := drainWriter(t, func(w *queryStreamWriter) {
		if err := w.Begin([]models.ColumnInfo{{Name: "msg", Type: "String"}}); err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := w.WriteRow(map[string]any{"msg": "partial"}); err != nil {
			t.Fatalf("WriteRow: %v", err)
		}
		// Failure after streaming started: the envelope is already open, so it
		// must be closed validly with an error marker.
		if err := w.WriteError(errors.New("stream interrupted")); err != nil {
			t.Fatalf("WriteError: %v", err)
		}
	})

	// Status is still "success" (already written), but the body is valid JSON,
	// carries the partial rows, and includes an error field for diagnostics.
	data, ok := got["data"].(map[string]any)
	if !ok {
		t.Fatalf("data missing after mid-stream error: %v", got)
	}
	logs, ok := data["data"].([]any)
	if !ok || len(logs) != 1 {
		t.Fatalf("expected 1 partial row, got %v", data["data"])
	}
	if data["error"] != "stream interrupted" {
		t.Fatalf("expected error field, got %v", data["error"])
	}
}
