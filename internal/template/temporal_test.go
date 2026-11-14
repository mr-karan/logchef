package template

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestTemporalGolden(t *testing.T) {
	data, err := os.ReadFile("testdata/temporal.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Type      VariableType `json:"type"`
		Value     any          `json:"value"`
		SQL       string       `json:"sql"`
		LogsQL    string       `json:"logsql"`
		LogchefQL string       `json:"logchefql"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(string(fixture.Type), func(t *testing.T) {
			for dialect, want := range map[Dialect]string{ClickHouseSQL: fixture.SQL, LogsQL: fixture.LogsQL, LogchefQL: fixture.LogchefQL} {
				got, err := dialect.Substitute("d = {{d}}", []Variable{{Name: "d", Type: fixture.Type, Value: fixture.Value}}, Builtins{})
				if err != nil || got != want {
					t.Errorf("%s %v dialect %d: got %q, %v; want %q", fixture.Type, fixture.Value, dialect, got, err, want)
				}
			}
		})
	}
}

func TestInvalidDay(t *testing.T) {
	for _, value := range []any{"2026-02-29", "2024-02-30", "2026-1-02", "2026-11-12 00:00:00", "2026-11-12T00:00:00Z", "2026-11-12' OR 1=1", "today", "yesterday", 0, true} {
		for _, dialect := range []Dialect{ClickHouseSQL, LogsQL, LogchefQL} {
			if _, err := dialect.Substitute("d = {{d}}", []Variable{{Name: "d", Type: TypeDay, Value: value}}, Builtins{}); err == nil {
				t.Errorf("dialect %d accepted %v", dialect, value)
			}
		}
	}
}

func TestDayQuoteAndOptionalContexts(t *testing.T) {
	for _, dialect := range []Dialect{ClickHouseSQL, LogsQL, LogchefQL} {
		got, err := dialect.Substitute("d = '{{d}}' [[ AND x = {{missing}} ]]", []Variable{{Name: "d", Type: TypeDay, Value: "2026-11-12"}}, Builtins{})
		if err != nil || got != "d = '2026-11-12' " {
			t.Fatalf("dialect %d: %q, %v", dialect, got, err)
		}
	}
}

func TestEmptyDay(t *testing.T) {
	for _, dialect := range []Dialect{ClickHouseSQL, LogsQL, LogchefQL} {
		variables := []Variable{{Name: "d", Type: TypeDay, Value: ""}}
		got, err := dialect.Substitute("SELECT 1[[ AND d = {{d}} ]]", variables, Builtins{})
		if err != nil || got != "SELECT 1" {
			t.Errorf("dialect %d optional empty day: got %q, %v", dialect, got, err)
		}
		_, err = dialect.Substitute("d = {{d}}", variables, Builtins{})
		if err == nil || !strings.Contains(err.Error(), "variable {{d}} requires a value") {
			t.Errorf("dialect %d required empty day: got %v", dialect, err)
		}
	}
}

func TestDayArrays(t *testing.T) {
	variables := []Variable{{Name: "d", Type: TypeDay, Value: []string{"2024-02-29", "2026-11-12"}}}
	for dialect, want := range map[Dialect]string{
		ClickHouseSQL: "d IN ('2024-02-29', '2026-11-12')",
		LogsQL:        `d IN ("2024-02-29", "2026-11-12")`,
	} {
		got, err := dialect.Substitute("d IN ({{d}})", variables, Builtins{})
		if err != nil || got != want {
			t.Errorf("dialect %d day array: got %q, %v; want %q", dialect, got, err, want)
		}
	}
	_, err := LogchefQL.Substitute("d IN ({{d}})", variables, Builtins{})
	if err == nil || !strings.Contains(err.Error(), "LogchefQL requires a single value") {
		t.Errorf("LogchefQL day array: got %v", err)
	}
}
