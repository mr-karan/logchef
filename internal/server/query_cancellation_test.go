package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestClickHousePreviewClientDisconnect(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR for read-only ClickHouse checks")
	}
	var logs syncLogBuffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	env := newErrorResponseServer(t, vlUpstream, log)
	configureErrorResponseClickHouse(t, env, addr)
	env.s.config.Query.DefaultPreviewLimit = 10000
	env.s.config.Query.MaxPreviewLimit = 10000
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- env.s.app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() {
		if err := env.s.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
		if err := <-served; err != nil {
			t.Error(err)
		}
	})
	logs.Reset()
	const query = "SELECT number, sleepEachRow(0.01) FROM numbers(10000) SETTINGS max_block_size=1, max_threads=1"
	path := fmt.Sprintf("http://%s/api/v1/teams/%d/sources/%d/logs/query", listener.Addr(), env.team, env.chSource)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(fmt.Sprintf(`{"query_text":%q,"limit":10000}`, query)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(env.cookie)
	client := &http.Client{Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var first [1]byte
	if _, err := io.ReadFull(resp.Body, first[:]); err != nil {
		t.Fatal(err)
	}
	queryID := resp.Header.Get("X-LogChef-Query-ID")
	if queryID == "" {
		t.Fatal("missing active query ID")
	}
	cancel()
	_ = resp.Body.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		queryTracker.mu.RLock()
		_, active := queryTracker.queries[queryID]
		queryTracker.mu.RUnlock()
		if !active {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("disconnected query did not stop")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var history []*models.QueryHistory
	for time.Now().Before(deadline) {
		history, err = env.s.sqlite.ListQueryHistory(t.Context(), 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(history) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(history) != 1 {
		t.Errorf("cancelled query history rows=%d, want 1", len(history))
	} else if entry := history[0]; entry.QueryText != query || entry.SourceID != env.chSource || entry.TeamID != env.team || entry.DurationMs <= 0 || entry.RowCount >= 10000 {
		t.Errorf("incorrect cancelled query history: %+v", entry)
	}
	output := logs.String()
	var diagnostics []string
	for line := range strings.Lines(output) {
		if strings.Contains(line, "level=ERROR") || strings.Contains(line, "level=WARN") {
			t.Errorf("cancellation logged above Info: %s", line)
		}
		if strings.Contains(line, `msg="query failed"`) || strings.Contains(line, `msg="failed to stream query"`) {
			diagnostics = append(diagnostics, line)
		}
	}
	if len(diagnostics) > 1 {
		t.Errorf("cancellation diagnostics=%d, want at most 1", len(diagnostics))
	}
	t.Logf("history rows=%d; cancellation diagnostics=%d\n%s", len(history), len(diagnostics), strings.Join(diagnostics, "\n"))
}
