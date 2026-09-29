package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/cache"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// Requests run through the actual handlers, datasource service, SQLite metadata
// store, and result cache. The provider supplies controlled query failures.
type failingDashboardQueryProvider struct {
	fakeClickHouseCompiler
	sourceType models.SourceType
	err        error
	partial    bool
	executions atomic.Int32
	release    chan struct{}
}

func (p *failingDashboardQueryProvider) Type() models.SourceType { return p.sourceType }

func (p *failingDashboardQueryProvider) QueryLogs(context.Context, *models.Source, datasource.QueryRequest) (*models.QueryResult, error) {
	p.executions.Add(1)
	<-p.release
	return nil, p.err
}

func (p *failingDashboardQueryProvider) QueryLogsStream(_ context.Context, _ *models.Source, _ datasource.QueryRequest, w datasource.StreamWriter) (models.QueryStats, error) {
	p.executions.Add(1)
	<-p.release
	if p.partial {
		if err := w.Begin([]models.ColumnInfo{{Name: "message", Type: "String"}}); err != nil {
			return models.QueryStats{}, err
		}
		if err := w.WriteRow(map[string]any{"message": "first row"}); err != nil {
			return models.QueryStats{}, err
		}
	}
	return models.QueryStats{}, p.err
}

func TestDashboardCacheQueryEndpointFailures(t *testing.T) {
	for _, endpoint := range []string{"logs", "logchefql"} {
		for _, sourceType := range []models.SourceType{models.SourceTypeClickHouse, models.SourceTypeVictoriaLogs} {
			for _, failure := range []struct {
				name    string
				err     error
				partial bool
			}{
				{"database", errors.New("database query failed"), false},
				{"canceled", fmt.Errorf("query: %w", context.Canceled), false},
				{"deadline", fmt.Errorf("query: %w", context.DeadlineExceeded), false},
				{"unsupported", datasource.ErrOperationNotSupported, false},
				{"source missing", models.ErrNotFound, false},
				{"validation", &datasource.ValidationError{Field: "query", Message: "invalid"}, false},
				{"partial rows", errors.New("database stream interrupted"), true},
			} {
				if failure.partial && sourceType != models.SourceTypeClickHouse {
					continue
				}
				t.Run(endpoint+"/"+string(sourceType)+"/"+failure.name, func(t *testing.T) {
					s := newDashboardTestServer(t)
					s.config = &config.Config{}
					s.config.Query.DefaultPreviewLimit = 100
					s.config.Query.MaxPreviewLimit = 1000
					s.config.Query.DefaultTimeoutSeconds = 10
					s.config.DashboardCache.MaxTTL = time.Minute
					s.config.DashboardCache.MaxEntryBytes = 4096
					s.dashCache = cache.New(cache.Config{Enabled: true, MaxEntryBytes: 4096, MaxBytes: 8192})
					t.Cleanup(s.dashCache.Close)
					provider := &failingDashboardQueryProvider{
						sourceType: sourceType, err: failure.err, partial: failure.partial, release: make(chan struct{}),
					}
					s.datasources = datasource.NewService(s.sqlite, s.log)
					s.datasources.Register(provider)
					source := &models.Source{Name: "dashboard-cache-test", SourceType: sourceType}
					if sourceType == models.SourceTypeVictoriaLogs {
						source.ConnectionConfig = json.RawMessage(`{"url":"http://localhost:9428"}`)
					}
					if err := s.sqlite.CreateSource(t.Context(), source); err != nil {
						t.Fatal(err)
					}
					app := fiber.New()
					handler := s.handleQueryLogs
					if endpoint == "logchefql" {
						handler = s.handleLogchefQLQuery
					}
					withUser(app, http.MethodPost, "/teams/:teamID/sources/:sourceID/query", &models.User{ID: 92001}, handler)
					path := fmt.Sprintf("/teams/1/sources/%d/query", source.ID)
					body := `{"query_text":"SELECT * FROM logs","query":"level=\"error\"","start_time":"2026-09-01 00:00:00","end_time":"2026-09-01 01:00:00"`
					if endpoint == "logs" {
						body = `{"query_text":"SELECT * FROM logs"`
					}
					type result struct {
						status int
						body   map[string]any
					}
					request := func(cached bool) result {
						t.Helper()
						payload := body
						if cached {
							payload += `,"cache":{"scope":"dashboard","ttl_seconds":30}`
						}
						req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload+"}"))
						req.Header.Set("Content-Type", "application/json")
						resp, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
						if err != nil {
							t.Error(err)
							return result{}
						}
						defer resp.Body.Close()
						if cached && resp.Header.Get("X-Logchef-Cache") != "BYPASS" {
							t.Errorf("cache header=%q, want BYPASS", resp.Header.Get("X-Logchef-Cache"))
						}
						var parsed map[string]any
						if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
							t.Error(err)
						}
						if data, ok := parsed["data"].(map[string]any); ok {
							delete(data, "query_id")
						}
						return result{status: resp.StatusCode, body: parsed}
					}
					close(provider.release)
					// A fill is admitted under the dashboard class, capped per
					// user; the cache's fill semaphore is the global bound.
					s.config.DashboardCache.MaxConcurrentFills = 1
					queryID, err := queryTracker.StartQuery(QueryClassDashboard, 92001, source.ID, 1, "occupied", func() {}, 0, 0)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { queryTracker.RemoveQuery(queryID) })
					rejected := request(true)
					queryTracker.RemoveQuery(queryID)
					if rejected.status != http.StatusTooManyRequests || rejected.body["error_type"] != string(models.ValidationErrorType) {
						t.Fatalf("admission response=%#v", rejected)
					}
					if got := provider.executions.Load(); got != 0 {
						t.Fatalf("saturated limit executed %d queries", got)
					}
					response := request(true)
					if got := provider.executions.Load(); got != 1 {
						t.Fatalf("cached failure executed %d times, want 1", got)
					}
					uncached := request(false)
					if got := provider.executions.Load(); got != 2 {
						t.Fatalf("uncached reference executed %d total queries, want 2", got)
					}
					if !reflect.DeepEqual(response, uncached) {
						t.Fatalf("cached response=%#v; uncached=%#v", response, uncached)
					}
					wantStatus := http.StatusInternalServerError
					wantErrorType := models.DatabaseErrorType
					switch {
					case sourceType == models.SourceTypeClickHouse:
						wantStatus = http.StatusOK
					case failure.name == "canceled" || failure.name == "deadline":
						// A caller that went away is not a server failure.
						wantStatus = http.StatusRequestTimeout
						wantErrorType = models.ExternalServiceErrorType
					case failure.name == "unsupported" || (endpoint == "logs" && failure.name == "validation"):
						wantStatus = http.StatusBadRequest
						wantErrorType = models.ValidationErrorType
					case endpoint == "logs" && failure.name == "source missing":
						wantStatus = http.StatusNotFound
						wantErrorType = models.NotFoundErrorType
					}
					if uncached.status != wantStatus {
						t.Fatalf("response status=%d, want %d", uncached.status, wantStatus)
					}
					if !failure.partial && uncached.body["status"] != "error" {
						t.Fatalf("missing error envelope: %v", uncached.body)
					}
					if !failure.partial && uncached.body["error_type"] != string(wantErrorType) {
						t.Fatalf("error type=%v, want %s", uncached.body["error_type"], wantErrorType)
					}
					if failure.partial {
						data, ok := uncached.body["data"].(map[string]any)
						if !ok || data["error"] != failure.err.Error() {
							t.Fatalf("missing partial-stream error: %v", uncached.body)
						}
						logsKey := "data"
						if endpoint == "logchefql" {
							logsKey = "logs"
						}
						rows, ok := data[logsKey].([]any)
						if !ok || len(rows) != 1 {
							t.Fatalf("partial rows lost: %v", data)
						}
					}
					// Errors must not be retained as successful cache entries.
					request(true)
					if got := provider.executions.Load(); got != 3 {
						t.Fatalf("later request executed %d total queries, want 3", got)
					}
				})
			}
		}
	}
}
