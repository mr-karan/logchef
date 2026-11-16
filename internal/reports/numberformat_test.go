package reports

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

// TestFormatNumberSharedCases runs the fixture that the frontend's
// numberFormat test also runs.
func TestFormatNumberSharedCases(t *testing.T) {
	raw, err := os.ReadFile("../../pkg/models/testdata/report_number_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name   string   `json:"name"`
			Value  *string  `json:"value"`
			Float  *float64 `json:"float"`
			Format struct {
				Notation string   `json:"notation"`
				Decimals *int     `json:"decimals"`
				Grouping *bool    `json:"grouping"`
				Scale    *float64 `json:"scale"`
				Unit     string   `json:"unit"`
				Prefix   string   `json:"prefix"`
				Suffix   string   `json:"suffix"`
			} `json:"format"`
			Text string `json:"text"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("fixture has no cases")
	}
	for _, c := range fixture.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var n Number
			switch {
			case c.Value != nil:
				e, ok := parseExact(*c.Value)
				if !ok {
					t.Fatalf("value %q does not parse", *c.Value)
				}
				n = exactNumber(e)
			case c.Float != nil:
				n = floatNumber(*c.Float)
			default:
				t.Fatal("case has neither value nor float")
			}
			f := models.DashboardNumberFormat(c.Format)
			if got := FormatNumber(n, f); got != c.Text {
				t.Errorf("FormatNumber = %q, want %q", got, c.Text)
			}
		})
	}
}
