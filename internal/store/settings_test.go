package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
)

func TestSettingFallbackLogs(t *testing.T) {
	s, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "settings.db")},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	getters := []struct {
		name   string
		get    func(context.Context) bool
		parses bool
	}{
		{"string", func(ctx context.Context) bool {
			return store.GetSettingWithDefault(ctx, s, "test.fallback", "default") == "default"
		}, false},
		{"bool", func(ctx context.Context) bool { return store.GetBoolSetting(ctx, s, "test.fallback", true) }, true},
		{"int", func(ctx context.Context) bool { return store.GetIntSetting(ctx, s, "test.fallback", 42) == 42 }, true},
		{"float", func(ctx context.Context) bool { return store.GetFloat64Setting(ctx, s, "test.fallback", 1.5) == 1.5 }, true},
		{"duration", func(ctx context.Context) bool {
			return store.GetDurationSetting(ctx, s, "test.fallback", time.Second) == time.Second
		}, true},
	}
	for _, getter := range getters {
		t.Run(getter.name, func(t *testing.T) {
			for _, mode := range []string{"missing", "read error", "invalid"} {
				if mode == "invalid" && !getter.parses {
					continue
				}
				t.Run(mode, func(t *testing.T) {
					ctx := context.Background()
					if err := s.DeleteSetting(ctx, "test.fallback"); err != nil {
						t.Fatal(err)
					}
					if mode == "invalid" {
						if err := s.UpsertSetting(ctx, "test.fallback", "private-invalid-value", "string", "alerts", "", true); err != nil {
							t.Fatal(err)
						}
					}
					if mode == "read error" {
						canceled, cancel := context.WithCancel(ctx)
						cancel()
						ctx = canceled
					}
					logs.Reset()
					if !getter.get(ctx) {
						t.Fatal("getter did not return default")
					}
					if mode == "missing" {
						if logs.Len() != 0 {
							t.Fatalf("missing setting logged %s", logs.String())
						}
						return
					}
					if lines := strings.Count(logs.String(), "\n"); lines != 1 {
						t.Fatalf("log lines = %d, want 1; output = %q", lines, logs.String())
					}
					var record struct {
						Level string `json:"level"`
						Key   string `json:"key"`
						Error string `json:"error"`
					}
					if err := json.Unmarshal(logs.Bytes(), &record); err != nil {
						t.Fatal(err)
					}
					if record.Level != "WARN" || record.Key != "test.fallback" || record.Error == "" {
						t.Fatalf("log record = %+v", record)
					}
					if mode == "read error" && !strings.Contains(record.Error, "context canceled") {
						t.Fatalf("read error = %q", record.Error)
					}
					if strings.Contains(logs.String(), "private-invalid-value") {
						t.Fatal("log contains sensitive setting value")
					}
				})
			}
		})
	}
}
