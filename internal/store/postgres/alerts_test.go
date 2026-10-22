package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/storetest"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestPruneAlertHistoryPlan(t *testing.T) {
	dsn := resetSchema(t)
	opener(dsn)(t)
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	query := storetest.GeneratedQuery(t, "sqlc/queries.sql.go", "pruneAlertHistory")
	rows, err := conn.Query(ctx, "EXPLAIN "+query, int64(1), int64(1), int32(1))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatal(err)
		}
		t.Log(detail)
		if strings.Contains(detail, "(SubPlan ") || strings.Contains(detail, "target.") {
			t.Errorf("pruning repeats a subquery per history row: %s", detail)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentResolvePreservesNewIncident(t *testing.T) {
	dsn := resetSchema(t)
	s := opener(dsn)(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	monitor := connect(t, ctx, dsn)
	source := &models.Source{
		Name: "resolve fixture", MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{Host: "localhost:9000", Database: "logs", TableName: "logs"},
	}
	if err := source.SyncConnectionConfig(); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	alert := &models.Alert{
		SourceID: source.ID, Name: "resolve fixture", Query: "SELECT count() FROM logs",
		QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
		LookbackSeconds: 300, FrequencySeconds: 60, IsActive: true,
		ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
		Severity: models.AlertSeverityWarning, LastState: models.AlertStateFiring,
	}
	if err := s.CreateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	entry, err := s.InsertAlertHistory(ctx, alert.ID, models.AlertStatusTriggered, nil, "fired", map[string]any{"delivery_in_progress": true})
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	done := make(chan struct{})
	var newer *models.AlertHistoryEntry
	if err := s.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.ResolveAlertHistory(ctx, entry.ID, "caller A"); err != nil {
			return err
		}
		go func() {
			defer close(done)
			result <- s.ResolveAlertHistory(ctx, entry.ID, "stale B")
		}()
		t.Cleanup(func() {
			cancel()
			<-done
		})
		awaitLockWaitOrDone(t, ctx, monitor, done)
		var err error
		newer, err = tx.InsertAlertHistory(ctx, alert.ID, models.AlertStatusTriggered, nil, "fired again", nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, models.ErrNotFound) {
		t.Errorf("stale caller B error = %v, want not found", err)
	} else {
		t.Logf("stale caller B error = %v", err)
	}
	stored, err := s.GetAlertHistoryEntry(ctx, alert.ID, newer.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("newer incident status=%s message=%q", stored.Status, stored.Message)
	if stored.Status != models.AlertStatusTriggered || stored.Message != "fired again" {
		t.Errorf("stale caller changed newer incident: status=%s message=%q", stored.Status, stored.Message)
	}
}
