package victorialogs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
