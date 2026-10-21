package datasource

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestEvaluateAlertRejectsNonSelectQueries(t *testing.T) {
	provider := &ClickHouseProvider{manager: &clickhouse.Manager{}}
	result, err := provider.EvaluateAlert(context.Background(), &models.Source{ID: 1}, AlertQueryRequest{Query: "DROP TABLE x"})
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) || validationErr.Field != "query" {
		t.Fatalf("EvaluateAlert() = %v, %v; want query validation error before connection lookup", result, err)
	}
}

func TestEvaluateAlertClickHouse(t *testing.T) {
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
		want  any
	}{
		{query: "SELECT count() AS value FROM numbers(1501)", want: uint64(1501)},
		{query: "SELECT count() AS value FROM (SELECT number FROM numbers(1501) LIMIT 1201)", want: uint64(1201)},
		// ClickHouse nests block comments and the parser does not, so the raw
		// text would run the hidden SELECT 42 instead of the validated literal.
		{query: "/* /* */ SELECT '*/ SELECT 42 AS value -- '", want: "*/ SELECT 42 AS value -- "},
	} {
		t.Run(tc.query, func(t *testing.T) {
			result, err := provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: tc.query})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Logs) != 1 || len(result.Logs[0]) != 1 {
				t.Fatalf("EvaluateAlert() rows = %v, want one row with one column", result.Logs)
			}
			for _, got := range result.Logs[0] {
				if got != tc.want {
					t.Fatalf("EvaluateAlert() value = %#v, want %#v", got, tc.want)
				}
			}
		})
	}

	result, err := provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: "SELECT number AS value FROM numbers(1000000)"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Logs) != alertMaxRows || !result.Stats.Truncated {
		t.Fatalf("EvaluateAlert() buffered %d rows (truncated=%v), want %d truncated", len(result.Logs), result.Stats.Truncated, alertMaxRows)
	}

	result, err = provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: "SELECT number AS value FROM numbers(10) WHERE number > 100"})
	if err != nil || len(result.Logs) != 0 || result.Stats.Truncated {
		t.Fatalf("EvaluateAlert(empty) = %v, %v; want zero rows without error", result, err)
	}

	result, err = provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: "SELECT count() AS value, repeat('xx', 600000) AS detail FROM numbers(1501)"})
	if err == nil {
		t.Fatalf("EvaluateAlert(first row over byte limit) = %v, want error instead of an empty result", result.Logs)
	}

	// The parser keeps these as identifiers while ClickHouse reads an escaped
	// backtick or a heredoc string, which would run the hidden 42 AS value.
	for _, query := range []string{
		"SELECT 'n' AS `\\`, '`, 42 AS value -- '",
		`SELECT 'n' AS "\", '", 42 AS value -- '`,
		"SELECT $$ AS a, '$$, 42 AS value -- '",
	} {
		t.Run(query, func(t *testing.T) {
			result, err := provider.EvaluateAlert(ctx, source, AlertQueryRequest{Query: query})
			if _, ok := errors.AsType[*ValidationError](err); !ok {
				t.Fatalf("EvaluateAlert() = %v, %v; want query validation error", result, err)
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

func TestClickHouseProviderSourceMetadata(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := NewService(nil, log)
	service.Register(NewClickHouseProvider(nil, log))
	source := &models.Source{SourceType: models.SourceTypeClickHouse}
	if err := service.ApplySourceMetadata(source); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		got, want any
	}{
		{"query languages", source.QueryLanguages, []models.QueryLanguage{models.QueryLanguageLogchefQL, models.QueryLanguageClickHouseSQL}},
		{"saved query editor modes", source.SavedQueryEditorModes, []models.SavedQueryEditorMode{models.SavedQueryEditorModeBuilder, models.SavedQueryEditorModeNative}},
		{"alert editor modes", source.AlertEditorModes, []models.AlertEditorMode{models.AlertEditorModeCondition, models.AlertEditorModeNative}},
		{"capabilities", source.Capabilities, []string{"schema_inspection", "histogram", "field_values", "source_inspection", "ai_sql_generation", "log_context", "exports", "live_tail"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !reflect.DeepEqual(tt.got, tt.want) {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}
