package loki

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// The integration tests run against a real Loki with auth_enabled, such as
// the dev stack's loki-local (dev/loki.yaml), when LOGCHEF_TEST_LOKI_URL is
// set. Each run writes under its own tenant and test_run label.
func integrationURL(t *testing.T) string {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("LOGCHEF_TEST_LOKI_URL"))
	if url == "" {
		t.Skip("LOGCHEF_TEST_LOKI_URL is not set; skipping Loki integration tests")
	}
	return url
}

func newRunID(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	return "it" + hex.EncodeToString(buf)
}

type pushStream struct {
	Stream map[string]string `json:"stream"`
	Values [][2]string       `json:"values"`
}

func push(t *testing.T, url, tenant string, streams []pushStream) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"streams": streams})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/loki/api/v1/push", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Scope-OrgID", tenant)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("push returned %d", resp.StatusCode)
	}
}

// waitForRows polls until Loki returns want rows for the query, because
// pushed entries reach the querier asynchronously.
func waitForRows(t *testing.T, p *Provider, source *models.Source, req datasource.QueryRequest, want int) *models.QueryResult {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		result, err := p.QueryLogs(t.Context(), source, req)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Logs) >= want || time.Now().After(deadline) {
			return result
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestIntegrationQueryLogs(t *testing.T) {
	url := integrationURL(t)
	runID := newRunID(t)
	tenant := "logchef-" + runID

	// 11,700 entries 100ms apart across three streams, and 300 more that share
	// one timestamp with 4,900 entries newer than it, so the first page of
	// 5,000 cuts the tie.
	now := time.Now().UTC().Truncate(time.Second)
	base := now.Add(-30 * time.Minute)
	const distinct, tie, newerThanTie = 11700, 300, 4900
	streams := make([]pushStream, 3)
	for i := range streams {
		streams[i].Stream = map[string]string{"service_name": "it-api", "test_run": runID, "pod": fmt.Sprintf("p%d", i)}
	}
	for i := range distinct {
		ts := base.Add(time.Duration(i) * 100 * time.Millisecond).UnixNano()
		streams[i%3].Values = append(streams[i%3].Values, [2]string{fmt.Sprint(ts), fmt.Sprintf(`{"n":%d,"msg":"entry %d"}`, i, i)})
	}
	tieTS := base.Add(time.Duration(distinct-1-newerThanTie)*100*time.Millisecond + 50*time.Millisecond).UnixNano()
	for i := range tie {
		streams[i%3].Values = append(streams[i%3].Values, [2]string{fmt.Sprint(tieTS), fmt.Sprintf(`{"tie":%d}`, i)})
	}
	push(t, url, tenant, streams)

	p := testProvider()
	t.Cleanup(func() { _ = p.Close() })
	source := testSource(t, models.LokiConnectionInfo{BaseURL: url, TenantID: tenant, Selector: `{test_run="` + runID + `"}`})
	start, end := base.Add(-time.Minute), now.Add(time.Minute)
	selector := `{test_run="` + runID + `"}`

	t.Run("12,000 entries page by time with no loss at the tie", func(t *testing.T) {
		result := waitForRows(t, p, source, datasource.QueryRequest{
			RawQuery: selector, StartTime: &start, EndTime: &end, Limit: 12000, MaxLimit: 100000, MaxResponseBytes: 64 << 20,
		}, distinct+tie)
		if len(result.Logs) != distinct+tie || result.Stats.Truncation != models.TruncationNone {
			t.Fatalf("got %d rows, truncation %q", len(result.Logs), result.Stats.Truncation)
		}
		assertNewestFirstDistinct(t, result.Logs)
		ties := 0
		for _, row := range result.Logs {
			if strings.HasPrefix(row["_msg"].(string), `{"tie"`) {
				ties++
			}
		}
		if ties != tie {
			t.Fatalf("%d tie entries, want %d", ties, tie)
		}
		if row := result.Logs[0]; row["service_name"] != "it-api" || row["test_run"] != runID {
			t.Fatalf("newest row %v", row)
		}
	})

	t.Run("a limit below the data is a row truncation", func(t *testing.T) {
		result, err := p.QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: selector, StartTime: &start, EndTime: &end, Limit: 7000, MaxLimit: 100000})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Logs) != 7000 || result.Stats.Truncation != models.TruncationRows {
			t.Fatalf("got %d rows, truncation %q", len(result.Logs), result.Stats.Truncation)
		}
	})

	t.Run("24 hours with a 1 second step returns rows", func(t *testing.T) {
		dayStart := now.Add(-24 * time.Hour)
		result, err := p.QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: selector, StartTime: &dayStart, EndTime: &end, Limit: 10, Step: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Logs) != 10 {
			t.Fatalf("got %d rows", len(result.Logs))
		}
	})

	for _, query := range []string{
		`({test_run="` + runID + `"})`,
		"# a comment first\n" + selector,
	} {
		t.Run("log query "+strings.ReplaceAll(query, "\n", " "), func(t *testing.T) {
			result, err := p.QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: query, StartTime: &start, EndTime: &end, Limit: 6000, MaxLimit: 100000})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Logs) != 6000 || result.Columns[0].Name != "_time" || result.Columns[1].Name != "_msg" {
				t.Fatalf("got %d rows, columns %v", len(result.Logs), result.Columns)
			}
			assertNewestFirstDistinct(t, result.Logs)
		})
	}

	t.Run("a metric query returns time and value rows", func(t *testing.T) {
		result, err := p.QueryLogs(t.Context(), source, datasource.QueryRequest{
			RawQuery: `sum by (pod) (count_over_time(` + selector + ` [5m]))`, StartTime: &start, EndTime: &end, Limit: 1000, Step: 5 * time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := columnNames(result.Columns); got != "value,time,pod" {
			t.Fatalf("columns %s", got)
		}
		if len(result.Logs) == 0 {
			t.Fatal("no metric rows")
		}
		for _, row := range result.Logs {
			if _, err := time.Parse(time.RFC3339Nano, row["time"].(string)); err != nil || row["value"] == "" {
				t.Fatalf("row %v", row)
			}
		}
	})

	t.Run("schema has labels and structured metadata", func(t *testing.T) {
		columns, err := p.GetSourceSchema(t.Context(), source)
		if err != nil {
			t.Fatal(err)
		}
		names := columnNames(columns)
		if !strings.HasPrefix(names, "_time,_msg,") || !strings.Contains(names, "pod") || !strings.Contains(names, "test_run") {
			t.Fatalf("schema %s", names)
		}
	})

	t.Run("inspection reads the version", func(t *testing.T) {
		inspection, err := p.InspectSource(t.Context(), source)
		if err != nil {
			t.Fatal(err)
		}
		if inspection.Details[1].Key != "version" || !strings.HasPrefix(inspection.Details[1].Value, "3.") {
			t.Fatalf("details %+v", inspection.Details)
		}
	})

	t.Run("a parse error is a validation error", func(t *testing.T) {
		_, err := p.QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: selector + " |= ", StartTime: &start, EndTime: &end})
		validation, ok := errors.AsType[*datasource.ValidationError](err)
		if !ok || !strings.Contains(validation.Message, "parse error") {
			t.Fatalf("error %v", err)
		}
	})

	t.Run("a missing tenant is an auth error", func(t *testing.T) {
		noTenant := testSource(t, models.LokiConnectionInfo{BaseURL: url, Selector: selector})
		noTenant.ID = 2
		_, err := p.QueryLogs(t.Context(), noTenant, datasource.QueryRequest{RawQuery: selector, StartTime: &start, EndTime: &end})
		upstream, ok := errors.AsType[*datasource.UpstreamError](err)
		if !ok || upstream.Kind != datasource.UpstreamAuth {
			t.Fatalf("error %v", err)
		}
	})

	t.Run("validation accepts the source", func(t *testing.T) {
		raw, _ := json.Marshal(models.LokiConnectionInfo{BaseURL: url, TenantID: tenant, Selector: selector})
		result, err := p.ValidateConnection(t.Context(), &models.ValidateConnectionRequest{SourceType: models.SourceTypeLoki, Connection: raw})
		if err != nil || !strings.Contains(result.Message, "Connection successful") {
			t.Fatalf("result %+v error %v", result, err)
		}
	})
}

// A LogchefQL query and the LogQL a person would write for it return the
// same rows.
func TestIntegrationLogchefQL(t *testing.T) {
	url := integrationURL(t)
	runID := newRunID(t)
	tenant := "logchef-" + runID

	now := time.Now().UTC().Truncate(time.Second)
	lines := map[string][]string{
		"error": {"upstream Gateway timeout", "gateway reset by peer", "db error", "bad gateway", "disk full"},
		"info":  {"gateway ok", "request served"},
	}
	var streams []pushStream
	for level, msgs := range lines {
		stream := pushStream{Stream: map[string]string{"service_name": "it-api", "test_run": runID, "level": level}}
		for i, msg := range msgs {
			stream.Values = append(stream.Values, [2]string{fmt.Sprint(now.Add(-time.Duration(i+1) * time.Second).UnixNano()), msg})
		}
		streams = append(streams, stream)
	}
	push(t, url, tenant, streams)

	p := testProvider()
	t.Cleanup(func() { _ = p.Close() })
	source := testSource(t, models.LokiConnectionInfo{BaseURL: url, TenantID: tenant, Selector: `{test_run="` + runID + `"}`})
	start, end := now.Add(-time.Minute), now.Add(time.Minute)
	waitForRows(t, p, source, datasource.QueryRequest{RawQuery: `{test_run="` + runID + `"}`, StartTime: &start, EndTime: &end, Limit: 100}, 7)

	columns, err := p.GetSourceSchema(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	source.Columns = columns
	compiled, err := p.CompileLogchefQL(t.Context(), source, datasource.LogchefQLCompileRequest{Query: `level="error" and _msg~"gateway"`})
	if err != nil {
		t.Fatal(err)
	}
	logchefQL, err := p.QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: compiled.Query, StartTime: &start, EndTime: &end, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	native, err := p.QueryLogs(t.Context(), source, datasource.QueryRequest{
		RawQuery: `{test_run="` + runID + `", level="error"} |~ "(?i)gateway"`, StartTime: &start, EndTime: &end, Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, want := rowKeys(logchefQL.Logs), rowKeys(native.Logs)
	if len(want) != 3 || strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("LogchefQL (%s) rows\n%s\nnative rows\n%s", compiled.Query, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func rowKeys(rows []map[string]any) []string {
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = fmt.Sprint(row["_time"], " ", row["level"], " ", row["_msg"])
	}
	return keys
}
