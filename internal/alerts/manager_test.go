package alerts

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlitetest"
	"github.com/mr-karan/logchef/pkg/models"
)

func newManagerTestAlert(t *testing.T) (*sqlite.DB, *models.Alert, *slog.Logger) {
	t.Helper()
	return newManagerTestAlertAtPath(t, sqlitetest.Path(t))
}

func newManagerTestAlertAtPath(t *testing.T, path string) (*sqlite.DB, *models.Alert, *slog.Logger) {
	t.Helper()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(ctx, sqlite.Options{
		Logger: logger,
		Config: config.SQLiteConfig{Path: path},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, createManagerTestAlert(t, db), logger
}

func TestManagerEvaluationErrorPreservesFiringState(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	m := NewManager(Options{
		DB: db, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100},
		Datasources: datasource.NewService(db, logger),
	})
	ctx := t.Context()
	if err := m.handleTriggered(ctx, alert, 12); err != nil {
		t.Fatal(err)
	}
	before, err := db.GetAlert(ctx, alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.evaluateAlert(ctx, before, time.Now()); err == nil {
		t.Fatal("evaluateAlert succeeded without a datasource provider")
	}
	after, err := db.GetAlert(ctx, alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.LastState != models.AlertStateFiring {
		t.Errorf("state after evaluation error = %s, want firing", after.LastState)
	}
	if after.LastEvaluatedAt == nil || after.LastEvaluatedAt.Before(*before.LastEvaluatedAt) {
		t.Errorf("evaluation timestamp = %v, want at least %v", after.LastEvaluatedAt, before.LastEvaluatedAt)
	}
	if after.LastTriggeredAt == nil || !after.LastTriggeredAt.Equal(*before.LastTriggeredAt) {
		t.Errorf("trigger timestamp changed from %v to %v", before.LastTriggeredAt, after.LastTriggeredAt)
	}
	history, err := db.ListAlertHistory(ctx, alert.ID, 10)
	if err != nil || len(history) != 2 {
		t.Fatalf("history = %d entries, %v; want trigger and error", len(history), err)
	}
	if history[0].Status != models.AlertStatusError {
		t.Errorf("latest history status = %s, want error", history[0].Status)
	}
	if err := m.handleResolved(ctx, after, 0); err != nil {
		t.Fatal(err)
	}
	resolved, err := db.GetAlert(ctx, alert.ID)
	if err != nil || resolved.LastState != models.AlertStateResolved {
		t.Fatalf("state after resolution = %+v, %v; want resolved", resolved, err)
	}
}

// failHistoryWrites installs a trigger that rejects the matching alert_history

// writes and returns a function that removes it.

func TestCanceledEvaluationDoesNotRecordFailure(t *testing.T) {
	db, alert, _ := newManagerTestAlert(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	manager := NewManager(Options{DB: db, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	manager.recordEvaluationError(ctx, alert, fmt.Errorf("query failed: %w", context.Canceled))
	history, err := db.ListAlertHistory(t.Context(), alert.ID, 10)
	if err != nil || len(history) != 0 {
		t.Fatalf("cancellation created failure history = %v, %v", history, err)
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("cancellation logged an evaluation error: %s", logs.String())
	}
}

func createManagerTestAlert(t *testing.T, db store.Store) *models.Alert {
	t.Helper()
	ctx := t.Context()
	source := &models.Source{
		Name: "logs.alerts", MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{Host: "localhost:9000", Database: "logs", TableName: "alerts"},
	}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	alert := &models.Alert{
		SourceID: source.ID, Name: "errors", Query: "SELECT count() FROM logs",
		QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
		LookbackSeconds: 300, FrequencySeconds: 60, IsActive: true, NotifyOnResolve: true,
		ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
		Severity: models.AlertSeverityWarning, LastState: models.AlertStateResolved,
	}
	if err := db.CreateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	return alert
}
