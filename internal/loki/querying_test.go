package loki

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/logchefql"
	"github.com/mr-karan/logchef/pkg/models"
)

// The detected fields body is the shape that the dev seed returned from Loki
// 3.7.6: structured metadata has "parsers": null.
const detectedFieldsBody = `{"fields":[
 {"label":"status","type":"int","cardinality":3,"parsers":["json"],"jsonPath":["status"]},
 {"label":"trace_id","type":"string","cardinality":4,"parsers":null},
 {"label":"latency","type":"duration","cardinality":4,"parsers":["logfmt"]},
 {"label":"level","type":"string","cardinality":3,"parsers":["json"]},
 {"label":"detected_level","type":"string","cardinality":3,"parsers":null}
],"limit":1000}`

func schemaLoki(t *testing.T, seen map[string]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path] = r.URL.RawQuery
		switch r.URL.Path {
		case "/loki/api/v1/labels":
			_, _ = io.WriteString(w, `{"status":"success","data":["service_name","level","env"]}`)
		case "/loki/api/v1/detected_fields":
			_, _ = io.WriteString(w, detectedFieldsBody)
		case "/loki/api/v1/status/buildinfo":
			_, _ = io.WriteString(w, `{"version":"3.7.6"}`)
		case "/loki/api/v1/index/stats":
			_, _ = io.WriteString(w, `{"streams":4,"chunks":5,"entries":120,"bytes":4096}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestGetSourceSchema(t *testing.T) {
	tests := []struct {
		parser string
		want   string
	}{
		{"", "_time:DateTime,_msg:String,env:label,level:label,service_name:label,trace_id:string,detected_level:string"},
		{"json", "_time:DateTime,_msg:String,env:label,level:label,service_name:label,status:int,trace_id:string,detected_level:string"},
		{"logfmt", "_time:DateTime,_msg:String,env:label,level:label,service_name:label,trace_id:string,latency:duration,detected_level:string"},
	}
	for _, tt := range tests {
		t.Run("parser "+tt.parser, func(t *testing.T) {
			seen := map[string]string{}
			server := schemaLoki(t, seen)
			source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL, Selector: `{env="dev"}`, Parser: tt.parser})
			columns, err := testProvider().GetSourceSchema(t.Context(), source)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, c := range columns {
				got = append(got, c.Name+":"+c.Type)
			}
			if strings.Join(got, ",") != tt.want {
				t.Fatalf("schema\n got %s\nwant %s", strings.Join(got, ","), tt.want)
			}
			for _, path := range []string{"/loki/api/v1/labels", "/loki/api/v1/detected_fields"} {
				if !strings.Contains(seen[path], "query=%7Benv%3D%22dev%22%7D") {
					t.Fatalf("%s sent %q, want the source selector", path, seen[path])
				}
			}
			if !strings.Contains(seen["/loki/api/v1/detected_fields"], "limit=1000") {
				t.Fatalf("detected_fields sent %q", seen["/loki/api/v1/detected_fields"])
			}
		})
	}
}

func TestInspectSource(t *testing.T) {
	server := schemaLoki(t, map[string]string{})
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL, TenantID: "dev", Selector: `{env="dev"}`})
	source.MetaSeverityField = "detected_level"
	inspection, err := testProvider().InspectSource(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	details := map[string]string{}
	for _, d := range inspection.Details {
		details[d.Key] = d.Value
	}
	for key, want := range map[string]string{"backend": "Loki", "version": "3.7.6", "tenant": "dev", "selector": `{env="dev"}`, "parser": "none"} {
		if details[key] != want {
			t.Errorf("detail %s = %q, want %q", key, details[key], want)
		}
	}
	storage := map[string]string{}
	for _, m := range inspection.Storage {
		storage[m.Key] = m.Value
		if !strings.Contains(m.Hint, "Excludes data not yet flushed") {
			t.Errorf("metric %s hint %q", m.Key, m.Hint)
		}
	}
	if storage["streams"] != "4" || storage["entries"] != "120" || storage["bytes"] != "4096" || storage["chunks"] != "5" {
		t.Fatalf("storage %v", storage)
	}
	if inspection.Schema == nil || len(inspection.Schema.Fields) == 0 || inspection.Activity != nil {
		t.Fatalf("inspection %+v", inspection)
	}
}

// Field value requests without a field or a limit fail with a validation error.
func TestEmptyFieldValueRequestsAreValidationErrors(t *testing.T) {
	p := testProvider()
	source := testSource(t, models.LokiConnectionInfo{BaseURL: "http://loki.test"})
	_, valuesErr := p.GetFieldValues(t.Context(), source, datasource.FieldValuesRequest{})
	_, allValuesErr := p.GetAllFieldValues(t.Context(), source, datasource.AllFieldValuesRequest{})
	for _, err := range []error{valuesErr, allValuesErr} {
		if _, ok := errors.AsType[*datasource.ValidationError](err); !ok {
			t.Fatalf("error %v, want a validation error", err)
		}
	}
}

func TestCompileLogchefQL(t *testing.T) {
	columns := []models.ColumnInfo{
		{Name: "_time", Type: timeColumnType},
		{Name: "_msg", Type: stringColumnType},
		{Name: "env", Type: labelColumnType},
		{Name: "level", Type: labelColumnType},
		{Name: "status", Type: "int"},
		{Name: "trace_id", Type: "string"},
	}
	tests := []struct {
		name, parser, query, want string
	}{
		{"empty query is the source selector", "", "", `{env="dev"}`},
		{"stream label is hoisted, the line is a line filter", "", `level="error" and _msg~"gateway"`, `{env="dev", level="error"} |~ "(?i)gateway"`},
		{"other fields are label filters after the source parser", "json", `level="error" and trace_id="abc"`, `{env="dev", level="error"} | json | drop __error__, __error_details__ | trace_id="abc"`},
		{"numeric comparison drops conversion errors", "logfmt", `status>=500`, `{env="dev"} | logfmt | drop __error__, __error_details__ | status>=500 | __error__=""`},
		{"select keeps the stream labels", "", `level="error" | trace_id`, `{env="dev", level="error"} | keep trace_id, env, level`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := testSource(t, models.LokiConnectionInfo{BaseURL: "http://loki.test", Selector: `{env="dev"}`, Parser: tt.parser})
			source.Columns = columns
			compiled, err := testProvider().CompileLogchefQL(t.Context(), source, datasource.LogchefQLCompileRequest{Query: tt.query})
			if err != nil {
				t.Fatal(err)
			}
			if compiled.Query != tt.want || compiled.FilterOnly != tt.want {
				t.Fatalf("query\n got %s\nwant %s", compiled.Query, tt.want)
			}
			if !compiled.Valid || compiled.Language != models.QueryLanguageLogQL {
				t.Fatalf("compiled %+v", compiled)
			}
		})
	}

	t.Run("an unsupported construct returns the parse error with the result", func(t *testing.T) {
		source := testSource(t, models.LokiConnectionInfo{BaseURL: "http://loki.test", Selector: `{env="dev"}`})
		source.Columns = columns
		compiled, err := testProvider().CompileLogchefQL(t.Context(), source, datasource.LogchefQLCompileRequest{Query: `_msg~"a" or level="error"`})
		if compiled == nil || compiled.Valid || compiled.Error == nil || err == nil || compiled.Language != models.QueryLanguageLogQL {
			t.Fatalf("compiled %+v, error %v", compiled, err)
		}
		if compiled.Error.Code != logchefql.ErrUnsupportedFeature {
			t.Fatalf("code %s", compiled.Error.Code)
		}
	})
}

func alertLoki(t *testing.T, status int, contentType, body string, seen *url.Values) *models.Source {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/loki/api/v1/query" {
			t.Errorf("path %s", r.URL.Path)
		}
		*seen = r.URL.Query()
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
}

func TestEvaluateAlert(t *testing.T) {
	at := time.Date(2026, 11, 16, 10, 0, 0, 123, time.UTC)
	req := datasource.AlertQueryRequest{Language: models.QueryLanguageLogQL, Query: ` sum by (pod, app) (count_over_time({app="api"}[5m])) `, At: at, LookbackSeconds: 300}

	t.Run("an instant query at At returns value first and sorted series", func(t *testing.T) {
		var seen url.Values
		source := alertLoki(t, http.StatusOK, "application/json", `{"status":"success","data":{"resultType":"vector","result":[
			{"metric":{"app":"api","pod":"b"},"value":[1794823200,"7"]},
			{"metric":{"app":"api","pod":"a","value":"label"},"value":[1794823200,"3"]}
		],"stats":{"summary":{"execTime":0.25,"totalLinesProcessed":10}}}}`, &seen)
		result, err := testProvider().EvaluateAlert(t.Context(), source, req)
		if err != nil {
			t.Fatal(err)
		}
		if seen.Get("time") != "1794823200000000123" || seen.Get("query") != `sum by (pod, app) (count_over_time({app="api"}[5m]))` {
			t.Fatalf("params %v", seen)
		}
		names := make([]string, len(result.Columns))
		for i, column := range result.Columns {
			names[i] = column.Name
		}
		if !slices.Equal(names, []string{"value", "app", "pod"}) {
			t.Fatalf("columns %v", names)
		}
		if len(result.Logs) != 2 || result.Logs[0]["value"] != "3" || result.Logs[0]["pod"] != "a" || result.Logs[1]["value"] != "7" {
			t.Fatalf("rows %v", result.Logs)
		}
		if result.Stats.ExecutionTimeMs != 250 || result.Stats.RowsRead != 10 || result.Stats.RowsReturned != 2 {
			t.Fatalf("stats %+v", result.Stats)
		}

		samples, err := alerts.SamplesFromResult(result, []string{"pod"})
		if err != nil || len(samples) != 2 || samples[0].Value != 3 || samples[1].Value != 7 {
			t.Fatalf("grouped samples %+v, %v", samples, err)
		}
		samples, err = alerts.SamplesFromResult(result, nil)
		if err != nil || len(samples) != 1 || samples[0].Value != 3 {
			t.Fatalf("ungrouped samples %+v, %v", samples, err)
		}
	})

	t.Run("an empty vector is zero rows", func(t *testing.T) {
		var seen url.Values
		source := alertLoki(t, http.StatusOK, "application/json", `{"status":"success","data":{"resultType":"vector","result":[]}}`, &seen)
		result, err := testProvider().EvaluateAlert(t.Context(), source, req)
		if err != nil || len(result.Logs) != 0 || result.Logs == nil {
			t.Fatalf("result %+v, %v", result, err)
		}
	})

	t.Run("a log query gets the named error", func(t *testing.T) {
		var seen url.Values
		source := alertLoki(t, http.StatusBadRequest, "text/plain; charset=utf-8", "log queries are not supported as an instant query type, please change your query to a range query type", &seen)
		_, err := testProvider().EvaluateAlert(t.Context(), source, req)
		assertValidation(t, err, logQueryAlertMessage)
	})

	t.Run("a scalar result gets the named error", func(t *testing.T) {
		var seen url.Values
		source := alertLoki(t, http.StatusOK, "application/json", `{"status":"success","data":{"resultType":"scalar","result":[1794823200,"2"]}}`, &seen)
		_, err := testProvider().EvaluateAlert(t.Context(), source, req)
		assertValidation(t, err, logQueryAlertMessage)
	})

	t.Run("a parse error keeps Loki's message", func(t *testing.T) {
		var seen url.Values
		source := alertLoki(t, http.StatusBadRequest, "text/plain", "parse error at line 1, col 29: syntax error", &seen)
		_, err := testProvider().EvaluateAlert(t.Context(), source, req)
		assertValidation(t, err, "parse error at line 1, col 29: syntax error")
	})

	t.Run("a request without At or with another language is refused before Loki", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("the request reached Loki") }))
		t.Cleanup(server.Close)
		source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
		noAt := req
		noAt.At = time.Time{}
		logsQL := req
		logsQL.Language = models.QueryLanguageLogsQL
		empty := req
		empty.Query = "  "
		for _, r := range []datasource.AlertQueryRequest{noAt, logsQL, empty} {
			if _, err := testProvider().EvaluateAlert(t.Context(), source, r); err == nil {
				t.Fatalf("request %+v was accepted", r)
			}
		}
	})
}
