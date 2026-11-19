package loki

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestLeadingSelector(t *testing.T) {
	cases := map[string]string{
		`{app="api"} |= "x"`:                    `{app="api"}`,
		`  {app="a}b", env=~"p\"}"} | json`:     `{app="a}b", env=~"p\"}"}`,
		"{app=`a}b`} |= `}`":                    "{app=`a}b`}",
		`({app="api"})`:                         "",
		`sum(count_over_time({app="api"}[5m]))`: "",
		`# {app="api"}` + "\n" + `{app="web"}`:  "",
		`{app="api"`:                            "",
	}
	for query, want := range cases {
		if got := leadingSelector(query); got != want {
			t.Errorf("leadingSelector(%q) = %q, want %q", query, got, want)
		}
	}
}

func TestSidebarWindowCapsTheRangeAtTheLookback(t *testing.T) {
	end := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	conn := models.LokiConnectionInfo{Limits: &models.LokiLimits{SidebarLookbackSeconds: 600}}
	cases := []struct {
		name  string
		start time.Time
		want  time.Duration
	}{
		{"a short range is kept", end.Add(-5 * time.Minute), 5 * time.Minute},
		{"a long range is capped", end.Add(-24 * time.Hour), 10 * time.Minute},
		{"sub-millisecond precision is dropped", end.Add(-time.Minute - 1500*time.Microsecond), time.Minute + time.Millisecond},
		{"a range under a millisecond is one millisecond", end.Add(-time.Microsecond), time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start, gotEnd, err := sidebarWindow(conn, tc.start, end)
			if err != nil {
				t.Fatal(err)
			}
			if !gotEnd.Equal(end) || gotEnd.Sub(start) != tc.want {
				t.Fatalf("window [%s, %s], want %s ending at %s", start, gotEnd, tc.want, end)
			}
		})
	}
	if _, _, err := sidebarWindow(conn, end, end); !datasource.IsValidationError(err) {
		t.Fatalf("an empty range: error %v", err)
	}
}

// The preload reads at most 30 labels with at most four requests in flight.
func TestGetAllFieldValuesBoundsThePreload(t *testing.T) {
	var mu sync.Mutex
	inFlight, maxInFlight, valueCalls := 0, 0, 0
	labels := make([]string, 40)
	for i := range labels {
		labels[i] = fmt.Sprintf("l%02d", i)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/loki/api/v1/labels", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{"status": "success", "data": labels})
	})
	mux.HandleFunc("/loki/api/v1/label/{name}/values", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		valueCalls++
		maxInFlight = max(maxInFlight, inFlight)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		writeJSON(t, w, map[string]any{"status": "success", "data": []string{"c", "a", "b"}})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	p := testProvider()
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
	end := time.Now()
	result, err := p.GetAllFieldValues(t.Context(), source, datasource.AllFieldValuesRequest{StartTime: end.Add(-time.Hour), EndTime: end, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 30 || valueCalls != 30 || maxInFlight > 4 {
		t.Fatalf("%d labels, %d value calls, %d in flight", len(result), valueCalls, maxInFlight)
	}
	got := result["l00"]
	if got.FieldType != "label" || !got.IsLowCardinality || got.TotalDistinct != 3 || fmt.Sprint(got.Values) != "[{a 0} {b 0}]" {
		t.Fatalf("l00 %+v", got)
	}
	if _, ok := result["l30"]; ok {
		t.Fatal("the 31st label was read")
	}
}

type metadataStream struct {
	Stream map[string]string `json:"stream"`
	Values [][3]any          `json:"values"`
}

func TestIntegrationFieldValues(t *testing.T) {
	url := integrationURL(t)
	runID := newRunID(t)
	tenant := "logchef-" + runID

	// Three error lines and five info lines. code is a JSON field of the line
	// and trace is structured metadata.
	now := time.Now().UTC().Truncate(time.Second)
	entry := func(i int, code, trace string) [3]any {
		return [3]any{fmt.Sprint(now.Add(-time.Duration(i+1) * time.Second).UnixNano()), `{"code":"` + code + `"}`, map[string]string{"trace": trace}}
	}
	labels := func(level string) map[string]string {
		return map[string]string{"service_name": "it-fv", "test_run": runID, "level": level}
	}
	streams := []metadataStream{
		{Stream: labels("error"), Values: [][3]any{entry(0, "500", "tA"), entry(1, "500", "tA"), entry(2, "503", "tB")}},
		{Stream: labels("info"), Values: [][3]any{entry(3, "200", "tB"), entry(4, "200", "tB"), entry(5, "200", "tB"), entry(6, "200", "tB"), entry(7, "200", "tB")}},
	}
	push(t, url, tenant, streams)

	p := testProvider()
	t.Cleanup(func() { _ = p.Close() })
	source := testSource(t, models.LokiConnectionInfo{BaseURL: url, TenantID: tenant, Selector: `{test_run="` + runID + `"}`, Parser: "json"})
	start, end := now.Add(-time.Minute), now.Add(time.Minute)
	values := func(t *testing.T, field, query string, language models.QueryLanguage, limit int) *datasource.FieldValuesResult {
		t.Helper()
		result, err := p.GetFieldValues(t.Context(), source, datasource.FieldValuesRequest{
			FieldName: field, FieldType: "string", Language: language, QueryText: query, StartTime: start, EndTime: end, Limit: limit,
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	waitForRows(t, p, source, datasource.QueryRequest{RawQuery: `{test_run="` + runID + `"}`, StartTime: &start, EndTime: &end, Limit: 100}, 8)

	cases := []struct {
		name, field, query string
		language           models.QueryLanguage
		limit              int
		want               string
		label              bool
	}{
		{name: "a stream label", field: "level", limit: 10, want: "info=5 error=3", label: true},
		{name: "structured metadata", field: "trace", limit: 10, want: "tB=6 tA=2"},
		{name: "a parsed field", field: "code", limit: 10, want: "200=5 500=2 503=1"},
		{name: "the limit keeps the most frequent", field: "code", limit: 1, want: "200=5"},
		{name: "a LogchefQL filter", field: "code", query: `level="error"`, language: models.QueryLanguageLogchefQL, limit: 10, want: "500=2 503=1"},
		{name: "a native filter", field: "trace", query: `{test_run="` + runID + `"} | json | code="500" # a comment`, language: models.QueryLanguageLogQL, limit: 10, want: "tA=2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := values(t, tc.field, tc.query, tc.language, tc.limit)
			var got []string
			for _, v := range result.Values {
				got = append(got, fmt.Sprintf("%s=%d", v.Value, v.Count))
			}
			if strings.Join(got, " ") != tc.want || result.IsLowCardinality != tc.label || result.TotalDistinct != int64(len(got)) {
				t.Fatalf("got %v (low cardinality %v, distinct %d), want %s", got, result.IsLowCardinality, result.TotalDistinct, tc.want)
			}
		})
	}

	t.Run("a metric query is rejected", func(t *testing.T) {
		_, err := p.GetFieldValues(t.Context(), source, datasource.FieldValuesRequest{
			FieldName: "level", Language: models.QueryLanguageLogQL, QueryText: `rate({test_run="` + runID + `"}[1m])`, StartTime: start, EndTime: end, Limit: 10,
		})
		validation, ok := errors.AsType[*datasource.ValidationError](err)
		if !ok || !strings.HasPrefix(validation.Message, "Field values need a LogQL log query") {
			t.Fatalf("error %v", err)
		}
	})

	t.Run("the preload lists stream labels under the query's selector", func(t *testing.T) {
		result, err := p.GetAllFieldValues(t.Context(), source, datasource.AllFieldValuesRequest{
			Language: models.QueryLanguageLogchefQL, QueryText: `level="error"`, StartTime: start, EndTime: end, Limit: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for name, field := range result {
			var vs []string
			for _, v := range field.Values {
				vs = append(vs, v.Value)
			}
			got[name] = strings.Join(vs, ",")
		}
		want := map[string]string{"level": "error", "service_name": "it-fv", "test_run": runID}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
	})
}

func writeJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Error(err)
	}
}
