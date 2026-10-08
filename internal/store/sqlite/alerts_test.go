package sqlite

import (
	"context"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func createTestAlert(t *testing.T, ctx context.Context, db *DB, sourceID models.SourceID, name string, frequencySeconds int, isActive bool) models.AlertID {
	t.Helper()
	alert := &models.Alert{
		SourceID:          sourceID,
		Name:              name,
		QueryLanguage:     models.QueryLanguageClickHouseSQL,
		EditorMode:        models.AlertEditorModeNative,
		Query:             "SELECT count() FROM logs",
		LookbackSeconds:   300,
		ThresholdOperator: models.AlertThresholdGreaterThan,
		ThresholdValue:    10,
		FrequencySeconds:  frequencySeconds,
		Severity:          models.AlertSeverityWarning,
		IsActive:          isActive,
		LastState:         models.AlertStateResolved,
	}
	if err := db.CreateAlert(ctx, alert); err != nil {
		t.Fatalf("CreateAlert(%s): %v", name, err)
	}
	return alert.ID
}

func createTestSource(t *testing.T, ctx context.Context, db *DB) models.SourceID {
	t.Helper()
	src := &models.Source{
		Name:        "logs.alerts",
		MetaTSField: "timestamp",
		Connection:  models.ConnectionInfo{Host: "localhost:9000", Database: "logs", TableName: "alerts"},
	}
	if err := src.SyncConnectionConfig(); err != nil {
		t.Fatalf("SyncConnectionConfig: %v", err)
	}
	if err := db.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	return src.ID
}

// setLastEvaluatedAt stores the result of a SQLite time expression evaluated
// against SQLite's own clock, so fixtures stay relative to the query's 'now'.
func setLastEvaluatedAt(t *testing.T, ctx context.Context, db *DB, alertID models.AlertID, expr string) {
	t.Helper()
	if _, err := db.writeDB.ExecContext(ctx, "UPDATE alerts SET last_evaluated_at = "+expr+" WHERE id = ?", alertID); err != nil {
		t.Fatalf("set last_evaluated_at for alert %d: %v", alertID, err)
	}
}

func dueAlertIDs(t *testing.T, ctx context.Context, db *DB) map[models.AlertID]bool {
	t.Helper()
	due, err := db.ListActiveAlertsDue(ctx)
	if err != nil {
		t.Fatalf("ListActiveAlertsDue: %v", err)
	}
	ids := make(map[models.AlertID]bool, len(due))
	for _, alert := range due {
		ids[alert.ID] = true
	}
	return ids
}

// Regression for issue #124: RFC3339 last_evaluated_at values ("...T...Z")
// compared as text against datetime()'s "YYYY-MM-DD HH:MM:SS" cutoff, so an
// overdue alert stayed "not due" until the cutoff moved to the next date.
func TestListActiveAlertsDue_TimestampFormats(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()
	sourceID := createTestSource(t, ctx, db)

	cases := []struct {
		name             string
		frequencySeconds int
		isActive         bool
		lastEvaluatedAt  string // SQLite expression; empty leaves it NULL
		wantDue          bool
	}{
		{name: "never evaluated", frequencySeconds: 300, isActive: true, wantDue: true},
		{name: "inactive never evaluated", frequencySeconds: 300, isActive: false, wantDue: false},
		{
			name:             "recent RFC3339",
			frequencySeconds: 3600,
			isActive:         true,
			lastEvaluatedAt:  "strftime('%Y-%m-%dT%H:%M:%SZ', 'now')",
			wantDue:          false,
		},
		{
			// Midnight of the cutoff's own date: always at or before the cutoff,
			// and always on the same date, which the text comparison got wrong.
			name:             "overdue same-day RFC3339",
			frequencySeconds: 300,
			isActive:         true,
			lastEvaluatedAt:  "strftime('%Y-%m-%dT00:00:00Z', 'now', '-300 seconds')",
			wantDue:          true,
		},
		{
			name:             "overdue same-day legacy format",
			frequencySeconds: 300,
			isActive:         true,
			lastEvaluatedAt:  "strftime('%Y-%m-%d 00:00:00', 'now', '-300 seconds')",
			wantDue:          true,
		},
		{
			name:             "overdue across midnight RFC3339",
			frequencySeconds: 300,
			isActive:         true,
			lastEvaluatedAt:  "strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-1 day', '-300 seconds')",
			wantDue:          true,
		},
		{
			name:             "exactly one interval ago",
			frequencySeconds: 300,
			isActive:         true,
			lastEvaluatedAt:  "strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-300 seconds')",
			wantDue:          true,
		},
		{
			name:             "short frequency elapsed",
			frequencySeconds: 60,
			isActive:         true,
			lastEvaluatedAt:  "strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-600 seconds')",
			wantDue:          true,
		},
		{
			name:             "long frequency not elapsed",
			frequencySeconds: 3600,
			isActive:         true,
			lastEvaluatedAt:  "strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-600 seconds')",
			wantDue:          false,
		},
		{
			name:             "inactive overdue",
			frequencySeconds: 300,
			isActive:         false,
			lastEvaluatedAt:  "strftime('%Y-%m-%dT%H:%M:%SZ', 'now', '-1 day')",
			wantDue:          false,
		},
	}

	ids := make([]models.AlertID, len(cases))
	for i, tc := range cases {
		ids[i] = createTestAlert(t, ctx, db, sourceID, tc.name, tc.frequencySeconds, tc.isActive)
		if tc.lastEvaluatedAt != "" {
			setLastEvaluatedAt(t, ctx, db, ids[i], tc.lastEvaluatedAt)
		}
	}

	due := dueAlertIDs(t, ctx, db)
	for i, tc := range cases {
		if due[ids[i]] != tc.wantDue {
			t.Errorf("%s: due = %v, want %v", tc.name, due[ids[i]], tc.wantDue)
		}
	}
}

// The timestamps written by MarkAlertEvaluated and MarkAlertTriggered must be
// comparable with the due cutoff. A zero frequency puts the cutoff at 'now',
// so any evaluation written before the query is due.
func TestListActiveAlertsDue_AfterMark(t *testing.T) {
	marks := map[string]func(*DB, context.Context, models.AlertID) error{
		"MarkAlertEvaluated": (*DB).MarkAlertEvaluated,
		"MarkAlertTriggered": (*DB).MarkAlertTriggered,
	}
	for name, mark := range marks {
		t.Run(name, func(t *testing.T) {
			db := newTxTestDB(t)
			ctx := context.Background()
			alertID := createTestAlert(t, ctx, db, createTestSource(t, ctx, db), name, 3600, true)

			if err := mark(db, ctx, alertID); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if dueAlertIDs(t, ctx, db)[alertID] {
				t.Fatalf("alert due right after %s with a 3600s frequency", name)
			}

			if _, err := db.writeDB.ExecContext(ctx, "UPDATE alerts SET frequency_seconds = 0 WHERE id = ?", alertID); err != nil {
				t.Fatalf("set frequency_seconds: %v", err)
			}
			if !dueAlertIDs(t, ctx, db)[alertID] {
				t.Fatalf("alert not due after %s once its interval elapsed", name)
			}
		})
	}
}
