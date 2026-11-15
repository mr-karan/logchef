package storetest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func testAlertInstances(t *testing.T, ctx context.Context, s store.Store) {
	src := mkSource(t, ctx, s, "alert-instances")
	alert := &models.Alert{
		SourceID: src.ID, Name: "instances", Query: "SELECT count() FROM logs",
		QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
		LookbackSeconds: 300, FrequencySeconds: 60, IsActive: true,
		ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
		Severity: models.AlertSeverityWarning, LastState: models.AlertStateResolved,
	}
	if err := s.CreateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	t.Run("RoundTrip", func(t *testing.T) { testAlertInstanceRoundTrip(t, ctx, s, alert.ID) })
	t.Run("Notified", func(t *testing.T) { testAlertInstanceNotified(t, ctx, s, alert.ID) })
	t.Run("EvalSeq", func(t *testing.T) { testAlertEvalSeq(t, ctx, s, alert) })
	t.Run("ResolveClearsInstances", func(t *testing.T) { testResolveClearsInstances(t, ctx, s, alert.ID) })
	t.Run("CascadeOnDelete", func(t *testing.T) { testAlertInstanceCascade(t, ctx, s, alert.ID) })
}

// normalizeInstance puts timestamps in UTC and an empty group in one form, so
// instances from both backends compare with reflect.DeepEqual.
func normalizeInstance(instance *models.AlertInstance) *models.AlertInstance {
	out := *instance
	out.StateSince = out.StateSince.UTC()
	if out.LastSeenAt != nil {
		seen := out.LastSeenAt.UTC()
		out.LastSeenAt = &seen
	}
	if len(out.Group) == 0 {
		out.Group = nil
	}
	return &out
}

func testAlertInstanceRoundTrip(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID) {
	since := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	seen := since.Add(time.Minute)
	value := 12.5
	historyID := int64(41)
	grouped := &models.AlertInstance{
		AlertID: alertID, GroupKey: `["checkout"]`, Group: map[string]string{"service": "checkout"},
		State: models.AlertInstanceStateFiring, Value: &value, StateSince: since, LastSeenAt: &seen,
		HistoryID: &historyID, Notified: true,
	}
	ungrouped := &models.AlertInstance{AlertID: alertID, State: models.AlertInstanceStateFiring, StateSince: since}
	for _, instance := range []*models.AlertInstance{grouped, ungrouped} {
		if err := s.UpsertAlertInstance(ctx, instance); err != nil {
			t.Fatal(err)
		}
	}
	assertInstances(t, ctx, s, alertID, ungrouped, grouped)

	grouped.Notified = false
	grouped.HistoryID = nil
	if err := s.UpsertAlertInstance(ctx, grouped); err != nil {
		t.Fatal(err)
	}
	assertInstances(t, ctx, s, alertID, ungrouped, grouped)
}

func assertInstances(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID, want ...*models.AlertInstance) {
	t.Helper()
	got, err := s.ListAlertInstances(ctx, alertID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("ListAlertInstances = %d instances, want %d", len(got), len(want))
	}
	for i := range want {
		if g, w := normalizeInstance(got[i]), normalizeInstance(want[i]); !reflect.DeepEqual(g, w) {
			t.Errorf("instance %d = %+v, want %+v", i, g, w)
		}
	}
}

func testAlertInstanceNotified(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID) {
	historyID := int64(7)
	instance := &models.AlertInstance{AlertID: alertID, State: models.AlertInstanceStateFiring, StateSince: time.Now().UTC(), HistoryID: &historyID}
	if err := s.UpsertAlertInstance(ctx, instance); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkAlertInstanceNotified(ctx, alertID, "", historyID+1); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("notified for another episode err = %v, want ErrNotFound", err)
	}
	if err := s.MarkAlertInstanceNotified(ctx, alertID, "", historyID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListAlertInstances(ctx, alertID)
	if err != nil || got[0].GroupKey != "" || !got[0].Notified {
		t.Fatalf("instance after notified = %+v, %v; want notified", got, err)
	}
}

func testAlertEvalSeq(t *testing.T, ctx context.Context, s store.Store, alert *models.Alert) {
	seq := alert.EvalSeq
	if stored := evalSeq(t, ctx, s, alert.ID); stored != seq {
		t.Fatalf("stored eval_seq = %d, want %d as read with the alert", stored, seq)
	}
	if err := s.AdvanceAlertEvalSeq(ctx, alert.ID, seq); err != nil {
		t.Fatal(err)
	}
	if err := s.AdvanceAlertEvalSeq(ctx, alert.ID, seq); !errors.Is(err, models.ErrConflict) {
		t.Errorf("stale advance err = %v, want ErrConflict", err)
	}
	boom := errors.New("boom")
	if err := s.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.AdvanceAlertEvalSeq(ctx, alert.ID, seq+1); err != nil {
			return err
		}
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("rolled back advance err = %v", err)
	}
	assertEvalSeq(t, ctx, s, alert.ID, seq+1)

	if err := s.UpdateAlert(ctx, alert); err != nil {
		t.Fatal(err)
	}
	assertEvalSeq(t, ctx, s, alert.ID, seq+2)
	for _, read := range []func() ([]*models.Alert, error){
		func() ([]*models.Alert, error) { return s.ListAlertsBySource(ctx, alert.SourceID) },
		func() ([]*models.Alert, error) { return s.ListActiveAlertsDue(ctx) },
	} {
		alerts, err := read()
		if err != nil {
			t.Fatal(err)
		}
		for _, listed := range alerts {
			if listed.ID == alert.ID && listed.EvalSeq != seq+2 {
				t.Errorf("listed eval_seq = %d, want %d", listed.EvalSeq, seq+2)
			}
		}
	}
}

func testResolveClearsInstances(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID) {
	entry, err := s.InsertAlertHistory(ctx, alertID, models.AlertStatusTriggered, nil, "fired", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertAlertInstance(ctx, &models.AlertInstance{AlertID: alertID, State: models.AlertInstanceStateFiring, StateSince: entry.TriggeredAt, HistoryID: &entry.ID}); err != nil {
		t.Fatal(err)
	}
	seq := evalSeq(t, ctx, s, alertID)
	if err := s.ResolveAlertHistory(ctx, entry.ID, "resolved manually"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListAlertInstances(ctx, alertID); err != nil || len(got) != 0 {
		t.Errorf("instances after resolve = %+v, %v; want none", got, err)
	}
	if after := evalSeq(t, ctx, s, alertID); after <= seq {
		t.Errorf("eval_seq after resolve = %d, want above %d", after, seq)
	}
}

func testAlertInstanceCascade(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID) {
	if err := s.UpsertAlertInstance(ctx, &models.AlertInstance{AlertID: alertID, State: models.AlertInstanceStateFiring, StateSince: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAlert(ctx, alertID); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ListAlertInstances(ctx, alertID); err != nil || len(got) != 0 {
		t.Errorf("instances after alert delete = %+v, %v; want none", got, err)
	}
}

func evalSeq(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID) int64 {
	t.Helper()
	alert, err := s.GetAlert(ctx, alertID)
	if err != nil {
		t.Fatal(err)
	}
	return alert.EvalSeq
}

func assertEvalSeq(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID, want int64) {
	t.Helper()
	if got := evalSeq(t, ctx, s, alertID); got != want {
		t.Errorf("eval_seq = %d, want %d", got, want)
	}
}
