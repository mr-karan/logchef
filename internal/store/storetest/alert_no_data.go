package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// testAlertNoData covers the storage of the no-data policy (RFC 0006 unit
// A3): the two rule options, no_data episodes as open episodes, and the
// no_data summary state.
func testAlertNoData(t *testing.T, ctx context.Context, s store.Store) {
	src := mkSource(t, ctx, s, "alert-no-data")
	newAlert := func(t *testing.T, policy models.AlertNoDataPolicy, notify bool) *models.Alert {
		t.Helper()
		alert := &models.Alert{
			SourceID: src.ID, Name: "no data", Query: "SELECT count() FROM logs",
			QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
			LookbackSeconds: 300, FrequencySeconds: 60, IsActive: true, NoDataPolicy: policy, NotifyOnResolve: notify,
			ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
			Severity: models.AlertSeverityWarning, LastState: models.AlertStateResolved,
		}
		if err := s.CreateAlert(ctx, alert); err != nil {
			t.Fatal(err)
		}
		return alert
	}
	t.Run("OptionsRoundTrip", func(t *testing.T) { testAlertRuleOptions(t, ctx, s, newAlert(t, models.AlertNoDataKeep, false)) })
	t.Run("NoDataEpisode", func(t *testing.T) { testNoDataEpisode(t, ctx, s, newAlert(t, models.AlertNoDataNoData, true).ID) })
	t.Run("SummaryState", func(t *testing.T) { testNoDataSummaryState(t, ctx, s, newAlert(t, models.AlertNoDataNoData, true).ID) })
}

func testAlertRuleOptions(t *testing.T, ctx context.Context, s store.Store, alert *models.Alert) {
	got, err := s.GetAlert(ctx, alert.ID)
	if err != nil || got.NoDataPolicy != models.AlertNoDataKeep || got.NotifyOnResolve {
		t.Fatalf("after create = %+v, %v; want keep without resolve notifications", got, err)
	}
	got.NoDataPolicy = models.AlertNoDataNoData
	got.NotifyOnResolve = true
	if err := s.UpdateAlert(ctx, got); err != nil {
		t.Fatal(err)
	}
	if got, err = s.GetAlert(ctx, alert.ID); err != nil || got.NoDataPolicy != models.AlertNoDataNoData || !got.NotifyOnResolve {
		t.Fatalf("after update = %+v, %v; want no_data with resolve notifications", got, err)
	}
	unset := &models.Alert{SourceID: alert.SourceID, Name: "unset", Query: alert.Query, QueryLanguage: alert.QueryLanguage,
		EditorMode: alert.EditorMode, LookbackSeconds: 300, FrequencySeconds: 60, ThresholdOperator: alert.ThresholdOperator,
		Severity: alert.Severity, LastState: models.AlertStateResolved}
	if err := s.CreateAlert(ctx, unset); err != nil || unset.NoDataPolicy != models.AlertNoDataZero {
		t.Fatalf("rule written without a policy = %q, %v; want the zero policy", unset.NoDataPolicy, err)
	}
}

// testNoDataEpisode: a no_data row is an open episode for every query that
// reads, moves, resolves or prunes open episodes.
func testNoDataEpisode(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID) {
	key := `["checkout"]`
	group := map[string]string{"service": "checkout"}
	entry, err := s.InsertAlertHistory(ctx, alertID, key, group, models.AlertStatusNoData, nil, "no data", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertAlertInstance(ctx, &models.AlertInstance{AlertID: alertID, GroupKey: key, Group: group, State: models.AlertInstanceStateNoData, StateSince: entry.TriggeredAt, HistoryID: &entry.ID}); err != nil {
		t.Fatal(err)
	}
	open, err := s.ListOpenAlertHistory(ctx, alertID)
	if err != nil || len(open) != 1 || open[0].ID != entry.ID {
		t.Fatalf("open episodes = %+v, %v; want the no_data row", open, err)
	}
	if latest, err := s.GetLatestUnresolvedAlertHistory(ctx, alertID); err != nil || latest.ID != entry.ID {
		t.Fatalf("latest unresolved = %+v, %v; want the no_data row", latest, err)
	}
	testNoDataEpisodePruned(t, ctx, s, alertID, entry.ID)
	testNoDataEpisodeMoves(t, ctx, s, alertID, entry.ID)

	if err := s.ResolveAlertGroup(ctx, alertID, key, "forgotten"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetAlertHistoryEntry(ctx, alertID, entry.ID); err != nil || got.Status != models.AlertStatusResolved || got.ResolvedAt == nil {
		t.Fatalf("resolved row = %+v, %v; want resolved", got, err)
	}
	if err := s.SetOpenAlertHistoryStatus(ctx, entry.ID, models.AlertStatusTriggered, "late"); !models.IsNotFound(err) {
		t.Fatalf("moving a closed episode err = %v, want not found", err)
	}
}

// testNoDataEpisodePruned: pruning keeps an open no_data episode.
func testNoDataEpisodePruned(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID, historyID int64) {
	t.Helper()
	for i := range 3 {
		if _, err := s.InsertAlertHistory(ctx, alertID, "", nil, models.AlertStatusError, nil, "failed", nil); err != nil {
			t.Fatalf("error row %d: %v", i, err)
		}
	}
	if err := s.PruneAlertHistory(ctx, alertID, 1); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetAlertHistoryEntry(ctx, alertID, historyID); err != nil || got.Status != models.AlertStatusNoData {
		t.Fatalf("no_data row after pruning = %+v, %v; want it kept", got, err)
	}
}

// testNoDataEpisodeMoves: an open episode moves to triggered and back.
func testNoDataEpisodeMoves(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID, historyID int64) {
	t.Helper()
	if err := s.SetOpenAlertHistoryStatus(ctx, historyID, models.AlertStatusTriggered, "fired"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetAlertHistoryEntry(ctx, alertID, historyID); err != nil || got.Status != models.AlertStatusTriggered || got.Message != "fired" {
		t.Fatalf("moved row = %+v, %v; want triggered with the new message", got, err)
	}
	if err := s.SetOpenAlertHistoryStatus(ctx, historyID, models.AlertStatusNoData, "no data again"); err != nil {
		t.Fatal(err)
	}
}

// testNoDataSummaryState: last_state is firing while any group fires, else
// no_data while any group has no data. Only the start of firing moves
// last_triggered_at.
func testNoDataSummaryState(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID) {
	state := func() *models.Alert {
		t.Helper()
		alert, err := s.GetAlert(ctx, alertID)
		if err != nil {
			t.Fatal(err)
		}
		return alert
	}
	if err := s.MarkAlertEvaluated(ctx, alertID, models.AlertStateNoData); err != nil {
		t.Fatal(err)
	}
	if got := state(); got.LastState != models.AlertStateNoData || got.LastTriggeredAt != nil || got.LastEvaluatedAt == nil {
		t.Fatalf("after no_data = %s triggered %v evaluated %v; want no_data, never triggered, evaluated", got.LastState, got.LastTriggeredAt, got.LastEvaluatedAt)
	}
	if err := s.MarkAlertEvaluated(ctx, alertID, models.AlertStateFiring); err != nil {
		t.Fatal(err)
	}
	fired := state().LastTriggeredAt
	if fired == nil {
		t.Fatal("last_triggered_at not set when the rule started firing")
	}

	testNoDataRefreshState(t, ctx, s, alertID, state)

	if err := s.MarkAlertEvaluated(ctx, alertID, models.AlertStateFiring); err != nil {
		t.Fatal(err)
	}
	if again := state().LastTriggeredAt; again == nil || again.Before(*fired) {
		t.Fatalf("last_triggered_at = %v after firing again, want at or after %v", again, fired)
	}
}

// testNoDataRefreshState: resolving one group recomputes last_state from the
// groups that remain.
func testNoDataRefreshState(t *testing.T, ctx context.Context, s store.Store, alertID models.AlertID, state func() *models.Alert) {
	t.Helper()
	now := time.Now().UTC()
	fire := func(key string, status models.AlertStatus, instanceState models.AlertInstanceState) {
		t.Helper()
		entry, err := s.InsertAlertHistory(ctx, alertID, key, map[string]string{"service": key}, status, nil, "open", nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertAlertInstance(ctx, &models.AlertInstance{AlertID: alertID, GroupKey: key, Group: map[string]string{"service": key}, State: instanceState, StateSince: now, HistoryID: &entry.ID}); err != nil {
			t.Fatal(err)
		}
	}
	fire(`["a"]`, models.AlertStatusTriggered, models.AlertInstanceStateFiring)
	fire(`["b"]`, models.AlertStatusNoData, models.AlertInstanceStateNoData)
	if err := s.UpsertAlertInstance(ctx, &models.AlertInstance{AlertID: alertID, GroupKey: `["c"]`, State: models.AlertInstanceStateOK, StateSince: now}); err != nil {
		t.Fatal(err)
	}
	counts, err := s.CountAlertInstances(ctx, []models.AlertID{alertID})
	if err != nil || counts[alertID] != (models.AlertInstanceCounts{Firing: 1, NoData: 1, OK: 1}) {
		t.Fatalf("counts = %+v, %v; want one of each state", counts[alertID], err)
	}

	if err := s.ResolveAlertGroup(ctx, alertID, `["a"]`, "fixed"); err != nil {
		t.Fatal(err)
	}
	if got := state(); got.LastState != models.AlertStateNoData {
		t.Fatalf("last_state with one no_data group left = %s, want no_data", got.LastState)
	}
	if err := s.DeleteAlertInstance(ctx, alertID, `["c"]`); err != nil {
		t.Fatal(err)
	}
	if err := s.ResolveAlertGroup(ctx, alertID, `["b"]`, "fixed"); err != nil {
		t.Fatal(err)
	}
	if got := state(); got.LastState != models.AlertStateResolved {
		t.Fatalf("last_state with no group left = %s, want resolved", got.LastState)
	}
	instances, err := s.ListAlertInstances(ctx, alertID)
	if err != nil || len(instances) != 0 {
		t.Fatalf("instances = %+v, %v; want none", instances, err)
	}
}
