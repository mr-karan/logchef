package victorialogs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestValidateConnectionUsesHeadersAcrossHealthAndQueryValidation(t *testing.T) {
	t.Parallel()

	var gotAuthorization string
	var gotAccountID string
	var gotProjectID string
	var gotCustom string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case "/select/logsql/field_names":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			gotAuthorization = r.Header.Get("Authorization")
			gotAccountID = r.Header.Get("AccountID")
			gotProjectID = r.Header.Get("ProjectID")
			gotCustom = r.Header.Get("X-Test-Header")
			if got := r.Form.Get("query"); got != "*" {
				t.Fatalf("unexpected query: %q", got)
			}
			if got := r.Form.Get("ignore_pipes"); got != "1" {
				t.Fatalf("unexpected ignore_pipes: %q", got)
			}
			_, _ = w.Write([]byte(`{"values":[{"value":"service","hits":1}]}`))
		case "/select/logsql/query":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("limit"); got != "1" {
				t.Fatalf("unexpected query probe limit: %q", got)
			}
			// Reserved auth/tenant headers must be present on the real query
			// probe too, proving both validation calls carry them.
			if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
				t.Fatalf("unexpected authorization header on query probe: %q", got)
			}
			if got := r.Header.Get("AccountID"); got != "12" {
				t.Fatalf("unexpected account header on query probe: %q", got)
			}
			_, _ = w.Write([]byte(`{"_time":"2026-04-08T10:00:00Z","_msg":"probe"}` + "\n"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
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
	if result == nil || result.Message != "Connection successful. Credentials, tenant scope, and immutable filters validated." {
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

func TestValidateConnectionRejectsIncompleteTenantConfiguration(t *testing.T) {
	t.Parallel()

	provider := newTestProvider(nil)
	_, err := provider.ValidateConnection(context.Background(), &models.ValidateConnectionRequest{
		SourceType: models.SourceTypeVictoriaLogs,
		Connection: mustJSON(t, models.VictoriaLogsConnectionInfo{
			BaseURL: "https://logs.example.com",
			Tenant: models.VictoriaLogsTenant{
				AccountID: "12",
			},
		}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "account_id and project_id must be provided together") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidateConnectionReturnsHelpfulAuthError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case "/select/logsql/field_names":
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestProvider(server)
	_, err := provider.ValidateConnection(context.Background(), &models.ValidateConnectionRequest{
		SourceType: models.SourceTypeVictoriaLogs,
		Connection: mustJSON(t, models.VictoriaLogsConnectionInfo{
			BaseURL: server.URL,
			Auth: models.VictoriaLogsAuth{
				Mode:  "bearer",
				Token: "bad-token",
			},
		}),
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "rejected the provided credentials or token") {
		t.Fatalf("unexpected validation error: %v", err)
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
		if got := r.Form.Get("query"); got != "level:error | sort by (_time desc)" {
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
	})

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
			if got := r.Form.Get("query"); got != `level:="error"` {
				t.Fatalf("unexpected translated query: %q", got)
			}
			_, _ = w.Write([]byte(`{"values":[{"value":"api","hits":8},{"value":"worker","hits":3}]}`))
		case "/select/logsql/facets":
			if got := r.Form.Get("keep_const_fields"); got != "1" {
				t.Fatalf("unexpected keep_const_fields: %q", got)
			}
			if got := r.Form.Get("query"); got != `level:="error"` {
				t.Fatalf("unexpected translated query for facets: %q", got)
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
	})

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
		Language:  models.QueryLanguageLogchefQL,
		StartTime: time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 8, 1, 0, 0, 0, time.UTC),
		Limit:     5,
		QueryText: `level = "error"`,
	})
	if err != nil {
		t.Fatalf("GetFieldValues returned error: %v", err)
	}
	if values.TotalDistinct != 2 {
		t.Fatalf("unexpected total distinct values: %d", values.TotalDistinct)
	}

	allValues, err := provider.GetAllFieldValues(context.Background(), source, datasource.AllFieldValuesRequest{
		Language:  models.QueryLanguageLogchefQL,
		StartTime: time.Date(2026, 4, 8, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 4, 8, 1, 0, 0, 0, time.UTC),
		Limit:     5,
		QueryText: `level = "error"`,
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
	var gotAlertQuery string
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
			gotAlertQuery = r.Form.Get("query")
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
	})

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
		Language:        models.QueryLanguageLogsQL,
		Query:           `count() if (level:error)`,
		LookbackSeconds: 300,
		QueryTimeout:    &timeout,
	})
	if err != nil {
		t.Fatalf("EvaluateAlert returned error: %v", err)
	}
	if gotAlertQuery != "_time:5m count() if (level:error)" {
		t.Fatalf("unexpected alert query: %q", gotAlertQuery)
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

func TestInspectSourceIncludesSchemaAndActivity(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC().Truncate(time.Hour)
	latest := now.Add(-5 * time.Minute).Format(time.RFC3339)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		switch r.URL.Path {
		case "/select/logsql/field_names":
			_, _ = w.Write([]byte(`{"values":[{"value":"service","hits":10},{"value":"_msg","hits":12},{"value":"_stream","hits":12},{"value":"_stream_id","hits":12}]}`))
		case "/select/logsql/hits":
			switch r.Form.Get("step") {
			case "1h":
				_, _ = fmt.Fprintf(w, `{"hits":[{"fields":{},"timestamps":["%s"],"values":[8],"total":8}]}`, now.Format(time.RFC3339))
			case "1d":
				dayOne := now.Add(-24 * time.Hour).Format(time.RFC3339)
				dayTwo := now.Format(time.RFC3339)
				_, _ = fmt.Fprintf(w, `{"hits":[{"fields":{},"timestamps":["%s","%s"],"values":[12,20],"total":32}]}`, dayOne, dayTwo)
			default:
				t.Fatalf("unexpected step: %q", r.Form.Get("step"))
			}
		case "/select/logsql/query":
			_, _ = fmt.Fprintf(w, `{"_time":"%s","_msg":"latest row","service":"api"}`, latest)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{
		BaseURL: server.URL,
		Scope: models.VictoriaLogsScope{
			Query: `{app="payments"}`,
		},
	})

	inspection, err := provider.InspectSource(context.Background(), source)
	if err != nil {
		t.Fatalf("InspectSource returned error: %v", err)
	}
	if inspection.Activity == nil {
		t.Fatalf("expected activity inspection, got nil")
	}
	if inspection.Activity.Rows1h != 8 {
		t.Fatalf("unexpected rows_1h: %d", inspection.Activity.Rows1h)
	}
	if inspection.Activity.Rows24h != 8 {
		t.Fatalf("unexpected rows_24h: %d", inspection.Activity.Rows24h)
	}
	if inspection.Activity.Rows7d != 32 {
		t.Fatalf("unexpected rows_7d: %d", inspection.Activity.Rows7d)
	}
	if inspection.Activity.LatestTS == nil || inspection.Activity.LatestTS.Format(time.RFC3339) != latest {
		t.Fatalf("unexpected latest_ts: %#v", inspection.Activity.LatestTS)
	}
	if inspection.Schema == nil || len(inspection.Schema.Fields) == 0 {
		t.Fatalf("expected schema fields, got %#v", inspection.Schema)
	}
	fieldNames := make([]string, 0, len(inspection.Schema.Fields))
	for _, field := range inspection.Schema.Fields {
		fieldNames = append(fieldNames, field.Name)
	}
	expectedFields := []string{"_time", "level", "_msg", "_stream_id", "_stream", "service"}
	if !reflect.DeepEqual(fieldNames, expectedFields) {
		t.Fatalf("unexpected inspection fields: %#v", fieldNames)
	}
	if !slices.ContainsFunc(inspection.Details, func(detail datasource.InspectionDetail) bool {
		return detail.Key == "scope" && detail.Value == `{app="payments"}`
	}) {
		t.Fatalf("expected immutable scope detail, got %#v", inspection.Details)
	}
}

func newTestProvider(server *httptest.Server) *Provider {
	provider := NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if server != nil {
		provider.client = server.Client()
	}
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

func mustSource(t *testing.T, conn models.VictoriaLogsConnectionInfo) *models.Source {
	t.Helper()

	source := &models.Source{
		ID:                1,
		Name:              "VictoriaLogs Dev",
		SourceType:        models.SourceTypeVictoriaLogs,
		MetaTSField:       "_time",
		MetaSeverityField: "level",
		ConnectionConfig:  mustJSON(t, conn),
	}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatalf("sync connection config: %v", err)
	}
	return source
}

func TestAppendDefaultSort(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "bare star gets sort",
			query: "*",
			want:  "* | sort by (_time desc)",
		},
		{
			name:  "pipe-free filter gets sort",
			query: `level:="error"`,
			want:  `level:="error" | sort by (_time desc)`,
		},
		{
			name:  "already piped query is left alone",
			query: `* | stats count()`,
			want:  `* | stats count()`,
		},
		{
			name:  "translated query with sort pipe is left alone",
			query: `level:="error" | sort by (_time desc)`,
			want:  `level:="error" | sort by (_time desc)`,
		},
		{
			name:  "fields projection gets sort inserted before it",
			query: `level:="error" | fields _time, _msg, level`,
			want:  `level:="error" | sort by (_time desc) | fields _time, _msg, level`,
		},
		{
			name:  "stats pipeline is left alone",
			query: `level:="error" | stats count() as value`,
			want:  `level:="error" | stats count() as value`,
		},
		{
			name:  "pipe inside quoted literal is not a pipe stage",
			query: `_msg:"a|b"`,
			want:  `_msg:"a|b" | sort by (_time desc)`,
		},
		{
			name:  "escaped quote inside literal keeps quote state",
			query: `_msg:"a\"|b"`,
			want:  `_msg:"a\"|b" | sort by (_time desc)`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := appendDefaultSort(tc.query); got != tc.want {
				t.Fatalf("appendDefaultSort(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

func newPermissiveVLServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case "/select/logsql/field_names":
			_, _ = w.Write([]byte(`{"values":[{"value":"service","hits":1}]}`))
		case "/select/logsql/query":
			_, _ = w.Write([]byte(`{"_time":"2026-04-08T10:00:00Z","_msg":"probe"}` + "\n"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
}

// Finding #16: custom headers must never override the provider-computed
// auth/tenant headers. Reject the collision at validation time.
func TestValidateConnectionRejectsReservedCustomHeaders(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"Authorization", "authorization", "AccountID", "accountid", "ProjectID", "projectid"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			provider := newTestProvider(nil)
			_, err := provider.ValidateConnection(context.Background(), &models.ValidateConnectionRequest{
				SourceType: models.SourceTypeVictoriaLogs,
				Connection: mustJSON(t, models.VictoriaLogsConnectionInfo{
					BaseURL: "https://logs.example.com",
					Headers: map[string]string{key: "attacker-value"},
				}),
			})
			if err == nil {
				t.Fatalf("expected reserved header %q to be rejected", key)
			}
			if !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("unexpected error for %q: %v", key, err)
			}
		})
	}
}

// Finding #16: even if a reserved header somehow reaches applyHeaders, the
// computed auth/tenant values must win on the wire.
func TestApplyHeadersSkipsReservedCustomHeaders(t *testing.T) {
	t.Parallel()

	conn := models.VictoriaLogsConnectionInfo{
		BaseURL: "https://logs.example.com",
		Auth: models.VictoriaLogsAuth{
			Mode:  "bearer",
			Token: "real-token",
		},
		Tenant: models.VictoriaLogsTenant{
			AccountID: "12",
			ProjectID: "34",
		},
		Headers: map[string]string{
			"Authorization": "Bearer forged",
			"accountid":     "999",
			"ProjectID":     "888",
			"X-Api-Key":     "keep-me",
		},
	}

	req, err := http.NewRequest(http.MethodGet, conn.BaseURL, http.NoBody)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	applyHeaders(req, conn)

	if got := req.Header.Get("Authorization"); got != "Bearer real-token" {
		t.Fatalf("reserved Authorization header was overridden: %q", got)
	}
	if got := req.Header.Get("AccountID"); got != "12" {
		t.Fatalf("reserved AccountID header was overridden: %q", got)
	}
	if got := req.Header.Get("ProjectID"); got != "34" {
		t.Fatalf("reserved ProjectID header was overridden: %q", got)
	}
	if got := req.Header.Get("X-Api-Key"); got != "keep-me" {
		t.Fatalf("legitimate custom header was dropped: %q", got)
	}
}

// Finding #15: switching auth mode must only carry over the credential the NEW
// mode uses, so a stale token/password cannot be resurrected via a later switch.
func TestUpdateSourceCarriesOnlyActiveModeCredential(t *testing.T) {
	t.Parallel()

	server := newPermissiveVLServer(t)
	defer server.Close()
	provider := newTestProvider(server)

	source := mustSource(t, models.VictoriaLogsConnectionInfo{
		BaseURL: server.URL,
		Auth: models.VictoriaLogsAuth{
			Mode:  "bearer",
			Token: "old-token",
		},
	})

	// bearer -> basic: the bearer token must be dropped, not carried over.
	if _, err := provider.UpdateSource(context.Background(), source, &models.UpdateSourceRequest{
		Connection: mustJSON(t, models.VictoriaLogsConnectionInfo{
			BaseURL: server.URL,
			Auth: models.VictoriaLogsAuth{
				Mode:     "basic",
				Username: "user",
				Password: "pass",
			},
		}),
	}); err != nil {
		t.Fatalf("switch to basic failed: %v", err)
	}
	afterBasic, err := source.VictoriaLogsConnection()
	if err != nil {
		t.Fatalf("read connection after basic switch: %v", err)
	}
	if afterBasic.Auth.Token != "" {
		t.Fatalf("bearer token survived switch to basic: %q", afterBasic.Auth.Token)
	}
	if afterBasic.Auth.Password != "pass" {
		t.Fatalf("unexpected password after basic switch: %q", afterBasic.Auth.Password)
	}

	// basic -> bearer with a blank (redacted) token: the old token must NOT be
	// resurrected, so bearer validation fails for lack of a token.
	_, err = provider.UpdateSource(context.Background(), source, &models.UpdateSourceRequest{
		Connection: mustJSON(t, models.VictoriaLogsConnectionInfo{
			BaseURL: server.URL,
			Auth: models.VictoriaLogsAuth{
				Mode: "bearer",
			},
		}),
	})
	if err == nil {
		t.Fatal("expected bearer switch with blank token to fail (no token to resurrect)")
	}
	if !strings.Contains(err.Error(), "token is required") {
		t.Fatalf("unexpected error switching back to bearer: %v", err)
	}
}

// Regression guard for Finding #15: within the same mode, a blank redacted
// credential still means "keep the existing one".
func TestUpdateSourceKeepsActiveModeCredentialOnBlank(t *testing.T) {
	t.Parallel()

	server := newPermissiveVLServer(t)
	defer server.Close()
	provider := newTestProvider(server)

	source := mustSource(t, models.VictoriaLogsConnectionInfo{
		BaseURL: server.URL,
		Auth: models.VictoriaLogsAuth{
			Mode:  "bearer",
			Token: "keep-token",
		},
	})

	if _, err := provider.UpdateSource(context.Background(), source, &models.UpdateSourceRequest{
		Connection: mustJSON(t, models.VictoriaLogsConnectionInfo{
			BaseURL: server.URL,
			Auth:    models.VictoriaLogsAuth{Mode: "bearer"},
		}),
	}); err != nil {
		t.Fatalf("same-mode bearer update failed: %v", err)
	}
	got, err := source.VictoriaLogsConnection()
	if err != nil {
		t.Fatalf("read connection: %v", err)
	}
	if got.Auth.Token != "keep-token" {
		t.Fatalf("blank token should keep existing, got %q", got.Auth.Token)
	}
}

// Finding #17: validation must probe the real query path, not just field_names.
func TestValidateConnectionProbesRealQueryPath(t *testing.T) {
	t.Parallel()

	var queryProbed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		case "/select/logsql/field_names":
			// A proxy that allows field_names but denies /query.
			_, _ = w.Write([]byte(`{"values":[{"value":"service","hits":1}]}`))
		case "/select/logsql/query":
			queryProbed = true
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	provider := newTestProvider(server)
	_, err := provider.ValidateConnection(context.Background(), &models.ValidateConnectionRequest{
		SourceType: models.SourceTypeVictoriaLogs,
		Connection: mustJSON(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL}),
	})
	if err == nil {
		t.Fatal("expected validation to fail when the query path is denied")
	}
	if !queryProbed {
		t.Fatal("validation did not probe the real query path")
	}
	if !strings.Contains(err.Error(), "denied access") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// Finding #18: lifecycle operations for the same source ID must be serialised so
// concurrent init/remove/health calls cannot deadlock or interleave unsafely.
func TestProviderLifecycleOpsAreConcurrencySafe(t *testing.T) {
	t.Parallel()

	server := newPermissiveVLServer(t)
	defer server.Close()
	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(4)
		go func() { defer wg.Done(); _ = provider.InitializeSource(context.Background(), source) }()
		go func() { defer wg.Done(); _ = provider.RemoveSource(source.ID) }()
		go func() { defer wg.Done(); _ = provider.GetSourceHealth(context.Background(), source.ID) }()
		go func() { defer wg.Done(); _ = provider.CheckSourceConnectionStatus(context.Background(), source) }()
	}
	wg.Wait()
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
