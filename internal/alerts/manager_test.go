package alerts

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
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

func TestStopCancelsActiveEvaluation(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(context.Background(), sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "alerts.db")}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	source := &models.Source{Name: "logs", MetaTSField: "timestamp", Connection: models.ConnectionInfo{Host: "localhost:9000", Database: "logs", TableName: "logs"}}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	alert := &models.Alert{SourceID: source.ID, Name: "slow", QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative, Query: "SELECT 1", LookbackSeconds: 60, ThresholdOperator: models.AlertThresholdGreaterThan, FrequencySeconds: 60, Severity: models.AlertSeverityWarning, IsActive: true, LastState: models.AlertStateResolved}
	if err := db.CreateAlert(context.Background(), alert); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(Options{Logger: log, DB: db, Config: config.AlertsConfig{Enabled: true}})
	entered := make(chan struct{})
	manager.evalFn = func(ctx context.Context, _ *models.Alert) error { close(entered); <-ctx.Done(); return ctx.Err() }
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	manager.Start(ctx)
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("evaluation did not start")
	}
	start := time.Now()
	manager.Stop()
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Stop waited %s for active evaluation", elapsed)
	}
}
