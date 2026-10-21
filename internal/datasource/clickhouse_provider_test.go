package datasource

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestEvaluateAlertRejectsNonSelectQueries(t *testing.T) {
	provider := &ClickHouseProvider{manager: &clickhouse.Manager{}}
	source := &models.Source{ID: 1}
	for _, query := range []string{
		"DROP TABLE x",
		"INSERT INTO x SELECT 1",
		"/* c */ DROP TABLE x",
		"SYSTEM SHUTDOWN",
		"OPTIMIZE TABLE x",
		"KILL QUERY WHERE 1",
		"SELECT 1; DROP TABLE x",
	} {
		t.Run(query, func(t *testing.T) {
			result, err := provider.EvaluateAlert(context.Background(), source, AlertQueryRequest{Query: query})
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Field != "query" {
				t.Fatalf("EvaluateAlert() = %v, %v; want query validation error before connection lookup", result, err)
			}
		})
	}
}

func TestEvaluateAlertClickHousePreservesAggregate(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager := clickhouse.NewManager(log)
	defer manager.Close()
	source := &models.Source{ID: 1, Connection: models.ConnectionInfo{Host: addr, Database: "system", TableName: "numbers"}}
	if err := manager.AddSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	provider := NewClickHouseProvider(manager, log)
	for _, tc := range []struct {
		query string
		want  uint64
	}{
		{query: "SELECT count() AS value FROM numbers(1501)", want: 1501},
		{query: "SELECT count() AS value FROM (SELECT number FROM numbers(1501) LIMIT 1201)", want: 1201},
	} {
		t.Run(tc.query, func(t *testing.T) {
			result, err := provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: tc.query})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Logs) != 1 || result.Logs[0]["value"] != tc.want {
				t.Fatalf("EvaluateAlert() rows = %v, want one row with value %d", result.Logs, tc.want)
			}
		})
	}
}

func TestHasLeadingTimestampSortKey(t *testing.T) {
	tests := []struct {
		name  string
		keys  []string
		field string
		want  bool
	}{
		{name: "leading field matches", keys: []string{"event_time", "service"}, field: "event_time", want: true},
		{name: "quoted leading field matches", keys: []string{"`event_time`"}, field: "event_time", want: true},
		{name: "non-leading field is rejected", keys: []string{"service", "event_time"}, field: "event_time", want: false},
		{name: "missing metadata is rejected", field: "event_time", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasLeadingTimestampSortKey(&clickhouse.TableInfo{SortKeys: tt.keys}, tt.field); got != tt.want {
				t.Fatalf("hasLeadingTimestampSortKey() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCompileLogchefQLWithoutColumnsDoesNotMutateSource(t *testing.T) {
	provider := &ClickHouseProvider{}
	source := &models.Source{}
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			result, err := provider.CompileLogchefQL(context.Background(), source, LogchefQLCompileRequest{Query: `message = "hello"`})
			if err != nil {
				t.Error(err)
				return
			}
			if !result.Valid {
				t.Error("expected valid best-effort translation")
			}
		})
	}
	wg.Wait()
	if source.Columns != nil {
		t.Fatal("compilation mutated source columns")
	}
}
