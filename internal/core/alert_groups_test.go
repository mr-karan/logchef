package core

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestAlertGroupByValidation(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	owner := newTestUser(t, db, "group-owner@example.com", "Owner")
	source := newTestSource(t, db, "group-alerts")
	ds := newFakeDatasourceService(db, log, nil)

	for _, tc := range []struct {
		name    string
		groupBy []string
		mode    models.AlertEditorMode
		want    string
	}{
		{name: "five columns", groupBy: []string{"a", "b", "c", "d", "e"}, want: "at most 4"},
		{name: "blank name", groupBy: []string{"service", " "}, want: "blank column name"},
		{name: "repeated name", groupBy: []string{"service", " service "}, want: `"service" twice`},
		{name: "condition mode in a native language", groupBy: []string{"service"}, mode: models.AlertEditorModeCondition, want: "needs query_language logchefql"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := newTestCreateAlertRequest()
			req.GroupBy = tc.groupBy
			if tc.mode != "" {
				req.EditorMode = tc.mode
				req.ConditionJSON = `level="error"`
			}
			_, err := CreateAlert(ctx, db, ds, log, source.ID, owner, req)
			if !errors.Is(err, ErrInvalidAlertConfiguration) || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("CreateAlert(group_by %q) err = %v, want invalid configuration with %q", tc.groupBy, err, tc.want)
			}
		})
	}

	req := newTestCreateAlertRequest()
	req.GroupBy = []string{" service ", "region"}
	alert, err := CreateAlert(ctx, db, ds, log, source.ID, owner, req)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"service", "region"}; !slices.Equal(alert.GroupBy, want) {
		t.Errorf("stored group_by = %q, want trimmed %q", alert.GroupBy, want)
	}
}

// TestUpdateGroupByClosesEpisodes: changing group_by closes every open
// episode with "rule changed"; any other edit keeps the state.
func TestUpdateGroupByClosesEpisodes(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	owner := newTestUser(t, db, "regroup-owner@example.com", "Owner")
	source := newTestSource(t, db, "regroup-alerts")
	ds := newFakeDatasourceService(db, log, nil)
	alert, err := CreateAlert(ctx, db, ds, log, source.ID, owner, newTestCreateAlertRequest())
	if err != nil {
		t.Fatal(err)
	}
	entry, err := db.InsertAlertHistory(ctx, alert.ID, "", nil, models.AlertStatusTriggered, nil, "fired", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertAlertInstance(ctx, &models.AlertInstance{AlertID: alert.ID, State: models.AlertInstanceStateFiring, StateSince: entry.TriggeredAt, HistoryID: &entry.ID}); err != nil {
		t.Fatal(err)
	}

	renamed := "renamed"
	if _, _, err := UpdateAlert(ctx, db, ds, log, owner, alert.ID, &models.UpdateAlertRequest{Name: &renamed}); err != nil {
		t.Fatal(err)
	}
	if open, err := db.ListOpenAlertHistory(ctx, alert.ID); err != nil || len(open) != 1 {
		t.Fatalf("open episodes after a rename = %d, %v; want 1", len(open), err)
	}

	groupBy := []string{"service"}
	_, updated, err := UpdateAlert(ctx, db, ds, log, owner, alert.ID, &models.UpdateAlertRequest{GroupBy: &groupBy})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(updated.GroupBy, groupBy) || updated.LastState != models.AlertStateResolved {
		t.Errorf("updated alert = group_by %q state %s, want [service] resolved", updated.GroupBy, updated.LastState)
	}
	closed, err := db.GetAlertHistoryEntry(ctx, alert.ID, entry.ID)
	if err != nil || closed.Status != models.AlertStatusResolved || closed.Message != "rule changed" {
		t.Fatalf("episode after a group_by change = %+v, %v; want resolved with rule changed", closed, err)
	}
	if instances, err := db.ListAlertInstances(ctx, alert.ID); err != nil || len(instances) != 0 {
		t.Errorf("instances after a group_by change = %+v, %v; want none", instances, err)
	}
}

func TestTestAlertQueryGroupedSamples(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	source := newTestSource(t, db, "grouped-test")
	result := &models.QueryResult{Columns: []models.ColumnInfo{{Name: "service"}, {Name: "value"}}}
	for i := range 25 {
		result.Logs = append(result.Logs, map[string]any{"service": string(rune('a' + i)), "value": uint64(i)})
	}
	ds := newFakeDatasourceService(db, log, &fakeProvider{
		evaluateAlertFn: func(context.Context, *models.Source, datasource.AlertQueryRequest) (*models.QueryResult, error) {
			return result, nil
		},
	})
	req := &models.TestAlertQueryRequest{
		QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
		Query:             "SELECT service, count() AS value FROM logs GROUP BY service",
		ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 20, GroupBy: []string{"service"},
	}
	resp, err := testAlertQuery(ctx, db, ds, source.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.GroupsTotal != 25 || len(resp.Samples) != models.MaxTestAlertSamples || !resp.ThresholdMet {
		t.Fatalf("response = groups %d samples %d met %v, want 25 groups, %d samples, met by a group past the listed ones",
			resp.GroupsTotal, len(resp.Samples), resp.ThresholdMet, models.MaxTestAlertSamples)
	}
	if first := resp.Samples[0]; first.Group["service"] != "a" || first.Value != 0 || first.ThresholdMet {
		t.Errorf("first sample = %+v, want group a, value 0, not met", first)
	}
	for _, warning := range resp.Warnings {
		if strings.Contains(warning, "single numeric value") {
			t.Errorf("grouped test warned about several rows: %q", warning)
		}
	}

	result.Logs = append(result.Logs, map[string]any{"service": "a", "value": uint64(1)})
	if _, err := testAlertQuery(ctx, db, ds, source.ID, req); !errors.Is(err, ErrInvalidAlertConfiguration) || !strings.Contains(err.Error(), "more than one row") {
		t.Fatalf("test with a duplicate group err = %v, want a 400 naming the duplicate", err)
	}
}
