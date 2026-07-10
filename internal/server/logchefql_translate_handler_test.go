package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// Regression coverage for the /logchefql/translate bug where an RFC3339
// start/end pair against a ClickHouse source silently omitted full_sql from an
// otherwise-200 response instead of erroring like /logchefql/query does.
//
// fakeTranslateStore is a store.Store stub implementing only GetSource; every
// other method panics via the nil-embedded interface, which is fine because
// the translate path under test never reaches them.
type fakeTranslateStore struct {
	store.Store
	source *models.Source
}

func (f *fakeTranslateStore) GetSource(ctx context.Context, id models.SourceID) (*models.Source, error) {
	return f.source, nil
}

// fakeClickHouseCompiler is a minimal datasource.Provider stub that simulates
// the real ClickHouse compiler's time-format sensitivity (validateTimeFormat
// in internal/logchefql requires "YYYY-MM-DD HH:MM:SS") without needing a live
// ClickHouse connection.
type fakeClickHouseCompiler struct {
	datasource.Provider
}

var sqlTimeFormatRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$`)

func (f *fakeClickHouseCompiler) Type() models.SourceType { return models.SourceTypeClickHouse }
func (f *fakeClickHouseCompiler) Capabilities() []datasource.Capability { return nil }
func (f *fakeClickHouseCompiler) SupportedQueryLanguages() []models.QueryLanguage {
	return []models.QueryLanguage{models.QueryLanguageLogchefQL, models.QueryLanguageClickHouseSQL}
}
func (f *fakeClickHouseCompiler) SupportedSavedQueryEditorModes() []models.SavedQueryEditorMode {
	return nil
}
func (f *fakeClickHouseCompiler) SupportedAlertEditorModes() []models.AlertEditorMode { return nil }
func (f *fakeClickHouseCompiler) CheckSourceConnectionStatus(ctx context.Context, source *models.Source) bool {
	return true
}
func (f *fakeClickHouseCompiler) PopulateSourceDetails(ctx context.Context, source *models.Source) error {
	return nil
}

func (f *fakeClickHouseCompiler) CompileLogchefQL(ctx context.Context, source *models.Source, req datasource.LogchefQLCompileRequest) (*datasource.CompiledLogchefQL, error) {
	compiled := &datasource.CompiledLogchefQL{
		Language:   models.QueryLanguageClickHouseSQL,
		Valid:      true,
		FilterOnly: "1=1",
		Query:      "1=1",
	}
	if req.StartTime == "" || req.EndTime == "" || req.Timezone == "" {
		return compiled, nil
	}
	// Mirrors ClickHouseProvider.CompileLogchefQL -> logchefql.BuildFullQuery:
	// building full_sql requires the SQL time format, same as /query.
	if !sqlTimeFormatRegex.MatchString(req.StartTime) || !sqlTimeFormatRegex.MatchString(req.EndTime) {
		return compiled, fmt.Errorf("invalid time format: expected 'YYYY-MM-DD HH:MM:SS', got %q/%q", req.StartTime, req.EndTime)
	}
	compiled.Query = fmt.Sprintf("SELECT * FROM logs WHERE 1=1 AND timestamp BETWEEN '%s' AND '%s'", req.StartTime, req.EndTime)
	return compiled, nil
}

func newTranslateTestApp() *fiber.App {
	source := &models.Source{ID: 1, SourceType: models.SourceTypeClickHouse}

	svc := datasource.NewService(&fakeTranslateStore{source: source}, slog.Default())
	svc.Register(&fakeClickHouseCompiler{})

	s := &Server{datasources: svc, log: slog.Default()}

	app := fiber.New()
	app.Post("/teams/:teamID/sources/:sourceID/logchefql/translate", s.handleLogchefQLTranslate)
	return app
}

func postTranslate(t *testing.T, app *fiber.App, body map[string]any) (int, map[string]any) {
	t.Helper()

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/teams/1/sources/1/logchefql/translate", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}

	var parsed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, parsed
}

func TestHandleLogchefQLTranslateRejectsRFC3339Times(t *testing.T) {
	t.Parallel()

	app := newTranslateTestApp()
	status, body := postTranslate(t, app, map[string]any{
		"query":      `level="error"`,
		"start_time": "2026-07-10T00:00:00Z",
		"end_time":   "2026-07-10T01:00:00Z",
		"timezone":   "UTC",
	})

	if status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %v)", status, body)
	}
	if body["status"] != "error" {
		t.Errorf("status field = %v, want %q", body["status"], "error")
	}
	message, _ := body["message"].(string)
	if message == "" {
		t.Errorf("expected a non-empty error message, got body: %v", body)
	}
}

func TestHandleLogchefQLTranslateAcceptsSQLFormatTimes(t *testing.T) {
	t.Parallel()

	app := newTranslateTestApp()
	status, body := postTranslate(t, app, map[string]any{
		"query":      `level="error"`,
		"start_time": "2026-07-10 00:00:00",
		"end_time":   "2026-07-10 01:00:00",
		"timezone":   "UTC",
	})

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %v)", status, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object in response, got: %v", body)
	}
	fullSQL, _ := data["full_sql"].(string)
	if fullSQL == "" {
		t.Fatalf("expected non-empty full_sql, got data: %v", data)
	}
}

// Without time params at all, translate should still succeed with SQL
// (WHERE-only) but no full_sql — this is the documented preview-only mode and
// must not regress from the RFC3339 fix above.
func TestHandleLogchefQLTranslateWithoutTimeParamsOmitsFullSQL(t *testing.T) {
	t.Parallel()

	app := newTranslateTestApp()
	status, body := postTranslate(t, app, map[string]any{
		"query": `level="error"`,
	})

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %v)", status, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object in response, got: %v", body)
	}
	if _, present := data["full_sql"]; present {
		t.Fatalf("expected full_sql to be omitted without time params, got data: %v", data)
	}
}
