package storetest

import (
	"context"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// AlertInstancesUpgradeSeed is portable SQL that seeds alert history at the
// schema version before add_alert_instances (RFC 0006 unit A1). Alerts 1 to 4
// are firing: delivered, retry pending, interrupted, and two open rows. Alert
// 5 is not firing. Closed rows sit beside the open ones.
var AlertInstancesUpgradeSeed = []string{
	`INSERT INTO users (id, email, full_name, role, status) VALUES (1, 'owner@test.dev', 'Owner', 'member', 'active')`,
	`INSERT INTO sources (id, name, _meta_is_auto_created, _meta_ts_field, source_type, connection_config, identity_key)
		VALUES (1, 'logs', FALSE, 'timestamp', 'clickhouse', '{}', 'clickhouse:ch:9000/logs/app')`,
	`INSERT INTO alerts (id, source_id, name, query_language, editor_mode, query, lookback_seconds, threshold_operator,
			threshold_value, frequency_seconds, severity, is_active, last_state, last_evaluated_at, webhook_urls_json, created_by)
		VALUES
		(1, 1, 'delivered', 'clickhouse-sql', 'native', 'SELECT count() FROM logs', 300, 'gt', 10, 60, 'warning', TRUE, 'firing', '2026-01-01T00:10:00Z', '["https://hooks.example.com/a"]', 1),
		(2, 1, 'retry pending', 'clickhouse-sql', 'native', 'SELECT count() FROM logs', 300, 'gt', 10, 60, 'warning', TRUE, 'firing', '2026-01-01T00:10:00Z', '["https://hooks.example.com/a"]', 1),
		(3, 1, 'interrupted', 'clickhouse-sql', 'native', 'SELECT count() FROM logs', 300, 'gt', 10, 60, 'warning', TRUE, 'firing', '2026-01-01T00:10:00Z', '["https://hooks.example.com/a"]', 1),
		(4, 1, 'two open rows', 'clickhouse-sql', 'native', 'SELECT count() FROM logs', 300, 'gt', 10, 60, 'warning', TRUE, 'firing', '2026-01-01T00:10:00Z', '["https://hooks.example.com/a"]', NULL),
		(5, 1, 'quiet', 'clickhouse-sql', 'native', 'SELECT count() FROM logs', 300, 'gt', 10, 60, 'warning', TRUE, 'resolved', '2026-01-01T00:10:00Z', '["https://hooks.example.com/a"]', 1)`,
	`INSERT INTO alert_history (id, alert_id, status, triggered_at, resolved_at, value, message, payload_json) VALUES
		(10, 1, 'resolved', '2025-12-31T00:00:00Z', '2025-12-31T01:00:00Z', 15, 'resolved', '{"delivery_attempts":1,"delivery_failed":false}'),
		(11, 1, 'triggered', '2026-01-01T00:00:00Z', NULL, 21, 'fired', '{"delivery_attempts":1,"delivery_failed":false,"delivery_in_progress":false}'),
		(20, 2, 'triggered', '2026-01-01T00:01:00Z', NULL, 22, 'fired', '{"delivery_attempts":1,"delivery_failed":true,"delivery_in_progress":false}'),
		(30, 3, 'triggered', '2026-01-01T00:02:00Z', NULL, 23, 'fired', '{"delivery_attempts":1,"delivery_failed":false,"delivery_in_progress":true}'),
		(40, 4, 'triggered', '2026-01-01T00:03:00Z', NULL, 24, 'fired', '{"delivery_attempts":1,"delivery_failed":false,"delivery_in_progress":false}'),
		(41, 4, 'triggered', '2026-01-01T00:04:00Z', NULL, 25, 'fired again', '{"delivery_attempts":1,"delivery_failed":false,"delivery_in_progress":false}'),
		(42, 4, 'error', '2026-01-01T00:05:00Z', NULL, NULL, 'Evaluation failed', '{"status":"error"}'),
		(50, 5, 'resolved', '2025-12-30T00:00:00Z', '2025-12-30T01:00:00Z', 30, 'resolved', NULL)`,
}

// CheckAlertInstancesUpgrade asserts, on a store migrated over
// AlertInstancesUpgradeSeed, that every history row survived with its id,
// that each firing rule has one backfilled instance, and that the first tick
// after the upgrade sends no firing message for an upgraded rule.
func CheckAlertInstancesUpgrade(t *testing.T, s store.Store) {
	t.Helper()
	checkUpgradedHistory(t, s)
	checkBackfilledInstances(t, s)
	firstTickAfterUpgrade(t, s)
}

func checkUpgradedHistory(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	want := map[models.AlertID]map[int64]models.AlertStatus{
		1: {10: models.AlertStatusResolved, 11: models.AlertStatusTriggered},
		2: {20: models.AlertStatusTriggered},
		3: {30: models.AlertStatusTriggered},
		4: {40: models.AlertStatusResolved, 41: models.AlertStatusTriggered, 42: models.AlertStatusError},
		5: {50: models.AlertStatusResolved},
	}
	for alertID, statuses := range want {
		rows, err := s.ListAlertHistory(ctx, alertID, 100)
		if err != nil {
			t.Fatal(err)
		}
		got := make(map[int64]models.AlertStatus, len(rows))
		for _, row := range rows {
			got[row.ID] = row.Status
		}
		if !reflect.DeepEqual(got, statuses) {
			t.Errorf("alert %d history = %v, want %v", alertID, got, statuses)
		}
	}
	superseded, err := s.GetAlertHistoryEntry(ctx, 4, 40)
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Message != "superseded at upgrade" || superseded.ResolvedAt == nil {
		t.Errorf("older open row = %+v, want closed as superseded at upgrade", superseded)
	}
}

func checkBackfilledInstances(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	lastEvaluated := time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC)
	backfill := func(alertID models.AlertID, historyID int64, value float64, minute int) []*models.AlertInstance {
		return []*models.AlertInstance{{
			AlertID: alertID, State: models.AlertInstanceStateFiring, Value: &value,
			StateSince: time.Date(2026, 1, 1, 0, minute, 0, 0, time.UTC), LastSeenAt: &lastEvaluated,
			HistoryID: &historyID, Notified: true,
		}}
	}
	want := map[models.AlertID][]*models.AlertInstance{
		1: backfill(1, 11, 21, 0),
		2: backfill(2, 20, 22, 1),
		3: backfill(3, 30, 23, 2),
		4: backfill(4, 41, 25, 4),
		5: {},
	}
	for alertID, instances := range want {
		got, err := s.ListAlertInstances(ctx, alertID)
		if err != nil {
			t.Fatal(err)
		}
		normalized := make([]*models.AlertInstance, 0, len(got))
		for _, instance := range got {
			normalized = append(normalized, normalizeInstance(instance))
		}
		if !reflect.DeepEqual(normalized, instances) {
			t.Errorf("alert %d instances = %+v, want %+v", alertID, normalized, instances)
		}
	}
}

// firstTickAfterUpgrade runs the job runner over the alert.evaluate jobs that
// the upgrade backfilled, until every rule was evaluated once. Every rule reads
// a value above its threshold. Only the rule that was not firing may notify.
func firstTickAfterUpgrade(t *testing.T, s store.Store) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sources := datasource.NewService(s, logger)
	sources.Register(fixedValueProvider{value: 12})
	sender := &recordingSender{}
	manager := alerts.NewManager(alerts.Options{
		Config:      config.AlertsConfig{Enabled: true, MaxConcurrentEvaluations: 4, HistoryLimit: 50},
		DB:          s,
		Datasources: sources,
		Logger:      logger,
		Sender:      sender,
	})
	runner := jobs.New(jobs.Options{Store: s, Logger: logger, PollInterval: 20 * time.Millisecond})
	runner.Register(manager.EvaluationKind())
	runner.Start(context.Background())
	stop := func() {
		if err := runner.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	}
	defer stop()

	seeded := time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC)
	deadline := time.Now().Add(10 * time.Second)
	for alertID := models.AlertID(1); alertID <= 5; alertID++ {
		for {
			alert, err := s.GetAlert(context.Background(), alertID)
			if err != nil {
				t.Fatal(err)
			}
			if alert.LastEvaluatedAt != nil && alert.LastEvaluatedAt.After(seeded) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("alert %d was not evaluated by the first tick", alertID)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	stop()

	sent := sender.sent()
	if len(sent) != 1 || sent[0].AlertID != 5 || sent[0].Status != models.AlertStatusTriggered {
		t.Errorf("first tick notifications = %+v, want one firing for alert 5", sent)
	}
}

type fixedValueProvider struct {
	datasource.Provider
	value float64
}

func (fixedValueProvider) Type() models.SourceType { return models.SourceTypeClickHouse }

func (p fixedValueProvider) EvaluateAlert(context.Context, *models.Source, datasource.AlertQueryRequest) (*models.QueryResult, error) {
	return &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "value", Type: "Float64"}},
		Logs:    []map[string]any{{"value": p.value}},
	}, nil
}

type recordingSender struct {
	mu            sync.Mutex
	notifications []alerts.AlertNotification
}

func (r *recordingSender) Send(_ context.Context, notification alerts.AlertNotification) alerts.DeliveryResults {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notifications = append(r.notifications, notification)
	return nil
}

func (r *recordingSender) sent() []alerts.AlertNotification {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]alerts.AlertNotification(nil), r.notifications...)
}
