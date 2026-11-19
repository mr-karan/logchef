package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// AlertJobsUpgradeSeed is portable SQL that seeds alerts at the schema version
// before add_alert_evaluate_jobs (RFC 0006 unit J2), plus the system setting
// that the migration removes.
var AlertJobsUpgradeSeed = []string{
	`INSERT INTO sources (id, name, _meta_is_auto_created, _meta_ts_field, source_type, connection_config, identity_key)
		VALUES (1, 'logs', FALSE, 'timestamp', 'clickhouse', '{}', 'clickhouse:ch:9000/logs/app')`,
	`INSERT INTO alerts (id, source_id, name, query_language, editor_mode, query, lookback_seconds, threshold_operator,
			threshold_value, frequency_seconds, severity, is_active, last_state, last_evaluated_at)
		VALUES
		(1, 1, 'evaluated', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 60, 'warning', TRUE, 'resolved', '2026-01-01T00:10:00Z'),
		(2, 1, 'below the floor', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 5, 'warning', TRUE, 'resolved', '2026-01-01T00:10:00Z'),
		(3, 1, 'paused', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 300, 'warning', FALSE, 'resolved', NULL),
		(4, 1, 'never evaluated', 'clickhouse-sql', 'native', 'SELECT 1', 300, 'gt', 10, 120, 'warning', TRUE, 'resolved', NULL)`,
	`INSERT INTO system_settings (key, value, value_type, category, description, is_sensitive) VALUES
		('alerts.evaluation_interval', '1m', 'duration', 'alerts', 'How often to evaluate alert rules', FALSE),
		('alerts.test_kept', 'x', 'string', 'alerts', 'Not touched by the migration', FALSE)`,
}

// CheckAlertJobsUpgrade asserts the alert.evaluate jobs that the migration
// backfilled for AlertJobsUpgradeSeed: one per alert, due one frequency after
// the last evaluation or now, with the 10-second floor on the interval.
func CheckAlertJobsUpgrade(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()
	evaluated := time.Date(2026, 1, 1, 0, 10, 0, 0, time.UTC)
	for _, want := range []struct {
		key     string
		expr    string
		enabled bool
		next    time.Time // zero: due at the migration
	}{
		{key: "1", expr: "60", enabled: true, next: evaluated.Add(time.Minute)},
		{key: "2", expr: "10", enabled: true, next: evaluated.Add(5 * time.Second)},
		{key: "3", expr: "300", enabled: false},
		{key: "4", expr: "120", enabled: true},
	} {
		job, err := s.GetJob(ctx, string(alerts.EvaluateKind), want.key)
		if err != nil {
			t.Fatalf("job of alert %s: %v", want.key, err)
		}
		def := models.JobDefinition{Kind: string(alerts.EvaluateKind), Key: want.key, ScheduleKind: "interval", ScheduleExpr: want.expr, Timezone: "UTC", Misfire: "run_once", Enabled: want.enabled}
		if job.JobDefinition != def {
			t.Errorf("job of alert %s = %+v, want %+v", want.key, job.JobDefinition, def)
		}
		switch {
		case !want.next.IsZero() && !job.NextRunAt.Equal(want.next):
			t.Errorf("job of alert %s next_run_at = %v, want %v", want.key, job.NextRunAt, want.next)
		case want.next.IsZero() && time.Since(job.NextRunAt).Abs() > time.Minute:
			t.Errorf("job of alert %s next_run_at = %v, want the migration time", want.key, job.NextRunAt)
		}
	}

	// The backfilled jobs are due in the store's own time comparison; the
	// paused rule is not claimed.
	claimed, err := s.ClaimJobs(ctx, string(alerts.EvaluateKind), "upgrade-check", time.Minute, 10)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, job := range claimed {
		keys[job.Key] = true
	}
	if len(claimed) != 3 || !keys["1"] || !keys["2"] || !keys["4"] {
		t.Errorf("claimed backfilled jobs %v, want alerts 1, 2 and 4", keys)
	}

	if _, err := s.GetSetting(ctx, "alerts.evaluation_interval"); !models.IsNotFound(err) {
		t.Errorf("alerts.evaluation_interval after upgrade: %v, want not found", err)
	}
	if _, err := s.GetSetting(ctx, "alerts.test_kept"); err != nil {
		t.Errorf("an unrelated setting did not survive: %v", err)
	}
}
