package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestAlertTimeMacros: a {{__time_filter}} query passes create, update and
// the test route; a user variable is refused; a macro query that the alert
// SQL check rejects after expansion is refused on save with the run-time error.
func TestAlertTimeMacros(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	owner := newTestUser(t, db, "macro-owner@example.com", "Owner")
	source := &models.Source{
		Name: "macro-alerts", MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{Host: "ch:9000", Username: "default", Database: "default", TableName: "macro_alerts"},
	}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	ds := datasource.NewService(db, log)
	// No connection is added, so a query that passes validation fails only
	// when it reaches ClickHouse.
	ds.Register(datasource.NewClickHouseProvider(clickhouse.NewManager(log), log))

	macroQuery := "SELECT count() AS value FROM logs WHERE {{__time_filter}} AND level = 'error'"
	req := newTestCreateAlertRequest()
	req.Query = macroQuery
	alert, err := CreateAlert(ctx, db, ds, log, source.ID, owner, req)
	if err != nil {
		t.Fatalf("CreateAlert(macro query) = %v", err)
	}
	updated := "SELECT count() AS value FROM logs WHERE {{__time_filter}}"
	if _, err := UpdateAlert(ctx, db, ds, log, owner, alert.ID, &models.UpdateAlertRequest{Query: &updated}); err != nil {
		t.Fatalf("UpdateAlert(macro query) = %v", err)
	}
	test := &models.TestAlertQueryRequest{
		QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative, Query: macroQuery,
		LookbackSeconds: 300, ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 1,
	}
	if _, err := testAlertQuery(ctx, db, ds, source.ID, test); err == nil || errors.Is(err, ErrInvalidAlertConfiguration) {
		t.Fatalf("test route with a macro query err = %v, want it past validation and failing at the connection", err)
	}

	variable := newTestCreateAlertRequest()
	variable.Query = "SELECT count() AS value FROM logs WHERE {{__time_filter}} AND service = {{service}}"
	if _, err := CreateAlert(ctx, db, ds, log, source.ID, owner, variable); !isInvalidAlertQuery(err) {
		t.Errorf("CreateAlert(user variable) err = %v, want invalid alert query", err)
	}

	cut := "SELECT count() AS value FROM logs WHERE {{__time_filter}} SETTINGS max_result_rows = 10"
	_, runErr := ds.EvaluateAlert(ctx, source.ID, datasource.AlertQueryRequest{At: time.Now(), LookbackSeconds: 300, Query: cut})
	if runErr == nil {
		t.Fatal("EvaluateAlert ran a query that ends early on SETTINGS")
	}
	bad := newTestCreateAlertRequest()
	bad.Query = cut
	_, saveErr := CreateAlert(ctx, db, ds, log, source.ID, owner, bad)
	if !errors.Is(saveErr, ErrInvalidAlertConfiguration) || !strings.HasSuffix(saveErr.Error(), runErr.Error()) {
		t.Errorf("save error = %v, want the run-time error %q", saveErr, runErr)
	}
}
