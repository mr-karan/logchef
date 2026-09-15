package server

import (
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestPrepareFieldValuesQueryUsesLogchefQLVariableTypes(t *testing.T) {
	query, err := prepareFieldValuesQuery(
		`service="{{ service }}" and event_id>{{ event_id }} [[level="{{ level }}"]]`,
		models.QueryLanguageLogchefQL,
		[]models.TemplateVariable{
			{Name: "service", Type: "string", Value: `api's`},
			{Name: "event_id", Type: "number", Value: int64(42)},
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	want := `service="api\'s" and event_id>42 `
	if query != want {
		t.Fatalf("prepared query = %q, want %q", query, want)
	}
}

func TestPrepareFieldValuesQueryLeavesNativeQueryUntouched(t *testing.T) {
	query := `SELECT * FROM logs WHERE message = {{ message }}`
	got, err := prepareFieldValuesQuery(query, models.QueryLanguageClickHouseSQL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != query {
		t.Fatalf("native query = %q, want %q", got, query)
	}
}

func TestParseFieldValuesVariables(t *testing.T) {
	variables, err := parseFieldValuesVariables(`[{"name":"event_id","type":"number","value":42}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(variables) != 1 || variables[0].Name != "event_id" || variables[0].Value.(float64) != 42 {
		t.Fatalf("variables = %+v, want event_id=42", variables)
	}

	if _, err := parseFieldValuesVariables("not-json"); err == nil {
		t.Fatal("invalid variables were accepted")
	}
}

func TestSubstituteTranslateVariablesPreservesEmbeddedValuesAndOptionalClauses(t *testing.T) {
	req := &TranslateRequest{
		Query: `message="prefix {{ trade_id }}" [[and level="{{ level }}"]]`,
		Variables: []models.TemplateVariable{
			{Name: "trade_id", Type: "text", Value: `api's`},
		},
	}
	if err := substituteTranslateVariables(req); err != nil {
		t.Fatal(err)
	}

	want := `message="prefix api\'s" `
	if req.Query != want {
		t.Fatalf("translated query = %q, want %q", req.Query, want)
	}
}
