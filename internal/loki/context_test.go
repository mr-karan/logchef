package loki

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestContextRequestBounds(t *testing.T) {
	conn := models.LokiConnectionInfo{Limits: &models.LokiLimits{MaxEntriesPerQuery: 100}}
	labels := map[string]string{"app": "api"}
	cases := []struct {
		name string
		req  datasource.LogContextRequest
		ok   bool
	}{
		{"within the page size", datasource.LogContextRequest{StreamLabels: labels, BeforeLimit: 50, AfterLimit: 50, BeforeOffset: 50}, true},
		{"offset plus limit over the page size", datasource.LogContextRequest{StreamLabels: labels, BeforeLimit: 50, AfterLimit: 50, AfterOffset: 51}, false},
		{"no stream labels", datasource.LogContextRequest{BeforeLimit: 10, AfterLimit: 10}, false},
		{"a label name that is not one", datasource.LogContextRequest{StreamLabels: map[string]string{`a"} or {b`: "x"}, BeforeLimit: 10, AfterLimit: 10}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkContextRequest(conn, tc.req)
			if (err == nil) != tc.ok || (err != nil && !datasource.IsValidationError(err)) {
				t.Fatalf("error %v", err)
			}
		})
	}
}

func TestExactSelector(t *testing.T) {
	got := exactSelector(map[string]string{"pod": "a", "app": `say "hi"`}, map[string]struct{}{"zone": {}, "extra": {}})
	if want := `{app="say \"hi\"", pod="a", extra="", zone=""}`; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestIntegrationLogContext(t *testing.T) {
	url := integrationURL(t)
	runID := newRunID(t)
	tenant := "logchef-" + runID

	// Streams a and b interleave one second apart. a+ has a's labels plus
	// one more. c is the only stream with team="c" and also has zone.
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	labels := func(extra ...string) map[string]string {
		l := map[string]string{"service_name": "it-ctx", "test_run": runID}
		for i := 0; i < len(extra); i += 2 {
			l[extra[i]] = extra[i+1]
		}
		return l
	}
	stream := func(l map[string]string, prefix string, first, n int) pushStream {
		s := pushStream{Stream: l}
		for i := range n {
			s.Values = append(s.Values, [2]string{fmt.Sprint(base.Add(time.Duration(first+2*i) * time.Second).UnixNano()), fmt.Sprintf("%s%d", prefix, i)})
		}
		return s
	}
	push(t, url, tenant, []pushStream{
		stream(labels("pod", "a"), "a", 0, 10),
		stream(labels("pod", "b"), "b", 1, 10),
		stream(labels("pod", "a", "extra", "x"), "ax", 5, 3),
		stream(labels("team", "c", "zone", "z"), "c", 0, 2),
	})

	p := testProvider()
	t.Cleanup(func() { _ = p.Close() })
	selector := `{test_run="` + runID + `"}`
	source := testSource(t, models.LokiConnectionInfo{BaseURL: url, TenantID: tenant, Selector: selector})
	start, end := base.Add(-time.Minute), base.Add(time.Minute)
	waitForRows(t, p, source, datasource.QueryRequest{RawQuery: selector, StartTime: &start, EndTime: &end, Limit: 100}, 25)
	columns, err := p.GetSourceSchema(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	source.Columns = columns

	// The target is a5, ten seconds after base. A LogchefQL query with a
	// select pipe returns it with every stream label.
	compiled, err := p.CompileLogchefQL(t.Context(), source, datasource.LogchefQLCompileRequest{Query: `pod="a" and _msg="a5" | _msg`})
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: compiled.Query, StartTime: &start, EndTime: &end, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var target map[string]any
	for _, row := range result.Logs {
		if row["_msg"] == "a5" && row["extra"] == nil {
			target = row
		}
	}
	if target == nil {
		t.Fatalf("no a5 row in %v (query %s)", result.Logs, compiled.Query)
	}
	rowLabels := streamLabelsOf(target, columns)
	targetTime, err := time.Parse(time.RFC3339Nano, target["_time"].(string))
	if err != nil {
		t.Fatal(err)
	}

	contextOf := func(t *testing.T, streamLabels map[string]string, req datasource.LogContextRequest) (*models.LogContextResponse, error) {
		t.Helper()
		req.StreamLabels = streamLabels
		req.TargetTime = &targetTime
		return p.GetLogContext(t.Context(), source, req)
	}
	messages := func(rows []map[string]any) string {
		var msgs []string
		for _, row := range rows {
			msgs = append(msgs, row["_msg"].(string))
		}
		return strings.Join(msgs, " ")
	}

	t.Run("context reads only the target's stream, in order", func(t *testing.T) {
		resp, err := contextOf(t, rowLabels, datasource.LogContextRequest{BeforeLimit: 3, AfterLimit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if got := messages(resp.BeforeLogs) + " | " + messages(resp.AfterLogs); got != "a3 a4 a5 | a6 a7 a8" {
			t.Fatalf("context %s", got)
		}
	})

	t.Run("offsets and the excluded boundary page past what was read", func(t *testing.T) {
		resp, err := contextOf(t, rowLabels, datasource.LogContextRequest{BeforeLimit: 2, AfterLimit: 2, BeforeOffset: 1, AfterOffset: 1})
		if err != nil {
			t.Fatal(err)
		}
		if got := messages(resp.BeforeLogs) + " | " + messages(resp.AfterLogs); got != "a3 a4 | a7 a8" {
			t.Fatalf("context %s", got)
		}
		resp, err = contextOf(t, rowLabels, datasource.LogContextRequest{BeforeLimit: 2, AfterLimit: 20, ExcludeBoundary: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := messages(resp.BeforeLogs) + " | " + messages(resp.AfterLogs); got != "a3 a4 | a6 a7 a8 a9" {
			t.Fatalf("context %s", got)
		}
	})

	notOne := func(t *testing.T, streamLabels map[string]string) {
		t.Helper()
		_, err := contextOf(t, streamLabels, datasource.LogContextRequest{BeforeLimit: 3, AfterLimit: 3})
		validation, ok := errors.AsType[*datasource.ValidationError](err)
		if !ok || validation.Message != notOneStream {
			t.Fatalf("error %v", err)
		}
	}
	t.Run("a missing label is rejected", func(t *testing.T) {
		notOne(t, labels())
	})
	t.Run("a missing label is rejected when one stream matches", func(t *testing.T) {
		notOne(t, labels("team", "c"))
	})
	t.Run("an extra label is rejected", func(t *testing.T) {
		notOne(t, labels("pod", "a", "zone", "z"))
	})
	t.Run("a label that label_format rewrote is rejected", func(t *testing.T) {
		rewritten, err := p.QueryLogs(t.Context(), source, datasource.QueryRequest{
			RawQuery: `{test_run="` + runID + `", pod="a", extra=""} |= "a5" | label_format pod="z"`, StartTime: &start, EndTime: &end, Limit: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(rewritten.Logs) != 1 {
			t.Fatalf("rows %v", rewritten.Logs)
		}
		notOne(t, streamLabelsOf(rewritten.Logs[0], columns))
	})
}

// streamLabelsOf is what the Explorer sends: the row's value for each schema
// field of type label.
func streamLabelsOf(row map[string]any, columns []models.ColumnInfo) map[string]string {
	labels := map[string]string{}
	for _, column := range columns {
		if value, ok := row[column.Name].(string); ok && column.Type == labelColumnType {
			labels[column.Name] = value
		}
	}
	return labels
}
