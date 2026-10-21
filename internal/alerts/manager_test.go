package alerts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
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
	return newManagerTestAlertAtPath(t, filepath.Join(t.TempDir(), "alerts.db"))
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
	if err := m.evaluateAlert(ctx, before); err == nil {
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

func TestManagerDeliveryHistoryWriteFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		seed      bool
		trigger   string
		wantSends int
	}{
		{name: "initial reservation", trigger: "BEFORE INSERT ON alert_history", wantSends: 0},
		{name: "retry reservation", seed: true, trigger: "BEFORE UPDATE OF payload_json ON alert_history", wantSends: 0},
		{name: "delivery outcome", trigger: `BEFORE UPDATE OF payload_json ON alert_history
			WHEN json_extract(NEW.payload_json, '$.delivery_attempts') = json_extract(OLD.payload_json, '$.delivery_attempts')`, wantSends: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "alerts.db")
			db, alert, _ := newManagerTestAlertAtPath(t, path)
			ctx := t.Context()
			if tc.seed {
				if _, err := db.InsertAlertHistory(ctx, alert.ID, models.AlertStatusTriggered, nil, "fired", map[string]any{
					"delivery_attempts": 1, "delivery_failed": true,
				}); err != nil {
					t.Fatal(err)
				}
			}
			control, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = control.Close() })
			if _, err := control.ExecContext(ctx, "CREATE TRIGGER fail_history_write "+tc.trigger+" BEGIN SELECT RAISE(FAIL, 'history write unavailable'); END"); err != nil {
				t.Fatal(err)
			}
			sends := 0
			sender := alertSenderFunc(func(_ context.Context, _ AlertNotification) error {
				sends++
				entry, err := db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
				if err != nil {
					t.Errorf("history at send = %v, want persisted reservation", err)
				} else if got := entry.Payload["delivery_attempts"]; got != float64(sends) {
					t.Errorf("reserved attempts at send = %v, want %d", got, sends)
				}
				return errors.New("outbound delivery unavailable")
			})
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			for range 8 {
				m := NewManager(Options{DB: db, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})
				if err := m.handleTriggered(ctx, alert, 12); err != nil {
					t.Fatal(err)
				}
			}
			if sends != tc.wantSends {
				t.Errorf("sends during history write failures = %d, want %d", sends, tc.wantSends)
			}
			if tc.wantSends == 3 {
				entry, err := db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
				if err != nil || entry.Payload["delivery_attempts"] != float64(3) {
					t.Fatalf("persisted attempts = %+v, %v; want 3", entry, err)
				}
				count := 0
				decoder := json.NewDecoder(&logs)
				for {
					var record struct {
						Level     string         `json:"level"`
						Message   string         `json:"msg"`
						AlertID   models.AlertID `json:"alert_id"`
						HistoryID int64          `json:"history_id"`
						Attempts  int            `json:"attempts"`
					}
					if err := decoder.Decode(&record); err != nil {
						if errors.Is(err, io.EOF) {
							break
						}
						t.Fatal(err)
					}
					if record.Message == "alert delivery retries exhausted" {
						count++
						if record.Level != "ERROR" || record.AlertID != alert.ID || record.HistoryID != entry.ID || record.Attempts != 3 {
							t.Errorf("exhaustion log = %+v, want error with alert %d, history %d, 3 attempts", record, alert.ID, entry.ID)
						}
					}
				}
				if count != 1 {
					t.Errorf("exhaustion log count = %d, want 1", count)
				}
			}
			if _, err := control.ExecContext(ctx, "DROP TRIGGER fail_history_write"); err != nil {
				t.Fatal(err)
			}
			if tc.wantSends == 0 {
				m := NewManager(Options{DB: db, Logger: logger, Sender: alertSenderFunc(func(context.Context, AlertNotification) error {
					sends++
					return nil
				})})
				if err := m.handleTriggered(ctx, alert, 12); err != nil {
					t.Fatal(err)
				}
				if sends != 1 {
					t.Errorf("sends after history writes recover = %d, want 1", sends)
				}
			}
		})
	}
}
