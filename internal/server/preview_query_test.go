package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/cache"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// succeedingQueryProvider answers every query with one row, buffered or streamed.
type succeedingQueryProvider struct {
	fakeClickHouseCompiler
	sourceType models.SourceType
}

var (
	previewTestColumns  = []models.ColumnInfo{{Name: "message", Type: "String"}}
	previewTestRow      = map[string]any{"message": "first row"}
	previewTestStats    = models.QueryStats{ExecutionTimeMs: 7, RowsReturned: 1}
	previewTestWarnings = []models.QueryWarning{{Code: "LIMIT_APPLIED", Message: "limit applied"}}
)

func (p *succeedingQueryProvider) Type() models.SourceType { return p.sourceType }

func (p *succeedingQueryProvider) QueryLogs(context.Context, *models.Source, datasource.QueryRequest) (*models.QueryResult, error) {
	return &models.QueryResult{
		Logs:     []map[string]any{previewTestRow},
		Columns:  previewTestColumns,
		Stats:    previewTestStats,
		Warnings: previewTestWarnings,
	}, nil
}

func (p *succeedingQueryProvider) QueryLogsStream(_ context.Context, _ *models.Source, _ datasource.QueryRequest, w datasource.StreamWriter) (models.QueryStats, error) {
	w.SetWarnings(previewTestWarnings)
	if err := w.Begin(previewTestColumns); err != nil {
		return models.QueryStats{}, err
	}
	if err := w.WriteRow(previewTestRow); err != nil {
		return models.QueryStats{}, err
	}
	return previewTestStats, w.Finish(previewTestStats)
}

func TestPreviewQueryEndpointSuccessResponses(t *testing.T) {
	commonData := `"columns":[{"name":"message","type":"String"}],` +
		`"stats":{"execution_time_ms":7,"rows_read":0,"rows_returned":1},` +
		`"warnings":[{"code":"LIMIT_APPLIED","message":"limit applied"}]`
	generatedSQL := `SELECT * FROM logs WHERE 1=1 AND timestamp BETWEEN '2026-09-01 00:00:00' AND '2026-09-01 01:00:00'`
	endpoints := []struct {
		name        string
		body        string
		wantData    string
		wantHistory models.QueryHistory
	}{
		{
			name:        "logs",
			body:        `{"query_text":"SELECT * FROM logs"`,
			wantData:    `{"data":[{"message":"first row"}],` + commonData + `}`,
			wantHistory: models.QueryHistory{QueryText: "SELECT * FROM logs", QueryLanguage: models.QueryLanguageClickHouseSQL, DurationMs: 7, RowCount: 1},
		},
		{
			name: "logchefql",
			body: `{"query":"level=\"error\"","start_time":"2026-09-01 00:00:00","end_time":"2026-09-01 01:00:00","timezone":"UTC"`,
			wantData: `{"logs":[{"message":"first row"}],` + commonData + `,"conditions":null,"fields_used":null,` +
				`"generated_sql":` + jsonString(t, generatedSQL) + `,"generated_query":` + jsonString(t, generatedSQL) +
				`,"generated_query_language":"clickhouse-sql"}`,
			wantHistory: models.QueryHistory{QueryText: `level="error"`, QueryLanguage: models.QueryLanguageLogchefQL, DurationMs: 7, RowCount: 1},
		},
	}
	for _, endpoint := range endpoints {
		for _, sourceType := range []models.SourceType{models.SourceTypeClickHouse, models.SourceTypeVictoriaLogs} {
			t.Run(endpoint.name+"/"+string(sourceType), func(t *testing.T) {
				s := newDashboardTestServer(t)
				s.config = &config.Config{}
				s.config.Query.DefaultPreviewLimit = 100
				s.config.Query.MaxPreviewLimit = 1000
				s.config.Query.DefaultTimeoutSeconds = 10
				s.config.DashboardCache.MaxTTL = time.Minute
				s.config.DashboardCache.MaxEntryBytes = 4096
				s.config.DashboardCache.MaxConcurrentFills = 1
				s.dashCache = cache.New(cache.Config{Enabled: true, MaxEntryBytes: 4096, MaxBytes: 8192})
				t.Cleanup(s.dashCache.Close)
				s.historyQueue = make(chan *models.QueryHistory, 4)
				s.datasources = datasource.NewService(s.store, s.log)
				s.datasources.Register(&succeedingQueryProvider{sourceType: sourceType})
				source := &models.Source{Name: "preview-success", SourceType: sourceType}
				if sourceType == models.SourceTypeVictoriaLogs {
					source.ConnectionConfig = json.RawMessage(`{"url":"http://localhost:9428"}`)
				}
				if err := s.store.CreateSource(t.Context(), source); err != nil {
					t.Fatal(err)
				}
				app := fiber.New()
				handler := s.handleQueryLogs
				if endpoint.name == "logchefql" {
					handler = s.handleLogchefQLQuery
				}
				user, src := authorizeTestSource(t, s.store, source.ID)
				withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/query", user, handler)
				path := fmt.Sprintf("/teams/%d/sources/%d/query", src.TeamID(), source.ID)

				request := func(payload string) (http.Header, map[string]any) {
					t.Helper()
					req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload+"}"))
					req.Header.Set("Content-Type", "application/json")
					resp, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second, FailOnTimeout: true})
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						t.Fatalf("status=%d, want 200", resp.StatusCode)
					}
					var parsed map[string]any
					if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
						t.Fatal(err)
					}
					return resp.Header, parsed
				}
				var wantData map[string]any
				if err := json.Unmarshal([]byte(endpoint.wantData), &wantData); err != nil {
					t.Fatal(err)
				}
				checkBody := func(body map[string]any) string {
					t.Helper()
					data, ok := body["data"].(map[string]any)
					if body["status"] != "success" || !ok {
						t.Fatalf("body=%v, want a success envelope", body)
					}
					queryID, _ := data["query_id"].(string)
					if queryID == "" {
						t.Fatalf("query_id missing: %v", data)
					}
					delete(data, "query_id")
					if !reflect.DeepEqual(data, wantData) {
						t.Fatalf("data=%v\nwant %v", data, wantData)
					}
					return queryID
				}

				header, body := request(endpoint.body)
				queryID := checkBody(body)
				wantHeader := ""
				if sourceType == models.SourceTypeClickHouse {
					wantHeader = queryID
				}
				if got := header.Get("X-LogChef-Query-ID"); got != wantHeader {
					t.Fatalf("X-LogChef-Query-ID=%q, want %q", got, wantHeader)
				}
				select {
				case entry := <-s.historyQueue:
					want := endpoint.wantHistory
					want.UserID, want.TeamID, want.SourceID = user.ID, src.TeamID(), source.ID
					if !reflect.DeepEqual(*entry, want) {
						t.Fatalf("history=%+v, want %+v", *entry, want)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("query history was not recorded")
				}

				cached := endpoint.body + `,"cache":{"scope":"dashboard","ttl_seconds":30}`
				for _, wantCache := range []string{"MISS", "HIT"} {
					header, body := request(cached)
					checkBody(body)
					if got := header.Get("X-Logchef-Cache"); got != wantCache {
						t.Fatalf("X-Logchef-Cache=%q, want %q", got, wantCache)
					}
				}
				if len(s.historyQueue) != 0 {
					t.Fatal("a dashboard cache fill recorded query history")
				}
			})
		}
	}
}

func jsonString(t *testing.T, s string) string {
	t.Helper()
	encoded, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// sourceSchemaStore supplies transient schema fields that metadata stores do not persist.
type sourceSchemaStore struct {
	store.Store
	columns []models.ColumnInfo
}

func (s sourceSchemaStore) GetSource(ctx context.Context, id models.SourceID) (*models.Source, error) {
	source, err := s.Store.GetSource(ctx, id)
	if err == nil {
		source.Columns = s.columns
	}
	return source, err
}

type inferredColumnsQueryProvider struct{ succeedingQueryProvider }

func (p *inferredColumnsQueryProvider) QueryLogs(context.Context, *models.Source, datasource.QueryRequest) (*models.QueryResult, error) {
	return &models.QueryResult{Logs: []map[string]any{{"zebra": "z", "alpha": "a", "middle": "m", "extra": "e"}}}, nil
}

func TestPreviewQueryInferredColumnOrder(t *testing.T) {
	for _, endpoint := range []struct {
		name string
		body string
		want []models.ColumnInfo
	}{
		{"logs", `{"query_text":"SELECT * FROM logs"}`, []models.ColumnInfo{{Name: "alpha", Type: "String"}, {Name: "extra", Type: "String"}, {Name: "middle", Type: "String"}, {Name: "zebra", Type: "String"}}},
		{"logchefql", `{"query":"alpha=\"a\"","start_time":"2026-09-01 00:00:00","end_time":"2026-09-01 01:00:00","timezone":"UTC"}`, []models.ColumnInfo{{Name: "zebra", Type: "String"}, {Name: "alpha", Type: "String"}, {Name: "middle", Type: "String"}, {Name: "extra", Type: "String"}}},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			s := newDashboardTestServer(t)
			s.config = &config.Config{Query: config.QueryConfig{DefaultPreviewLimit: 100, MaxPreviewLimit: 1000, DefaultTimeoutSeconds: 10}}
			s.store = sourceSchemaStore{Store: s.store, columns: []models.ColumnInfo{{Name: "zebra", Type: "String"}, {Name: "alpha", Type: "String"}, {Name: "middle", Type: "String"}}}
			s.datasources = datasource.NewService(s.store, s.log)
			s.datasources.Register(&inferredColumnsQueryProvider{succeedingQueryProvider{sourceType: models.SourceTypeVictoriaLogs}})
			source := &models.Source{Name: "inferred-columns", SourceType: models.SourceTypeVictoriaLogs, ConnectionConfig: json.RawMessage(`{"url":"http://localhost:9428"}`)}
			if err := s.store.CreateSource(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			user, src := authorizeTestSource(t, s.store, source.ID)
			app := fiber.New()
			handler := s.handleQueryLogs
			if endpoint.name == "logchefql" {
				handler = s.handleLogchefQLQuery
			}
			withAuthorizedSource(app, s, "/teams/:teamID/sources/:sourceID/query", user, handler)
			path := fmt.Sprintf("/teams/%d/sources/%d/query", src.TeamID(), source.ID)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(endpoint.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body struct {
				Status string `json:"status"`
				Data   struct {
					Columns []models.ColumnInfo `json:"columns"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK || body.Status != "success" {
				t.Fatalf("status=%d, body=%+v", resp.StatusCode, body)
			}
			if !reflect.DeepEqual(body.Data.Columns, endpoint.want) {
				t.Fatalf("columns=%v, want %v", body.Data.Columns, endpoint.want)
			}
		})
	}
}
