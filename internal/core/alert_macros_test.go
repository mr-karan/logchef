package core

import (
	"context"
	"errors"
	"slices"
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
	if _, _, err := UpdateAlert(ctx, db, ds, log, owner, alert.ID, &models.UpdateAlertRequest{Query: &updated}); err != nil {
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

// TestTestAlertQueryTimeFilterWarning: only a ClickHouse SQL query can read
// every row, since LogsQL and LogQL providers bound the query to the lookback
// ending at the evaluation instant. A SQL query is bounded by a time macro or
// a WHERE clause on the source's timestamp column.
func TestTestAlertQueryTimeFilterWarning(t *testing.T) {
	db := newTestDB(t)
	log := discardLogger()
	ctx := context.Background()
	source := &models.Source{
		Name: "time-filter-warning", MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{Host: "ch:9000", Username: "default", Database: "default", TableName: "time_filter_warning"},
	}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	ds := newFakeDatasourceService(db, log, &fakeProvider{
		queryLanguages: []models.QueryLanguage{models.QueryLanguageClickHouseSQL, models.QueryLanguageLogsQL, models.QueryLanguageLogQL},
		evaluateAlertFn: func(context.Context, *models.Source, datasource.AlertQueryRequest) (*models.QueryResult, error) {
			return &models.QueryResult{Columns: []models.ColumnInfo{{Name: "value"}}, Logs: []map[string]any{{"value": 1}}}, nil
		},
	})
	const warning = "Query appears to lack a time-based filter"
	sql, logsql, logql := models.QueryLanguageClickHouseSQL, models.QueryLanguageLogsQL, models.QueryLanguageLogQL
	native, condition := models.AlertEditorModeNative, models.AlertEditorModeCondition
	for _, tc := range []struct {
		name     string
		language models.QueryLanguage
		mode     models.AlertEditorMode
		query    string
		warns    bool
	}{
		{"sql time_filter macro", sql, native, "SELECT count() AS value FROM logs WHERE {{__time_filter}}", false},
		{"sql from and to macros", sql, native, "SELECT count() AS value FROM logs WHERE event_time BETWEEN {{__from}} AND {{__to}}", false},
		{"sql from macro", sql, native, "SELECT count() AS value FROM logs WHERE event_time >= {{__from}}", false},
		{"sql to macro", sql, native, "SELECT count() AS value FROM logs WHERE event_time < {{__to}}", false},
		{"sql timestamp column", sql, native, "SELECT count() AS value FROM logs WHERE level = 'error' AND `Timestamp` > now() - INTERVAL 5 MINUTE", false},
		{"sql prewhere on timestamp column", sql, native, "SELECT count() AS value FROM logs PREWHERE timestamp > now() - 300", false},
		{"sql where without time", sql, native, "SELECT count() AS value FROM logs WHERE level = 'error'", true},
		{"sql column with timestamp in its name", sql, native, "SELECT count() AS value FROM logs WHERE timestamp_ms > 0", true},
		{"sql timestamp only in select", sql, native, "SELECT max(timestamp) AS value FROM logs", true},
		{"sql no where", sql, native, "SELECT count() AS value FROM logs", true},
		{"sql condition", sql, condition, "SELECT count(*) as value\nFROM logs\nWHERE (level = 'error') AND {{__time_filter}}", false},
		{"logsql native", logsql, native, `level:="error" | stats count() as value`, false},
		{"logsql condition", logsql, condition, `level:="error" | stats count() as value`, false},
		{"logql native", logql, native, `sum(count_over_time({app="api"} |= "error" [5m]))`, false},
		{"logql condition", logql, condition, `sum(count_over_time({app="api"} |= "error" [300s]))`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := testAlertQuery(ctx, db, ds, source.ID, &models.TestAlertQueryRequest{
				QueryLanguage: tc.language, EditorMode: tc.mode, Query: tc.query, ConditionJSON: `level="error"`,
				LookbackSeconds: 300, ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 0,
			})
			if err != nil {
				t.Fatal(err)
			}
			warns := slices.ContainsFunc(resp.Warnings, func(w string) bool { return strings.HasPrefix(w, warning) })
			if warns != tc.warns {
				t.Errorf("time filter warning = %v, want %v; warnings %q", warns, tc.warns, resp.Warnings)
			}
		})
	}
}
