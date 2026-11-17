package storetest

import (
	"context"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/store"
)

// AlertDeliveriesUpgradeSeed is portable SQL that seeds open and closed
// episodes at the schema version before add_notification_deliveries (RFC
// 0006 unit D2). Group a was delivered, b has a retry pending, c was held
// back by a silence and owes its notification, d moved state inside a
// silence. Row 5 is closed.
var AlertDeliveriesUpgradeSeed = []string{
	`INSERT INTO users (id, email, full_name, role, status) VALUES (1, 'owner@test.dev', 'Owner', 'member', 'active')`,
	`INSERT INTO sources (id, name, _meta_is_auto_created, _meta_ts_field, source_type, connection_config, identity_key)
		VALUES (1, 'logs', FALSE, 'timestamp', 'clickhouse', '{}', 'clickhouse:ch:9000/logs/app')`,
	`INSERT INTO alerts (id, source_id, name, query_language, editor_mode, query, lookback_seconds, threshold_operator,
			threshold_value, frequency_seconds, severity, is_active, last_state, created_by)
		VALUES (1, 1, 'grouped', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 60, 'warning', TRUE, 'firing', 1)`,
	`INSERT INTO alert_history (id, alert_id, group_key, status, triggered_at, value, message, payload_json) VALUES
		(1, 1, 'a', 'triggered', '2026-01-01T00:00:00Z', 20, 'fired', '{"status":"triggered","delivery_attempts":1,"delivery_failed":false,"delivery_in_progress":false,"delivery_pending":[]}'),
		(2, 1, 'b', 'triggered', '2026-01-01T00:00:00Z', 20, 'fired', '{"status":"triggered","delivery_attempts":2,"delivery_failed":true,"delivery_error":"status 500","delivery_pending":["webhook:x"]}'),
		(3, 1, 'c', 'triggered', '2026-01-01T00:00:00Z', 20, 'fired', '{"status":"triggered","silenced_by":[4],"delivery_silenced":true}'),
		(4, 1, 'd', 'no_data', '2026-01-01T00:00:00Z', NULL, 'no data', '{"status":"no_data","silenced_by":[4],"delivery_silenced":true,"delivery_attempts":0}'),
		(5, 1, 'e', 'resolved', '2025-12-31T00:00:00Z', 20, 'resolved', '{"delivery_attempts":1,"delivery_failed":false}')`,
	`INSERT INTO alert_instances (alert_id, group_key, group_json, state, value, state_since, history_id, notified) VALUES
		(1, 'a', '{}', 'firing', 20, '2026-01-01T00:00:00Z', 1, TRUE),
		(1, 'b', '{}', 'firing', 20, '2026-01-01T00:00:00Z', 2, FALSE),
		(1, 'c', '{}', 'firing', 20, '2026-01-01T00:00:00Z', 3, FALSE),
		(1, 'd', '{}', 'no_data', NULL, '2026-01-01T00:00:00Z', 4, FALSE)`,
}

// CheckAlertDeliveriesUpgrade asserts that the migration kept every history
// row, marked notified each open episode that a silence did not hold back,
// and removed the delivery bookkeeping from open payloads only.
func CheckAlertDeliveriesUpgrade(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	history, err := s.ListAlertHistory(ctx, 1, 10)
	if err != nil || len(history) != 5 {
		t.Fatalf("history = %d rows, %v; want 5", len(history), err)
	}
	payloads := map[int64]map[string]any{}
	for _, entry := range history {
		payloads[entry.ID] = entry.Payload
	}
	for id := int64(1); id <= 4; id++ {
		for key := range payloads[id] {
			if strings.HasPrefix(key, "delivery_") {
				t.Errorf("open entry %d kept %s: %v", id, key, payloads[id])
			}
		}
		if payloads[id]["status"] == nil {
			t.Errorf("open entry %d lost its other keys: %v", id, payloads[id])
		}
	}
	if payloads[3]["silenced_by"] == nil {
		t.Errorf("held-back entry lost silenced_by: %v", payloads[3])
	}
	if payloads[5]["delivery_attempts"] == nil {
		t.Errorf("closed entry payload changed: %v", payloads[5])
	}
	instances, err := s.ListAlertInstances(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	notified := map[string]bool{}
	for _, instance := range instances {
		notified[instance.GroupKey] = instance.Notified
	}
	want := map[string]bool{"a": true, "b": true, "c": false, "d": false}
	for key, w := range want {
		if got, ok := notified[key]; !ok || got != w {
			t.Errorf("instance %s notified = %v (present %v), want %v", key, got, ok, w)
		}
	}
}
