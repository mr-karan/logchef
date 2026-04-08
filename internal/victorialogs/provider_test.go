package victorialogs

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestValidateConnectionUsesHealthEndpointHeaders(t *testing.T) {
	t.Parallel()

	var gotAuthorization string
	var gotAccountID string
	var gotProjectID string
	var gotCustom string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		gotAccountID = r.Header.Get("AccountID")
		gotProjectID = r.Header.Get("ProjectID")
		gotCustom = r.Header.Get("X-Test-Header")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	provider := newTestProvider(server)
	request := &models.ValidateConnectionRequest{
		SourceType: models.SourceTypeVictoriaLogs,
		Connection: mustJSON(t, models.VictoriaLogsConnectionInfo{
			BaseURL: server.URL,
			Auth: models.VictoriaLogsAuth{
				Mode:  "bearer",
				Token: "secret-token",
			},
			Tenant: models.VictoriaLogsTenant{
				AccountID: "12",
				ProjectID: "34",
			},
			Headers: map[string]string{
				"X-Test-Header": "enabled",
			},
		}),
	}

	result, err := provider.ValidateConnection(context.Background(), request)
	if err != nil {
		t.Fatalf("ValidateConnection returned error: %v", err)
	}
	if result == nil || result.Message != "Connection successful" {
		t.Fatalf("unexpected validation result: %#v", result)
	}
	if gotAuthorization != "Bearer secret-token" {
		t.Fatalf("unexpected authorization header: %q", gotAuthorization)
	}
	if gotAccountID != "12" || gotProjectID != "34" {
		t.Fatalf("unexpected tenant headers: account=%q project=%q", gotAccountID, gotProjectID)
	}
	if gotCustom != "enabled" {
		t.Fatalf("unexpected custom header: %q", gotCustom)
	}
}

func TestQueryLogsAppliesHeadersScopeAndLimit(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	timeout := 15

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/select/logsql/query" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.Form.Get("query"); got != "level:error" {
			t.Fatalf("unexpected query: %q", got)
		}
		if got := r.Form.Get("limit"); got != "100" {
			t.Fatalf("unexpected limit: %q", got)
		}
		if got := r.Form.Get("start"); got != "2026-04-08T10:00:00Z" {
			t.Fatalf("unexpected start: %q", got)
		}
		if got := r.Form.Get("end"); got != "2026-04-08T10:30:00Z" {
			t.Fatalf("unexpected end: %q", got)
		}
		if got := r.Form.Get("timeout"); got != "15s" {
			t.Fatalf("unexpected timeout: %q", got)
		}
		if got := r.Form["extra_stream_filters"]; !reflect.DeepEqual(got, []string{`{app="payments"}`}) {
			t.Fatalf("unexpected extra_stream_filters: %#v", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer query-token" {
			t.Fatalf("unexpected authorization header: %q", got)
		}

		w.Header().Set("VL-Request-Duration-Seconds", "0.5")
		_, _ = w.Write([]byte(
			`{"_time":"2026-04-08T10:01:00Z","level":"error","_msg":"boom","service":"api"}` + "\n" +
				`{"_time":"2026-04-08T10:02:00Z","service":"worker"}` + "\n",
		))
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{
		BaseURL: server.URL,
		Auth: models.VictoriaLogsAuth{
			Mode:  "bearer",
			Token: "query-token",
		},
		Scope: models.VictoriaLogsScope{
			Query: `{app="payments"}`,
		},
	}, "_time", "level")

	result, err := provider.QueryLogs(context.Background(), source, datasource.QueryRequest{
		RawQuery:     "level:error",
		StartTime:    &start,
		EndTime:      &end,
		Limit:        250,
		MaxLimit:     100,
		QueryTimeout: &timeout,
	})
	if err != nil {
		t.Fatalf("QueryLogs returned error: %v", err)
	}
	if len(result.Logs) != 2 {
		t.Fatalf("expected 2 log rows, got %d", len(result.Logs))
	}
	if result.Stats.RowsRead != 2 {
		t.Fatalf("unexpected rows read: %d", result.Stats.RowsRead)
	}
	if result.Stats.ExecutionTimeMs != 500 {
		t.Fatalf("unexpected execution time: %v", result.Stats.ExecutionTimeMs)
	}

	columnNames := make([]string, 0, len(result.Columns))
	for _, column := range result.Columns {
		columnNames = append(columnNames, column.Name)
	}
	expectedColumns := []string{"_time", "level", "_msg", "service"}
	if !reflect.DeepEqual(columnNames, expectedColumns) {
		t.Fatalf("unexpected columns: %#v", columnNames)
	}
}

func TestSchemaAndFieldValueDiscovery(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		switch r.URL.Path {
		case "/select/logsql/field_names":
			if got := r.Form["extra_filters"]; !reflect.DeepEqual(got, []string{"kubernetes.namespace:=prod"}) {
				t.Fatalf("unexpected extra_filters for field_names: %#v", got)
			}
			_, _ = w.Write([]byte(`{"values":[{"value":"service","hits":10},{"value":"level","hits":5},{"value":"_msg","hits":12}]}`))
		case "/select/logsql/field_values":
			if got := r.Form.Get("field"); got != "service" {
				t.Fatalf("unexpected field name: %q", got)
			}
			if got := r.Form.Get("limit"); got != "5" {
				t.Fatalf("unexpected limit: %q", got)
			}
			_, _ = w.Write([]byte(`{"values":[{"value":"api","hits":8},{"value":"worker","hits":3}]}`))
		case "/select/logsql/facets":
			if got := r.Form.Get("keep_const_fields"); got != "1" {
				t.Fatalf("unexpected keep_const_fields: %q", got)
			}
			_, _ = w.Write([]byte(`{"facets":[{"field_name":"service","values":[{"field_value":"api","hits":8}]}]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{
		BaseURL: server.URL,
		Scope: models.VictoriaLogsScope{
			Query: "kubernetes.namespace:=prod",
		},
	}, "_time", "level")

	columns, err := provider.GetSourceSchema(context.Background(), source)
	if err != nil {
		t.Fatalf("GetSourceSchema returned error: %v", err)
	}
	columnNames := make([]string, 0, len(columns))
	for _, column := range columns {
		columnNames = append(columnNames, column.Name)
	}
	expectedColumns := []string{"_time", "level", "_msg", "service"}
	if !reflect.DeepEqual(columnNames, expectedColumns) {
		t.Fatalf("unexpected schema columns: %#v", columnNames)
	}

	values, err := provider.GetFieldValues(context.Background(), source, datasource.FieldValuesRequest{
		FieldName: "service",
		FieldType: "String",
		StartTime: time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 8, 1, 0, 0, 0, time.UTC),
		Limit:     5,
		QueryText: "service:*",
	})
	if err != nil {
		t.Fatalf("GetFieldValues returned error: %v", err)
	}
	if values.TotalDistinct != 2 {
		t.Fatalf("unexpected total distinct values: %d", values.TotalDistinct)
	}

	allValues, err := provider.GetAllFieldValues(context.Background(), source, datasource.AllFieldValuesRequest{
		StartTime: time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 8, 1, 0, 0, 0, time.UTC),
		Limit:     5,
		QueryText: "service:*",
	})
	if err != nil {
		t.Fatalf("GetAllFieldValues returned error: %v", err)
	}
	if _, ok := allValues["service"]; !ok {
		t.Fatalf("expected service facet, got %#v", allValues)
	}
}

func TestHistogramAndEvaluateAlert(t *testing.T) {
	t.Parallel()

	timeout := 20
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		switch r.URL.Path {
		case "/select/logsql/hits":
			if got := r.Form.Get("field"); got != "service" {
				t.Fatalf("unexpected group-by field: %q", got)
			}
			_, _ = w.Write([]byte(`{"hits":[{"fields":{"service":"api"},"timestamps":["2026-04-08T10:00:00Z","2026-04-08T10:05:00Z"],"values":[5,9],"total":14}]}`))
		case "/select/logsql/stats_query":
			if got := r.Form["extra_filters"]; !reflect.DeepEqual(got, []string{"service:=api"}) {
				t.Fatalf("unexpected alert scope filters: %#v", got)
			}
			_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"service":"api"},"value":[1712570400,"7"]}]}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{
		BaseURL: server.URL,
		Scope: models.VictoriaLogsScope{
			Query: "service:=api",
		},
	}, "_time", "level")

	start := time.Date(2026, 4, 8, 10, 0, 0, 0, time.UTC)
	end := start.Add(10 * time.Minute)

	histogram, err := provider.Histogram(context.Background(), source, datasource.HistogramRequest{
		StartTime:    &start,
		EndTime:      &end,
		Window:       "5m",
		Query:        "level:error",
		GroupBy:      "service",
		QueryTimeout: &timeout,
	})
	if err != nil {
		t.Fatalf("Histogram returned error: %v", err)
	}
	if histogram.Granularity != "5m" {
		t.Fatalf("unexpected histogram granularity: %q", histogram.Granularity)
	}
	if len(histogram.Data) != 2 || histogram.Data[0].GroupValue != "api" {
		t.Fatalf("unexpected histogram data: %#v", histogram.Data)
	}

	result, err := provider.EvaluateAlert(context.Background(), source, datasource.AlertQueryRequest{
		Language:     models.QueryLanguageLogsQL,
		Query:        `count() if (level:error)`,
		QueryTimeout: &timeout,
	})
	if err != nil {
		t.Fatalf("EvaluateAlert returned error: %v", err)
	}
	if len(result.Logs) != 1 {
		t.Fatalf("unexpected alert rows: %#v", result.Logs)
	}
	if got := result.Logs[0]["value"]; got != "7" {
		t.Fatalf("unexpected alert value: %#v", got)
	}

	_, err = provider.EvaluateAlert(context.Background(), source, datasource.AlertQueryRequest{
		Language: models.QueryLanguageClickHouseSQL,
		Query:    "SELECT 1",
	})
	if err == nil {
		t.Fatal("expected error for unsupported alert language")
	}
}

func newTestProvider(server *httptest.Server) *Provider {
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	provider.client = server.Client()
	return provider
}

func mustJSON(t *testing.T, value interface{}) json.RawMessage {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return payload
}

func mustSource(t *testing.T, conn models.VictoriaLogsConnectionInfo, metaTSField, metaSeverityField string) *models.Source {
	t.Helper()

	source := &models.Source{
		ID:                1,
		Name:              "VictoriaLogs Dev",
		SourceType:        models.SourceTypeVictoriaLogs,
		MetaTSField:       metaTSField,
		MetaSeverityField: metaSeverityField,
		ConnectionConfig:  mustJSON(t, conn),
	}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatalf("sync connection config: %v", err)
	}
	return source
}

func TestOrderFieldNamesPrioritizesMetadata(t *testing.T) {
	t.Parallel()

	source := &models.Source{
		MetaTSField:       "_time",
		MetaSeverityField: "level",
	}
	got := orderFieldNames(source, []string{"service", "_msg", "level", "_time", "service"})
	want := []string{"_time", "level", "_msg", "service"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected ordered fields: got=%v want=%v", got, want)
	}
}
