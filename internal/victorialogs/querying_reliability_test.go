package victorialogs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestResolveQueryLimit covers the ClickHouse-mirroring limit policy: a
// limit-less call falls back to DefaultLimit (not MaxLimit), any explicit limit
// is capped at MaxLimit, and the injected/capped flags drive warnings.
func TestResolveQueryLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                  string
		limit, def, max       int
		wantApplied           int
		wantAdded, wantCapped bool
	}{
		{name: "no limit uses default", limit: 0, def: 500, max: 10000, wantApplied: 500, wantAdded: true},
		{name: "no limit no default falls back", limit: 0, def: 0, max: 10000, wantApplied: defaultQueryLimit, wantAdded: true},
		{name: "explicit within max", limit: 250, def: 500, max: 10000, wantApplied: 250},
		{name: "explicit capped at max", limit: 250, def: 500, max: 100, wantApplied: 100, wantCapped: true},
		{name: "default above max capped", limit: 0, def: 5000, max: 100, wantApplied: 100, wantAdded: true, wantCapped: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			applied, added, capped := resolveQueryLimit(tc.limit, tc.def, tc.max)
			if applied != tc.wantApplied || added != tc.wantAdded || capped != tc.wantCapped {
				t.Fatalf("resolveQueryLimit(%d,%d,%d) = (%d,%t,%t), want (%d,%t,%t)",
					tc.limit, tc.def, tc.max, applied, added, capped, tc.wantApplied, tc.wantAdded, tc.wantCapped)
			}
		})
	}
}

// TestApplyAlertLookback verifies the lookback filter is prepended only when the
// stored query does not already scope _time itself (LogsQL ANDs _time filters,
// so a double _time silently narrows the window).
func TestApplyAlertLookback(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		query    string
		lookback int
		want     string
	}{
		{name: "no existing time filter prepends", query: "level:error", lookback: 300, want: "_time:5m level:error"},
		{name: "existing _time filter is left intact", query: "_time:1h level:error", lookback: 300, want: "_time:1h level:error"},
		{name: "existing _time filter mid-query left intact", query: "level:error _time:2h", lookback: 300, want: "level:error _time:2h"},
		{name: "field ending in _time is not treated as _time filter", query: "custom_time:foo", lookback: 60, want: "_time:1m custom_time:foo"},
		{name: "compound field with dot ending in _time is not a _time filter", query: "custom._time:foo", lookback: 60, want: "_time:1m custom._time:foo"},
		{name: "compound field with dash ending in _time is not a _time filter", query: "custom-_time:foo", lookback: 60, want: "_time:1m custom-_time:foo"},
		{name: "options prefix without existing filter", query: "options(concurrency=2) level:error", lookback: 3600, want: "options(concurrency=2) _time:1h level:error"},
		{name: "options prefix with existing filter left intact", query: "options(concurrency=2) _time:30m level:error", lookback: 3600, want: "options(concurrency=2) _time:30m level:error"},
		{name: "options with nested parens split at matching paren", query: `options(global_filter=(service:="api")) level:error`, lookback: 3600, want: `options(global_filter=(service:="api")) _time:1h level:error`},
		{name: "negated _time under NOT still gets lookback", query: "NOT _time:1h", lookback: 300, want: "_time:5m NOT _time:1h"},
		{name: "top-level OR with _time is wrapped and bounded", query: "_time:5m OR level:critical", lookback: 300, want: "_time:5m (_time:5m OR level:critical)"},
		{name: "top-level OR without _time is wrapped and bounded", query: "level:error OR level:warn", lookback: 300, want: "_time:5m (level:error OR level:warn)"},
		{name: "OR nested in parens with top-level _time is left intact", query: "_time:2h AND (a OR b)", lookback: 300, want: "_time:2h AND (a OR b)"},
		{name: "_time only inside a parenthesized OR group is not bounding", query: "(level:error OR _time:5m)", lookback: 300, want: "_time:5m (level:error OR _time:5m)"},
		{name: "zero lookback returns trimmed", query: "  level:error  ", lookback: 0, want: "level:error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := applyAlertLookback(tc.query, tc.lookback); got != tc.want {
				t.Fatalf("applyAlertLookback(%q, %d) = %q, want %q", tc.query, tc.lookback, got, tc.want)
			}
		})
	}
}

// TestQueryLogsEnforcesResponseByteBudget proves QueryLogs stops decoding once
// the MaxResponseBytes budget is exceeded, so a query within the row limit but
// with large per-row payloads cannot buffer unbounded (the OOM class #93).
func TestQueryLogsEnforcesResponseByteBudget(t *testing.T) {
	t.Parallel()

	bigMsg := make([]byte, 400)
	for i := range bigMsg {
		bigMsg[i] = 'x'
	}
	body := ""
	for i := 0; i < 5; i++ {
		body += `{"_time":"2026-04-08T10:0` + string(rune('0'+i)) + `:00Z","_msg":"` + string(bigMsg) + `"}` + "\n"
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	result, err := provider.QueryLogs(context.Background(), source, datasource.QueryRequest{
		RawQuery:         "*",
		Limit:            1000,
		MaxLimit:         1000,
		MaxResponseBytes: 900, // ~2 rows of ~430 bytes fit
	})
	if err != nil {
		t.Fatalf("QueryLogs returned error: %v", err)
	}
	if len(result.Logs) == 0 || len(result.Logs) >= 5 {
		t.Fatalf("expected byte budget to truncate rows, got %d of 5", len(result.Logs))
	}
	if !result.Stats.Truncated || result.Stats.TruncatedReason != "byte_limit" {
		t.Fatalf("expected byte_limit truncation, got truncated=%t reason=%q", result.Stats.Truncated, result.Stats.TruncatedReason)
	}
}

// TestQueryLogsDefaultLimitAndWarnings verifies a limit-less call sends the
// DefaultLimit (not the 100k max) and surfaces a LIMIT_APPLIED warning; a
// capped call surfaces LIMIT_CAPPED.
func TestQueryLogsDefaultLimitAndWarnings(t *testing.T) {
	t.Parallel()

	var gotLimit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotLimit = r.Form.Get("limit")
		_, _ = w.Write([]byte(`{"_time":"2026-04-08T10:01:00Z","_msg":"ok"}` + "\n"))
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	// No explicit limit -> default is applied.
	result, err := provider.QueryLogs(context.Background(), source, datasource.QueryRequest{
		RawQuery:     "*",
		DefaultLimit: 500,
		MaxLimit:     100000,
	})
	if err != nil {
		t.Fatalf("QueryLogs returned error: %v", err)
	}
	if gotLimit != "500" {
		t.Fatalf("expected default limit 500 sent to VL, got %q", gotLimit)
	}
	if !hasWarning(result.Warnings, "LIMIT_APPLIED") {
		t.Fatalf("expected LIMIT_APPLIED warning, got %+v", result.Warnings)
	}

	// Explicit limit above max -> capped.
	result, err = provider.QueryLogs(context.Background(), source, datasource.QueryRequest{
		RawQuery:     "*",
		Limit:        250,
		DefaultLimit: 500,
		MaxLimit:     100,
	})
	if err != nil {
		t.Fatalf("QueryLogs (capped) returned error: %v", err)
	}
	if gotLimit != "100" {
		t.Fatalf("expected capped limit 100 sent to VL, got %q", gotLimit)
	}
	if !hasWarning(result.Warnings, "LIMIT_CAPPED") {
		t.Fatalf("expected LIMIT_CAPPED warning, got %+v", result.Warnings)
	}
}

// TestHistogramGroupByCapsSeriesAndSortsGlobally verifies the group-by path
// sends a fields_limit series cap (#99) and returns globally time-sorted
// buckets (#100).
func TestHistogramGroupByCapsSeriesAndSortsGlobally(t *testing.T) {
	t.Parallel()

	var gotFieldsLimit, gotField string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotFieldsLimit = r.Form.Get("fields_limit")
		gotField = r.Form.Get("field")
		// Two series with buckets deliberately out of global time order.
		_, _ = w.Write([]byte(`{"hits":[` +
			`{"fields":{"service":"x"},"timestamps":["2026-04-08T10:02:00Z"],"values":[5]},` +
			`{"fields":{"service":"y"},"timestamps":["2026-04-08T10:01:00Z"],"values":[3]}` +
			`]}`))
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	result, err := provider.Histogram(context.Background(), source, datasource.HistogramRequest{
		Query:   "*",
		Window:  "1m",
		GroupBy: "service",
	})
	if err != nil {
		t.Fatalf("Histogram returned error: %v", err)
	}
	if gotField != "service" {
		t.Fatalf("expected field=service, got %q", gotField)
	}
	if gotFieldsLimit != "10" {
		t.Fatalf("expected fields_limit=10 (series cap), got %q", gotFieldsLimit)
	}
	if len(result.Data) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(result.Data))
	}
	if result.Data[0].Bucket.After(result.Data[1].Bucket) {
		t.Fatalf("expected globally time-sorted buckets, got %v then %v", result.Data[0].Bucket, result.Data[1].Bucket)
	}
}

// TestQueryLogsSingleOversizedRowBounded proves a single row larger than the
// whole budget is refused (not materialized): the LimitReader caps the bytes
// pulled off the socket and the per-line pre-decode check trips the budget, so
// the row is never Unmarshaled. This is the #1 fix — the old code decoded the
// full row before checking the budget.
func TestQueryLogsSingleOversizedRowBounded(t *testing.T) {
	t.Parallel()

	huge := make([]byte, 4000)
	for i := range huge {
		huge[i] = 'x'
	}
	body := `{"_time":"2026-04-08T10:01:00Z","_msg":"` + string(huge) + `"}` + "\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	result, err := provider.QueryLogs(context.Background(), source, datasource.QueryRequest{
		RawQuery:         "*",
		Limit:            1000,
		MaxLimit:         1000,
		MaxResponseBytes: 500, // far smaller than the single ~4KB row
	})
	if err != nil {
		t.Fatalf("QueryLogs returned error: %v", err)
	}
	if len(result.Logs) != 0 {
		t.Fatalf("expected the oversized row to be refused, got %d rows", len(result.Logs))
	}
	if !result.Stats.Truncated || result.Stats.TruncatedReason != "byte_limit" {
		t.Fatalf("expected byte_limit truncation, got truncated=%t reason=%q", result.Stats.Truncated, result.Stats.TruncatedReason)
	}
	if result.Stats.BytesReturned > 500 {
		t.Fatalf("expected accounted bytes within budget, got %d", result.Stats.BytesReturned)
	}
}

// TestQueryLogsCountsBytesWhenUnbounded proves BytesReturned is populated even
// when no byte budget is set (#8): the old code only incremented it when
// MaxResponseBytes > 0, reporting 0 for unbounded queries.
func TestQueryLogsCountsBytesWhenUnbounded(t *testing.T) {
	t.Parallel()

	body := `{"_time":"2026-04-08T10:01:00Z","_msg":"a"}` + "\n" +
		`{"_time":"2026-04-08T10:02:00Z","_msg":"b"}` + "\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	result, err := provider.QueryLogs(context.Background(), source, datasource.QueryRequest{
		RawQuery: "*",
		Limit:    1000,
		MaxLimit: 1000,
		// MaxResponseBytes intentionally 0 (unbounded).
	})
	if err != nil {
		t.Fatalf("QueryLogs returned error: %v", err)
	}
	if len(result.Logs) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result.Logs))
	}
	if result.Stats.BytesReturned != len(body) {
		t.Fatalf("expected BytesReturned=%d (raw wire bytes) for unbounded query, got %d", len(body), result.Stats.BytesReturned)
	}
	if result.Stats.Truncated {
		t.Fatalf("did not expect truncation for unbounded query")
	}
}

// TestHistogramCatchAllSeriesLabeledOther verifies VL's catch-all aggregate
// series (empty `fields`, key "{}") is labeled as the "other" bucket and drives
// the truncation notice — never rendered as a genuine empty-value group (#2).
func TestHistogramCatchAllSeriesLabeledOther(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hits":[` +
			`{"fields":{"service":"x"},"timestamps":["2026-04-08T10:01:00Z"],"values":[5]},` +
			`{"fields":{},"timestamps":["2026-04-08T10:01:00Z"],"values":[7]}` +
			`]}`))
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	result, err := provider.Histogram(context.Background(), source, datasource.HistogramRequest{
		Query:   "*",
		Window:  "1m",
		GroupBy: "service",
	})
	if err != nil {
		t.Fatalf("Histogram returned error: %v", err)
	}
	byGroup := map[string]int{}
	for _, b := range result.Data {
		byGroup[b.GroupValue] += b.LogCount
	}
	if byGroup["x"] != 5 {
		t.Fatalf("expected real series x=5, got %d", byGroup["x"])
	}
	if byGroup[histogramOtherSeriesLabel] != 7 {
		t.Fatalf("expected catch-all labeled %q=7, got %d", histogramOtherSeriesLabel, byGroup[histogramOtherSeriesLabel])
	}
	if _, ok := byGroup[""]; ok {
		t.Fatalf("catch-all must not be labeled as an empty-value group")
	}
	if result.Notice == "" {
		t.Fatalf("expected a truncation notice when the catch-all series is present")
	}
	for _, bucket := range result.Data {
		switch bucket.GroupValue {
		case "x":
			if bucket.IsOther {
				t.Fatal("real group must not be marked as Other")
			}
		case histogramOtherSeriesLabel:
			if !bucket.IsOther {
				t.Fatal("catch-all group must be marked as Other")
			}
		}
	}
}

// TestHistogramEmptyValueGroupNotTruncated verifies a genuine empty-value group
// (fields present, value "") is preserved as "" and does NOT trip the false
// truncation notice (#2, points b and c).
func TestHistogramEmptyValueGroupNotTruncated(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"hits":[` +
			`{"fields":{"service":"x"},"timestamps":["2026-04-08T10:01:00Z"],"values":[5]},` +
			`{"fields":{"service":""},"timestamps":["2026-04-08T10:01:00Z"],"values":[3]},` +
			`{"fields":{"service":"Other"},"timestamps":["2026-04-08T10:01:00Z"],"values":[2]},` +
			`{"fields":{"service":"__other__"},"timestamps":["2026-04-08T10:01:00Z"],"values":[1]}` +
			`]}`))
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	result, err := provider.Histogram(context.Background(), source, datasource.HistogramRequest{
		Query:   "*",
		Window:  "1m",
		GroupBy: "service",
	})
	if err != nil {
		t.Fatalf("Histogram returned error: %v", err)
	}
	if result.Notice != "" {
		t.Fatalf("did not expect a truncation notice, got %q", result.Notice)
	}
	sawEmpty := false
	for _, b := range result.Data {
		if b.IsOther {
			t.Fatalf("genuine group %q must not be marked as Other", b.GroupValue)
		}
		if b.GroupValue == "" {
			sawEmpty = true
		}
	}
	if !sawEmpty {
		t.Fatalf("expected the empty-value group to be preserved as \"\"")
	}
}

// TestEvaluateAlertColumnsIncludeLabels verifies the alert result schema
// declares the metric-label columns, not just "value" (#6).
func TestEvaluateAlertColumnsIncludeLabels(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[` +
			`{"metric":{"service":"api","level":"error"},"value":[1712570460,"12"]}` +
			`]}}`))
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	result, err := provider.EvaluateAlert(context.Background(), source, datasource.AlertQueryRequest{
		Query: "* | stats count() as value",
	})
	if err != nil {
		t.Fatalf("EvaluateAlert returned error: %v", err)
	}
	names := map[string]string{}
	for _, c := range result.Columns {
		names[c.Name] = c.Type
	}
	for _, want := range []string{"service", "level", "value"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("expected column %q in %+v", want, result.Columns)
		}
	}
	if names["value"] != "Float64" {
		t.Fatalf("expected value column Float64, got %q", names["value"])
	}
}

// TestJoinBaseURLPreservesEncodedSlash proves an encoded %2F in a configured
// base path survives the join instead of being decoded to a literal "/" (#9).
func TestJoinBaseURLPreservesEncodedSlash(t *testing.T) {
	t.Parallel()

	got, err := joinBaseURL("https://vl.example.com/prefix%2Fsub", "/select/logsql/query")
	if err != nil {
		t.Fatalf("joinBaseURL error: %v", err)
	}
	want := "https://vl.example.com/prefix%2Fsub/select/logsql/query"
	if got != want {
		t.Fatalf("joinBaseURL = %q, want %q", got, want)
	}
}

// TestSplitTopLevelPipesQuoteStyles proves single- and backtick-quoted pipes are
// not treated as stage boundaries (#11), so appendDefaultSort still recognizes a
// pipe-free filter and appends the sort.
func TestSplitTopLevelPipesQuoteStyles(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		query string
		want  int
	}{
		{name: "single-quoted pipe not a boundary", query: `_msg:'a|b'`, want: 1},
		{name: "backtick-quoted pipe not a boundary", query: "_msg:`a|b`", want: 1},
		{name: "double-quoted pipe not a boundary", query: `_msg:"a|b"`, want: 1},
		{name: "real top-level pipe splits", query: `level:error | stats count()`, want: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := len(splitTopLevelPipes(tc.query)); got != tc.want {
				t.Fatalf("splitTopLevelPipes(%q) produced %d stages, want %d", tc.query, got, tc.want)
			}
		})
	}
}

// TestAppendDefaultSortProjectionSpellings proves the `keep` alias and case
// variants of `fields` are recognized as projections, so the default sort is
// inserted before them rather than the query being left unsorted (#12).
func TestAppendDefaultSortProjectionSpellings(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		query string
		want  string
	}{
		{name: "keep alias projection", query: `level:error | keep _time, _msg`, want: `level:error | sort by (_time desc) | keep _time, _msg`},
		{name: "uppercase FIELDS projection", query: `level:error | FIELDS _time`, want: `level:error | sort by (_time desc) | FIELDS _time`},
		{name: "single-quoted pipe stays pipe-free", query: `_msg:'a|b'`, want: `_msg:'a|b' | sort by (_time desc)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := appendDefaultSort(tc.query); got != tc.want {
				t.Fatalf("appendDefaultSort(%q) = %q, want %q", tc.query, got, tc.want)
			}
		})
	}
}

func hasWarning(warnings []models.QueryWarning, code string) bool {
	for _, w := range warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}
