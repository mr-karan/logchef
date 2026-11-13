package config_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
)

func TestRuntimeConfigRejectsNonPositiveDurations(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.New(ctx, sqlite.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Config: config.SQLiteConfig{Path: sqlitetest.Path(t)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, staticDuration := range []time.Duration{0, -time.Hour, 2 * time.Hour} {
		for _, value := range []string{"0s", "-1h", "3h"} {
			t.Run(staticDuration.String()+"/"+value, func(t *testing.T) {
				static := &config.Config{
					Alerts: config.AlertsConfig{EvaluationInterval: staticDuration, DefaultLookback: staticDuration},
					Auth:   config.AuthConfig{SessionDuration: staticDuration, DefaultTokenExpiry: staticDuration},
				}
				for _, key := range []string{"alerts.evaluation_interval", "alerts.default_lookback", "auth.session_duration", "auth.default_token_expiry"} {
					if err := db.UpsertSetting(ctx, key, value, "duration", "auth", "", false); err != nil {
						t.Fatal(err)
					}
				}
				cfg := config.LoadRuntimeConfig(ctx, static, db)
				for _, setting := range []struct {
					key           string
					got, fallback time.Duration
				}{
					{"alerts.evaluation_interval", cfg.Alerts.EvaluationInterval, time.Minute},
					{"alerts.default_lookback", cfg.Alerts.DefaultLookback, 5 * time.Minute},
					{"auth.session_duration", cfg.Auth.SessionDuration, 8 * time.Hour},
					{"auth.default_token_expiry", cfg.Auth.DefaultTokenExpiry, 2160 * time.Hour},
				} {
					want := setting.fallback
					if staticDuration > 0 {
						want = staticDuration
					}
					if value == "3h" {
						want = 3 * time.Hour
					}
					if setting.got != want {
						t.Errorf("%s = %v, want %v", setting.key, setting.got, want)
					}
				}
			})
		}
	}
}
