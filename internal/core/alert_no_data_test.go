package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestAlertNoDataPolicyAndNotifyOnResolve: a new rule defaults to the zero
// policy and resolve notifications, an unknown policy is a 400, and an edit
// changes either field without touching the other.
func TestAlertNoDataPolicyAndNotifyOnResolve(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	owner := newTestUser(t, db, "nodata-owner@example.com", "Owner")
	source := newTestSource(t, db, "nodata-alerts")
	ds := newFakeDatasourceService(db, log, nil)

	alert, err := CreateAlert(ctx, db, ds, log, source.ID, owner, newTestCreateAlertRequest())
	if err != nil {
		t.Fatal(err)
	}
	if alert.NoDataPolicy != models.AlertNoDataZero || !alert.NotifyOnResolve {
		t.Fatalf("defaults = policy %q notify_on_resolve %v, want zero and true", alert.NoDataPolicy, alert.NotifyOnResolve)
	}

	req := newTestCreateAlertRequest()
	req.NoDataPolicy = "ignore"
	if _, err := CreateAlert(ctx, db, ds, log, source.ID, owner, req); !errors.Is(err, ErrInvalidAlertConfiguration) || !strings.Contains(err.Error(), "no_data_policy") {
		t.Fatalf("CreateAlert(no_data_policy ignore) err = %v, want a 400 naming no_data_policy", err)
	}

	off := false
	req = newTestCreateAlertRequest()
	req.NoDataPolicy = models.AlertNoDataKeep
	req.NotifyOnResolve = &off
	kept, err := CreateAlert(ctx, db, ds, log, source.ID, owner, req)
	if err != nil {
		t.Fatal(err)
	}
	if stored, err := db.GetAlert(ctx, kept.ID); err != nil || stored.NoDataPolicy != models.AlertNoDataKeep || stored.NotifyOnResolve {
		t.Fatalf("stored = %+v, %v; want keep without resolve notifications", stored, err)
	}

	policy := models.AlertNoDataNoData
	_, updated, err := UpdateAlert(ctx, db, ds, log, owner, alert.ID, &models.UpdateAlertRequest{NoDataPolicy: &policy})
	if err != nil {
		t.Fatal(err)
	}
	if updated.NoDataPolicy != models.AlertNoDataNoData || !updated.NotifyOnResolve {
		t.Fatalf("after policy edit = %q, %v; want no_data with resolve notifications kept", updated.NoDataPolicy, updated.NotifyOnResolve)
	}
	_, updated, err = UpdateAlert(ctx, db, ds, log, owner, alert.ID, &models.UpdateAlertRequest{NotifyOnResolve: &off})
	if err != nil {
		t.Fatal(err)
	}
	if updated.NoDataPolicy != models.AlertNoDataNoData || updated.NotifyOnResolve {
		t.Fatalf("after notify edit = %q, %v; want no_data without resolve notifications", updated.NoDataPolicy, updated.NotifyOnResolve)
	}
	bad := models.AlertNoDataPolicy("ignore")
	if _, _, err := UpdateAlert(ctx, db, ds, log, owner, alert.ID, &models.UpdateAlertRequest{NoDataPolicy: &bad}); !errors.Is(err, ErrInvalidAlertConfiguration) {
		t.Fatalf("UpdateAlert(no_data_policy ignore) err = %v, want a 400", err)
	}
}

// TestTestAlertQueryNoData: a test with no rows reports no_data, and only the
// zero policy compares 0 with the threshold.
func TestTestAlertQueryNoData(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	source := newTestSource(t, db, "nodata-test")
	ds := newFakeDatasourceService(db, log, &fakeProvider{
		evaluateAlertFn: func(context.Context, *models.Source, datasource.AlertQueryRequest) (*models.QueryResult, error) {
			return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "service"}, {Name: "value"}}}, nil
		},
	})
	for _, tc := range []struct {
		policy  models.AlertNoDataPolicy
		groupBy []string
		met     bool
		warning string
	}{
		{policy: models.AlertNoDataZero, met: true, warning: "Using value 0"},
		{policy: models.AlertNoDataNoData, warning: "will report no data"},
		{policy: models.AlertNoDataKeep, warning: "keep its current state"},
		{policy: models.AlertNoDataZero, groupBy: []string{"service"}, met: true, warning: "Using value 0"},
		{policy: models.AlertNoDataNoData, groupBy: []string{"service"}, warning: "will report no data"},
	} {
		t.Run(string(tc.policy)+"/"+strings.Join(tc.groupBy, ","), func(t *testing.T) {
			resp, err := testAlertQuery(ctx, db, ds, source.ID, &models.TestAlertQueryRequest{
				QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
				Query:             "SELECT service, count() AS value FROM logs GROUP BY service",
				ThresholdOperator: models.AlertThresholdLessThan, ThresholdValue: 5,
				GroupBy: tc.groupBy, NoDataPolicy: tc.policy,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !resp.NoData || resp.ThresholdMet != tc.met {
				t.Errorf("response no_data %v met %v, want no_data true met %v", resp.NoData, resp.ThresholdMet, tc.met)
			}
			if len(resp.Warnings) == 0 || !strings.Contains(resp.Warnings[0], tc.warning) {
				t.Errorf("warnings = %q, want the first to contain %q", resp.Warnings, tc.warning)
			}
		})
	}
}

// TestUpdateGroupByDropsOkInstances: under no_data and keep a rule tracks ok
// groups without an episode. A group_by change drops them too, or the next
// evaluation would report the old groups as silent.
func TestUpdateGroupByDropsOkInstances(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	owner := newTestUser(t, db, "regroup-ok-owner@example.com", "Owner")
	source := newTestSource(t, db, "regroup-ok-alerts")
	ds := newFakeDatasourceService(db, log, nil)
	req := newTestCreateAlertRequest()
	req.GroupBy = []string{"service"}
	req.NoDataPolicy = models.AlertNoDataNoData
	alert, err := CreateAlert(ctx, db, ds, log, source.ID, owner, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertAlertInstance(ctx, &models.AlertInstance{AlertID: alert.ID, GroupKey: `["checkout"]`, Group: map[string]string{"service": "checkout"}, State: models.AlertInstanceStateOK, StateSince: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	groupBy := []string{"region"}
	if _, _, err := UpdateAlert(ctx, db, ds, log, owner, alert.ID, &models.UpdateAlertRequest{GroupBy: &groupBy}); err != nil {
		t.Fatal(err)
	}
	if instances, err := db.ListAlertInstances(ctx, alert.ID); err != nil || len(instances) != 0 {
		t.Fatalf("instances after a group_by change = %+v, %v; want none", instances, err)
	}
}
