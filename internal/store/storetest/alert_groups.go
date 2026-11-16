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

// testAlertGroups covers the storage of grouped rules (RFC 0006 unit A2):
// group_by on the rule, the group of each history row, per-group resolve and
// the instance counts of the alert list.
func testAlertGroups(t *testing.T, ctx context.Context, s store.Store) {
	src := mkSource(t, ctx, s, "alert-groups")
	newAlert := func(t *testing.T, groupBy []string) *models.Alert {
		t.Helper()
		alert := &models.Alert{
			SourceID: src.ID, Name: "grouped", Query: "SELECT service, count() AS value FROM logs GROUP BY service",
			QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
			LookbackSeconds: 300, FrequencySeconds: 60, IsActive: true, GroupBy: groupBy,
			ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
			Severity: models.AlertSeverityWarning, LastState: models.AlertStateResolved,
		}
		if err := s.CreateAlert(ctx, alert); err != nil {
			t.Fatal(err)
		}
		return alert
	}
	t.Run("GroupByRoundTrip", func(t *testing.T) { testAlertGroupByRoundTrip(t, ctx, s, newAlert(t, []string{"service", "region"})) })
	t.Run("GroupedHistory", func(t *testing.T) { testGroupedAlertHistory(t, ctx, s, newAlert(t, []string{"service"}).ID) })
	t.Run("ResolveOneGroup", func(t *testing.T) { testResolveAlertGroup(t, ctx, s, newAlert(t, []string{"service"}).ID) })
	t.Run("InstanceCounts", func(t *testing.T) {
		testAlertInstanceCounts(t, ctx, s, newAlert(t, []string{"service"}).ID, newAlert(t, nil).ID)
	})
}

func testAlertGroupByRoundTrip(t *testing.T, ctx context.Context, s store.Store, alert *models.Alert) {
	got, err := s.GetAlert(ctx, alert.ID)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"service", "region"}; !reflect.DeepEqual(got.GroupBy, want) {
		t.Fatalf("group_by after create = %q, want %q", got.GroupBy, want)
	}
	got.GroupBy = nil
	if err := s.UpdateAlert(ctx, got); err != nil {
		t.Fatal(err)
	}
	if got, err = s.GetAlert(ctx, alert.ID); err != nil || got.GroupBy != nil {
		t.Fatalf("group_by after clearing = %q, %v; want nil", got.GroupBy, err)
	}
}

func testGroupedAlertHistory(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID) {
	checkout := map[string]string{"service": "checkout"}
	older, err := s.InsertAlertHistory(ctx, alertID, `["checkout"]`, checkout, models.AlertStatusTriggered, nil, "checkout fired", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertAlertHistory(ctx, alertID, "", nil, models.AlertStatusError, nil, "evaluation failed", nil); err != nil {
		t.Fatal(err)
	}
	newer, err := s.InsertAlertHistory(ctx, alertID, `["search"]`, map[string]string{"service": "search"}, models.AlertStatusTriggered, nil, "search fired", nil)
	if err != nil {
		t.Fatal(err)
	}
	if older.GroupKey != `["checkout"]` || !reflect.DeepEqual(older.Group, checkout) {
		t.Errorf("inserted entry group = %q %v, want checkout", older.GroupKey, older.Group)
	}

	open, err := s.ListOpenAlertHistory(ctx, alertID)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 2 || open[0].ID != newer.ID || open[1].ID != older.ID {
		t.Fatalf("open history = %+v, want search then checkout", open)
	}
	if open[1].GroupKey != `["checkout"]` || !reflect.DeepEqual(open[1].Group, checkout) {
		t.Errorf("listed open entry group = %q %v, want checkout", open[1].GroupKey, open[1].Group)
	}
	all, err := s.ListAlertHistory(ctx, alertID, 10)
	if err != nil || len(all) != 3 {
		t.Fatalf("history = %+v, %v; want 3 entries", all, err)
	}
	if all[1].GroupKey != "" || all[1].Group != nil {
		t.Errorf("error entry group = %q %v, want none", all[1].GroupKey, all[1].Group)
	}
}

func testResolveAlertGroup(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID) {
	fire := func(key, service string) *models.AlertHistoryEntry {
		t.Helper()
		group := map[string]string{"service": service}
		entry, err := s.InsertAlertHistory(ctx, alertID, key, group, models.AlertStatusTriggered, nil, service+" fired", map[string]any{"delivery_in_progress": true})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertAlertInstance(ctx, &models.AlertInstance{
			AlertID: alertID, GroupKey: key, Group: group, State: models.AlertInstanceStateFiring,
			StateSince: entry.TriggeredAt, HistoryID: &entry.ID,
		}); err != nil {
			t.Fatal(err)
		}
		return entry
	}
	checkout := fire(`["checkout"]`, "checkout")
	search := fire(`["search"]`, "search")
	if err := s.MarkAlertEvaluated(ctx, alertID, models.AlertStateFiring); err != nil {
		t.Fatal(err)
	}

	seq := evalSeq(t, ctx, s, alertID)
	if err := s.ResolveAlertGroup(ctx, alertID, `["checkout"]`, "resolved checkout"); err != nil {
		t.Fatal(err)
	}
	assertOneGroupResolved(t, ctx, s, alertID, checkout.ID, search.ID, seq)

	if err := s.ResolveAlertGroup(ctx, alertID, `["checkout"]`, "again"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("resolve of a closed group err = %v, want ErrNotFound", err)
	}
	if err := s.ResolveAlertGroup(ctx, alertID, `["search"]`, "resolved search"); err != nil {
		t.Fatal(err)
	}
	if alert, err := s.GetAlert(ctx, alertID); err != nil || alert.LastState != models.AlertStateResolved {
		t.Fatalf("alert after the last resolve = %+v, %v; want resolved", alert, err)
	}
}

func testAlertInstanceCounts(t *testing.T, ctx context.Context, s store.Store, grouped, quiet models.AlertID) {
	now := time.Now().UTC()
	for _, instance := range []models.AlertInstance{
		{AlertID: grouped, GroupKey: `["a"]`, State: models.AlertInstanceStateFiring, StateSince: now},
		{AlertID: grouped, GroupKey: `["b"]`, State: models.AlertInstanceStateFiring, StateSince: now},
		{AlertID: grouped, GroupKey: `["c"]`, State: models.AlertInstanceStateOK, StateSince: now},
	} {
		if err := s.UpsertAlertInstance(ctx, &instance); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := s.CountAlertInstances(ctx, []models.AlertID{grouped, quiet})
	if err != nil {
		t.Fatal(err)
	}
	want := map[models.AlertID]models.AlertInstanceCounts{grouped: {Firing: 2, OK: 1}}
	if !reflect.DeepEqual(counts, want) {
		t.Errorf("counts = %+v, want %+v", counts, want)
	}
	if counts, err := s.CountAlertInstances(ctx, nil); err != nil || len(counts) != 0 {
		t.Errorf("counts of no alerts = %+v, %v; want empty", counts, err)
	}
}

// assertOneGroupResolved checks the state after checkout was resolved while
// search keeps firing.
func assertOneGroupResolved(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID, checkoutID, searchID, seq int64) {
	t.Helper()
	resolved, err := s.GetAlertHistoryEntry(ctx, alertID, checkoutID)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Status != models.AlertStatusResolved || resolved.ResolvedAt == nil || resolved.Message != "resolved checkout" {
		t.Errorf("resolved entry = %+v, want resolved with the message", resolved)
	}
	if resolved.Payload["delivery_in_progress"] != false {
		t.Errorf("resolved entry payload = %v, want its delivery interrupted", resolved.Payload)
	}
	untouched, err := s.GetAlertHistoryEntry(ctx, alertID, searchID)
	if err != nil || untouched.Status != models.AlertStatusTriggered || untouched.Payload["delivery_in_progress"] != true {
		t.Fatalf("other group's entry = %+v, %v; want it open and untouched", untouched, err)
	}
	instances, err := s.ListAlertInstances(ctx, alertID)
	if err != nil || len(instances) != 1 || instances[0].GroupKey != `["search"]` {
		t.Fatalf("instances after one resolve = %+v, %v; want search only", instances, err)
	}
	alert, err := s.GetAlert(ctx, alertID)
	if err != nil {
		t.Fatal(err)
	}
	if alert.LastState != models.AlertStateFiring || alert.EvalSeq <= seq {
		t.Errorf("alert after one resolve = state %s eval_seq %d, want firing above %d", alert.LastState, alert.EvalSeq, seq)
	}
}
