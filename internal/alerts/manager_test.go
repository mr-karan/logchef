package alerts

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
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
	return db, createManagerTestAlert(t, db), logger
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
		LookbackSeconds: 300, FrequencySeconds: 60, IsActive: true,
		ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
		Severity: models.AlertSeverityWarning, LastState: models.AlertStateResolved,
	}
	if err := db.CreateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	return alert
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

func (f alertSenderFunc) Send(ctx context.Context, notification AlertNotification) DeliveryResults {
	return DeliveryResults{{Target: deliveryTargetID("test", "target"), Err: f(ctx, notification)}}
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
			db, alert, _ := newManagerTestAlert(t)
			ctx := t.Context()
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
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
			wantExhausted := 0
			if tc.succeedAt == 0 {
				wantExhausted = 1
			}
			if got := countLogs(t, &logs, "alert delivery retries exhausted"); got != wantExhausted {
				t.Errorf("exhaustion log count = %d, want %d", got, wantExhausted)
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

type logRecord struct {
	Level     string         `json:"level"`
	Message   string         `json:"msg"`
	AlertID   models.AlertID `json:"alert_id"`
	HistoryID int64          `json:"history_id"`
	Attempts  int            `json:"attempts"`
}

func decodeLogs(t *testing.T, logs *bytes.Buffer, msg string) []logRecord {
	t.Helper()
	var matched []logRecord
	decoder := json.NewDecoder(bytes.NewReader(logs.Bytes()))
	for {
		var record logRecord
		if err := decoder.Decode(&record); err != nil {
			if errors.Is(err, io.EOF) {
				return matched
			}
			t.Fatal(err)
		}
		if record.Message == msg {
			matched = append(matched, record)
		}
	}
}

func countLogs(t *testing.T, logs *bytes.Buffer, msg string) int {
	t.Helper()
	return len(decodeLogs(t, logs, msg))
}

// failHistoryWrites installs a trigger that rejects the matching alert_history
// writes and returns a function that removes it.
func failHistoryWrites(t *testing.T, path, event string) (restore func()) {
	t.Helper()
	ctx := t.Context()
	control, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = control.Close() })
	if _, err := control.ExecContext(ctx, "CREATE TRIGGER fail_history_write "+event+" BEGIN SELECT RAISE(FAIL, 'history write unavailable'); END"); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if _, err := control.ExecContext(ctx, "DROP TRIGGER fail_history_write"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagerReservationWriteFailureSendsNothing(t *testing.T) {
	for _, tc := range []struct {
		name  string
		seed  bool
		event string
	}{
		{name: "initial reservation", event: "BEFORE INSERT ON alert_history"},
		{name: "retry reservation", seed: true, event: "BEFORE UPDATE OF payload_json ON alert_history"},
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
			restore := failHistoryWrites(t, path, tc.event)
			sends := 0
			sender := alertSenderFunc(func(context.Context, AlertNotification) error {
				sends++
				return errors.New("outbound delivery unavailable")
			})
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			for range 8 {
				m := NewManager(Options{DB: db, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})
				if err := m.handleTriggered(ctx, alert, 12); err != nil {
					t.Fatal(err)
				}
			}
			if sends != 0 {
				t.Errorf("sends while the reservation write fails = %d, want 0", sends)
			}

			restore()
			sender = alertSenderFunc(func(context.Context, AlertNotification) error {
				sends++
				return nil
			})
			m := NewManager(Options{DB: db, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})
			if err := m.handleTriggered(ctx, alert, 12); err != nil {
				t.Fatal(err)
			}
			if sends != 1 {
				t.Errorf("sends after the reservation write recovers = %d, want 1", sends)
			}
		})
	}
}

func TestManagerOutcomeWriteFailureStopsAtMaxAttempts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.db")
	db, alert, _ := newManagerTestAlertAtPath(t, path)
	ctx := t.Context()
	failHistoryWrites(t, path, `BEFORE UPDATE OF payload_json ON alert_history
		WHEN json_extract(NEW.payload_json, '$.delivery_attempts') = json_extract(OLD.payload_json, '$.delivery_attempts')`)
	sends := 0
	sender := alertSenderFunc(func(context.Context, AlertNotification) error {
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
	if sends != maxDeliveryAttempts {
		t.Errorf("sends while the outcome write fails = %d, want %d", sends, maxDeliveryAttempts)
	}
	entry, err := db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
	if err != nil || entry.Payload["delivery_attempts"] != float64(maxDeliveryAttempts) {
		t.Fatalf("persisted attempts = %+v, %v; want %d", entry, err, maxDeliveryAttempts)
	}
	exhausted := decodeLogs(t, &logs, "alert delivery retries exhausted")
	if len(exhausted) != 1 {
		t.Fatalf("exhaustion log count = %d, want 1", len(exhausted))
	}
	if got := exhausted[0]; got.Level != "ERROR" || got.AlertID != alert.ID || got.HistoryID != entry.ID || got.Attempts != maxDeliveryAttempts {
		t.Errorf("exhaustion log = %+v, want error with alert %d, history %d, %d attempts", got, alert.ID, entry.ID, maxDeliveryAttempts)
	}
}

func TestManagerStopCancelsEvaluation(t *testing.T) {
	db, _, logger := newManagerTestAlert(t)
	manager := NewManager(Options{DB: db, Logger: logger, Config: config.AlertsConfig{Enabled: true, EvaluationInterval: time.Hour}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	manager.evalFn = func(ctx context.Context, _ *models.Alert) error { close(started); <-ctx.Done(); return ctx.Err() }
	manager.Start(ctx)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("evaluation did not start")
	}
	stopped := make(chan struct{})
	go func() { manager.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		cancel()
		<-stopped
		t.Fatal("Stop did not cancel in-flight evaluation within one second")
	}
}

func TestManagerSkipsRemainingAlertsAfterCancellation(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	alert.ID = 0
	alert.Name = "another alert"
	if err := db.CreateAlert(t.Context(), alert); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Options{DB: db, Logger: logger})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	evaluations := 0
	manager.evalFn = func(context.Context, *models.Alert) error { evaluations++; cancel(); return nil }
	manager.evaluateCycle(ctx)
	if evaluations != 1 {
		t.Errorf("evaluated %d alerts after cancellation; want one", evaluations)
	}
}

func TestCanceledEvaluationDoesNotRecordFailure(t *testing.T) {
	db, alert, _ := newManagerTestAlert(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	manager := NewManager(Options{DB: db, Logger: logger, Config: config.AlertsConfig{HistoryLimit: 100}})
	manager.evalFn = func(ctx context.Context, current *models.Alert) error {
		err := fmt.Errorf("query failed: %w", context.Canceled)
		manager.recordEvaluationError(ctx, current, err)
		return err
	}
	manager.evaluateCycle(t.Context())
	history, err := db.ListAlertHistory(t.Context(), alert.ID, 10)
	if err != nil || len(history) != 0 {
		t.Fatalf("cancellation created failure history = %v, %v", history, err)
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) {
		t.Fatalf("cancellation logged an evaluation error: %s", logs.String())
	}
}

func TestManagerEvaluatesAfterStartupSweepFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "alerts.db")
	db, alert, logger := newManagerTestAlertAtPath(t, path)
	if _, err := db.InsertAlertHistory(t.Context(), alert.ID, models.AlertStatusTriggered, nil, "fired", map[string]any{"delivery_in_progress": true}); err != nil {
		t.Fatal(err)
	}
	failHistoryWrites(t, path, "BEFORE UPDATE OF payload_json ON alert_history")
	manager := NewManager(Options{DB: db, Logger: logger, Config: config.AlertsConfig{Enabled: true, EvaluationInterval: time.Hour}})
	evaluated := make(chan struct{}, 1)
	manager.evalFn = func(context.Context, *models.Alert) error { evaluated <- struct{}{}; return nil }
	manager.Start(t.Context())
	defer manager.Stop()
	select {
	case <-evaluated:
	case <-time.After(time.Second):
		t.Fatal("startup sweep failure prevented alert evaluation")
	}
}

func TestManagerReservesDeliveryDeadline(t *testing.T) {
	db, alert, logger := newManagerTestAlert(t)
	for attempt := 1; attempt <= 2; attempt++ {
		started := time.Now().UTC()
		sender := alertSenderFunc(func(ctx context.Context, _ AlertNotification) error {
			entry, err := db.GetLatestUnresolvedAlertHistory(ctx, alert.ID)
			if err != nil {
				t.Fatal(err)
			}
			raw, ok := entry.Payload["delivery_deadline_at"].(string)
			if !ok {
				t.Errorf("reservation deadline = %v, want RFC 3339 UTC time", entry.Payload["delivery_deadline_at"])
				return errors.New("retry")
			}
			deadline, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				t.Fatal(err)
			}
			bound := alertEvaluationTimeout + smtpDialTimeout + deliveryBookkeepingTimeout
			if deadline.Before(started.Add(bound)) || deadline.After(time.Now().UTC().Add(bound)) || !strings.HasSuffix(raw, "Z") {
				t.Errorf("reservation deadline = %s, want current attempt plus %s in UTC", raw, bound)
			}
			return errors.New("retry")
		})
		manager := NewManager(Options{DB: db, Logger: logger, Sender: sender, Config: config.AlertsConfig{HistoryLimit: 100}})
		if err := manager.handleTriggered(t.Context(), alert, 12); err != nil {
			t.Fatal(err)
		}
		entry, err := db.GetLatestUnresolvedAlertHistory(t.Context(), alert.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := entry.Payload["delivery_deadline_at"]; exists {
			t.Error("finished attempt retained its delivery deadline")
		}
	}
}

type blockedRecoveryStore struct {
	store.Store
	started chan struct{}
}

func (s *blockedRecoveryStore) RecoverAlertDeliveries(ctx context.Context) error {
	close(s.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestManagerStopCancelsStartupSweep(t *testing.T) {
	db, _, logger := newManagerTestAlert(t)
	blocked := &blockedRecoveryStore{Store: db, started: make(chan struct{})}
	manager := NewManager(Options{DB: blocked, Logger: logger, Config: config.AlertsConfig{Enabled: true}})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	go func() { manager.Start(ctx); close(started) }()
	<-blocked.started
	stopped := make(chan struct{})
	go func() { manager.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		cancel()
		<-stopped
		t.Error("Stop could not cancel the startup sweep")
	}
	<-started
}
