package victorialogs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestTailLogsReportsUnexpectedUpstreamCloseAsError proves the item-5 fix: an
// EOF the caller never asked for (ctx still live) must be surfaced as an
// error, not silently folded into "completed" the way it was before.
func TestTailLogsReportsUnexpectedUpstreamCloseAsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"_time":"2026-04-08T10:00:00Z","_msg":"a"}` + "\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// Handler returns here: the upstream closes the connection on its own.
		// The client's context is never cancelled.
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	var got []map[string]any
	err := provider.TailLogs(context.Background(), source, datasource.TailRequest{}, func(rows []map[string]any) error {
		got = append(got, rows...)
		return nil
	})
	if !errors.Is(err, ErrTailUpstreamClosed) {
		t.Fatalf("expected ErrTailUpstreamClosed, got %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the one row emitted before the close, got %d: %v", len(got), got)
	}
}

// TestTailLogsCleanStopOnCallerCancel proves the counterpart: a stop the
// caller itself asked for (ctx cancellation, e.g. client disconnect or
// session TTL) must still be reported as a clean stop (nil, or
// context.Canceled), never as ErrTailUpstreamClosed.
func TestTailLogsCleanStopOnCallerCancel(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"_time":"2026-04-08T10:00:00Z","_msg":"a"}` + "\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// Hold the connection open until the client cancels, rather than
		// returning on our own (which is the case the other test covers).
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- provider.TailLogs(ctx, source, datasource.TailRequest{}, func(rows []map[string]any) error {
			cancel() // simulate the caller (client disconnect / session TTL) stopping the tail
			return nil
		})
	}()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("expected nil or context.Canceled on caller-initiated stop, got %v", err)
		}
		if errors.Is(err, ErrTailUpstreamClosed) {
			t.Fatalf("caller-initiated cancellation must not be reported as ErrTailUpstreamClosed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TailLogs did not return after ctx cancellation")
	}
}

// TestTailLogsFlushesBufferedRowsOnCancel proves the #23 fix: rows already
// accumulated in the batch when ctx is cancelled must still reach the emitter
// instead of being silently dropped.
func TestTailLogsFlushesBufferedRowsOnCancel(t *testing.T) {
	t.Parallel()

	const rowCount = 3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < rowCount; i++ {
			_, _ = w.Write([]byte(`{"_time":"2026-04-08T10:00:00Z","_msg":"a"}` + "\n"))
		}
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		// Hold the connection open past the cancellation below, rather than
		// closing it ourselves, so the only thing that stops the tail is the
		// client's own ctx cancellation.
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var got []map[string]any
	done := make(chan error, 1)
	go func() {
		done <- provider.TailLogs(ctx, source, datasource.TailRequest{}, func(rows []map[string]any) error {
			mu.Lock()
			got = append(got, rows...)
			mu.Unlock()
			return nil
		})
	}()

	// Give the decode goroutine time to land all rowCount rows in the batch —
	// comfortably under tailFlushInterval (200ms), so the periodic ticker
	// cannot have flushed them on its own before the cancel below, which is
	// what this test needs to exercise.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("expected nil or context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TailLogs did not return after ctx cancellation")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(got) != rowCount {
		t.Fatalf("expected the %d buffered rows to be flushed on cancel, got %d: %v", rowCount, len(got), got)
	}
}

// TestTailLogsDecodeGoroutineDoesNotLeakOnEmitError proves the #21 fix: when
// the main loop exits because the emitter returns an error (not because ctx
// was cancelled), a decode goroutine already parked trying to hand off the
// next row must still be unblocked and exit, rather than leaking forever.
// ctx here is context.Background() — nothing external will ever unblock a
// leaked goroutine, so this only passes if TailLogs itself guarantees the
// unblock.
func TestTailLogsDecodeGoroutineDoesNotLeakOnEmitError(t *testing.T) {
	// Not parallel: takes a process-wide goroutine stack dump, which needs a
	// stable view to search reliably.

	// Send comfortably more rows than tailBatchSize so the decode goroutine —
	// which has no artificial delay and can decode far faster than the main
	// loop drains rowCh once it stops — is very likely still trying to hand
	// off a later row via rowCh at the exact moment the main loop returns.
	const rowCount = 500
	var body strings.Builder
	for i := 0; i < rowCount; i++ {
		body.WriteString(`{"_time":"2026-04-08T10:00:00Z","_msg":"a"}` + "\n")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body.String()))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	provider := newTestProvider(server)
	source := mustSource(t, models.VictoriaLogsConnectionInfo{BaseURL: server.URL})

	emitErr := errors.New("emit: simulated consumer failure")
	var calls int
	err := provider.TailLogs(context.Background(), source, datasource.TailRequest{}, func(rows []map[string]any) error {
		calls++
		if calls == 1 {
			return emitErr
		}
		return nil
	})
	if !errors.Is(err, emitErr) {
		t.Fatalf("expected the simulated emit error, got %v", err)
	}

	// By the time TailLogs returned above, its deferred cancel of the local
	// run context has already executed. Poll briefly for the decode
	// goroutine's stack frame to disappear rather than asserting on the
	// first sample, to absorb ordinary goroutine-scheduling jitter.
	deadline := time.Now().Add(2 * time.Second)
	for {
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		if !strings.Contains(string(buf[:n]), "victorialogs.(*Provider).TailLogs.func1") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("decode goroutine leaked after an emit-error exit:\n%s", buf[:n])
		}
		time.Sleep(20 * time.Millisecond)
	}
}
