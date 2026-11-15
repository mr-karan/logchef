package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/cache"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

const aggregationDenyMessage = "this feature needs log rows, and the query aggregates. Remove the pipe after the filter"

// aggregatingProvider compiles every aggregation to one fixed query text, so
// two aliases share the compiled query the way they share one LogsQL text.
// It records the aggregation that each run receives.
type aggregatingProvider struct {
	succeedingQueryProvider
	mu   sync.Mutex
	runs []*logchefql.AggregationInfo
}

func (p *aggregatingProvider) CompileLogchefQL(_ context.Context, _ *models.Source, req datasource.LogchefQLCompileRequest) (*datasource.CompiledLogchefQL, error) {
	pq, err := logchefql.ParseLogchefQL(req.Query)
	if err != nil {
		return nil, err
	}
	agg := logchefql.AggregationOf(logchefql.ConvertToAST(pq))
	if agg == nil || !req.AllowAggregation {
		return nil, fmt.Errorf("query %q allow=%v: want an allowed aggregation", req.Query, req.AllowAggregation)
	}
	language := models.QueryLanguageClickHouseSQL
	if p.sourceType == models.SourceTypeVictoriaLogs {
		language = models.QueryLanguageLogsQL
	}
	return &datasource.CompiledLogchefQL{
		Language:    language,
		Valid:       true,
		Query:       "compiled aggregation",
		FilterOnly:  "",
		Aggregation: agg.Info(req.Interval),
	}, nil
}

func (p *aggregatingProvider) record(req datasource.QueryRequest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.runs = append(p.runs, req.Aggregation)
}

func (p *aggregatingProvider) QueryLogs(ctx context.Context, source *models.Source, req datasource.QueryRequest) (*models.QueryResult, error) {
	p.record(req)
	return p.succeedingQueryProvider.QueryLogs(ctx, source, req)
}

func (p *aggregatingProvider) QueryLogsStream(ctx context.Context, source *models.Source, req datasource.QueryRequest, w datasource.StreamWriter) (models.QueryStats, error) {
	p.record(req)
	return p.succeedingQueryProvider.QueryLogsStream(ctx, source, req, w)
}

func newAggregationTestServer(t *testing.T, provider datasource.Provider, sourceType models.SourceType) (*Server, *models.Source) {
	t.Helper()
	s := newDashboardTestServer(t)
	s.config = &config.Config{}
	s.config.Query.DefaultPreviewLimit = 100
	s.config.Query.MaxPreviewLimit = 1000
	s.config.Query.DefaultTimeoutSeconds = 10
	s.config.DashboardCache.MaxTTL = time.Minute
	s.config.DashboardCache.MaxEntryBytes = 4096
	s.config.DashboardCache.MaxConcurrentFills = 1
	s.dashCache = cache.New(cache.Config{Enabled: true, MaxEntryBytes: 4096, MaxBytes: 16384})
	t.Cleanup(s.dashCache.Close)
	s.historyQueue = make(chan *models.QueryHistory, 16)
	s.datasources = datasource.NewService(s.store, s.log)
	s.datasources.Register(provider)
	source := &models.Source{Name: "aggregation-" + string(sourceType), SourceType: sourceType, MetaTSField: "_time"}
	if sourceType == models.SourceTypeVictoriaLogs {
		source.ConnectionConfig = json.RawMessage(`{"url":"http://localhost:9428"}`)
	}
	if err := s.store.CreateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	return s, source
}

func postJSON(t *testing.T, app *fiber.App, path, body string) (status int, header http.Header, parsed map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, parsed
}

// The aggregation object reaches the client from a streamed response
// (ClickHouse), a buffered one (VictoriaLogs) and a cache hit, and two aliases
// that compile to one query text get two cache entries.
func TestLogchefQLQueryReturnsAggregation(t *testing.T) {
	for _, sourceType := range []models.SourceType{models.SourceTypeClickHouse, models.SourceTypeVictoriaLogs} {
		t.Run(string(sourceType), func(t *testing.T) {
			provider := &aggregatingProvider{succeedingQueryProvider: succeedingQueryProvider{sourceType: sourceType}}
			s, source := newAggregationTestServer(t, provider, sourceType)
			app := fiber.New()
			user, src := authorizeTestSource(t, s.store, source.ID)
			withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/logchefql/query", user, s.handleLogchefQLQuery)
			path := fmt.Sprintf("/teams/%d/sources/%d/logchefql/query", src.TeamID(), source.ID)

			query := func(alias string, cached bool) (string, any) {
				t.Helper()
				body := fmt.Sprintf(`{"query":"level=error | count() as %s by time","interval":"5m","start_time":"2026-09-01T00:00:00Z","end_time":"2026-09-01T01:00:00Z","timezone":"UTC"`, alias)
				if cached {
					body += `,"cache":{"scope":"dashboard","ttl_seconds":30}`
				}
				status, header, parsed := postJSON(t, app, path, body+"}")
				if status != http.StatusOK {
					t.Fatalf("status=%d body=%v", status, parsed)
				}
				data, _ := parsed["data"].(map[string]any)
				return header.Get("X-Logchef-Cache"), data["aggregation"]
			}
			want := func(alias string) any {
				return map[string]any{
					"shape": "timeseries", "interval": "5m", "limit": float64(0),
					"columns": []any{
						map[string]any{"name": "time", "role": "time", "type": "DateTime"},
						map[string]any{"name": alias, "role": "value", "type": "UInt64"},
					},
				}
			}

			if _, got := query("errors", false); !reflect.DeepEqual(got, want("errors")) {
				t.Fatalf("uncached aggregation=%v, want %v", got, want("errors"))
			}
			for _, step := range []struct{ alias, cache string }{
				{"errors", "MISS"}, {"errors", "HIT"}, {"failures", "MISS"}, {"failures", "HIT"},
			} {
				status, got := query(step.alias, true)
				if status != step.cache {
					t.Fatalf("alias %s: X-Logchef-Cache=%q, want %q", step.alias, status, step.cache)
				}
				if !reflect.DeepEqual(got, want(step.alias)) {
					t.Fatalf("alias %s (%s): aggregation=%v, want %v", step.alias, step.cache, got, want(step.alias))
				}
			}

			provider.mu.Lock()
			defer provider.mu.Unlock()
			if len(provider.runs) != 3 {
				t.Fatalf("provider ran %d queries, want 3 (one uncached, two cache fills)", len(provider.runs))
			}
			for i, info := range provider.runs {
				if info == nil || info.Shape != logchefql.ShapeTimeseries || len(info.Columns) != 2 {
					t.Fatalf("run %d received aggregation %+v, want the compiled description", i, info)
				}
			}
		})
	}
}

// A provider that cannot compile the pipe answers AGGREGATION_NOT_SUPPORTED on
// the paths that opt in, and the log-row paths answer AGGREGATION_NOT_ALLOWED.
func TestLogchefQLAggregationPaths(t *testing.T) {
	s, source := newAggregationTestServer(t, victorialogs.NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil))), models.SourceTypeVictoriaLogs)
	app := fiber.New()
	user, src := authorizeTestSource(t, s.store, source.ID)
	base := fmt.Sprintf("/teams/%d/sources/%d", src.TeamID(), source.ID)
	withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/logchefql/query", user, s.handleLogchefQLQuery)
	withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/logchefql/translate", user, s.handleLogchefQLTranslate)
	withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/logs/windowed", user, s.handleWindowedQuery)
	app.Post("/validate", s.handleLogchefQLValidate)
	timeRange := `"start_time":"2026-09-01T00:00:00Z","end_time":"2026-09-01T01:00:00Z","timezone":"UTC"`

	t.Run("validate accepts the query", func(t *testing.T) {
		status, _, body := postJSON(t, app, "/validate", `{"query":"x=1 | count()"}`)
		data, _ := body["data"].(map[string]any)
		if status != http.StatusOK || data["valid"] != true {
			t.Fatalf("status=%d body=%v, want valid", status, body)
		}
	})

	t.Run("query answers not supported", func(t *testing.T) {
		status, _, body := postJSON(t, app, base+"/logchefql/query", `{"query":"x=1 | count()",`+timeRange+`}`)
		if status != http.StatusBadRequest || body["message"] != "aggregations are not supported for this source type" {
			t.Fatalf("status=%d body=%v", status, body)
		}
	})

	t.Run("query rejects an interval outside the table", func(t *testing.T) {
		status, _, body := postJSON(t, app, base+"/logchefql/query", `{"query":"x=1","interval":"7m",`+timeRange+`}`)
		message, _ := body["message"].(string)
		if status != http.StatusBadRequest || !strings.HasPrefix(message, `invalid interval "7m": use one of 1s, 5s,`) {
			t.Fatalf("status=%d body=%v", status, body)
		}
	})

	t.Run("translate answers not supported", func(t *testing.T) {
		status, _, body := postJSON(t, app, base+"/logchefql/translate", `{"query":"x=1 | count()"}`)
		data, _ := body["data"].(map[string]any)
		parseErr, _ := data["error"].(map[string]any)
		if status != http.StatusOK || data["valid"] != false || parseErr["code"] != logchefql.ErrAggregationNotSupported {
			t.Fatalf("status=%d body=%v", status, body)
		}
		if _, present := data["aggregation"]; present {
			t.Fatalf("aggregation present on a failed compile: %v", data)
		}
	})

	t.Run("windowed search denies", func(t *testing.T) {
		status, _, body := postJSON(t, app, base+"/logs/windowed", `{"query_language":"logchefql","query_text":"x=1 | count()",`+timeRange+`}`)
		if status != http.StatusBadRequest || body["message"] != aggregationDenyMessage {
			t.Fatalf("status=%d body=%v", status, body)
		}
	})

	t.Run("live tail denies", func(t *testing.T) {
		loaded, err := core.LoadSource(t.Context(), s.datasources, source.ID)
		if err != nil {
			t.Fatal(err)
		}
		tail := fiber.New()
		tail.Get("/tail", func(c fiber.Ctx) error {
			if _, _, ok := s.resolveTailQuery(c, loaded, source.ID); ok {
				t.Error("resolveTailQuery accepted an aggregation")
			}
			return nil
		})
		resp, err := tail.Test(httptest.NewRequest(http.MethodGet, "/tail?query=x%3D1%20%7C%20count()", http.NoBody))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusBadRequest || body["message"] != aggregationDenyMessage {
			t.Fatalf("status=%d body=%v", resp.StatusCode, body)
		}
	})
}
