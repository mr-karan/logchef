package datasource

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestRenderAlertQueryWithoutMacros: a query without macros runs exactly the
// statement it ran before macros existed.
func TestRenderAlertQueryWithoutMacros(t *testing.T) {
	source := &models.Source{MetaTSField: "timestamp"}
	from := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	for _, query := range []string{
		"SELECT count() AS value FROM logs WHERE timestamp >= now() - toIntervalSecond(300)",
		"SELECT service, count() AS value FROM logs WHERE level = 'error' GROUP BY service",
		"/* note */ SELECT '{{not_a_macro}}' AS value",
	} {
		want, err := formatAlertQuery(query)
		if err != nil {
			t.Fatal(err)
		}
		got, err := renderAlertQuery(source, query, from, from.Add(time.Hour))
		if err != nil || got != want {
			t.Errorf("renderAlertQuery(%q) = %q, %v; want %q", query, got, err, want)
		}
	}
}

func TestRenderAlertQueryTimeMacros(t *testing.T) {
	source := &models.Source{MetaTSField: "timestamp"}
	from := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	got, err := renderAlertQuery(source, "SELECT count() AS value FROM logs WHERE {{__time_filter}} AND x < {{__to}}", from, to)
	if err != nil {
		t.Fatal(err)
	}
	want, err := formatAlertQuery("SELECT count() AS value FROM logs WHERE `timestamp` >= toDateTime64('2026-01-01 09:00:00.000', 3, 'UTC') AND `timestamp` < toDateTime64('2026-01-01 10:00:00.000', 3, 'UTC') AND x < toDateTime64('2026-01-01 10:00:00.000', 3, 'UTC')")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("rendered = %q, want %q", got, want)
	}

	for _, query := range []string{
		"SELECT count() AS value FROM logs WHERE service = {{service}}",
		"SELECT count() AS value FROM logs WHERE {{__time_filter}} AND service = {{service}}",
		"SELECT count() AS value FROM logs WHERE {{__time_filter}} [[AND service = {{service}}]]",
		"SELECT count() AS value FROM logs WHERE message = '{{__time_filter}}x' OR {{__interval_s}} > 0",
	} {
		_, err := renderAlertQuery(source, query, from, to)
		var validationErr *ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "query" {
			t.Errorf("renderAlertQuery(%q) err = %v, want a query validation error", query, err)
		}
	}
}

// TestEvaluateAlertWindowClickHouse: {{__time_filter}} with At one hour ago
// reads only the rows of the lookback that ends at At.
func TestEvaluateAlertWindowClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := clickhouse.NewManager(log)
	defer manager.Close()
	source := &models.Source{ID: 1, MetaTSField: "ts", Connection: models.ConnectionInfo{Host: addr, Database: "system", TableName: "one"}}
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	provider := NewClickHouseProvider(manager, log)
	// Two rows an hour old and one ten seconds old.
	query := "SELECT count() AS value FROM (SELECT arrayJoin([now64(3) - INTERVAL 3600 SECOND, now64(3) - INTERVAL 3590 SECOND, now64(3) - INTERVAL 10 SECOND]) AS ts) WHERE {{__time_filter}}"
	now := time.Now().UTC()
	for _, tc := range []struct {
		name string
		at   time.Time
		want uint64
	}{
		{name: "an hour ago", at: now.Add(-59 * time.Minute), want: 2},
		{name: "now", at: now, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := provider.EvaluateAlert(ctx, source, AlertQueryRequest{At: tc.at, LookbackSeconds: 300, Query: query})
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Logs[0]["value"]; got != tc.want {
				t.Fatalf("count = %#v, want %d", got, tc.want)
			}
		})
	}
}

func TestEvaluateAlertNeedsAt(t *testing.T) {
	provider := &ClickHouseProvider{manager: &clickhouse.Manager{}}
	_, err := provider.EvaluateAlert(context.Background(), &models.Source{ID: 1}, AlertQueryRequest{Query: "SELECT 1 AS value"})
	if err == nil || !strings.Contains(err.Error(), "instant") {
		t.Fatalf("EvaluateAlert without At err = %v, want it refused", err)
	}
}
