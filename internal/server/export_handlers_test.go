package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
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

func TestExportOptionalBlocksWithoutVariables(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	for _, operation := range []string{"stream", "job"} {
		t.Run(operation, func(t *testing.T) {
			s := newDashboardTestServer(t)
			s.config = &config.Config{Export: config.ExportConfig{
				MaxRows: 10, DefaultTimeoutSeconds: 5, MaxTimeoutSeconds: 10,
				ArtifactTTL: time.Hour, Formats: []string{"ndjson"},
			}}
			s.clickhouse = clickhouse.NewManager(s.log)
			t.Cleanup(func() { _ = s.clickhouse.Close() })
			s.datasources = datasource.NewService(s.sqlite, s.log)
			s.datasources.Register(datasource.NewClickHouseProvider(s.clickhouse, s.log))
			source := &models.Source{Name: "export", SourceType: models.SourceTypeClickHouse,
				Connection: models.ConnectionInfo{Host: addr, Database: "system", TableName: "one"}}
			if err := s.sqlite.CreateSource(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			if err := s.clickhouse.AddSource(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			user, src := authorizeTestSource(t, s.sqlite, source.ID)
			handler := s.handleExportLogs
			if operation == "job" {
				handler = s.handleCreateExportJob
			}
			app := fiber.New()
			withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/export", user, handler)
			req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/teams/%d/sources/%d/export", src.TeamID(), source.ID),
				strings.NewReader(`{"query_text":"SELECT 1 AS value [[WHERE 1={{x}}]]","format":"ndjson"}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if operation == "stream" {
				if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), `"row":{"value":1}`) || strings.Contains(string(body), `"type":"error"`) {
					t.Fatalf("status=%d body=%s", resp.StatusCode, body)
				}
				return
			}
			if resp.StatusCode != http.StatusAccepted {
				t.Fatalf("status=%d body=%s", resp.StatusCode, body)
			}
			var result struct {
				Data models.ExportJobResponse `json:"data"`
			}
			if err := json.Unmarshal(body, &result); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				job, err := s.sqlite.GetExportJob(t.Context(), result.Data.ID)
				if err != nil {
					t.Fatal(err)
				}
				if job.Status == models.ExportJobStatusFailed {
					t.Fatalf("export failed: %s", job.ErrorMessage)
				}
				if job.Status == models.ExportJobStatusComplete {
					t.Cleanup(func() { _ = os.Remove(job.FilePath) })
					data, err := os.ReadFile(job.FilePath)
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(string(data), `"row":{"value":1}`) || strings.Contains(string(data), `"type":"error"`) {
						t.Fatalf("artifact=%s", data)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("export did not finish before deadline")
		})
	}
}

// exportCapabilityProvider reports the exports capability only when exports is set.
type exportCapabilityProvider struct {
	fakeClickHouseCompiler
	exports bool
}

func (p *exportCapabilityProvider) Describe() datasource.Descriptor {
	descriptor := p.fakeClickHouseCompiler.Describe()
	if p.exports {
		descriptor.Capabilities = []datasource.Capability{datasource.CapabilityExports}
	}
	return descriptor
}

func TestExportRequestValidation(t *testing.T) {
	const (
		unsupportedFormat = "Unsupported export format. Use csv or ndjson."
		streamingCSV      = "Streaming CSV exports are not supported. Create an export job instead."
	)
	cases := []struct {
		name       string
		noExports  bool
		formats    []string
		accept     string
		body       string
		wantStream string
		wantJob    string
	}{
		{name: "source without exports", noExports: true, body: `{"raw_sql":"SELECT 1"}`,
			wantStream: "Exports are not supported for this source type yet", wantJob: "Exports are not supported for this source type yet"},
		{name: "invalid body", body: `{`, wantStream: "Invalid request body", wantJob: "Invalid request body"},
		{name: "missing query", body: `{"format":"ndjson"}`,
			wantStream: "raw_sql (or query_text) is required", wantJob: "raw_sql (or query_text) is required"},
		{name: "unknown format", body: `{"raw_sql":"SELECT 1","format":"xml"}`, wantStream: unsupportedFormat, wantJob: unsupportedFormat},
		{name: "format not allowed", formats: []string{"csv"}, body: `{"query_text":"SELECT 1","format":"jsonl"}`,
			wantStream: unsupportedFormat, wantJob: unsupportedFormat},
		{name: "explicit csv", body: `{"raw_sql":"SELECT 1","format":"CSV","query_timeout":120}`,
			wantStream: streamingCSV, wantJob: "query timeout cannot exceed 60 seconds for Download"},
		{name: "csv from accept", accept: "text/csv", body: `{"raw_sql":"SELECT 1","query_timeout":120}`,
			wantStream: streamingCSV, wantJob: "query timeout cannot exceed 60 seconds for Download"},
		{name: "default format", formats: []string{"ndjson"}, body: `{"raw_sql":"SELECT 1","query_timeout":120}`,
			wantStream: "Query timeout cannot exceed 60 seconds for Download", wantJob: unsupportedFormat},
		{name: "invalid timeout", body: `{"raw_sql":"SELECT 1","format":"ndjson","query_timeout":0}`,
			wantStream: "Query timeout must be a positive number", wantJob: "Query timeout must be a positive number"},
	}
	for _, tc := range cases {
		for _, endpoint := range []string{"stream", "job"} {
			t.Run(tc.name+"/"+endpoint, func(t *testing.T) {
				s := newDashboardTestServer(t)
				s.config = &config.Config{}
				s.config.Export.Formats = []string{"csv", "ndjson"}
				if tc.formats != nil {
					s.config.Export.Formats = tc.formats
				}
				s.config.Export.DefaultTimeoutSeconds = 30
				s.config.Export.MaxTimeoutSeconds = 60
				s.datasources = datasource.NewService(s.sqlite, s.log)
				s.datasources.Register(&exportCapabilityProvider{exports: !tc.noExports})
				source := &models.Source{Name: "export-validation", SourceType: models.SourceTypeClickHouse}
				if err := s.sqlite.CreateSource(t.Context(), source); err != nil {
					t.Fatal(err)
				}
				user, src := authorizeTestSource(t, s.sqlite, source.ID)
				app := fiber.New()
				handler, want := s.handleExportLogs, tc.wantStream
				if endpoint == "job" {
					handler, want = s.handleCreateExportJob, tc.wantJob
				}
				withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/export", user, handler)
				req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/teams/%d/sources/%d/export", src.TeamID(), source.ID), strings.NewReader(tc.body))
				req.Header.Set("Content-Type", "application/json")
				if tc.accept != "" {
					req.Header.Set("Accept", tc.accept)
				}
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				var body map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != http.StatusBadRequest || body["message"] != want || body["error_type"] != string(models.ValidationErrorType) {
					t.Fatalf("status=%d body=%v, want 400 %q", resp.StatusCode, body, want)
				}
			})
		}
	}
}
