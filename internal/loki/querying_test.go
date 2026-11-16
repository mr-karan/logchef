package loki

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/datasource"
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

// Methods that later units of RFC 0011 own fail with a validation error.
func TestLaterUnitsAreValidationErrors(t *testing.T) {
	p := testProvider()
	source := testSource(t, models.LokiConnectionInfo{BaseURL: "http://loki.test"})
	_, histogramErr := p.Histogram(t.Context(), source, datasource.HistogramRequest{})
	_, valuesErr := p.GetFieldValues(t.Context(), source, datasource.FieldValuesRequest{})
	_, allValuesErr := p.GetAllFieldValues(t.Context(), source, datasource.AllFieldValuesRequest{})
	_, alertErr := p.EvaluateAlert(t.Context(), source, datasource.AlertQueryRequest{})
	for _, err := range []error{histogramErr, valuesErr, allValuesErr, alertErr} {
		if _, ok := errors.AsType[*datasource.ValidationError](err); !ok {
			t.Fatalf("error %v, want a validation error", err)
		}
	}
}
