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

// histogramCounts runs a histogram and maps each bucket's start, as an offset
// from base, to its count. A grouped bucket is keyed by offset and group.
func histogramCounts(t *testing.T, p *Provider, source *models.Source, req datasource.HistogramRequest, base time.Time) map[string]int {
	t.Helper()
	result, err := p.Histogram(t.Context(), source, req)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, b := range result.Data {
		key := b.Bucket.Sub(base).String()
		if req.GroupBy != "" {
			key += " " + b.GroupValue
			if b.IsOther {
				key += "*"
			}
		}
		counts[key] += b.LogCount
	}
	return counts
}

func assertCounts(t *testing.T, got, want map[string]int) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("buckets\n got %v\nwant %v", got, want)
	}
}

// The histogram runs through Loki's query frontend with
// split_queries_by_interval set (dev/loki.yaml), so every request passes the
// splitter. Windows are open at the start and closed at the end.
func TestIntegrationHistogram(t *testing.T) {
	url := integrationURL(t)
	runID := newRunID(t)
	tenant := "logchef-" + runID

	base := time.Now().UTC().Truncate(time.Minute).Add(-20 * time.Minute)
	at := func(ms int64) time.Time { return base.Add(time.Duration(ms) * time.Millisecond) }
	entries := func(run string, offsets ...int64) pushStream {
		s := pushStream{Stream: map[string]string{"service_name": "it-hist", "test_run": run}}
		for _, ms := range offsets {
			s.Values = append(s.Values, [2]string{fmt.Sprint(at(ms).UnixNano()), fmt.Sprintf("entry at %dms", ms)})
		}
		return s
	}
	// [start, end) is [15s, 3m20s) with a one minute window.
	const start, end = int64(15_000), int64(200_000)
	edges := entries(runID+"-edges",
		start-1, start, 30_000, 60_000, 90_000, 120_000, 150_000, 190_000, end, end+1)
	inner := entries(runID+"-inner", start-1, 30_000, 90_000, 150_000, 190_000, end+1)
	single := entries(runID+"-single", 5_000, 30_000, 60_000)
	var groups []pushStream
	for i := range 12 {
		s := entries(runID+"-groups", 30_000)
		s.Stream["grp"] = fmt.Sprintf("g%02d", i)
		s.Values = nil
		for j := 0; j <= i; j++ {
			s.Values = append(s.Values, [2]string{fmt.Sprint(at(30_000 + int64(j)).UnixNano()), "grouped"})
		}
		groups = append(groups, s)
	}
	push(t, url, tenant, append([]pushStream{edges, inner, single}, groups...))

	p := testProvider()
	t.Cleanup(func() { _ = p.Close() })
	source := testSource(t, models.LokiConnectionInfo{BaseURL: url, TenantID: tenant, Selector: `{service_name="it-hist"}`})
	selector := func(run string) string { return `{test_run="` + run + `"}` }
	wideStart, wideEnd := at(-60_000), at(300_000)
	waitForRows(t, p, source, datasource.QueryRequest{RawQuery: `{service_name="it-hist"}`, StartTime: &wideStart, EndTime: &wideEnd, Limit: 1000}, 10+6+3+78)
	request := func(query string, from, to int64) datasource.HistogramRequest {
		s, e := at(from), at(to)
		return datasource.HistogramRequest{Query: query, StartTime: &s, EndTime: &e, Window: "1m"}
	}

	t.Run("partial edges, inner boundaries and the range ends", func(t *testing.T) {
		got := histogramCounts(t, p, source, request(selector(runID+"-edges"), start, end), base)
		// Not counted: start - 1ms, start, end + 1ms. 60s and 120s fall in the
		// bucket before them, and end in the last bucket.
		assertCounts(t, got, map[string]int{"0s": 2, "1m0s": 2, "2m0s": 1, "3m0s": 2})
	})

	t.Run("without boundary entries the buckets sum to the search", func(t *testing.T) {
		got := histogramCounts(t, p, source, request(selector(runID+"-inner"), start, end), base)
		sum := 0
		for _, n := range got {
			sum += n
		}
		s, e := at(start), at(end)
		rows, err := p.QueryLogs(t.Context(), source, datasource.QueryRequest{RawQuery: selector(runID + "-inner"), StartTime: &s, EndTime: &e, Limit: 100})
		if err != nil {
			t.Fatal(err)
		}
		if sum != 4 || len(rows.Logs) != sum {
			t.Fatalf("histogram sum %d (%v), search rows %d", sum, got, len(rows.Logs))
		}
	})

	t.Run("an aligned range with a trailing comment", func(t *testing.T) {
		got := histogramCounts(t, p, source, request(selector(runID+"-edges")+" # every entry", 0, 180_000), base)
		assertCounts(t, got, map[string]int{"0s": 4, "1m0s": 2, "2m0s": 1})
	})

	t.Run("a range inside one bucket", func(t *testing.T) {
		got := histogramCounts(t, p, source, request(selector(runID+"-single"), 10_000, 50_000), base)
		assertCounts(t, got, map[string]int{"0s": 1})
	})

	t.Run("a group-by over 12 values returns the top 10 and Other", func(t *testing.T) {
		req := request(selector(runID+"-groups"), 0, 60_000)
		req.GroupBy = "grp"
		result, err := p.Histogram(t.Context(), source, req)
		if err != nil {
			t.Fatal(err)
		}
		got := histogramCounts(t, p, source, req, base)
		want := map[string]int{"0s Other*": 1 + 2}
		for i := 2; i < 12; i++ {
			want[fmt.Sprintf("0s g%02d", i)] = i + 1
		}
		assertCounts(t, got, want)
		if !strings.Contains(result.Notice, `top 10 series by "grp"`) {
			t.Fatalf("notice %q", result.Notice)
		}
	})

	t.Run("a metric query is rejected with Loki's message", func(t *testing.T) {
		_, err := p.Histogram(t.Context(), source, request(`count_over_time(`+selector(runID+"-edges")+` [1m])`, start, end))
		validation, ok := errors.AsType[*datasource.ValidationError](err)
		if !ok || !strings.HasPrefix(validation.Message, "The histogram needs a LogQL log query: parse error") {
			t.Fatalf("error %v", err)
		}
	})
}
