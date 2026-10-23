package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestClickHousePreviewClientDisconnect(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR for read-only ClickHouse checks")
	}
	for _, outcome := range []string{"disconnect", "cancel endpoint"} {
		t.Run(outcome, func(t *testing.T) {
			var logs syncLogBuffer
			log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			env := newErrorResponseServer(t, vlUpstream, log)
			configureErrorResponseClickHouse(t, env, addr)
			user, err := env.s.store.GetUserByEmail(t.Context(), "admin@example.com")
			if err != nil {
				t.Fatal(err)
			}
			env.s.config.Query.DefaultPreviewLimit = 10000
			env.s.config.Query.MaxPreviewLimit = 10000
			baseURL, shutdown := (&oauthEnv{t: t, srv: env.s}).serve()
			t.Cleanup(shutdown)
			logs.Reset()
			const query = "SELECT number, sleepEachRow(0.01) FROM numbers(10000) SETTINGS max_block_size=1, max_threads=1"
			path := fmt.Sprintf("%s/api/v1/teams/%d/sources/%d/logs/query", baseURL, env.team, env.chSource)
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
			if outcome == "cancel endpoint" {
				cancelPath := fmt.Sprintf("/api/v1/teams/%d/sources/%d/logs/query/%s/cancel", env.team, env.chSource, queryID)
				got := doAuthzRequest(t, env.s, http.MethodPost, cancelPath, "", withSession(env.cookie))
				if got.status != http.StatusOK {
					t.Fatalf("cancel response=%d %s", got.status, got.message)
				}
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				var result struct {
					Data struct {
						Error string `json:"error"`
					} `json:"data"`
				}
				if err := json.Unmarshal(append(first[:], body...), &result); err != nil {
					t.Fatalf("cancelled stream must remain valid JSON: %v", err)
				}
				if result.Data.Error != "Request cancelled" {
					t.Errorf("stream error=%q, want Request cancelled", result.Data.Error)
				}
			} else {
				cancel()
				_ = resp.Body.Close()
			}
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
			if err := env.s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			history, err := env.s.store.ListQueryHistory(t.Context(), user.ID, 10)
			if err != nil {
				t.Fatal(err)
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
		})
	}
}

func TestClickHousePreviewContextCancellation(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR for read-only ClickHouse checks")
	}
	for _, outcome := range []string{"cancel", "deadline"} {
		t.Run(outcome, func(t *testing.T) {
			var logs syncLogBuffer
			log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			env := newErrorResponseServer(t, vlUpstream, log)
			configureErrorResponseClickHouse(t, env, addr)
			user, err := env.s.store.GetUserByEmail(t.Context(), "admin@example.com")
			if err != nil {
				t.Fatal(err)
			}
			app := fiber.New()
			app.Use(requestLogger(log))
			withAuthorizedSource(app, env.s, "/teams/:teamID/sources/:sourceID/query", user, func(c fiber.Ctx) error {
				var ctx context.Context
				var cancel context.CancelFunc
				if outcome == "deadline" {
					ctx, cancel = context.WithTimeout(env.s.baseCtx, 200*time.Millisecond)
				} else {
					ctx, cancel = context.WithCancel(env.s.baseCtx)
					timer := time.AfterFunc(200*time.Millisecond, cancel)
					defer timer.Stop()
				}
				defer cancel()
				c.SetContext(ctx)
				return env.s.handleQueryLogs(c)
			})
			logs.Reset()
			path := fmt.Sprintf("/teams/%d/sources/%d/query", env.team, env.chSource)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"query_text":"SELECT sleep(2)"}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			message := "Request cancelled"
			if outcome == "deadline" {
				message = "Request timed out"
			}
			if resp.StatusCode != http.StatusRequestTimeout || !strings.Contains(string(body), message) {
				t.Errorf("response=%d %s, want 408 with %q", resp.StatusCode, body, message)
			}
			if err := env.s.Shutdown(context.Background()); err != nil {
				t.Fatal(err)
			}
			history, err := env.s.store.ListQueryHistory(t.Context(), user.ID, 10)
			if err != nil {
				t.Fatal(err)
			}
			output := logs.String()
			if outcome == "cancel" {
				if len(history) != 1 || history[0].QueryText != "SELECT sleep(2)" || history[0].RowCount != 0 || history[0].DurationMs <= 0 {
					t.Errorf("cancelled query history=%+v, want one attempted query with zero rows", history)
				}
				if strings.Contains(output, "level=ERROR") || strings.Contains(output, "level=WARN") {
					t.Errorf("cancellation logged above Info:\n%s", output)
				}
			} else if strings.Contains(string(body), "cancelled") {
				t.Error("deadline was classified as cancellation")
			}
			if outcome == "deadline" {
				warned := false
				for line := range strings.Lines(output) {
					if strings.Contains(line, "level=WARN msg=http") && strings.Contains(line, "status=408") {
						warned = true
					}
				}
				if !warned {
					t.Errorf("deadline must retain its Warn access log:\n%s", output)
				}
			}
			t.Logf("status=%d history rows=%d message=%s", resp.StatusCode, len(history), message)
		})
	}
}
