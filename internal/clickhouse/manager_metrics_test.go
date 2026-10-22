package clickhouse

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	vm "github.com/VictoriaMetrics/metrics"

	"github.com/mr-karan/logchef/pkg/models"
)

type blockingQueryHook struct {
	entered chan struct{}
	release chan struct{}
}

func (h blockingQueryHook) BeforeQuery(ctx context.Context, _ string) (context.Context, error) {
	close(h.entered)
	<-h.release
	return ctx, nil
}

func (blockingQueryHook) AfterQuery(context.Context, string, error, time.Duration) {}

// silentListener accepts connections and never answers, so a ping stays in
// flight until its timeout.
func silentListener(t *testing.T) (addr string, dialed <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	ch := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}()
	return ln.Addr().String(), ch
}

func TestManagerRemoveSourceLeavesNoSeriesAfterInFlightWork(t *testing.T) {
	addr, dialed := silentListener(t)
	source := &models.Source{ID: 9101, Name: "removed-source"}
	source.Connection.Host = addr

	hook := blockingQueryHook{entered: make(chan struct{}), release: make(chan struct{})}
	m := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)), hook)
	if err := m.AddSource(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	client, err := m.GetConnection(source.ID)
	if err != nil {
		t.Fatal(err)
	}

	queryDone := make(chan struct{})
	go func() {
		defer close(queryDone)
		_, _ = client.Query(context.Background(), "SELECT 1")
	}()
	<-hook.entered
	pingDone := make(chan struct{})
	go func() {
		defer close(pingDone)
		_ = client.Ping(context.Background(), "", "")
	}()
	<-dialed

	if err := m.RemoveSource(source.ID); err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	close(hook.release)
	<-queryDone
	<-pingDone

	var buf bytes.Buffer
	vm.WritePrometheus(&buf, false)
	if strings.Contains(buf.String(), `source_id="9101"`) {
		t.Fatalf("series for removed source still exposed:\n%s", buf.String())
	}
}
