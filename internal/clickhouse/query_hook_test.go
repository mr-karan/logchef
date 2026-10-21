package clickhouse

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLiveQueryCancellationLogLevel(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("LOGCHEF_TEST_CLICKHOUSE_ADDR not set")
	}
	for _, outcome := range []string{"cancel", "deadline"} {
		t.Run(outcome, func(t *testing.T) {
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			client, err := NewClient(ClientOptions{Host: addr, Database: "default", Username: "default"}, log)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			if _, err := client.Query(context.Background(), "SELECT 1"); err != nil {
				t.Fatal(err)
			}
			logs.Reset()
			var ctx context.Context
			var cancel context.CancelFunc
			wantErr, level := context.Canceled, "DEBUG"
			if outcome == "deadline" {
				ctx, cancel = context.WithTimeout(context.Background(), 200*time.Millisecond)
				wantErr, level = context.DeadlineExceeded, "ERROR"
			} else {
				ctx, cancel = context.WithCancel(context.Background())
				timer := time.AfterFunc(200*time.Millisecond, cancel)
				defer timer.Stop()
			}
			defer cancel()
			_, err = client.Query(ctx, "SELECT sleep(3)")
			if !errors.Is(err, wantErr) {
				t.Fatalf("query error = %v, want %v", err, wantErr)
			}
			if !strings.Contains(logs.String(), "level="+level+" msg=\"query failed\"") {
				t.Errorf("query log missing level %s: %s", level, logs.String())
			}
			if outcome == "cancel" && strings.Contains(logs.String(), "level=ERROR") {
				t.Errorf("cancelled query logged error: %s", logs.String())
			}
		})
	}
}
