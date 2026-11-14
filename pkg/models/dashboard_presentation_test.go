package models

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestDashboardPresentation(t *testing.T) {
	for _, tc := range []struct{ viz, feature string }{
		{`{"type":"stat","thresholds":[{"value":null,"color":"green"},{"value":"9007199254740993.01","color":"red"}],"color_mode":"background"}`, "thresholds"},
		{`{"type":"stat","color_mode":"value"}`, "thresholds"},
		{`{"type":"table","rules":[{"columns":["details"],"op":"contains","value":"retry","scope":"row","color":"red"}]}`, "table_rules"},
		{`{"type":"table","column_formats":{"ts":{"kind":"time","date":"iso","time":"seconds"},"n":{"decimals":2}}}`, "column_formats"},
	} {
		panel := strings.Replace(v2TablePanel, `{"type":"table","visible_columns":["a","b"]}`, tc.viz, 1)
		if tc.feature == "thresholds" {
			panel = strings.Replace(panel, `"type":"stat"`, `"type":"stat","value":"n"`, 1)
		}
		raw := v2Blob(v2Layout, panel)
		canonical, err := CanonicalDashboardPanels(raw)
		if err != nil {
			t.Fatal(err)
		}
		features, err := DashboardFeatures(canonical)
		if err != nil || !slices.Contains(features, tc.feature) {
			t.Fatalf("features %v %v", features, err)
		}
	}
	for _, viz := range []string{
		`{"type":"stat","value":"n","thresholds":[]}`,
		`{"type":"stat","value":"n","thresholds":[{"color":"red"}]}`,
		`{"type":"stat","value":"n","thresholds":[{"value":null,"color":"pink"}]}`,
		`{"type":"stat","value":"n","thresholds":[{"value":null,"color":"red"},{"value":"9007199254740993.01","color":"red"},{"value":"9007199254740993.009","color":"green"}]}`,
		`{"type":"stat","value":"n","thresholds":[{"value":null,"color":"red"},{"value":"1e401","color":"red"}]}`,
		`{"type":"stat","value":"n","thresholds":[{"value":null,"color":"green"},{"value":"5","color":"amber"},{"value":"5.0","color":"red"}]}`,
		`{"type":"table","color_mode":"value"}`,
		`{"type":"table","rules":[{"columns":["n"],"op":">","value":"NaN","color":"red","scope":"cell"}]}`,
		`{"type":"table","rules":[{"columns":["n"],"op":"contains","value":"retry","color":"red","scope":"all"}]}`,
		`{"type":"table","column_formats":{"ts":{"kind":"time","decimals":2}}}`,
		`{"type":"table","column_formats":{"ts":{"kind":"time","time":"hours"}}}`,
	} {
		panel := strings.Replace(v2TablePanel, `{"type":"table","visible_columns":["a","b"]}`, viz, 1)
		if err := ValidateDashboardPanels(v2Blob(v2Layout, panel)); err == nil {
			t.Errorf("accepted %s", viz)
		}
	}
	for _, literal := range []string{`"1e401"`, `"1e-401"`, `"0x1f"`, `"NaN"`, `"` + strings.Repeat("1", 101) + `"`, `"` + strings.Repeat("0", 513) + `"`} {
		if _, ok := dashboardExact(json.RawMessage(literal)); ok {
			t.Errorf("accepted %s", literal)
		}
	}
}
