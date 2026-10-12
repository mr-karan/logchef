package datasource

import (
	"errors"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestValidateSourceStreamFilters(t *testing.T) {
	tooMany := make([]string, maxStreamFilters+1)
	for i := range tooMany {
		tooMany[i] = `{app="api"}`
	}
	oversized := `{app="` + strings.Repeat("a", maxStreamFilterBytes) + `"}`
	for _, tc := range []struct {
		name       string
		sourceType models.SourceType
		filters    []string
		valid      bool
	}{
		{"no filters on ClickHouse", models.SourceTypeClickHouse, nil, true},
		{"empty list on ClickHouse", models.SourceTypeClickHouse, []string{}, true},
		{"selector on ClickHouse", models.SourceTypeClickHouse, []string{`{app="api"}`}, false},
		{"stream selector", models.SourceTypeVictoriaLogs, []string{`{app="api"}`}, true},
		{"picker JSON object", models.SourceTypeVictoriaLogs, []string{`{"app":["api","web"]}`, `{"env":["prod"]}`}, true},
		{"maximum count", models.SourceTypeVictoriaLogs, tooMany[:maxStreamFilters], true},
		{"too many", models.SourceTypeVictoriaLogs, tooMany, false},
		{"oversized", models.SourceTypeVictoriaLogs, []string{oversized}, false},
		{"JSON array", models.SourceTypeVictoriaLogs, []string{`[{"app":"api"}]`}, false},
		{"bare filter", models.SourceTypeVictoriaLogs, []string{`app:="api"`}, false},
		{"empty filter", models.SourceTypeVictoriaLogs, []string{""}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSourceStreamFilters(tc.sourceType, tc.filters)
			if tc.valid != (err == nil) {
				t.Fatalf("error = %v, want valid %v", err, tc.valid)
			}
			if err != nil && !IsValidationError(err) {
				t.Fatalf("error %T is not a ValidationError", err)
			}
		})
	}
}

func TestValidateStreamFiltersIgnoresSourceType(t *testing.T) {
	if err := ValidateStreamFilters([]string{`{app="api"}`}); err != nil {
		t.Fatal(err)
	}
	var validationErr *ValidationError
	if err := ValidateStreamFilters([]string{"app"}); !errors.As(err, &validationErr) || validationErr.Field != "extra_stream_filters" {
		t.Fatalf("error = %v", err)
	}
}
