package alerts

import (
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

// max() over no row returns NULL from ClickHouse. A NULL value is no sample,
// so the no-data policy decides, instead of an evaluation error that keeps a
// firing group firing.
func TestNullValueIsNoSample(t *testing.T) {
	ungrouped := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "value", Type: "Nullable(Float64)"}},
		Logs:    []map[string]any{{"value": nil}},
	}
	for _, language := range []models.QueryLanguage{models.QueryLanguageClickHouseSQL, models.QueryLanguageLogsQL, models.QueryLanguageLogchefQL} {
		samples, err := SamplesFromResult(ungrouped, nil, language)
		if err != nil || len(samples) != 0 {
			t.Errorf("%s ungrouped NULL: samples %v, error %v; want no sample", language, samples, err)
		}
	}

	var nilPointer *float64
	grouped := groupedResult(
		map[string]any{"service": "api", "value": nilPointer},
		map[string]any{"service": "web", "value": 3},
	)
	samples, err := SamplesFromResult(grouped, []string{"service"}, models.QueryLanguageClickHouseSQL)
	if err != nil || len(samples) != 1 || samples[0].Group["service"] != "web" {
		t.Fatalf("grouped NULL: samples %v, error %v; want web only", samples, err)
	}

	// Text in a ClickHouse value column is a broken query, not missing data.
	text := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "value", Type: "String"}},
		Logs:    []map[string]any{{"value": ""}},
	}
	if _, err := SamplesFromResult(text, nil, models.QueryLanguageClickHouseSQL); err == nil {
		t.Error("an empty string value in ClickHouse SQL is no error, want one")
	}
}
