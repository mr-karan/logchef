package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/notify"
	"github.com/mr-karan/logchef/pkg/models"
)

// fakePagerDuty records the event and dedup key of each message it is sent.
type fakePagerDuty struct {
	mu     sync.Mutex
	events []string
}

func (f *fakePagerDuty) Send(_ context.Context, _ *models.NotificationDestination, _ string, _ int64, msg notify.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, string(msg.Event)+" "+msg.DedupKey)
	return nil
}

// pagerDutyDedupKey is the RFC 0006 incident key of one group:
// logchef:alert:<id>: and the first 16 hex characters of the SHA-256 of the
// group key.
func pagerDutyDedupKey(alertID models.AlertID, groupKey string) string {
	sum := sha256.Sum256([]byte(groupKey))
	return fmt.Sprintf("logchef:alert:%d:%s", alertID, hex.EncodeToString(sum[:])[:16])
}

// TestGroupByChangeResolvesOldGroups: a group_by change closes every open
// episode, and each group that announced its firing sends a resolved with
// its own PagerDuty dedup key, to the destinations that the firing reached,
// even when the same edit replaces them. A group that a silence kept
// unannounced sends nothing, and neither does a rule that does not notify on
// resolve or a group that a silence mutes at the edit.
func TestGroupByChangeResolvesOldGroups(t *testing.T) {
	for _, tc := range []struct {
		name            string
		notifyOnResolve bool
		silence         bool
		want            []string // the group keys that resolve
	}{
		{name: "notify on resolve", notifyOnResolve: true, want: []string{`["checkout"]`, `["search"]`}},
		{name: "notify on resolve off", notifyOnResolve: false},
		{name: "silenced at the edit", notifyOnResolve: true, silence: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newTestDB(t)
			log := discardLogger()
			ctx := context.Background()
			owner := newTestUser(t, db, "regroup-pd@example.com", "Owner")
			source := newTestSource(t, db, "regroup-pd")
			ds := newFakeDatasourceService(db, log, nil)
			req := newTestCreateAlertRequest()
			req.Query = "SELECT service, region, count() AS value FROM logs GROUP BY service, region"
			req.GroupBy = []string{"service"}
			req.NotifyOnResolve = &tc.notifyOnResolve
			alert, err := CreateAlert(ctx, db, ds, log, source.ID, owner, req)
			if err != nil {
				t.Fatal(err)
			}
			pd := &models.NotificationDestination{Name: "on-call", Type: models.DestinationTypePagerDuty, Scope: models.DestinationScopeGlobal,
				Secret: "0123456789abcdef0123456789abcdef", IsActive: true}
			if err := db.CreateNotificationDestination(ctx, pd); err != nil {
				t.Fatal(err)
			}
			if err := db.SetAlertDestinations(ctx, alert.ID, []models.DestinationID{pd.ID}); err != nil {
				t.Fatal(err)
			}
			for _, g := range []struct {
				service  string
				notified bool
			}{{"checkout", true}, {"search", true}, {"billing", false}} {
				key := fmt.Sprintf("[%q]", g.service)
				value := 12.0
				entry, err := db.InsertAlertHistory(ctx, alert.ID, key, map[string]string{"service": g.service}, models.AlertStatusTriggered, &value, "fired", nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.UpsertAlertInstance(ctx, &models.AlertInstance{AlertID: alert.ID, GroupKey: key, Group: map[string]string{"service": g.service},
					State: models.AlertInstanceStateFiring, Value: &value, StateSince: entry.TriggeredAt, HistoryID: &entry.ID, Notified: g.notified}); err != nil {
					t.Fatal(err)
				}
			}
			if tc.silence {
				start, end := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
				if err := db.CreateAlertSilence(ctx, &models.AlertSilence{SourceID: source.ID, Comment: "maintenance", ScheduleKind: models.SilenceOnce, StartsAt: &start, EndsAt: &end}); err != nil {
					t.Fatal(err)
				}
			}

			groupBy := []string{"service", "region"}
			if _, _, err := UpdateAlert(ctx, db, ds, log, owner, alert.ID, &models.UpdateAlertRequest{GroupBy: &groupBy, DestinationIDs: &[]models.DestinationID{}}); err != nil {
				t.Fatal(err)
			}
			if open, err := db.ListOpenAlertHistory(ctx, alert.ID); err != nil || len(open) != 0 {
				t.Fatalf("open episodes after the group_by change = %d, %v; want none", len(open), err)
			}

			pagerDuty := &fakePagerDuty{}
			drainer := notify.NewDrainer(notify.DrainerOptions{Store: db, Logger: log, Senders: map[models.DestinationType]notify.Sender{models.DestinationTypePagerDuty: pagerDuty}})
			if err := drainer.Drain(ctx); err != nil {
				t.Fatal(err)
			}
			want := []string{}
			for _, key := range tc.want {
				want = append(want, "resolved "+pagerDutyDedupKey(alert.ID, key))
			}
			got := slices.Clone(pagerDuty.events)
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("PagerDuty received %q, want %q", got, want)
			}
		})
	}
}
