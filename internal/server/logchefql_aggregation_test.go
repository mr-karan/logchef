package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/cache"
	"github.com/mr-karan/logchef/internal/clickhouse"
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
	s.queryRecords = make(chan queryRecord, 16)
	s.datasources = datasource.NewService(s.store, s.log)
	s.datasources.Register(provider)
	source := &models.Source{Name: "aggregation-" + string(sourceType), SourceType: sourceType, MetaTSField: "_time"}
	switch sourceType {
	case models.SourceTypeClickHouse:
		// A ClickHouse test source needs no connection.
	case models.SourceTypeVictoriaLogs:
		source.ConnectionConfig = json.RawMessage(`{"url":"http://localhost:9428"}`)
	case models.SourceTypeLoki:
		source.ConnectionConfig = json.RawMessage(`{"base_url":"http://localhost:3100","selector":"{app=~\".+\"}"}`)
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

// The paths that opt in compile and run the pipe on VictoriaLogs, and the
// log-row paths answer AGGREGATION_NOT_ALLOWED.
func TestLogchefQLAggregationPaths(t *testing.T) {
	s, source := newAggregationTestServer(t, victorialogs.NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil))), models.SourceTypeVictoriaLogs)
	var gotQuery string
	vl := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.FormValue("query")
		if r.URL.Path == "/select/logsql/field_values" {
			_, _ = io.WriteString(w, `{"values":[{"value":"api","hits":3}]}`)
			return
		}
		// VictoriaLogs leaves out a key whose value is empty.
		_, _ = io.WriteString(w, "{\"__lc_v0\":\"3\",\"service\":\"api\"}\n{\"__lc_v0\":\"1\"}\n")
	}))
	t.Cleanup(vl.Close)
	source.ConnectionConfig = json.RawMessage(`{"base_url":"` + vl.URL + `"}`)
	if err := s.store.UpdateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	user, src := authorizeTestSource(t, s.store, source.ID)
	base := fmt.Sprintf("/teams/%d/sources/%d", src.TeamID(), source.ID)
	withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/logchefql/query", user, s.handleLogchefQLQuery)
	withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/logchefql/translate", user, s.handleLogchefQLTranslate)
	withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/logs/windowed", user, s.handleWindowedQuery)
	asUser := func(c fiber.Ctx) error {
		c.Locals("user", user)
		c.Locals("auth_method", "session")
		return c.Next()
	}
	app.Get("/teams/:teamID/sources/:sourceID/fields/values", asUser, s.requireTeamHasSource(models.TokenScopeLogsRead), s.handleGetAllFieldValues)
	app.Get("/teams/:teamID/sources/:sourceID/fields/:fieldName/values", asUser, s.requireTeamHasSource(models.TokenScopeLogsRead), s.handleGetFieldValues)
	app.Post("/validate", s.handleLogchefQLValidate)
	timeRange := `"start_time":"2026-09-01T00:00:00Z","end_time":"2026-09-01T01:00:00Z","timezone":"UTC"`

	t.Run("validate accepts the query", func(t *testing.T) {
		status, _, body := postJSON(t, app, "/validate", `{"query":"x=1 | count()"}`)
		data, _ := body["data"].(map[string]any)
		if status != http.StatusOK || data["valid"] != true {
			t.Fatalf("status=%d body=%v, want valid", status, body)
		}
	})

	t.Run("query runs the aggregation", func(t *testing.T) {
		status, _, body := postJSON(t, app, base+"/logchefql/query", `{"query":"x=1 | count() by service",`+timeRange+`}`)
		if status != http.StatusOK {
			t.Fatalf("status=%d body=%v", status, body)
		}
		if want := "x:=1 | stats by (service) count() as __lc_v0 | sort by (__lc_v0 desc, service)"; gotQuery != want {
			t.Fatalf("VictoriaLogs query=%q, want %q", gotQuery, want)
		}
		data, _ := body["data"].(map[string]any)
		wantLogs := []any{
			map[string]any{"service": "api", "count": "3"},
			map[string]any{"service": "", "count": "1"},
		}
		wantColumns := []any{
			map[string]any{"name": "service", "type": "String"},
			map[string]any{"name": "count", "type": "UInt64"},
		}
		if !reflect.DeepEqual(data["logs"], wantLogs) || !reflect.DeepEqual(data["columns"], wantColumns) {
			t.Fatalf("logs=%v columns=%v, want %v %v", data["logs"], data["columns"], wantLogs, wantColumns)
		}
		if agg, _ := data["aggregation"].(map[string]any); agg["shape"] != "grouped" {
			t.Fatalf("aggregation=%v, want grouped", data["aggregation"])
		}
	})

	t.Run("query rejects an interval outside the table", func(t *testing.T) {
		status, _, body := postJSON(t, app, base+"/logchefql/query", `{"query":"x=1","interval":"7m",`+timeRange+`}`)
		message, _ := body["message"].(string)
		if status != http.StatusBadRequest || !strings.HasPrefix(message, `invalid interval "7m": use one of 1s, 5s,`) {
			t.Fatalf("status=%d body=%v", status, body)
		}
	})

	t.Run("query rejects too many buckets as ClickHouse does", func(t *testing.T) {
		// 90 days at 1h gives 2160 buckets.
		wideRange := `"start_time":"2026-08-17T00:00:00Z","end_time":"2026-11-15T00:00:00Z","timezone":"UTC"`
		status, _, body := postJSON(t, app, base+"/logchefql/query", `{"query":"x=1 | count() by time(1h)",`+wideRange+`}`)
		want := "1h over this range gives 2160 buckets. The maximum is 1000. Use 3h or longer"
		if status != http.StatusBadRequest || body["message"] != want {
			t.Fatalf("status=%d body=%v, want 400 %q", status, body, want)
		}
	})

	t.Run("translate compiles", func(t *testing.T) {
		status, _, body := postJSON(t, app, base+"/logchefql/translate", `{"query":"x=1 | count()"}`)
		data, _ := body["data"].(map[string]any)
		if status != http.StatusOK || data["valid"] != true || data["generated_query"] != "x:=1 | stats count() as __lc_v0" {
			t.Fatalf("status=%d body=%v", status, body)
		}
		if agg, _ := data["aggregation"].(map[string]any); agg["shape"] != "scalar" {
			t.Fatalf("aggregation=%v, want scalar", data["aggregation"])
		}
	})

	t.Run("translate rejects time with no interval", func(t *testing.T) {
		status, _, body := postJSON(t, app, base+"/logchefql/translate", `{"query":"x=1 | count() by time"}`)
		data, _ := body["data"].(map[string]any)
		parseErr, _ := data["error"].(map[string]any)
		if status != http.StatusOK || data["valid"] != false || parseErr["message"] != "this query buckets by time, so the request needs an interval" {
			t.Fatalf("status=%d body=%v", status, body)
		}
		if _, present := data["aggregation"]; present {
			t.Fatalf("aggregation present on a failed compile: %v", data)
		}
	})

	// The field sidebar stays open on an aggregated query, so its values
	// come from the filter in front of the pipe.
	fieldValues := func(t *testing.T, path, query string) (int, map[string]any) {
		t.Helper()
		gotQuery = ""
		params := url.Values{"type": {"String"}, "start_time": {"2026-09-01T00:00:00Z"}, "end_time": {"2026-09-01T01:00:00Z"}, "query_language": {"logchefql"}, "query": {query}}
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, base+path+"?"+params.Encode(), http.NoBody))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, body
	}
	for _, path := range []string{"/fields/service/values", "/fields/values"} {
		t.Run("field values "+path+" use the filter of an aggregation", func(t *testing.T) {
			status, body := fieldValues(t, path, "x=1 | count() by service")
			if status != http.StatusOK || !strings.HasPrefix(gotQuery, "x:=1") || strings.Contains(gotQuery, "stats") {
				t.Fatalf("status=%d body=%v VictoriaLogs query=%q, want 200 over the filter", status, body, gotQuery)
			}
		})
	}
	t.Run("field values of a bare aggregation read every log", func(t *testing.T) {
		status, body := fieldValues(t, "/fields/service/values", "| count() by service")
		if status != http.StatusOK || gotQuery != "*" {
			t.Fatalf("status=%d body=%v VictoriaLogs query=%q, want 200 over *", status, body, gotQuery)
		}
	})
	t.Run("field values refuse a malformed filter as a bad request", func(t *testing.T) {
		status, body := fieldValues(t, "/fields/service/values", "x=")
		if status != http.StatusBadRequest || gotQuery != "" {
			t.Fatalf("status=%d body=%v VictoriaLogs query=%q, want 400 and no backend request", status, body, gotQuery)
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

// ClickHouse reads a field-values filter as LogchefQL whether or not the
// request names query_language, so a request without it gets the filter of an
// aggregation too, and a filter that does not parse is the caller's error.
func TestClickHouseFieldValuesWithoutQueryLanguage(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })
	s, source := newAggregationTestServer(t, datasource.NewClickHouseProvider(manager, log), models.SourceTypeClickHouse)
	user, src := authorizeTestSource(t, s.store, source.ID)
	asUser := func(c fiber.Ctx) error {
		c.Locals("user", user)
		c.Locals("auth_method", "session")
		return c.Next()
	}
	app := fiber.New()
	app.Get("/teams/:teamID/sources/:sourceID/fields/values", asUser, s.requireTeamHasSource(models.TokenScopeLogsRead), s.handleGetAllFieldValues)
	app.Get("/teams/:teamID/sources/:sourceID/fields/:fieldName/values", asUser, s.requireTeamHasSource(models.TokenScopeLogsRead), s.handleGetFieldValues)
	base := fmt.Sprintf("/teams/%d/sources/%d", src.TeamID(), source.ID)
	end := time.Now().UTC()
	fieldValues := func(t *testing.T, path, query string) (int, map[string]any) {
		t.Helper()
		params := url.Values{"type": {"String"}, "start_time": {end.Add(-time.Hour).Format(time.RFC3339)}, "end_time": {end.Format(time.RFC3339)}, "query": {query}}
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, base+path+"?"+params.Encode(), http.NoBody), fiber.TestConfig{Timeout: 20 * time.Second, FailOnTimeout: true})
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, body
	}

	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to read field values from ClickHouse")
	}
	admin, err := ch.Open(&ch.Options{Addr: []string{addr}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	view := fmt.Sprintf("logchef_field_values_%d", time.Now().UnixNano())
	if err := admin.Exec(t.Context(), "CREATE VIEW default."+view+` AS SELECT now64(3) - INTERVAL 1 MINUTE AS timestamp,
		toLowCardinality(['error', 'error', 'info'][number + 1]) AS level,
		toLowCardinality(['api', 'api', 'web'][number + 1]) AS service FROM numbers(3)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Exec(context.Background(), "DROP VIEW default."+view) })
	source.MetaTSField = "timestamp"
	source.Connection = models.ConnectionInfo{Host: addr, Database: "default", TableName: view}
	if err := s.store.UpdateSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if err := manager.AddSource(t.Context(), source); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/fields/service/values", "/fields/values"} {
		t.Run("malformed filter on "+path+" is a bad request", func(t *testing.T) {
			status, body := fieldValues(t, path, "level=")
			if want := "Invalid request: query: invalid filter: expected a value after `level=`"; status != http.StatusBadRequest || body["message"] != want {
				t.Fatalf("status=%d body=%v, want 400 %q", status, body, want)
			}
		})
	}

	wantService := map[string]any{
		"field_name": "service", "field_type": "LowCardinality(String)", "is_low_cardinality": true,
		"values": []any{map[string]any{"value": "api", "count": float64(2)}}, "total_distinct": float64(1),
	}
	t.Run("one field reads the filter of an aggregation", func(t *testing.T) {
		status, body := fieldValues(t, "/fields/service/values", "level=error | count() by service")
		data, _ := body["data"].(map[string]any)
		if status != http.StatusOK || !reflect.DeepEqual(data["values"], wantService["values"]) {
			t.Fatalf("status=%d body=%v, want 200 with api counted twice", status, body)
		}
	})
	t.Run("every field reads the filter of an aggregation", func(t *testing.T) {
		status, body := fieldValues(t, "/fields/values", "level=error | count() by service")
		data, _ := body["data"].(map[string]any)
		if status != http.StatusOK || !reflect.DeepEqual(data["service"], wantService) {
			t.Fatalf("status=%d body=%v, want 200 with service %v", status, body, wantService)
		}
	})
}
