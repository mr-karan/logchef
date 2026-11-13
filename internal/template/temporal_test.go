package template

import (
	"encoding/json"
	"os"
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
				got, err := dialect.Substitute("d = {{d}}", []Variable{{Name: "d", Type: fixture.Type, Value: fixture.Value}})
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
			if _, err := dialect.Substitute("d = {{d}}", []Variable{{Name: "d", Type: TypeDay, Value: value}}); err == nil {
				t.Errorf("dialect %d accepted %v", dialect, value)
			}
		}
	}
}

func TestDayQuoteAndOptionalContexts(t *testing.T) {
	for _, dialect := range []Dialect{ClickHouseSQL, LogsQL, LogchefQL} {
		got, err := dialect.Substitute("d = '{{d}}' [[ AND x = {{missing}} ]]", []Variable{{Name: "d", Type: TypeDay, Value: "2026-11-12"}})
		if err != nil || got != "d = '2026-11-12' " {
			t.Fatalf("dialect %d: %q, %v", dialect, got, err)
		}
	}
}
