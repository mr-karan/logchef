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
		{name: "options prefix without existing filter", query: "options(concurrency=2) level:error", lookback: 3600, want: "options(concurrency=2) _time:1h level:error"},
		{name: "options prefix with existing filter left intact", query: "options(concurrency=2) _time:30m level:error", lookback: 3600, want: "options(concurrency=2) _time:30m level:error"},
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

func hasWarning(warnings []models.QueryWarning, code string) bool {
	for _, w := range warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}
