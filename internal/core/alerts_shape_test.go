package core

import (
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

// A ClickHouse rule that reads the current time runs, but a backtest refuses
// it, so the test warns. A rule with {{__time_filter}} gets no warning.
func TestRuleShapeWarningsCurrentTime(t *testing.T) {
	source := &models.Source{MetaTSField: "ts", Connection: models.ConnectionInfo{Database: "logs", TableName: "app"}}
	result := &models.QueryResult{Columns: []models.ColumnInfo{{Name: "value", Type: "UInt64"}}}
	for query, want := range map[string]string{
		"SELECT count() AS value FROM logs.app WHERE ts > now() - INTERVAL 5 MINUTE": "cannot be backtested",
		"SELECT count() AS value FROM logs.app WHERE {{__time_filter}}":              "",
	} {
		alert := &models.Alert{QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative, Query: query}
		warnings := ruleShapeWarnings(source, alert, result)
		switch {
		case want == "" && len(warnings) != 0:
			t.Errorf("%s: warnings %q, want none", query, warnings)
		case want != "" && (len(warnings) != 1 || !strings.Contains(warnings[0], want)):
			t.Errorf("%s: warnings %q, want one with %q", query, warnings, want)
		}
	}
}

// A ClickHouse rule reads its value from the first column, so a result whose
// first column is not named value gets a warning that names the column.
func TestRuleShapeWarningsValueColumn(t *testing.T) {
	source := &models.Source{MetaTSField: "ts", Connection: models.ConnectionInfo{Database: "logs", TableName: "app"}}
	alert := &models.Alert{QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative, Query: "SELECT count() FROM logs.app WHERE {{__time_filter}}"}
	result := &models.QueryResult{Columns: []models.ColumnInfo{{Name: "count()", Type: "UInt64"}}}
	warnings := ruleShapeWarnings(source, alert, result)
	if len(warnings) != 1 || !strings.Contains(warnings[0], `reads its value from the column "count()"`) {
		t.Fatalf("warnings %q, want one naming count()", warnings)
	}
	grouped := *alert
	grouped.GroupBy = []string{"service"}
	result.Columns = []models.ColumnInfo{{Name: "service", Type: "String"}, {Name: "value", Type: "UInt64"}}
	if warnings := ruleShapeWarnings(source, &grouped, result); len(warnings) != 0 {
		t.Fatalf("warnings %q for a grouped rule with a value column, want none", warnings)
	}
}
