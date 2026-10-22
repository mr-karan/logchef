package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// Regression coverage for /logchefql/translate: when the compiler rejects the
// time range, the handler must return that error like /logchefql/query does,
// not a 200 that silently omits full_sql.
//
// fakeTranslateStore is a store.Store stub implementing only GetSource and
// TeamHasSource (for the global admin that requireTeamHasSource sees); every
// other method panics via the nil-embedded interface, which is fine because
// the translate path under test never reaches them.
type fakeTranslateStore struct {
	store.Store
	source *models.Source
}

func (f *fakeTranslateStore) GetSource(ctx context.Context, id models.SourceID) (*models.Source, error) {
	return f.source, nil
}

func (f *fakeTranslateStore) TeamHasSource(context.Context, models.TeamID, models.SourceID) (bool, error) {
	return true, nil
}

// fakeClickHouseCompiler is a minimal datasource.Provider stub. It fails a
// time-bounded compile only with the artificial timeRangeErr a test injects;
// it does not model how the real compiler parses times.
type fakeClickHouseCompiler struct {
	datasource.Provider
	timeRangeErr error
}

func (f *fakeClickHouseCompiler) Type() models.SourceType { return models.SourceTypeClickHouse }
func (f *fakeClickHouseCompiler) Describe() datasource.Descriptor {
	return datasource.Descriptor{
		QueryLanguages:        []models.QueryLanguage{models.QueryLanguageLogchefQL, models.QueryLanguageClickHouseSQL},
		LogchefQLNeedsColumns: true,
	}
}
func (f *fakeClickHouseCompiler) CheckSourceConnectionStatus(ctx context.Context, source *models.Source) bool {
	return true
}
func (f *fakeClickHouseCompiler) PopulateSourceDetails(ctx context.Context, source *models.Source) error {
	return nil
}

func (f *fakeClickHouseCompiler) InspectSource(context.Context, *models.Source) (*datasource.SourceInspection, error) {
	return &datasource.SourceInspection{Schema: &datasource.SourceSchemaInspection{
		Fields: []datasource.SourceSchemaField{{Name: "level", Type: "String"}},
	}}, nil
}

// The inspection above supplies the schema required by the ClickHouse compile
// path. This fake has no separate schema endpoint because inspection is cached.
func (f *fakeClickHouseCompiler) GetSourceSchema(context.Context, *models.Source) ([]models.ColumnInfo, error) {
	return nil, fmt.Errorf("schema unavailable")
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
	if f.timeRangeErr != nil {
		return compiled, f.timeRangeErr
	}
	compiled.Query = fmt.Sprintf("SELECT * FROM logs WHERE 1=1 AND timestamp BETWEEN '%s' AND '%s'", req.StartTime, req.EndTime)
	return compiled, nil
}

func newTranslateTestApp(timeRangeErr error) *fiber.App {
	source := &models.Source{ID: 1, SourceType: models.SourceTypeClickHouse}

	db := &fakeTranslateStore{source: source}
	svc := datasource.NewService(db, slog.Default())
	svc.Register(&fakeClickHouseCompiler{timeRangeErr: timeRangeErr})

	s := &Server{datasources: svc, sqlite: db, log: slog.Default()}

	app := fiber.New()
	admin := &models.User{ID: 1, Role: models.UserRoleAdmin}
	withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/logchefql/translate", admin, s.handleLogchefQLTranslate)
	return app
}

func postTranslate(t *testing.T, app *fiber.App, body map[string]any) (status int, parsed map[string]any) {
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
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, parsed
}

func TestHandleLogchefQLTranslatePropagatesTimeRangeCompileError(t *testing.T) {
	t.Parallel()

	app := newTranslateTestApp(errors.New("artificial time range failure"))
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
	if body["message"] != "artificial time range failure" {
		t.Errorf("message = %v, want the compiler error", body["message"])
	}
}

func TestHandleLogchefQLTranslateReturnsFullSQLWithTimeRange(t *testing.T) {
	t.Parallel()

	app := newTranslateTestApp(nil)
	status, body := postTranslate(t, app, map[string]any{
		"query":      `level="error"`,
		"start_time": "2026-07-10T00:00:00Z",
		"end_time":   "2026-07-10T01:00:00Z",
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
// must not regress from the error path above.
func TestHandleLogchefQLTranslateWithoutTimeParamsOmitsFullSQL(t *testing.T) {
	t.Parallel()

	app := newTranslateTestApp(nil)
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
