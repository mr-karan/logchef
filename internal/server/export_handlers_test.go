package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// Regression guard for the P0 where /exports and /logs/export required raw_sql
// and rejected the web UI's query_text body ("raw_sql is required"). Both fields
// must be accepted, preferring raw_sql, falling back to query_text.
func TestExportQueryTextFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		rawSQL    string
		queryText string
		want      string
	}{
		{name: "raw_sql only", rawSQL: "SELECT 1", queryText: "", want: "SELECT 1"},
		{name: "query_text only (the regression)", rawSQL: "", queryText: "SELECT 2", want: "SELECT 2"},
		{name: "both prefer raw_sql", rawSQL: "SELECT 1", queryText: "SELECT 2", want: "SELECT 1"},
		{name: "raw_sql blank falls through", rawSQL: "   ", queryText: "SELECT 3", want: "SELECT 3"},
		{name: "neither", rawSQL: "", queryText: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := exportQueryText(tc.rawSQL, tc.queryText); got != tc.want {
				t.Fatalf("exportQueryText(%q, %q) = %q, want %q", tc.rawSQL, tc.queryText, got, tc.want)
			}
		})
	}
}

// A query_text-only export body (as the web UI sends) must bind and resolve to a
// non-empty query — the exact shape that regressed.
func TestCreateExportJobRequestAcceptsQueryText(t *testing.T) {
	t.Parallel()

	var req models.CreateExportJobRequest
	body := `{"query_text":"SELECT _timestamp FROM smtp.logs LIMIT 1","format":"csv"}`
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal query_text-only body: %v", err)
	}
	if got := exportQueryText(req.RawSQL, req.QueryText); strings.TrimSpace(got) == "" {
		t.Fatalf("query_text-only body resolved to empty query (regression); RawSQL=%q QueryText=%q", req.RawSQL, req.QueryText)
	}
}

func TestNormalizeExplicitExportFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		want   string
		wantOK bool
	}{
		{name: "csv", input: "csv", want: "csv", wantOK: true},
		{name: "ndjson", input: "ndjson", want: "ndjson", wantOK: true},
		{name: "jsonl alias", input: "jsonl", want: "ndjson", wantOK: true},
		{name: "mixed case", input: "NdJsOn", want: "ndjson", wantOK: true},
		{name: "blank", input: "", want: "", wantOK: false},
		{name: "invalid", input: "xlsx", want: "", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, ok := normalizeExplicitExportFormat(tc.input)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("normalizeExplicitExportFormat(%q) = (%q, %v), want (%q, %v)", tc.input, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestInferExportFormatFromAccept(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		accept string
		want   string
	}{
		{name: "csv accept", accept: "text/csv", want: "csv"},
		{name: "ndjson accept", accept: "application/x-ndjson", want: "ndjson"},
		{name: "jsonl accept", accept: "application/jsonl", want: "ndjson"},
		{name: "wildcard defaults ndjson", accept: "*/*", want: "ndjson"},
		{name: "blank defaults ndjson", accept: "", want: "ndjson"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := inferExportFormatFromAccept(tc.accept); got != tc.want {
				t.Fatalf("inferExportFormatFromAccept(%q) = %q, want %q", tc.accept, got, tc.want)
			}
		})
	}
}

func TestExportJobURLsAreRelativePaths(t *testing.T) {
	t.Parallel()

	job := &models.ExportJob{
		ID:        "export-123",
		SourceID:  9,
		Status:    models.ExportJobStatusComplete,
		Format:    "csv",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	resp := exportJobResponse(models.TeamID(5), job)

	wantStatus := "/api/v1/teams/5/sources/9/exports/export-123"
	if resp.StatusURL != wantStatus {
		t.Fatalf("StatusURL = %q, want %q", resp.StatusURL, wantStatus)
	}
	if strings.Contains(resp.StatusURL, "://") {
		t.Fatalf("StatusURL should be a relative path, got %q", resp.StatusURL)
	}

	wantDownload := "/api/v1/teams/5/sources/9/exports/export-123/download"
	if resp.DownloadURL != wantDownload {
		t.Fatalf("DownloadURL = %q, want %q", resp.DownloadURL, wantDownload)
	}
	if strings.Contains(resp.DownloadURL, "://") {
		t.Fatalf("DownloadURL should be a relative path, got %q", resp.DownloadURL)
	}
}
