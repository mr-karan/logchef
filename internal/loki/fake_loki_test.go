package loki

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// fakeEntry is one log line held by fakeLoki.
type fakeEntry struct {
	ts     int64
	labels map[string]string
	line   string
}

type rangeCall struct {
	start, end int64
	limit      int
	direction  string
	step       string
	query      string
}

// fakeLoki serves query_range over a fixed set of entries with Loki's log
// semantics: [start, end), newest first (oldest first for direction=forward),
// at most limit entries. At a tie it
// returns the entries in the reverse of Logchef's order, so a pager that
// assumed one order at a boundary loses entries.
type fakeLoki struct {
	t       *testing.T
	mu      sync.Mutex
	entries []fakeEntry
	calls   []rangeCall
	// maxEntries is Loki's max_entries_limit_per_query; 0 means no limit.
	maxEntries int
	// handle replaces query_range when set.
	handle func(w http.ResponseWriter, r *http.Request)
}

func newFakeLoki(t *testing.T) (*fakeLoki, *httptest.Server) {
	t.Helper()
	f := &fakeLoki{t: t}
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	return f, server
}

func (f *fakeLoki) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path != "/loki/api/v1/query_range" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	call := rangeCall{direction: q.Get("direction"), step: q.Get("step"), query: q.Get("query")}
	call.start, _ = strconv.ParseInt(q.Get("start"), 10, 64)
	call.end, _ = strconv.ParseInt(q.Get("end"), 10, 64)
	call.limit, _ = strconv.Atoi(q.Get("limit"))
	f.calls = append(f.calls, call)
	if f.handle != nil {
		f.handle(w, r)
		return
	}
	if f.maxEntries > 0 && call.limit > f.maxEntries {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, "max entries limit per query exceeded, limit > max_entries_limit_per_query (%d > %d)", call.limit, f.maxEntries)
		return
	}

	var selected []fakeEntry
	for _, e := range f.entries {
		if e.ts >= call.start && e.ts < call.end {
			selected = append(selected, e)
		}
	}
	forward := call.direction == "forward"
	slices.SortFunc(selected, func(a, b fakeEntry) int {
		if a.ts != b.ts {
			if (a.ts > b.ts) != forward {
				return -1
			}
			return 1
		}
		if c := strings.Compare(labelString(b.labels), labelString(a.labels)); c != 0 {
			return c
		}
		return strings.Compare(b.line, a.line)
	})
	if len(selected) > call.limit {
		selected = selected[:call.limit]
	}
	writeStreams(w, selected)
}

func writeStreams(w http.ResponseWriter, entries []fakeEntry) {
	var order []string
	streams := map[string]*lokiStream{}
	for _, e := range entries {
		key := labelString(e.labels)
		s, ok := streams[key]
		if !ok {
			s = &lokiStream{Stream: e.labels}
			streams[key] = s
			order = append(order, key)
		}
		s.Values = append(s.Values, [2]string{strconv.FormatInt(e.ts, 10), e.line})
	}
	result := make([]lokiStream, 0, len(order))
	for _, key := range order {
		result = append(result, *streams[key])
	}
	writeResult(w, "streams", result)
}

func writeResult(w http.ResponseWriter, resultType string, result any) {
	body := map[string]any{
		"status": "success",
		"data": map[string]any{
			"resultType": resultType,
			"result":     result,
			"stats":      map[string]any{"summary": map[string]any{"execTime": 0.002, "totalLinesProcessed": 7, "totalBytesProcessed": 70}},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func (f *fakeLoki) rangeCalls() []rangeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func testProvider() *Provider {
	return NewProvider(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func testSource(t *testing.T, conn models.LokiConnectionInfo) *models.Source {
	t.Helper()
	if conn.Selector == "" {
		conn.Selector = `{service_name=~".+"}`
	}
	raw, err := json.Marshal(conn)
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{ID: 1, Name: "loki", SourceType: models.SourceTypeLoki, MetaTSField: "_time", ConnectionConfig: raw}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	return source
}

func timeRange(from time.Time, d time.Duration) (start, end *time.Time) {
	to := from.Add(d)
	return &from, &to
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
