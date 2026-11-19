package core

import (
	"errors"
	"testing"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestValidateSavedViewDefinition(t *testing.T) {
	logchefql := datasource.Descriptor{QueryLanguages: []models.QueryLanguage{models.QueryLanguageLogchefQL, models.QueryLanguageClickHouseSQL}}
	sqlOnly := datasource.Descriptor{QueryLanguages: []models.QueryLanguage{models.QueryLanguageClickHouseSQL}}
	filter := func(s string) *string { return &s }
	width := 10

	tests := []struct {
		name       string
		def        models.SavedViewDefinition
		descriptor datasource.Descriptor
		ok         bool
	}{
		{"layout only", models.SavedViewDefinition{Version: 1, Columns: []models.SavedViewColumn{{Name: "msg"}}}, logchefql, true},
		{"valid filter", models.SavedViewDefinition{Version: 1, Filter: filter(`level = "error" and status >= 500`)}, logchefql, true},
		{"filter that does not parse", models.SavedViewDefinition{Version: 1, Filter: filter(`level = `)}, logchefql, false},
		{"filter on a source without LogchefQL", models.SavedViewDefinition{Version: 1, Filter: filter(`level = "error"`)}, sqlOnly, false},
		{"no filter on a source without LogchefQL", models.SavedViewDefinition{Version: 1, Columns: []models.SavedViewColumn{{Name: "msg"}}}, sqlOnly, true},
		{"model rule still applies", models.SavedViewDefinition{Version: 1, Columns: []models.SavedViewColumn{{Name: "msg", Width: &width}}}, logchefql, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSavedViewDefinition(tc.def, tc.descriptor)
			if tc.ok != (err == nil) {
				t.Fatalf("ValidateSavedViewDefinition = %v, want ok=%v", err, tc.ok)
			}
			if err != nil && !errors.Is(err, models.ErrInvalidSavedViewDefinition) {
				t.Fatalf("error %v does not wrap ErrInvalidSavedViewDefinition, so the API would not answer 400", err)
			}
		})
	}
}
