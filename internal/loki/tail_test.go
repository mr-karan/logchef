package loki

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// tailCollector records every emitted row by its _time and _msg.
type tailCollector struct {
	mu    sync.Mutex
	count map[string]int
	total int
}

func newTailCollector() *tailCollector { return &tailCollector{count: map[string]int{}} }

func (c *tailCollector) emit(rows []map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, row := range rows {
		c.count[fmt.Sprint(row["_time"], " ", row["_msg"])]++
		c.total++
	}
	return nil
}

func (c *tailCollector) seen(line string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for key, count := range c.count {
		if strings.HasSuffix(key, " "+line) {
			n += count
		}
	}
	return n
}

// assertEachOnce fails when any row was emitted more than once or the total
// differs from want.
func (c *tailCollector) assertEachOnce(t *testing.T, want int) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, count := range c.count {
		if count != 1 {
			t.Fatalf("%q emitted %d times", key, count)
		}
	}
	if c.total != want {
		t.Fatalf("emitted %d rows, want %d", c.total, want)
	}
}

// fakeTailer returns a tailer that starts at start and reads the fake Loki
// through the provider's real request path.
func fakeTailer(t *testing.T, server *httptest.Server, start time.Time, c *tailCollector, log *slog.Logger) *tailer {
	t.Helper()
	p := testProvider()
	conn, err := p.connectionForSource(testSource(t, models.LokiConnectionInfo{BaseURL: server.URL}))
	if err != nil {
		t.Fatal(err)
	}
	if log == nil {
		log = p.log
	}
	tl := newTailer(start.UnixNano(), defaultTailLookbackMargin, conn, c.emit, log)
	tl.fetch = p.tailFetch(conn, `{service_name="api"}`)
	return tl
}

func entriesFrom(from time.Time, n int, gap time.Duration, prefix string) []fakeEntry {
	entries := make([]fakeEntry, n)
	for i := range entries {
		entries[i] = fakeEntry{ts: from.Add(time.Duration(i) * gap).UnixNano(), labels: map[string]string{"service_name": "api"}, line: fmt.Sprintf("%s %d", prefix, i)}
	}
	return entries
}

func callsSince(f *fakeLoki, n int) []rangeCall {
	return f.rangeCalls()[n:]
}

func TestTailLateEntryEmittedOnce(t *testing.T) {
	f, server := newFakeLoki(t)
	start := time.Unix(1_800_000_000, 0)
	c := newTailCollector()
	tl := fakeTailer(t, server, start, c, nil)

	first := fakeEntry{ts: start.Add(10 * time.Second).UnixNano(), labels: map[string]string{"service_name": "api"}, line: "first"}
	f.entries = append(f.entries, first)
	if err := tl.tick(t.Context(), start.Add(11*time.Second).UnixNano()); err != nil {
		t.Fatal(err)
	}
	// It arrives after the cursor passed it, 3 seconds behind.
	f.entries = append(f.entries, fakeEntry{ts: first.ts - int64(3*time.Second), labels: map[string]string{"service_name": "api"}, line: "late"})
	for i := range 3 {
		if err := tl.tick(t.Context(), start.Add(time.Duration(13+2*i)*time.Second).UnixNano()); err != nil {
			t.Fatal(err)
		}
	}
	if c.seen("first") != 1 || c.seen("late") != 1 {
		t.Fatalf("first emitted %d times, late %d times", c.seen("first"), c.seen("late"))
	}
	c.assertEachOnce(t, 2)
	for _, call := range f.rangeCalls() {
		if call.direction != "forward" || call.start < start.UnixNano() {
			t.Fatalf("request %+v reads before the session or backward", call)
		}
	}
}

func TestTailBurstInsideMarginEmittedOnce(t *testing.T) {
	f, server := newFakeLoki(t)
	start := time.Unix(1_800_000_000, 0)
	c := newTailCollector()
	tl := fakeTailer(t, server, start, c, nil)

	f.entries = entriesFrom(start.Add(time.Second), 1500, time.Millisecond, "burst")
	for i := range 3 {
		if err := tl.tick(t.Context(), start.Add(time.Duration(3+i)*time.Second).UnixNano()); err != nil {
			t.Fatal(err)
		}
	}
	c.assertEachOnce(t, 1500)

	requests := map[[2]int64]bool{}
	for _, call := range f.rangeCalls() {
		window := [2]int64{call.start, call.end}
		if requests[window] {
			t.Fatalf("[%d, %d) requested twice", call.start, call.end)
		}
		requests[window] = true
	}
}

func TestTailTimestampTieAdvances(t *testing.T) {
	f, server := newFakeLoki(t)
	start := time.Unix(1_800_000_000, 0)
	tie := start.Add(time.Second)
	var logs bytes.Buffer
	c := newTailCollector()
	tl := fakeTailer(t, server, start, c, slog.New(slog.NewTextHandler(&logs, nil)))

	for i := range 1200 {
		f.entries = append(f.entries, fakeEntry{ts: tie.UnixNano(), labels: map[string]string{"service_name": "api"}, line: fmt.Sprintf("tie %d", i)})
	}
	after := fakeEntry{ts: tie.UnixNano() + 1, labels: map[string]string{"service_name": "api"}, line: "after"}
	f.entries = append(f.entries, after)
	if err := tl.tick(t.Context(), start.Add(2*time.Second).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if calls := f.rangeCalls(); len(calls) != 2 || calls[1].start != tie.UnixNano()+1 {
		t.Fatalf("calls %+v, want a second request from one nanosecond past the tie", calls)
	}
	if c.seen("after") != 1 {
		t.Fatal("the entry after the tie was not emitted")
	}
	c.assertEachOnce(t, 1001)
	if !strings.Contains(logs.String(), "more entries share one timestamp") {
		t.Fatalf("no warning logged: %s", logs.String())
	}
}

func TestTailBoundsRequestsPerTick(t *testing.T) {
	f, server := newFakeLoki(t)
	start := time.Unix(1_800_000_000, 0)
	c := newTailCollector()
	tl := fakeTailer(t, server, start, c, nil)

	f.entries = entriesFrom(start.Add(time.Second), 25000, 40*time.Microsecond, "flood")
	now := start.Add(3 * time.Second)
	for tick := 0; c.total < 25000; tick++ {
		if tick == 10 {
			t.Fatalf("emitted %d of 25,000 rows after 10 ticks", c.total)
		}
		before := len(f.rangeCalls())
		if err := tl.tick(t.Context(), now.UnixNano()); err != nil {
			t.Fatal(err)
		}
		calls := callsSince(f, before)
		if len(calls) > tailMaxPagesPerTick {
			t.Fatalf("tick %d sent %d requests", tick, len(calls))
		}
		// A drain that the bound cut continues with the same upper.
		if tick == 1 && calls[0].end != start.Add(3*time.Second).UnixNano() {
			t.Fatalf("tick 1 started a new window %+v instead of continuing the drain", calls[0])
		}
		now = now.Add(2 * time.Second)
	}
	c.assertEachOnce(t, 25000)
}

func TestTailLogsRejectsMetricQuery(t *testing.T) {
	f, server := newFakeLoki(t)
	f.handle = func(w http.ResponseWriter, _ *http.Request) {
		writeResult(w, "matrix", []lokiSeries{})
	}
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
	err := testProvider().TailLogs(t.Context(), source, datasource.TailRequest{Query: `count_over_time({service_name="api"}[1m])`, PollInterval: time.Millisecond}, newTailCollector().emit)
	var validation *datasource.ValidationError
	if !errors.As(err, &validation) || validation.Message != "Live tail needs a LogQL log query" {
		t.Fatalf("err = %v", err)
	}
	if calls := f.rangeCalls(); len(calls) != 1 {
		t.Fatalf("%d requests, want 1", len(calls))
	}
}

func TestTailLogsEndsAfterConsecutiveFailures(t *testing.T) {
	f, server := newFakeLoki(t)
	f.handle = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
	err := testProvider().TailLogs(t.Context(), source, datasource.TailRequest{Query: `{service_name="api"}`, PollInterval: time.Millisecond}, newTailCollector().emit)
	var upstream *datasource.UpstreamError
	if !errors.As(err, &upstream) || upstream.Kind != datasource.UpstreamUnavailable {
		t.Fatalf("err = %v", err)
	}
	if calls := f.rangeCalls(); len(calls) != tailMaxConsecutiveFailures {
		t.Fatalf("%d requests, want %d", len(calls), tailMaxConsecutiveFailures)
	}
}

func TestTailLogsStopsWhenEmitFails(t *testing.T) {
	f, server := newFakeLoki(t)
	stop := errors.New("client gone")
	f.handle = func(w http.ResponseWriter, _ *http.Request) {
		writeStreams(w, []fakeEntry{{ts: time.Now().UnixNano() - int64(time.Millisecond), labels: map[string]string{"service_name": "api"}, line: "x"}})
	}
	source := testSource(t, models.LokiConnectionInfo{BaseURL: server.URL})
	err := testProvider().TailLogs(t.Context(), source, datasource.TailRequest{Query: `{service_name="api"}`, PollInterval: time.Millisecond}, func([]map[string]any) error { return stop })
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v", err)
	}
	if calls := f.rangeCalls(); len(calls) != 1 {
		t.Fatalf("%d requests, want 1", len(calls))
	}
}

func TestIntegrationTail(t *testing.T) {
	url := integrationURL(t)
	runID := newRunID(t)
	tenant := "logchef-" + runID
	labels := map[string]string{"service_name": "it-tail", "test_run": runID}

	p := testProvider()
	t.Cleanup(func() { _ = p.Close() })
	source := testSource(t, models.LokiConnectionInfo{BaseURL: url, TenantID: tenant, Selector: `{test_run="` + runID + `"}`})

	ctx, cancel := context.WithCancel(t.Context())
	c := newTailCollector()
	done := make(chan error, 1)
	go func() {
		done <- p.TailLogs(ctx, source, datasource.TailRequest{Query: `{test_run="` + runID + `"}`, PollInterval: 200 * time.Millisecond}, c.emit)
	}()
	// Leave room behind the first entry, so the late one is still after the
	// session start.
	time.Sleep(4 * time.Second)

	waitFor := func(line string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for c.seen(line) == 0 {
			if time.Now().After(deadline) {
				t.Fatalf("%q was not tailed", line)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	first := time.Now().UTC()
	push(t, url, tenant, []pushStream{{Stream: labels, Values: [][2]string{{fmt.Sprint(first.UnixNano()), "first"}}}})
	waitFor("first")
	push(t, url, tenant, []pushStream{{Stream: labels, Values: [][2]string{{fmt.Sprint(first.Add(-3 * time.Second).UnixNano()), "late"}}}})
	waitFor("late")
	// Further polls rescan both entries inside the margin.
	time.Sleep(time.Second)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("tail ended with %v", err)
	}
	c.assertEachOnce(t, 2)
}
