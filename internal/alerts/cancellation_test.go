package alerts

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

type evaluationStartedHook struct {
	started chan struct{}
	once    sync.Once
}

func (h *evaluationStartedHook) BeforeQuery(ctx context.Context, query string) (context.Context, error) {
	if query == "SELECT sleep(3)" {
		h.once.Do(func() { close(h.started) })
	}
	return ctx, nil
}

func (*evaluationStartedHook) AfterQuery(context.Context, string, error, time.Duration) {}

func TestLiveEvaluationCancellationHistory(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("LOGCHEF_TEST_CLICKHOUSE_ADDR not set")
	}
	for _, backend := range []string{"sqlite", "postgres"} {
		for _, outcome := range []string{"shutdown", "timeout"} {
			t.Run(backend+"/"+outcome, func(t *testing.T) {
				ctx := context.Background()
				logPath := filepath.Join(t.TempDir(), "evaluation.log")
				output, err := os.Create(logPath)
				if err != nil {
					t.Fatal(err)
				}
				defer output.Close()
				log := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: slog.LevelDebug}))
				var db store.Store
				if backend == "sqlite" {
					db, err = sqlite.New(ctx, sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "alerts.db")}})
				} else {
					dsn := os.Getenv("LOGCHEF_TEST_POSTGRES_DSN")
					if dsn == "" {
						t.Skip("LOGCHEF_TEST_POSTGRES_DSN not set")
					}
					db, err = postgres.New(ctx, postgres.Options{Logger: log, Config: config.PostgresConfig{DSN: dsn}})
				}
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				source := &models.Source{Name: uuid.NewString(), MetaTSField: "event_time", Connection: models.ConnectionInfo{Host: addr, Username: "default", Database: "system", TableName: "query_log"}}
				if err := source.SyncConnectionConfig(); err != nil {
					t.Fatal(err)
				}
				if err := db.CreateSource(ctx, source); err != nil {
					t.Fatal(err)
				}
				defer db.DeleteSource(ctx, source.ID)
				alert := &models.Alert{SourceID: source.ID, Name: "slow", QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative, Query: "SELECT sleep(3)", LookbackSeconds: 60, ThresholdOperator: models.AlertThresholdGreaterThan, FrequencySeconds: 60, Severity: models.AlertSeverityWarning, IsActive: true, LastState: models.AlertStateResolved}
				if err := db.CreateAlert(ctx, alert); err != nil {
					t.Fatal(err)
				}
				defer db.DeleteAlert(ctx, alert.ID)
				hook := &evaluationStartedHook{started: make(chan struct{})}
				ch := clickhouse.NewManager(log, hook)
				defer ch.Close()
				if err := ch.AddSource(ctx, source); err != nil {
					t.Fatal(err)
				}
				client, err := ch.GetConnection(source.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := client.Query(ctx, "SELECT 1"); err != nil {
					t.Fatal(err)
				}
				ds := datasource.NewService(db, log)
				ds.Register(datasource.NewClickHouseProvider(ch, log))
				manager := NewManager(Options{Logger: log, DB: db, Datasources: ds, Config: config.AlertsConfig{Enabled: true, HistoryLimit: 10}})
				evaluated := make(chan struct{})
				manager.evalFn = func(ctx context.Context, alert *models.Alert) error {
					defer close(evaluated)
					return manager.evaluateAlert(ctx, alert)
				}
				if outcome == "timeout" {
					manager.evalTimeout = 200 * time.Millisecond
				}
				manager.Start(ctx)
				select {
				case <-hook.started:
				case <-time.After(5 * time.Second):
					manager.Stop()
					t.Fatal("live evaluation did not start")
				}
				if outcome == "timeout" {
					select {
					case <-evaluated:
					case <-time.After(5 * time.Second):
						manager.Stop()
						t.Fatal("evaluation deadline did not stop query")
					}
				} else {
					time.Sleep(100 * time.Millisecond)
				}
				manager.Stop()
				history, err := db.ListAlertHistory(ctx, alert.ID, 10)
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(logPath)
				if err != nil {
					t.Fatal(err)
				}
				logs := string(data)
				if outcome == "shutdown" {
					if len(history) != 0 {
						t.Errorf("shutdown wrote %d history rows", len(history))
					}
					if strings.Contains(logs, "level=ERROR") {
						t.Errorf("shutdown logged errors:\n%s", logs)
					}
				} else {
					if len(history) != 1 || history[0].Status != models.AlertStatusError || !strings.Contains(history[0].Message, "deadline exceeded") {
						t.Errorf("timeout history = %+v", history)
					}
					if !strings.Contains(logs, "level=ERROR msg=\"alert evaluation failed\"") {
						t.Errorf("timeout missing error log:\n%s", logs)
					}
				}
			})
		}
	}
}
