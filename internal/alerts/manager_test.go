package alerts

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestEvaluateAlertWithTimeoutIsolatesHungEvaluation proves the per-alert
// deadline bounds a single evaluation, so a wedged source (one whose evaluation
// never returns on its own) cannot freeze the sequential evaluation loop.
func TestEvaluateAlertWithTimeoutIsolatesHungEvaluation(t *testing.T) {
	t.Parallel()

	sawDeadline := make(chan bool, 1)
	m := &Manager{
		evalTimeout: 50 * time.Millisecond,
		evalFn: func(ctx context.Context, _ *models.Alert) error {
			_, ok := ctx.Deadline()
			sawDeadline <- ok
			// Simulate a hung source: block until the per-alert deadline fires.
			<-ctx.Done()
			return ctx.Err()
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- m.evaluateAlertWithTimeout(context.Background(), &models.Alert{ID: 1})
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context deadline exceeded, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("evaluateAlertWithTimeout did not return within the deadline; a hung source can freeze the loop")
	}

	if hadDeadline := <-sawDeadline; !hadDeadline {
		t.Fatal("evaluation context carried no deadline")
	}
}

func newManagerTestAlert(t *testing.T) (*sqlite.DB, *models.Alert, *slog.Logger) {
	t.Helper()
	ctx := t.Context()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(ctx, sqlite.Options{
		Logger: logger,
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "alerts.db")},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
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
		LookbackSeconds: 300, FrequencySeconds: 60, IsActive: true,
		ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
		Severity: models.AlertSeverityWarning, LastState: models.AlertStateResolved,
	}
	if err := db.CreateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	return db, alert, logger
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
	if err := m.evaluateAlert(ctx, before); err == nil || !strings.Contains(err.Error(), "no provider registered") {
		t.Fatalf("evaluateAlert error = %v, want missing datasource provider", err)
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
	due, err := db.ListActiveAlertsDue(ctx)
	if err != nil || len(due) != 0 {
		t.Errorf("due after evaluation error = %d alerts, %v; want none", len(due), err)
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

type alertSenderFunc func(context.Context, AlertNotification) error

func (f alertSenderFunc) Send(ctx context.Context, notification AlertNotification) error {
	return f(ctx, notification)
}

func TestManagerDeliveryRetries(t *testing.T) {
	for _, tc := range []struct {
		name         string
		legacy       bool
		succeedAt    int
		wantAttempts int
	}{
		{name: "persistent failure", wantAttempts: 3},
		{name: "legacy failure", legacy: true, wantAttempts: 3},
		{name: "successful retry", succeedAt: 2, wantAttempts: 2},
		{name: "successful first attempt", succeedAt: 1, wantAttempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, alert, logger := newManagerTestAlert(t)
			ctx := t.Context()
			attempts := 0
			var historyID int64
			if tc.legacy {
				entry, err := db.InsertAlertHistory(ctx, alert.ID, models.AlertStatusTriggered, nil, "fired", map[string]any{"delivery_failed": true})
				if err != nil {
					t.Fatal(err)
				}
				historyID = entry.ID
				attempts = 1
			}
			sender := alertSenderFunc(func(_ context.Context, notification AlertNotification) error {
				if notification.Status != models.AlertStatusTriggered {
					return nil
				}
				attempts++
				if tc.succeedAt > 0 && attempts >= tc.succeedAt {
					return nil
				}
				return errors.New("outbound delivery unavailable")
			})
			for range 8 {
				m := NewManager(Options{DB: db, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})
				if err := m.handleTriggered(ctx, alert, 12); err != nil {
					t.Fatal(err)
				}
			}
			if attempts != tc.wantAttempts {
				t.Errorf("delivery attempts = %d, want %d", attempts, tc.wantAttempts)
			}
			history, err := db.ListAlertHistory(ctx, alert.ID, 10)
			if err != nil || len(history) != 1 {
				t.Fatalf("history = %d entries, %v; want one firing entry", len(history), err)
			}
			if historyID != 0 && history[0].ID != historyID {
				t.Errorf("history ID changed from %d to %d", historyID, history[0].ID)
			}
			if got := history[0].Payload["delivery_attempts"]; got != float64(tc.wantAttempts) {
				t.Errorf("persisted delivery_attempts = %v, want %d", got, tc.wantAttempts)
			}
			if got := history[0].Payload["delivery_failed"]; got != (tc.succeedAt == 0) {
				t.Errorf("persisted delivery_failed = %v, want %t", got, tc.succeedAt == 0)
			}
			if err := NewManager(Options{DB: db, Logger: logger, Sender: sender}).handleResolved(ctx, alert, 0); err != nil {
				t.Fatal(err)
			}
			if err := NewManager(Options{DB: db, Logger: logger, Sender: sender}).handleTriggered(ctx, alert, 12); err != nil {
				t.Fatal(err)
			}
			if attempts != tc.wantAttempts+1 {
				t.Errorf("new firing delivery attempts = %d, want %d", attempts, tc.wantAttempts+1)
			}
		})
	}
}

func TestManagerStopTwice(t *testing.T) {
	m := NewManager(Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	m.Stop()
	m.Stop()
}
