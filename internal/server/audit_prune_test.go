package server

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// seedAuditEvents inserts n events that occurred at the given times, oldest
// first, and returns their IDs in that order.
func seedAuditEvents(t *testing.T, db *sqlite.DB, at ...time.Time) []string {
	t.Helper()
	ids := make([]string, len(at))
	for i, occurred := range at {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatal(err)
		}
		ev := &models.AuditEvent{
			ID:         id.String(),
			OccurredAt: occurred,
			Action:     "setting.update",
			Outcome:    models.AuditOutcomeSuccess,
			ActorType:  models.AuditActorSystem,
			AuthMethod: models.AuditAuthStartup,
		}
		if err := db.InsertAuditEvent(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
		ids[i] = ev.ID
	}
	return ids
}

func remainingAuditIDs(t *testing.T, db *sqlite.DB) []string {
	t.Helper()
	events, err := db.ListAuditEvents(context.Background(), models.AuditFilter{Ascending: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(events))
	for i := range events {
		ids[i] = events[i].ID
	}
	return ids
}

func TestCleanupPrunesAuditEvents(t *testing.T) {
	now := time.Now().UTC()
	minutesAgo := func(n int) []time.Time {
		at := make([]time.Time, n)
		for i := range at {
			at[i] = now.Add(time.Duration(i-n) * time.Minute)
		}
		return at
	}
	twoDaysAgo := now.Add(-48 * time.Hour)

	for _, tc := range []struct {
		name          string
		retentionDays int
		maxRows       int
		seed          []time.Time
		keepFrom      int // index of the oldest seeded event that must remain
	}{
		{"retention deletes events older than the window", 1, 0,
			append([]time.Time{twoDaysAgo, twoDaysAgo.Add(time.Minute), twoDaysAgo.Add(2 * time.Minute)}, minutesAgo(2)...), 3},
		{"max rows keeps the newest", 0, 10, minutesAgo(25), 15},
		{"zero bounds keep every event", 0, 0,
			append([]time.Time{twoDaysAgo.Add(-365 * 24 * time.Hour), twoDaysAgo}, minutesAgo(23)...), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newServerTestDB(t)
			cfg := testOAuthConfig()
			cfg.Audit.RetentionDays, cfg.Audit.MaxRows = tc.retentionDays, tc.maxRows
			srv := newServerForTest(t, cfg, db, nil)
			ids := seedAuditEvents(t, db, tc.seed...)

			srv.cleanupExpiredBackgroundState()

			got, want := remainingAuditIDs(t, db), ids[tc.keepFrom:]
			if len(got) != len(want) {
				t.Fatalf("%d events remain, want %d", len(got), len(want))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("remaining event %d is %s, want %s: pruning must delete the oldest first", i, got[i], want[i])
				}
			}
		})
	}
}
