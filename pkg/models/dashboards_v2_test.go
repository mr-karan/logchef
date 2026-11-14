package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// v2Blob wraps panels and a single-tab layout into a schema 2 blob.
func v2Blob(layout, panels string) json.RawMessage {
	return json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[` + layout + `]}],"panels":[` + panels + `]}`)
}

const (
	v2Layout     = `{"id":"p1","x":0,"y":0,"w":6,"h":2}`
	v2ChartPanel = `{"id":"p1","kind":"query","title":"a","team_id":1,"source_id":1,"query":{"mode":"count","language":"logchefql","text":"x","group_by":"service","interval":"auto","translate_limit":50},"viz":{"type":"chart","mark":"bar","stack":"stacked"}}`
	v2StatPanel  = `{"id":"p1","kind":"query","title":"a","team_id":1,"source_id":1,"query":{"mode":"count","language":"logchefql","text":"x","interval":"auto"},"viz":{"type":"stat","reduce":"sum"}}`
	v2TablePanel = `{"id":"p1","kind":"query","title":"a","team_id":1,"source_id":1,"query":{"mode":"result","language":"clickhouse-sql","text":"SELECT 1","limit":100},"viz":{"type":"table","visible_columns":["a","b"]}}`
	v2PiePanel   = `{"id":"p1","kind":"query","title":"a","team_id":1,"source_id":1,"query":{"mode":"count","language":"logsql","text":"*","group_by":"level","interval":"auto"},"viz":{"type":"pie","donut":true}}`
)

func TestValidateDashboardPanelsV2_Valid(t *testing.T) {
	for name, blob := range map[string]json.RawMessage{
		"chart":         v2Blob(v2Layout, v2ChartPanel),
		"stat":          v2Blob(v2Layout, v2StatPanel),
		"table":         v2Blob(v2Layout, v2TablePanel),
		"pie":           v2Blob(v2Layout, v2PiePanel),
		"empty":         json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[]}],"panels":[]}`),
		"toplist":       v2Blob(v2Layout, `{"id":"p1","kind":"query","title":"","team_id":1,"source_id":1,"query":{"mode":"count","language":"logchefql","text":"","group_by":"host"},"viz":{"type":"toplist"}}`),
		"settings":      json.RawMessage(`{"version":2,"features":["ignored-on-write"],"cache_ttl_seconds":0,"default_time_range":"7d","tabs":[{"id":"t","title":"Overview","layout":[]}],"panels":[]}`),
		"minimal chart": v2Blob(v2Layout, `{"id":"p1","kind":"query","title":"a","team_id":1,"source_id":1,"query":{"mode":"count","language":"clickhouse-sql","text":"SELECT 1"},"viz":{"type":"chart"}}`),
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateDashboardPanels(blob); err != nil {
				t.Fatalf("valid blob rejected: %v", err)
			}
		})
	}
}

func TestValidateDashboardPanelsV2_Violations(t *testing.T) {
	panel := func(query, viz string) json.RawMessage {
		return v2Blob(v2Layout, `{"id":"p1","kind":"query","title":"a","team_id":1,"source_id":1,"query":`+query+`,"viz":`+viz+`}`)
	}
	countQ := `{"mode":"count","language":"logchefql","text":"x"}`
	resultQ := `{"mode":"result","language":"clickhouse-sql","text":"SELECT 1"}`
	tests := []struct {
		name    string
		blob    json.RawMessage
		wantSub string
	}{
		{"oversize", json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"main","title":"` + strings.Repeat("x", MaxDashboardPanelsSizeV2) + `","layout":[]}],"panels":[]}`), "exceeds"},
		{"unknown top-level key", json.RawMessage(`{"version":2,"features":[],"variables":[],"tabs":[{"id":"main","title":"Main","layout":[]}],"panels":[]}`), `unknown field "variables"`},
		{"unknown panel key", v2Blob(v2Layout, `{"id":"p1","kind":"query","title":"a","description":"later","team_id":1,"source_id":1,"query":`+countQ+`,"viz":{"type":"stat"}}`), `unknown field "description"`},
		{"unknown query key", panel(`{"mode":"count","language":"logchefql","text":"x","range":"today-6d"}`, `{"type":"stat"}`), `unknown field "range"`},
		{"unknown viz key", panel(countQ, `{"type":"chart","thresholds":[]}`), `unknown field "thresholds"`},
		{"comparison needs mode", panel(resultQ, `{"type":"stat","value":"n","compare":{}}`), `invalid compare.mode`},
		{"null value", panel(`{"mode":"count","language":"logchefql","text":"x","group_by":null}`, `{"type":"stat"}`), "null at panels[0].query.group_by"},
		{"trailing data", json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[]}],"panels":[]} {}`), "after top-level value"},
		{"no tabs", json.RawMessage(`{"version":2,"features":[],"tabs":[],"panels":[]}`), "at least one tab"},
		{"two tabs are a later phase", json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"a","title":"A","layout":[]},{"id":"b","title":"B","layout":[]}],"panels":[]}`), "exceeds the max of 1"},
		{"empty tab id", json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"","title":"A","layout":[]}],"panels":[]}`), "empty id"},
		{"empty tab title", json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"a","title":"","layout":[]}],"panels":[]}`), "title of 0 characters"},
		{"long tab title", json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"a","title":"` + strings.Repeat("é", 61) + `","layout":[]}],"panels":[]}`), "title of 61 characters"},
		{"bad width", json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"a","title":"A","layout":[{"id":"p1","x":0,"y":0,"w":5,"h":2}]}],"panels":[]}`), "invalid width"},
		{"bad height", json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"a","title":"A","layout":[{"id":"p1","x":0,"y":0,"w":6,"h":7}]}],"panels":[]}`), "invalid height"},
		{"overlap", v2Blob(`{"id":"p1","x":0,"y":0,"w":6,"h":2},{"id":"p2","x":3,"y":1,"w":6,"h":2}`, v2StatPanel+`,`+strings.Replace(v2StatPanel, `"id":"p1"`, `"id":"p2"`, 1)), "overlap"},
		{"panel without layout", json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"a","title":"A","layout":[]}],"panels":[` + v2StatPanel + `]}`), "no layout entry"},
		{"layout without panel", json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"a","title":"A","layout":[` + v2Layout + `]}],"panels":[]}`), "does not reference any panel"},
		{"too many panels", tooManyV2Panels(), "exceeds the max of 24"},
		{"empty panel id", v2Blob(v2Layout, strings.Replace(v2StatPanel, `"id":"p1"`, `"id":""`, 1)), "empty id"},
		{"duplicate panel id", v2Blob(v2Layout, v2StatPanel+`,`+v2StatPanel), "duplicate panel id"},
		{"text panel is a later phase", v2Blob(v2Layout, `{"id":"p1","kind":"text","title":"","text":{"markdown":"x"}}`), `unknown field "text"`},
		{"bad kind", v2Blob(v2Layout, `{"id":"p1","kind":"note","title":"","team_id":1,"source_id":1,"query":`+countQ+`,"viz":{"type":"stat"}}`), "invalid kind"},
		{"locked in request", v2Blob(v2Layout, `{"id":"p1","kind":"query","title":"a","team_id":1,"source_id":1,"locked":false,"query":`+countQ+`,"viz":{"type":"stat"}}`), "carries locked"},
		{"bad team_id", v2Blob(v2Layout, strings.Replace(v2StatPanel, `"team_id":1`, `"team_id":0`, 1)), "invalid team_id"},
		{"bad source_id", v2Blob(v2Layout, strings.Replace(v2StatPanel, `"source_id":1`, `"source_id":-1`, 1)), "invalid source_id"},
		{"no query", v2Blob(v2Layout, `{"id":"p1","kind":"query","title":"a","team_id":1,"source_id":1,"viz":{"type":"stat"}}`), "has no query"},
		{"no viz", v2Blob(v2Layout, `{"id":"p1","kind":"query","title":"a","team_id":1,"source_id":1,"query":`+countQ+`}`), "has no viz"},
		{"bad mode", panel(`{"mode":"","language":"logchefql","text":"x"}`, `{"type":"stat"}`), "invalid query.mode"},
		{"language alias", panel(`{"mode":"count","language":"sql","text":"x"}`, `{"type":"stat"}`), "invalid query.language"},
		{"empty language", panel(`{"mode":"count","language":"","text":"x"}`, `{"type":"stat"}`), "invalid query.language"},
		{"group_by in result mode", panel(`{"mode":"result","language":"clickhouse-sql","text":"x","group_by":"a"}`, `{"type":"table"}`), "only count mode uses"},
		{"blank group_by", panel(`{"mode":"count","language":"logchefql","text":"x","group_by":" \u0085"}`, `{"type":"chart"}`), "blank query.group_by"},
		{"interval in result mode", panel(`{"mode":"result","language":"clickhouse-sql","text":"x","interval":"auto"}`, `{"type":"table"}`), "only count mode uses"},
		{"interval window is a later phase", panel(`{"mode":"count","language":"logchefql","text":"x","interval":"24h"}`, `{"type":"chart"}`), "invalid query.interval"},
		{"limit in count mode", panel(`{"mode":"count","language":"logchefql","text":"x","limit":10}`, `{"type":"stat"}`), "only result mode uses"},
		{"limit zero", panel(`{"mode":"result","language":"clickhouse-sql","text":"x","limit":0}`, `{"type":"table"}`), "invalid query.limit"},
		{"limit too large", panel(`{"mode":"result","language":"clickhouse-sql","text":"x","limit":10001}`, `{"type":"table"}`), "invalid query.limit"},
		{"translate_limit in result mode", panel(`{"mode":"result","language":"logchefql","text":"x","translate_limit":10}`, `{"type":"table"}`), "only count mode with logchefql"},
		{"translate_limit with sql", panel(`{"mode":"count","language":"clickhouse-sql","text":"x","translate_limit":10}`, `{"type":"stat"}`), "only count mode with logchefql"},
		{"translate_limit too large", panel(`{"mode":"count","language":"logchefql","text":"x","translate_limit":100001}`, `{"type":"stat"}`), "invalid query.translate_limit"},
		{"bad viz type", panel(countQ, `{"type":"gauge"}`), "invalid viz.type"},
		{"table in count mode", panel(countQ, `{"type":"table"}`), "table needs result mode"},
		{"result stat needs a value", panel(resultQ, `{"type":"stat"}`), "needs viz.value"},
		{"pie without group_by", panel(countQ, `{"type":"pie"}`), "needs query.group_by"},
		{"toplist without group_by", panel(countQ, `{"type":"toplist"}`), "needs query.group_by"},
		{"mark on stat", panel(countQ, `{"type":"stat","mark":"line"}`), "only a chart uses"},
		{"bad mark", panel(countQ, `{"type":"chart","mark":"bars"}`), "invalid viz.mark"},
		{"stack on stat", panel(countQ, `{"type":"stat","stack":"none"}`), "only a chart uses"},
		{"bad stack", panel(countQ, `{"type":"chart","stack":"grouped"}`), "invalid viz.stack"},
		{"reduce on chart", panel(countQ, `{"type":"chart","reduce":"sum"}`), "only a stat uses"},
		{"unknown reducer", panel(countQ, `{"type":"stat","reduce":"median"}`), "invalid viz.reduce"},
		{"donut on toplist", panel(`{"mode":"count","language":"logchefql","text":"x","group_by":"a"}`, `{"type":"toplist","donut":true}`), "only a pie uses"},
		{"visible_columns on chart", panel(countQ, `{"type":"chart","visible_columns":["a"]}`), "only a table uses"},
		{"empty visible_columns", panel(resultQ, `{"type":"table","visible_columns":[]}`), "empty viz.visible_columns"},
		{"blank visible column", panel(resultQ, `{"type":"table","visible_columns":[""]}`), "empty name"},
		{"duplicate visible column", panel(resultQ, `{"type":"table","visible_columns":["a","a"]}`), "twice"},
		{"cache ttl", json.RawMessage(`{"version":2,"features":[],"cache_ttl_seconds":-1,"tabs":[{"id":"a","title":"A","layout":[]}],"panels":[]}`), "cache_ttl_seconds"},
		{"calendar range is a later phase", json.RawMessage(`{"version":2,"features":[],"default_time_range":"today-6d","tabs":[{"id":"a","title":"A","layout":[]}],"panels":[]}`), "default_time_range"},
		{"version 3", json.RawMessage(`{"version":3,"features":[],"tabs":[],"panels":[]}`), "unsupported panels version 3"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateDashboardPanels(tc.blob)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func tooManyV2Panels() json.RawMessage {
	panels := make([]string, 0, 25)
	for i := range 25 {
		panels = append(panels, strings.Replace(v2StatPanel, `"id":"p1"`, fmt.Sprintf(`"id":"p%d"`, i), 1))
	}
	return json.RawMessage(`{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[]}],"panels":[` + strings.Join(panels, ",") + `]}`)
}

// upgradeV1 upgrades a single-panel schema 1 blob built from the given panel
// JSON and the standard layout, and fails the test on error.
func upgradeV1(t *testing.T, blob string) string {
	t.Helper()
	out, err := UpgradeDashboardPanels(json.RawMessage(blob))
	if err != nil {
		t.Fatalf("upgrade %s: %v", blob, err)
	}
	if err := ValidateDashboardPanels(out); err != nil {
		t.Fatalf("upgraded blob fails schema 2 validation: %v\n%s", err, out)
	}
	return string(out)
}

func v1Single(panel string) string {
	return `{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[` + panel + `]}`
}

func v2Single(panel string) string {
	return `{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}]}],"panels":[` + panel + `]}`
}

func TestUpgradeDashboardPanels_Golden(t *testing.T) {
	tests := []struct {
		name string
		v1   string
		want string
	}{
		{
			"timeseries with every option",
			`{"id":"p1","title":"5xx by service","type":"timeseries","team_id":1,"source_id":1,"query":"status>=500 & a<b","query_language":"logchefql","options":{"group_by":"service","limit":50,"columns":[],"chart":"bars","bar_mode":"grouped"}}`,
			`{"id":"p1","kind":"query","title":"5xx by service","team_id":1,"source_id":1,"query":{"mode":"count","language":"logchefql","text":"status>=500 & a<b","group_by":"service","interval":"auto","translate_limit":50},"viz":{"type":"chart","mark":"bar","stack":"none"}}`,
		},
		{
			"stat",
			`{"id":"p1","title":"errors","type":"stat","team_id":2,"source_id":3,"query":"level=\"error\"","query_language":"logchefql","options":{}}`,
			`{"id":"p1","kind":"query","title":"errors","team_id":2,"source_id":3,"query":{"mode":"count","language":"logchefql","text":"level=\"error\"","interval":"auto"},"viz":{"type":"stat","reduce":"sum"}}`,
		},
		{
			"breakdown bars",
			`{"id":"p1","title":"by host","type":"breakdown","team_id":1,"source_id":1,"query":"*","query_language":"logsql","options":{"group_by":"host"}}`,
			`{"id":"p1","kind":"query","title":"by host","team_id":1,"source_id":1,"query":{"mode":"count","language":"logsql","text":"*","group_by":"host","interval":"auto"},"viz":{"type":"toplist"}}`,
		},
		{
			"breakdown donut",
			`{"id":"p1","title":"by host","type":"breakdown","team_id":1,"source_id":1,"query":"","query_language":"logchefql","options":{"group_by":"host","breakdown_view":"donut"}}`,
			`{"id":"p1","kind":"query","title":"by host","team_id":1,"source_id":1,"query":{"mode":"count","language":"logchefql","text":"","group_by":"host","interval":"auto"},"viz":{"type":"pie","donut":true}}`,
		},
		{
			"table without a limit",
			`{"id":"p1","title":"rows","type":"table","team_id":1,"source_id":1,"query":"SELECT 1","query_language":"clickhouse-sql"}`,
			`{"id":"p1","kind":"query","title":"rows","team_id":1,"source_id":1,"query":{"mode":"result","language":"clickhouse-sql","text":"SELECT 1","limit":100},"viz":{"type":"table"}}`,
		},
		{
			"table with a limit of 50,000 and columns",
			`{"id":"p1","title":"rows","type":"table","team_id":1,"source_id":1,"query":"SELECT 1","query_language":"clickhouse-sql","options":{"limit":50000,"columns":["ts","message"]}}`,
			`{"id":"p1","kind":"query","title":"rows","team_id":1,"source_id":1,"query":{"mode":"result","language":"clickhouse-sql","text":"SELECT 1","limit":1000},"viz":{"type":"table","visible_columns":["ts","message"]}}`,
		},
		{
			"sql alias",
			`{"id":"p1","title":"t","type":"table","team_id":1,"source_id":1,"query":"SELECT 1","query_language":"sql","options":{"limit":20}}`,
			`{"id":"p1","kind":"query","title":"t","team_id":1,"source_id":1,"query":{"mode":"result","language":"clickhouse-sql","text":"SELECT 1","limit":20},"viz":{"type":"table"}}`,
		},
		{
			"logchefql stat with limit keeps the translate limit",
			`{"id":"p1","title":"t","type":"stat","team_id":1,"source_id":1,"query":"x","query_language":"logchefql","options":{"limit":50000}}`,
			`{"id":"p1","kind":"query","title":"t","team_id":1,"source_id":1,"query":{"mode":"count","language":"logchefql","text":"x","interval":"auto","translate_limit":50000},"viz":{"type":"stat","reduce":"sum"}}`,
		},
		{
			"sql stat drops the limit, which never translates",
			`{"id":"p1","title":"t","type":"stat","team_id":1,"source_id":1,"query":"SELECT 1","query_language":"clickhouse-sql","options":{"limit":500}}`,
			`{"id":"p1","kind":"query","title":"t","team_id":1,"source_id":1,"query":{"mode":"count","language":"clickhouse-sql","text":"SELECT 1","interval":"auto"},"viz":{"type":"stat","reduce":"sum"}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := upgradeV1(t, v1Single(tc.v1)); got != v2Single(tc.want) {
				t.Fatalf("upgrade mismatch\n got %s\nwant %s", got, v2Single(tc.want))
			}
		})
	}

	t.Run("dashboard settings", func(t *testing.T) {
		got := upgradeV1(t, `{"version":1,"cache_ttl_seconds":0,"default_time_range":"6h","layout":[],"panels":[]}`)
		want := `{"version":2,"features":[],"cache_ttl_seconds":0,"default_time_range":"6h","tabs":[{"id":"main","title":"Main","layout":[]}],"panels":[]}`
		if got != want {
			t.Fatalf("upgrade mismatch\n got %s\nwant %s", got, want)
		}
	})

	t.Run("schema 2 is returned unchanged", func(t *testing.T) {
		in := json.RawMessage(`{ "version": 2,  "features": [], "tabs": [{"id":"main","title":"Main","layout":[]}], "panels": [] }`)
		out, err := UpgradeDashboardPanels(in)
		if err != nil || string(out) != string(in) {
			t.Fatalf("schema 2 upgrade = %s, %v; want the input bytes", out, err)
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		a := upgradeV1(t, validPanelsBlobString())
		b := upgradeV1(t, validPanelsBlobString())
		if a != b {
			t.Fatalf("two upgrades of one blob differ")
		}
	})

	t.Run("invalid schema 1 fails to upgrade", func(t *testing.T) {
		for _, blob := range []string{`{"version":1,"layout":[],"panels":[{"id":"p1","type":"pie","team_id":1,"source_id":1,"query_language":"logchefql"}]}`, `{nope`, `{"version":3}`} {
			if _, err := UpgradeDashboardPanels(json.RawMessage(blob)); err == nil {
				t.Errorf("upgrade accepted %s", blob)
			}
		}
	})
}

func validPanelsBlobString() string { return string(validPanelsBlob()) }

// TestUpgradeDashboardPanels_Normalization holds the fixtures of "Upgrade from
// schema 1": blobs that differ only in a spelling that schema 1 accepts
// upgrade to the same bytes.
func TestUpgradeDashboardPanels_Normalization(t *testing.T) {
	ts := func(options string) string {
		p := `{"id":"p1","title":"a","type":"timeseries","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"`
		if options != "" {
			p += `,"options":` + options
		}
		return v1Single(p + `}`)
	}
	stat := func(options string) string {
		p := `{"id":"p1","title":"a","type":"stat","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"`
		if options != "" {
			p += `,"options":` + options
		}
		return v1Single(p + `}`)
	}
	breakdown := func(options string) string {
		return v1Single(`{"id":"p1","title":"a","type":"breakdown","team_id":1,"source_id":1,"query":"x","query_language":"logchefql","options":` + options + `}`)
	}
	table := func(options string) string {
		p := `{"id":"p1","title":"a","type":"table","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"`
		if options != "" {
			p += `,"options":` + options
		}
		return v1Single(p + `}`)
	}
	groups := []struct {
		name  string
		blobs []string
	}{
		{"sql alias", []string{
			v1Single(`{"id":"p1","title":"a","type":"table","team_id":1,"source_id":1,"query":"x","query_language":"sql"}`),
			v1Single(`{"id":"p1","title":"a","type":"table","team_id":1,"source_id":1,"query":"x","query_language":"clickhouse-sql"}`),
		}},
		{"chart absent, null and empty", []string{ts(""), ts(`{"chart":null}`), ts(`{"chart":""}`), ts(`{}`)}},
		{"chart on a stat is dropped", []string{stat(""), stat(`{"chart":"bars"}`)}},
		{"bar_mode absent, null and empty with bars", []string{ts(`{"chart":"bars"}`), ts(`{"chart":"bars","bar_mode":null}`), ts(`{"chart":"bars","bar_mode":""}`), ts(`{"chart":"bars","bar_mode":"stacked"}`)}},
		{"bar_mode on a line chart is dropped", []string{ts(`{"chart":"line"}`), ts(`{"chart":"line","bar_mode":"grouped"}`)}},
		{"area ignores bar_mode", []string{ts(`{"chart":"area"}`), ts(`{"chart":"area","bar_mode":"grouped"}`)}},
		{"breakdown_view absent and null", []string{breakdown(`{"group_by":"g"}`), breakdown(`{"group_by":"g","breakdown_view":null}`), breakdown(`{"group_by":"g","breakdown_view":"horizontal-bars"}`)}},
		{"breakdown_view empty on a timeseries", []string{ts(""), ts(`{"breakdown_view":""}`)}},
		{"group_by absent, null, empty and blank", []string{ts(""), ts(`{"group_by":null}`), ts(`{"group_by":""}`), ts(`{"group_by":"  "}`), ts(`{"group_by":"\u0085"}`)}},
		{"group_by on a stat is dropped", []string{stat(""), stat(`{"group_by":"service"}`)}},
		{"columns absent, null and empty", []string{table(""), table(`{"columns":null}`), table(`{"columns":[]}`)}},
		{"columns on a stat are dropped", []string{stat(""), stat(`{"columns":["a"]}`)}},
		{"limit absent and null", []string{table(""), table(`{"limit":null}`)}},
		{"options absent, null and empty", []string{stat(""), stat(`null`), stat(`{}`)}},
		{"title absent, null and empty", []string{
			v1Single(`{"id":"p1","type":"stat","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"}`),
			v1Single(`{"id":"p1","title":null,"type":"stat","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"}`),
			v1Single(`{"id":"p1","title":"","type":"stat","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"}`),
		}},
		{"query absent, null and empty", []string{
			v1Single(`{"id":"p1","title":"a","type":"stat","team_id":1,"source_id":1,"query_language":"logchefql"}`),
			v1Single(`{"id":"p1","title":"a","type":"stat","team_id":1,"source_id":1,"query":null,"query_language":"logchefql"}`),
			v1Single(`{"id":"p1","title":"a","type":"stat","team_id":1,"source_id":1,"query":"","query_language":"logchefql"}`),
		}},
		{"settings absent and null", []string{
			`{"version":1,"layout":[],"panels":[]}`,
			`{"version":1,"cache_ttl_seconds":null,"default_time_range":null,"layout":[],"panels":[]}`,
		}},
		{"unknown keys at every level", []string{
			`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"a","type":"table","team_id":1,"source_id":1,"query":"x","query_language":"logchefql","options":{"limit":5}}]}`,
			`{"version":1,"extra":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2,"color":"red"}],"panels":[{"id":"p1","title":"a","type":"table","team_id":1,"source_id":1,"query":"x","query_language":"logchefql","note":"n","options":{"limit":5,"theme":"dark"}}]}`,
		}},
		{"empty dashboard forms", []string{
			`{"version":1}`,
			`{"version":1,"layout":null,"panels":null}`,
			`{"version":1,"layout":[],"panels":[]}`,
		}},
		{"null coordinates are zero", []string{
			`{"version":1,"layout":[{"id":"p1","x":null,"y":null,"w":6,"h":2}],"panels":[{"id":"p1","type":"stat","team_id":1,"source_id":1,"query_language":"logchefql"}]}`,
			`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","type":"stat","team_id":1,"source_id":1,"query_language":"logchefql"}]}`,
		}},
		{"stored locked flag is dropped", []string{
			v1Single(`{"id":"p1","title":"a","type":"stat","team_id":1,"source_id":1,"query":"x","query_language":"logchefql","locked":true}`),
			v1Single(`{"id":"p1","title":"a","type":"stat","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"}`),
		}},
		{"case-insensitive keys with the last duplicate winning", []string{
			`{"VERSION":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"first","TITLE":"last","type":"stat","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"}]}`,
			`{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"last","type":"stat","team_id":1,"source_id":1,"query":"x","query_language":"logchefql"}]}`,
		}},
	}
	for _, g := range groups {
		t.Run(g.name, func(t *testing.T) {
			first := upgradeV1(t, g.blobs[0])
			for _, blob := range g.blobs[1:] {
				if got := upgradeV1(t, blob); got != first {
					t.Errorf("spelling %s upgraded differently:\n got %s\nwant %s", blob, got, first)
				}
			}
		})
	}

	t.Run("columns drop null and empty entries and keep the first duplicate", func(t *testing.T) {
		got := upgradeV1(t, table(`{"columns":[null,"","message","message","host"]}`))
		if !strings.Contains(got, `"visible_columns":["message","host"]`) {
			t.Fatalf("visible_columns not normalized: %s", got)
		}
		if got := upgradeV1(t, table(`{"columns":[null,""]}`)); strings.Contains(got, "visible_columns") {
			t.Fatalf("empty column list kept: %s", got)
		}
	})

	t.Run("group_by keeps its bytes and U+FEFF", func(t *testing.T) {
		got := upgradeV1(t, ts(`{"group_by":"\ufeff"}`))
		if !strings.Contains(got, `"group_by":"\ufeff"`) {
			t.Fatalf("U+FEFF group was dropped or re-encoded: %s", got)
		}
		got = upgradeV1(t, ts(`{"group_by":" service "}`))
		if !strings.Contains(got, `"group_by":" service "`) {
			t.Fatalf("kept group was trimmed: %s", got)
		}
	})

	t.Run("the winning raw string survives without re-encoding", func(t *testing.T) {
		got := upgradeV1(t, `{"version":1,"layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","title":"<first>","TITLE":"<last> & \/","type":"stat","team_id":1,"source_id":1,"query":"a<b>c&d","query_language":"logchefql"}]}`)
		for _, want := range []string{`"title":"<last> & \/"`, `"text":"a<b>c&d"`} {
			if !strings.Contains(got, want) {
				t.Errorf("upgraded blob lacks %s: %s", want, got)
			}
		}
	})

	t.Run("overlap after null coordinates is still rejected", func(t *testing.T) {
		blob := `{"version":1,"layout":[{"id":"p1","x":null,"y":null,"w":6,"h":2},{"id":"p2","x":0,"y":0,"w":6,"h":2}],"panels":[{"id":"p1","type":"stat","team_id":1,"source_id":1,"query_language":"logchefql"},{"id":"p2","type":"stat","team_id":1,"source_id":1,"query_language":"logchefql"}]}`
		if _, err := UpgradeDashboardPanels(json.RawMessage(blob)); err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("overlap at zero accepted: %v", err)
		}
	})
}

// boundaryV1Blob is a valid schema 1 blob of exactly MaxDashboardPanelsSize
// bytes with 24 panels; one query holds <, >, & and non-ASCII text.
func boundaryV1Blob(t *testing.T) json.RawMessage {
	t.Helper()
	layouts := make([]string, 0, 24)
	panels := make([]string, 0, 24)
	for i := range 24 {
		layouts = append(layouts, fmt.Sprintf(`{"id":"p%d","x":%d,"y":%d,"w":3,"h":2}`, i, (i%4)*3, (i/4)*2))
		query := "x"
		if i == 0 {
			query = `a<b & c>d é 日本 QUERY`
		}
		panels = append(panels, `{"id":"p`+strconv.Itoa(i)+`","title":"t`+strconv.Itoa(i)+`","type":"table","team_id":1,"source_id":1,"query":"`+query+`","query_language":"clickhouse-sql","options":{"limit":`+strconv.Itoa(i+1)+`,"columns":["a","b"]}}`)
	}
	head := `{"version":1,"layout":[` + strings.Join(layouts, ",") + `],"panels":[` + strings.Join(panels, ",") + `]}`
	pad := MaxDashboardPanelsSize - (len(head) - len("QUERY"))
	if pad < 0 {
		t.Fatalf("boundary fixture base is %d bytes, over the limit", len(head)-len("QUERY"))
	}
	blob := strings.Replace(head, "QUERY", strings.Repeat("é", pad/2)+strings.Repeat("x", pad%2), 1)
	if n := len(blob); n != MaxDashboardPanelsSize {
		t.Fatalf("boundary fixture is %d bytes, want %d", n, MaxDashboardPanelsSize)
	}
	return json.RawMessage(blob)
}

func TestUpgradeDashboardPanels_SizeBound(t *testing.T) {
	assertBound := func(t *testing.T, in json.RawMessage) json.RawMessage {
		t.Helper()
		if err := ValidateDashboardPanels(in); err != nil {
			t.Fatalf("fixture is not valid schema 1: %v", err)
		}
		out, err := UpgradeDashboardPanels(in)
		if err != nil {
			t.Fatalf("upgrade: %v", err)
		}
		if err := ValidateDashboardPanels(out); err != nil {
			t.Fatalf("upgraded blob fails schema 2 validation: %v", err)
		}
		var before dashboardPanels
		var after dashboardPanelsV2
		if err := json.Unmarshal(in, &before); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(out, &after); err != nil {
			t.Fatal(err)
		}
		for i, p := range before.Panels {
			q := after.Panels[i]
			if p.ID != q.ID || p.Title != q.Title || p.Query != q.Query.Text {
				t.Fatalf("upgrade changed decoded strings: %+v -> %+v", p, q)
			}
		}
		for i, l := range before.Layout {
			got := after.Tabs[0].Layout[i]
			if l.ID != got.ID || l.X != got.X || l.Y != got.Y || l.W != got.W || l.H != got.H {
				t.Fatalf("upgrade changed decoded layout: %+v -> %+v", l, got)
			}
		}
		var probe struct {
			Panels []json.RawMessage `json:"panels"`
		}
		if err := json.Unmarshal(in, &probe); err != nil {
			t.Fatal(err)
		}
		limit := len(in) + DashboardUpgradeBlobOverhead + DashboardUpgradePanelOverhead*len(probe.Panels)
		if len(out) > limit {
			t.Fatalf("upgraded blob is %d bytes, over the bound of %d for %d bytes and %d panels", len(out), limit, len(in), len(probe.Panels))
		}
		return out
	}

	t.Run("boundary fixture", func(t *testing.T) {
		in := boundaryV1Blob(t)
		out := assertBound(t, in)
		if !strings.Contains(string(out), `a<b & c>d é 日本`) {
			t.Fatalf("the special-character query was re-encoded")
		}
		if len(out) > MaxDashboardPanelsSizeV2 {
			t.Fatalf("upgraded boundary blob exceeds the schema 2 limit")
		}
	})

	t.Run("property over random valid blobs", func(t *testing.T) {
		seed := rand.Uint64()
		rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
		for i := range 300 {
			in := randomV1Blob(rng)
			if err := ValidateDashboardPanels(in); err != nil {
				t.Fatalf("seed %d iteration %d: generator produced an invalid schema 1 blob: %v\n%s", seed, i, err, in)
			}
			assertBound(t, in)
		}
	})
}

// randomV1Blob builds a random blob that the schema 1 validator accepts,
// drawing on every spelling that the upgrade normalizes.
func randomV1Blob(rng *rand.Rand) json.RawMessage {
	pick := func(items ...string) string { return items[rng.IntN(len(items))] }
	randString := func() string {
		parts := []string{"x", "a<b", "c>d", "e&f", "é", "日本語", `\"q\"`, `\\`, `A`, `\/`, " ", "\\u2028", "tab\\t", "ü😀"}
		var b strings.Builder
		for range rng.IntN(6) {
			b.WriteString(pick(parts...))
		}
		return b.String()
	}
	optString := func(key string, values ...string) string {
		switch rng.IntN(5) {
		case 4:
			return `"` + key + `":"` + pick(values...) + `","` + key + `":null`
		case 0:
			return ""
		case 1:
			return `"` + key + `":null`
		default:
			return `"` + key + `":"` + pick(values...) + `"`
		}
	}
	n := rng.IntN(MaxDashboardPanels + 1)
	widths := []int{2, 3, 4, 6, 8, 9, 12}
	layouts := make([]string, 0, n)
	panels := make([]string, 0, n)
	x, y, rowH := 0, 0, 0
	for i := range n {
		w := widths[rng.IntN(len(widths))]
		h := 1 + rng.IntN(MaxDashboardPanelH)
		if x+w > DashboardGridColumns {
			x, y, rowH = 0, y+rowH, 0
		}
		xs, ys := fmt.Sprint(x), fmt.Sprint(y)
		if x == 0 && rng.IntN(3) == 0 {
			xs = "null"
		}
		if y == 0 && rng.IntN(3) == 0 {
			ys = "null"
		}
		layouts = append(layouts, fmt.Sprintf(`{"id":"p%d","x":%s,"y":%s,"w":%d,"h":%d,"id":null,"x":null,"y":null,"w":null,"h":null,"junk":true}`, i, xs, ys, w, h))
		x += w
		rowH = max(rowH, h)

		typ := pick("timeseries", "stat", "breakdown", "table")
		language := pick("logchefql", "clickhouse-sql", "logsql", "sql")
		var opts []string
		switch typ {
		case "timeseries":
			if o := optString("chart", "", "bars", "line", "area"); o != "" {
				opts = append(opts, o)
			}
			if o := optString("bar_mode", "", "stacked", "grouped"); o != "" {
				opts = append(opts, o)
			}
			if o := optString("group_by", "", "  ", "service", " host ", "\\u0085", "\\ufeff"); o != "" {
				opts = append(opts, o)
			}
			if o := optString("breakdown_view", ""); o != "" {
				opts = append(opts, o)
			}
		case "breakdown":
			opts = append(opts, `"group_by":"`+pick("service", " host ", "日本")+`"`)
			if o := optString("breakdown_view", "horizontal-bars", "donut"); o != "" {
				opts = append(opts, o)
			}
		case "table":
			switch rng.IntN(4) {
			case 0:
			case 1:
				opts = append(opts, `"columns":null`)
			default:
				opts = append(opts, `"columns":[`+pick(`null,"","message","message","host"`, `"a","b","é"`, ``, `null,""`)+`]`)
			}
		}
		if rng.IntN(2) == 0 {
			opts = append(opts, fmt.Sprintf(`"limit":%d`, 1+rng.IntN(MaxDashboardPanelOptionLimit)))
		} else if rng.IntN(3) == 0 {
			opts = append(opts, `"limit":null`)
		}
		if rng.IntN(4) == 0 {
			opts = append(opts, `"unknown_option":"`+randString()+`"`)
		}
		panel := fmt.Sprintf(`{"id":"p%d"`, i)
		switch rng.IntN(3) {
		case 0:
			panel += `,"title":"` + randString() + `"`
		case 1:
			panel += `,"title":null`
		}
		panel += fmt.Sprintf(`,"type":%q,"team_id":%d,"source_id":%d`, typ, 1+rng.IntN(5), 1+rng.IntN(5))
		switch rng.IntN(3) {
		case 0:
			panel += `,"query":"` + randString() + `"`
		case 1:
			panel += `,"query":null`
		}
		panel += `,"query_language":"` + language + `"`
		switch {
		case typ == "breakdown" || rng.IntN(4) != 0:
			panel += `,"options":{` + strings.Join(opts, ",") + `}`
		case rng.IntN(2) == 0:
			panel += `,"options":null`
		}
		if rng.IntN(5) == 0 {
			panel += `,"locked":true`
		}
		if rng.IntN(4) == 0 {
			panel += `,"extra":"` + randString() + `"`
		}
		if rng.IntN(2) == 0 {
			for _, key := range []string{"id", "title", "query", "options"} {
				if key != "options" || typ != "breakdown" {
					panel += `,"` + key + `":null`
				}
			}
		}
		panels = append(panels, panel+"}")
	}
	var settings string
	switch rng.IntN(3) {
	case 0:
		settings = fmt.Sprintf(`,"cache_ttl_seconds":%d,"default_time_range":%q`, rng.IntN(MaxDashboardCacheTTLSeconds+1), pick("15m", "6h", "7d", "today", "yesterday", "2w"))
	case 1:
		settings = `,"cache_ttl_seconds":null,"default_time_range":null`
	}
	return json.RawMessage(`{"version":1,"unknown":1` + settings + `,"layout":[` + strings.Join(layouts, ",") + `],"panels":[` + strings.Join(panels, ",") + `]}`)
}

func TestDashboardFeatures(t *testing.T) {
	for name, blob := range map[string]json.RawMessage{
		"upgraded schema 1": mustUpgrade(t, validPanelsBlob()),
		"schema 2 chart":    v2Blob(v2Layout, v2ChartPanel),
		"schema 2 table":    v2Blob(v2Layout, v2TablePanel),
	} {
		got, err := DashboardFeatures(blob)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != 0 || got == nil {
			t.Fatalf("%s: features = %v, want an empty list", name, got)
		}
	}
}

func mustUpgrade(t *testing.T, raw json.RawMessage) json.RawMessage {
	t.Helper()
	out, err := UpgradeDashboardPanels(raw)
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	return out
}

// TestDashboardSchema2FeatureTableComplete walks the schema 2 structs and the
// enum tables: every field and enum value must name a feature or the base
// mark, every named feature must have a rule, and the tables must not name a
// field that the structs lack.
func TestDashboardSchema2FeatureTableComplete(t *testing.T) {
	paths := map[string]struct{}{}
	var walk func(typ reflect.Type, prefix string)
	walk = func(typ reflect.Type, prefix string) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct {
			return
		}
		for f := range typ.Fields() {
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				t.Fatalf("field %s.%s has no json name", typ.Name(), f.Name)
			}
			path := name
			if prefix != "" {
				path = prefix + "." + name
			}
			paths[path] = struct{}{}
			walk(f.Type, path)
		}
	}
	walk(reflect.TypeFor[dashboardPanelsV2](), "")

	known := map[DashboardFeature]struct{}{dashboardFeatureBase: {}}
	for _, rule := range dashboardFeatureRules {
		known[rule.name] = struct{}{}
	}
	for path := range paths {
		origin, ok := dashboardSchema2FieldOrigins[path]
		if !ok {
			t.Errorf("schema 2 field %q has neither a feature nor the base mark", path)
			continue
		}
		if _, ok := known[origin]; !ok {
			t.Errorf("schema 2 field %q names feature %q, which has no rule", path, origin)
		}
	}
	for path := range dashboardSchema2FieldOrigins {
		if _, ok := paths[path]; !ok {
			t.Errorf("origin table names %q, which the schema 2 structs lack", path)
		}
	}

	enums := map[string]any{
		"panels.viz.compare.mode":       validDashboardCompareModes,
		"panels.viz.compare.good":       validDashboardCompareGood,
		"panels.viz.x_scale":            validDashboardXScales,
		"panels.viz.orientation":        validDashboardOrientations,
		"panels.kind":                   validDashboardPanelKinds,
		"panels.query.mode":             validDashboardQueryModes,
		"panels.query.language":         validDashboardQueryLanguages,
		"panels.query.interval":         validDashboardQueryIntervals,
		"panels.viz.type":               validDashboardVizTypes,
		"panels.viz.mark":               validDashboardChartMarks,
		"panels.viz.stack":              validDashboardChartStacks,
		"panels.viz.reduce":             validDashboardStatReduces,
		"panels.viz.columns.kind":       validDashboardColumnKinds,
		"panels.viz.columns.confidence": validDashboardColumnConfidences,
		"panels.viz.format.notation":    validDashboardNumberNotations,
		"panels.viz.format.unit":        validDashboardNumberUnits,
	}
	for path, table := range enums {
		if _, ok := paths[path]; !ok {
			t.Errorf("enum table for %q names a field the structs lack", path)
		}
		origins := dashboardSchema2EnumOrigins[path]
		for _, key := range reflect.ValueOf(table).MapKeys() {
			value := key.String()
			origin, ok := origins[value]
			if !ok {
				t.Errorf("enum value %q of %q has neither a feature nor the base mark", value, path)
				continue
			}
			if _, ok := known[origin]; !ok {
				t.Errorf("enum value %q of %q names feature %q, which has no rule", value, path, origin)
			}
		}
		for value := range origins {
			if !reflect.ValueOf(table).MapIndex(reflect.ValueOf(value).Convert(reflect.TypeOf(table).Key())).IsValid() {
				t.Errorf("enum origin %q of %q is not an accepted value", value, path)
			}
		}
	}
	for path := range dashboardSchema2EnumOrigins {
		if _, ok := enums[path]; !ok {
			t.Errorf("enum origins name %q, which has no enum table in this test", path)
		}
	}
}

func TestParseDashboardClient(t *testing.T) {
	str := func(s string) *string { return &s }
	tests := []struct {
		name       string
		schema     *string
		features   string
		wantSchema int
		wantErr    bool
	}{
		{"absent header is schema 1", nil, "", 1, false},
		{"explicit 1", str("1"), "", 1, false},
		{"2", str("2"), "", 2, false},
		{"2 with whitespace", str(" \t2\t "), "", 2, false},
		{"NBSP", str("\u00a02"), "", 0, true},
		{"EM SPACE", str("2\u2003"), "", 0, true},
		{"empty value", str(""), "", 0, true},
		{"zero", str("0"), "", 0, true},
		{"three", str("3"), "", 0, true},
		{"list", str("1,2"), "", 0, true},
		{"features on a schema 2 client", str("2"), " a , b,,unknown", 2, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client, err := ParseDashboardClient(tc.schema, tc.features)
			if tc.wantErr {
				if err == nil || err.Error() != "X-Logchef-Dashboard-Schema must be 1 or 2" {
					t.Fatalf("err = %v, want the schema header error", err)
				}
				return
			}
			if err != nil || client.Schema != tc.wantSchema {
				t.Fatalf("client = %+v, %v; want schema %d", client, err, tc.wantSchema)
			}
			if len(client.Features) != 0 {
				t.Fatalf("features = %v, want none known in this release", client.Features)
			}
		})
	}
}

func TestSummarizeDashboardPanels(t *testing.T) {
	t.Run("schema 1 reports the upgraded kinds", func(t *testing.T) {
		blob := json.RawMessage(`{"version":1,"layout":[{"id":"a","x":0,"y":0,"w":6,"h":2},{"id":"b","x":6,"y":0,"w":6,"h":2},{"id":"c","x":0,"y":2,"w":6,"h":2}],"panels":[` +
			`{"id":"a","type":"timeseries","team_id":3,"source_id":1,"query_language":"logchefql"},` +
			`{"id":"b","type":"breakdown","team_id":1,"source_id":1,"query_language":"logchefql","options":{"group_by":"g","breakdown_view":"donut"}},` +
			`{"id":"c","type":"timeseries","team_id":3,"source_id":2,"query_language":"logchefql"}]}`)
		got := SummarizeDashboardPanels(blob)
		want := DashboardPanelsSummary{PanelCount: 3, TabCount: 1, PanelTypes: []string{"chart", "pie"}, PanelTeamIDs: []int{1, 3}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("summary = %+v, want %+v", got, want)
		}
		if got.PanelTypesJSON() != `["chart","pie"]` || got.PanelTeamIDsJSON() != `[1,3]` {
			t.Fatalf("column JSON = %s %s", got.PanelTypesJSON(), got.PanelTeamIDsJSON())
		}
	})
	t.Run("schema 2", func(t *testing.T) {
		got := SummarizeDashboardPanels(v2Blob(v2Layout, v2TablePanel))
		want := DashboardPanelsSummary{PanelCount: 1, TabCount: 1, PanelTypes: []string{"table"}, PanelTeamIDs: []int{1}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("summary = %+v, want %+v", got, want)
		}
	})
	t.Run("empty", func(t *testing.T) {
		got := SummarizeDashboardPanels(EmptyDashboardPanelsJSON())
		if got.Corrupt || got.PanelCount != 0 || got.TabCount != 1 || got.PanelTypesJSON() != "[]" || got.PanelTeamIDsJSON() != "[]" {
			t.Fatalf("summary = %+v", got)
		}
	})
	t.Run("corrupt", func(t *testing.T) {
		for _, blob := range []string{`{nope`, `{"version":1,"panels":[{"id":"p1","type":"pie","team_id":1,"source_id":1,"query_language":"logchefql"}]}`, `{"version":2,"features":[],"tabs":[],"panels":[]}`} {
			got := SummarizeDashboardPanels(json.RawMessage(blob))
			want := DashboardPanelsSummary{PanelTypes: []string{}, PanelTeamIDs: []int{}, Corrupt: true}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("summary of %s = %+v, want the corrupt summary", blob, got)
			}
		}
	})
	t.Run("columns round-trip", func(t *testing.T) {
		types, teams, err := ParseDashboardSummaryColumns(`["chart","pie"]`, `[1,3]`)
		if err != nil || !slices.Equal(types, []string{"chart", "pie"}) || !slices.Equal(teams, []int{1, 3}) {
			t.Fatalf("parse = %v %v %v", types, teams, err)
		}
		if _, _, err := ParseDashboardSummaryColumns(`nope`, `[]`); err == nil {
			t.Fatal("bad column accepted")
		}
	})
}

func TestCanonicalDashboardPanels(t *testing.T) {
	in := json.RawMessage(`{ "version": 2, "features": ["tabs", "made-up"],
		"tabs": [ {"id":"main","title":"Main","layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}]} ],
		"panels": [ {"id":"p1","kind":"query","title":"a<b>&c","team_id":1,"source_id":1,"query":{"mode":"count","language":"logchefql","text":"x & y"},"viz":{"type":"stat"}} ] }`)
	got, err := CanonicalDashboardPanels(in)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	want := `{"version":2,"features":[],"tabs":[{"id":"main","title":"Main","layout":[{"id":"p1","x":0,"y":0,"w":6,"h":2}]}],"panels":[{"id":"p1","kind":"query","title":"a<b>&c","team_id":1,"source_id":1,"query":{"mode":"count","language":"logchefql","text":"x & y"},"viz":{"type":"stat"}}]}`
	if string(got) != want {
		t.Fatalf("canonical form\n got %s\nwant %s", got, want)
	}
	again, err := CanonicalDashboardPanels(got)
	if err != nil || string(again) != want {
		t.Fatalf("canonical form is not stable: %s %v", again, err)
	}
	if _, err := CanonicalDashboardPanels(json.RawMessage(`{"version":2,"features":[],"tabs":[],"panels":[]}`)); err == nil {
		t.Fatal("invalid blob accepted")
	}
}

func TestDashboardPanelsStoredFeatures(t *testing.T) {
	if got := DashboardPanelsStoredFeatures(json.RawMessage(`{"version":2,"features":["tabs","x"],"unknown":1}`)); !slices.Equal(got, []string{"tabs", "x"}) {
		t.Fatalf("stored features = %v", got)
	}
	if got := DashboardPanelsUnknownFeatures(json.RawMessage(`{"version":2,"features":["tabs"]}`)); !slices.Equal(got, []string{"tabs"}) {
		t.Fatalf("unknown features = %v", got)
	}
	for _, blob := range []string{`{"version":1,"features":["tabs"]}`, `{nope`, `{"version":2}`} {
		if got := DashboardPanelsStoredFeatures(json.RawMessage(blob)); len(got) != 0 {
			t.Errorf("stored features of %s = %v, want none", blob, got)
		}
	}
}

func TestRedactDashboardPanelsV2(t *testing.T) {
	raw := v2Blob(`{"id":"p1","x":0,"y":0,"w":6,"h":2},{"id":"p2","x":6,"y":0,"w":6,"h":2}`,
		v2ChartPanel+`,`+strings.Replace(strings.Replace(v2TablePanel, `"id":"p1"`, `"id":"p2"`, 1), `"team_id":1`, `"team_id":2`, 1))
	out, err := RedactDashboardPanels(raw, map[string]struct{}{"p2": {}})
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	var blob dashboardPanelsV2
	if err := json.Unmarshal(out, &blob); err != nil {
		t.Fatalf("decode redacted: %v", err)
	}
	if len(blob.Tabs) != 1 || len(blob.Tabs[0].Layout) != 2 {
		t.Fatalf("layout changed: %+v", blob.Tabs)
	}
	open, locked := blob.Panels[0], blob.Panels[1]
	if open.Locked != nil || open.Query == nil || open.Query.Text != "x" || open.Viz.Mark == nil {
		t.Errorf("accessible panel altered: %+v", open)
	}
	if locked.Locked == nil || !*locked.Locked || locked.Query != nil || locked.Viz == nil || locked.Viz.Type != DashboardVizTable || locked.Viz.VisibleColumns != nil {
		t.Errorf("locked panel not redacted: %+v %+v", locked, locked.Viz)
	}
	if locked.ID != "p2" || locked.Kind != DashboardPanelKindQuery || locked.Title != "a" || locked.TeamID != 2 || locked.SourceID != 1 {
		t.Errorf("locked panel lost placeholder fields: %+v", locked)
	}
	if strings.Contains(string(out), "SELECT 1") {
		t.Errorf("query text leaked: %s", out)
	}
	refs, err := DashboardPanelRefs(raw)
	if err != nil || len(refs) != 2 || refs[1].TeamID != 2 {
		t.Fatalf("refs = %+v, %v", refs, err)
	}
}

func TestUpgradeDashboardPanelsDuplicateNull(t *testing.T) {
	base := `{"version":1,"layout":[{"id":"p1","x":2,"y":3,"w":6,"h":2}],"panels":[{"id":"p1","title":"a\u0062","type":"table","team_id":1,"source_id":1,"query":"x\u2028","query_language":"logchefql","options":{"columns":["host"],"limit":37}}]}`
	for _, field := range []string{`"id":"p1"`, `"title":"a\u0062"`, `"query":"x\u2028"`, `"x":2`, `"y":3`, `"w":6`, `"h":2`, `"options":{"columns":["host"],"limit":37}`, `"columns":["host"]`, `"limit":37`} {
		t.Run(field, func(t *testing.T) {
			key, _, _ := strings.Cut(field, ":")
			in := json.RawMessage(strings.ReplaceAll(base, field, field+","+key+":null"))
			var decoded dashboardPanels
			if err := json.Unmarshal(in, &decoded); err != nil {
				t.Fatal(err)
			}
			canonical, err := json.Marshal(decoded)
			if err != nil {
				t.Fatal(err)
			}
			want := upgradeV1(t, string(canonical))
			got := upgradeV1(t, string(in))
			var gotValue, wantValue any
			if err := json.Unmarshal([]byte(got), &gotValue); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(gotValue, wantValue) {
				t.Fatalf("got %s; want %s", got, want)
			}
			if !strings.Contains(got, `a\u0062`) {
				t.Fatal("raw title token was not retained")
			}
		})
	}
	t.Run("reused column slice keeps a string when the next array has null", func(t *testing.T) {
		in := strings.Replace(base, `"columns":["host"]`, `"columns":["host"],"columns":[null]`, 1)
		if got := upgradeV1(t, in); !strings.Contains(got, `"visible_columns":["host"]`) {
			t.Fatalf("column lost during upgrade: %s", got)
		}
	})
}

func TestCanonicalDashboardPersistedSize(t *testing.T) {
	t.Run("rejects stored form over limit", func(t *testing.T) {
		raw := v2Blob(v2Layout, strings.Replace(v2StatPanel, `"text":"x"`, `"text":"`+strings.Repeat("\u2028", 100_000)+`"`, 1))
		if len(raw) >= MaxDashboardPanelsSizeV2 {
			t.Fatal("request must fit")
		}
		if out, err := CanonicalDashboardPanels(raw); err == nil || out != nil {
			t.Fatalf("got %d stored bytes and error %v; want rejection", len(out), err)
		}
	})
	storedOfSize := func(t *testing.T, size int) []byte {
		t.Helper()
		empty := v2Blob(v2Layout, strings.Replace(v2StatPanel, `"text":"x"`, `"text":""`, 1))
		const separators = 87_000
		padding := strings.Repeat("x", size-len(empty)-6*separators)
		return v2Blob(v2Layout, strings.Replace(v2StatPanel, `"text":"x"`, `"text":"`+strings.Repeat("\u2028", separators)+padding+`"`, 1))
	}
	for _, tc := range []struct {
		name   string
		stored int
		accept bool
	}{
		{"one byte under the limit", MaxDashboardPanelsSizeV2 - 1, true},
		{"exactly at the limit", MaxDashboardPanelsSizeV2, true},
		{"one byte over the limit", MaxDashboardPanelsSizeV2 + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := storedOfSize(t, tc.stored)
			if len(raw) >= MaxDashboardPanelsSizeV2 {
				t.Fatal("request must fit")
			}
			out, err := CanonicalDashboardPanels(raw)
			if !tc.accept {
				if err == nil || out != nil {
					t.Fatalf("got %d stored bytes and error %v; want rejection", len(out), err)
				}
				return
			}
			if err != nil || len(out) != tc.stored {
				t.Fatalf("got %d stored bytes and error %v; want %d bytes", len(out), err, tc.stored)
			}
			if err := ValidateDashboardPanels(out); err != nil {
				t.Fatalf("stored blob is invalid: %v", err)
			}
		})
	}
}

func TestParseDashboardClientKnownFeature(t *testing.T) {
	saved := dashboardFeatureRules
	t.Cleanup(func() { dashboardFeatureRules = saved })
	dashboardFeatureRules = append(dashboardFeatureRules, struct {
		name DashboardFeature
		uses func(*dashboardPanelsV2) bool
	}{name: "test_feature", uses: func(*dashboardPanelsV2) bool { return true }})
	for _, tc := range []struct {
		schema, features string
		support          bool
	}{
		{"2", " \ttest_feature\t,unknown,,test_feature", true},
		{"2", "TEST_FEATURE", false},
		{"2", "\u00a0test_feature", false},
		{"2", "test_feature\u2003", false},
		{"1", "test_feature", false},
	} {
		c, err := ParseDashboardClient(&tc.schema, tc.features)
		if err != nil || c.Supports("test_feature") != tc.support {
			t.Fatalf("%+v: %+v %v", tc, c, err)
		}
		want := 0
		if tc.support {
			want = 1
		}
		if len(c.Features) != want {
			t.Fatalf("features: %v", c.Features)
		}
	}
}

func TestDashboardResultFrames(t *testing.T) {
	panel := strings.Replace(v2TablePanel, `"viz":{"type":"table","visible_columns":["a","b"]}`, `"viz":{"type":"stat","value":"n","order_by":"day","reduce":"last","columns":{"n":{"kind":"number","confidence":"confirmed"}},"format":{"scale":1e-7,"decimals":2,"suffix":" Cr"}}`, 1)
	raw := v2Blob(v2Layout, panel)
	canonical, err := CanonicalDashboardPanels(raw)
	if err != nil {
		t.Fatal(err)
	}
	features, err := DashboardFeatures(canonical)
	if err != nil || !slices.Equal(features, []string{"formats", "result_frames"}) {
		t.Fatalf("features %v: %v", features, err)
	}
	for _, replacement := range [][2]string{{`"last"`, `"median"`}, {`"confirmed"`, `"guessed"`}, {`"decimals":2`, `"decimals":11`}, {`"scale":1e-7`, `"scale":0`}, {`"suffix":" Cr"`, `"suffix":"12345678901234567"`}, {`"kind":"number"`, `"kind":"numeric"`}, {`"value":"n"`, `"value":""`}} {
		if err := ValidateDashboardPanels(json.RawMessage(strings.Replace(string(raw), replacement[0], replacement[1], 1))); err == nil {
			t.Errorf("accepted %v", replacement)
		}
	}
	client, err := ParseDashboardClient(new("2"), "result_frames,formats")
	if err != nil || !client.Supports("result_frames") || !client.Supports("formats") {
		t.Fatalf("client %v: %v", client, err)
	}
}

func TestDashboardEmptyColumnKindsKeepFeature(t *testing.T) {
	raw := v2Blob(v2Layout, strings.Replace(v2TablePanel, `"visible_columns":["a","b"]`, `"columns":{}`, 1))
	canonical, err := CanonicalDashboardPanels(raw)
	if err != nil {
		t.Fatal(err)
	}
	features, err := DashboardFeatures(canonical)
	if err != nil || !slices.Equal(features, []string{"result_frames"}) {
		t.Fatalf("features %v: %v; blob %s", features, err, canonical)
	}
	if !slices.Equal(features, DashboardPanelsStoredFeatures(canonical)) {
		t.Fatalf("stored features disagree with canonical blob %s", canonical)
	}
}

func TestDashboardResultFrameFeatureTriggers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		panel    string
		features []string
	}{
		{"value only", strings.Replace(v2TablePanel, `"viz":{"type":"table","visible_columns":["a","b"]}`, `"viz":{"type":"stat","value":"n"}`, 1), []string{"result_frames"}},
		{"order", strings.Replace(v2TablePanel, `"viz":{"type":"table","visible_columns":["a","b"]}`, `"viz":{"type":"stat","value":"n","order_by":"day"}`, 1), []string{"result_frames"}},
		{"columns only", strings.Replace(v2TablePanel, `"visible_columns":["a","b"]`, `"columns":{"a":{"kind":"number","confidence":"confirmed"}}`, 1), []string{"result_frames"}},
		{"format only", strings.Replace(v2StatPanel, `"reduce":"sum"`, `"reduce":"sum","format":{"decimals":2}`, 1), []string{"formats"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			canonical, err := CanonicalDashboardPanels(v2Blob(v2Layout, tc.panel))
			if err != nil {
				t.Fatal(err)
			}
			features, err := DashboardFeatures(canonical)
			if err != nil || !slices.Equal(features, tc.features) {
				t.Fatalf("features %v: %v; blob %s", features, err, canonical)
			}
			again, err := CanonicalDashboardPanels(canonical)
			if err != nil || !bytes.Equal(canonical, again) {
				t.Fatalf("canonical bytes changed: %s -> %s; %v", canonical, again, err)
			}
		})
	}
	for _, reduce := range []string{"first", "last", "min", "max", "avg"} {
		t.Run(reduce, func(t *testing.T) {
			panel := strings.Replace(v2StatPanel, `"reduce":"sum"`, `"reduce":"`+reduce+`"`, 1)
			canonical, err := CanonicalDashboardPanels(v2Blob(v2Layout, panel))
			if err != nil {
				t.Fatal(err)
			}
			features, err := DashboardFeatures(canonical)
			if err != nil || !slices.Equal(features, []string{"result_frames"}) {
				t.Fatalf("features %v: %v; blob %s", features, err, canonical)
			}
		})
	}
}

func TestDashboardResultCharts(t *testing.T) {
	for _, viz := range []string{
		`{"type":"chart","x":"day","y":["a","b","c"],"series":"service","x_scale":"category","orientation":"horizontal","show_values":true,"legend":false,"mark":"bar","stack":"stacked"}`,
		`{"type":"pie","label":"service","value":"n","min_slice_percent":1,"donut":true}`,
		`{"type":"toplist","label":"service","value":"n","max_rows":20}`,
	} {
		viz = strings.TrimSuffix(viz, "}") + `,"format":{"decimals":3}}`
		panel := strings.Replace(v2TablePanel, `{"type":"table","visible_columns":["a","b"]}`, viz, 1)
		raw := v2Blob(v2Layout, panel)
		canonical, err := CanonicalDashboardPanels(raw)
		if err != nil {
			t.Fatal(err)
		}
		features, err := DashboardFeatures(canonical)
		if err != nil || !slices.Contains(features, "result_charts") {
			t.Fatalf("features %v: %v", features, err)
		}
		for _, future := range []string{`"thresholds":[]`, `"rules":[]`, `"combine":"sum"`} {
			bad := strings.Replace(string(raw), `"type":`, future+`,"type":`, 1)
			if err := ValidateDashboardPanels(json.RawMessage(bad)); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Errorf("expected unknown field for %s, got %v", future, err)
			}
		}
	}
	why := []string{"needs viz.x", "needs viz.x", "repeats a viz.y", "invalid role name", "viz.x_scale", "viz.orientation", "needs viz.label", "viz.min_slice_percent", "viz.min_slice_percent", "viz.max_rows", "role its visualization does not use", "role its visualization does not use"}
	for i, viz := range []string{
		`{"type":"chart","x":"day","y":[]}`, `{"type":"chart","y":["a"]}`,
		`{"type":"chart","x":"day","y":["a","a"]}`, `{"type":"chart","x":"day","y":[""]}`,
		`{"type":"chart","x":"day","y":["a"],"x_scale":"log"}`, `{"type":"chart","x":"day","y":["a"],"orientation":"diagonal"}`,
		`{"type":"pie","value":"n"}`, `{"type":"pie","label":"l","value":"n","min_slice_percent":0}`,
		`{"type":"pie","label":"l","value":"n","min_slice_percent":100}`, `{"type":"toplist","label":"l","value":"n","max_rows":0}`,
		`{"type":"stat","value":"n","x":"day"}`, `{"type":"table","label":"day"}`,
	} {
		raw := v2Blob(v2Layout, strings.Replace(v2TablePanel, `{"type":"table","visible_columns":["a","b"]}`, viz, 1))
		if err := ValidateDashboardPanels(raw); err == nil || !strings.Contains(err.Error(), why[i]) {
			t.Errorf("expected %s for %s, got %v", why[i], viz, err)
		}
	}
	for _, role := range []string{`"x":"time"`, `"y":[]`, `"series":"series"`, `"label":"series"`, `"value":"count"`} {
		raw := strings.Replace(v2ChartPanel, `"mark":"bar"`, role+`,"mark":"bar"`, 1)
		if err := ValidateDashboardPanels(v2Blob(v2Layout, raw)); err == nil || !strings.Contains(err.Error(), "result roles or columns in count mode") {
			t.Errorf("expected count role rejection for %s, got %v", role, err)
		}
	}
}

func TestDashboardCountChartFormats(t *testing.T) {
	for _, viz := range []string{
		`{"type":"chart","mark":"bar","show_values":true,"legend":false,"orientation":"horizontal","x_scale":"category"}`,
		`{"type":"chart","format":{"decimals":3}}`,
		`{"type":"pie","format":{"decimals":3}}`,
		`{"type":"toplist","format":{"decimals":3}}`,
	} {
		raw := v2Blob(v2Layout, strings.Replace(v2ChartPanel, `{"type":"chart","mark":"bar","stack":"stacked"}`, viz, 1))
		canonical, err := CanonicalDashboardPanels(raw)
		if err != nil {
			t.Fatalf("%s: %v", viz, err)
		}
		features, err := DashboardFeatures(canonical)
		if err != nil || !slices.Contains(features, "result_charts") {
			t.Fatalf("%s features %v: %v", viz, features, err)
		}
	}
}

func TestDashboardStatCompare(t *testing.T) {
	for _, options := range []string{`"compare":{"mode":"none"}`, `"compare":{"mode":"previous_row","good":"down"}`, `"sparkline":true`, `"sparkline":false`} {
		raw := v2Blob(v2Layout, strings.Replace(v2TablePanel, `{"type":"table","visible_columns":["a","b"]}`, `{"type":"stat","value":"n","order_by":"day","reduce":"last",`+options+`}`, 1))
		canonical, err := CanonicalDashboardPanels(raw)
		if err != nil {
			t.Fatal(err)
		}
		features, err := DashboardFeatures(canonical)
		if err != nil || !slices.Contains(features, "stat_compare") {
			t.Fatalf("features %v: %v", features, err)
		}
	}
	for _, viz := range []string{
		`{"type":"stat","value":"n","compare":{"mode":"previous_period"}}`,
		`{"type":"stat","value":"n","compare":{"mode":"none","good":"bad"}}`,
		`{"type":"stat","value":"n","compare":{"mode":"previous_row"}}`,
		`{"type":"stat","value":"n","order_by":"day","reduce":"sum","compare":{"mode":"previous_row"}}`,
		`{"type":"stat","value":"n","sparkline":true}`,
		`{"type":"table","sparkline":false}`,
		`{"type":"stat","value":"n","compare":{"mode":"none","extra":1}}`,
	} {
		raw := v2Blob(v2Layout, strings.Replace(v2TablePanel, `{"type":"table","visible_columns":["a","b"]}`, viz, 1))
		if err := ValidateDashboardPanels(raw); err == nil {
			t.Errorf("accepted %s", viz)
		}
	}
}
