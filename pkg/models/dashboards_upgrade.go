package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// The upgrade from schema 1 (RFC 0003 "Upgrade from schema 1"). The server
// upgrades a blob when it reads it and never rewrites a stored blob because
// someone read it. Every default that schema 1 applied at run time is written
// down, so an upgraded panel never picks up a schema 2 default that differs.
//
// Strings move as the raw JSON tokens that were read and are never encoded
// again, so the upgraded blob stays within DashboardUpgradeBlobOverhead plus
// DashboardUpgradePanelOverhead per panel of its input. These raw-string
// twins of the schema 2 structs exist for that reason only.
const (
	DashboardUpgradeBlobOverhead  = 64
	DashboardUpgradePanelOverhead = 128
)

// rawStringV1 retains a string token while matching encoding/json's scalar
// semantics: null leaves an earlier string value intact.
type rawStringV1 json.RawMessage

func (s *rawStringV1) UnmarshalJSON(raw []byte) error {
	if !bytes.Equal(raw, jsonNull) {
		*s = append((*s)[:0], raw...)
	}
	return nil
}

type rawDashboardPanelsV1 struct {
	Version          int             `json:"version"`
	Layout           []rawLayoutV1   `json:"layout"`
	Panels           []rawPanelV1    `json:"panels"`
	CacheTTLSeconds  *int            `json:"cache_ttl_seconds"`
	DefaultTimeRange json.RawMessage `json:"default_time_range"`
}

type rawLayoutV1 struct {
	ID rawStringV1 `json:"id"`
	X  int         `json:"x"`
	Y  int         `json:"y"`
	W  int         `json:"w"`
	H  int         `json:"h"`
}

type rawPanelV1 struct {
	ID            rawStringV1     `json:"id"`
	Title         rawStringV1     `json:"title"`
	Type          string          `json:"type"`
	TeamID        int             `json:"team_id"`
	SourceID      int             `json:"source_id"`
	Query         rawStringV1     `json:"query"`
	QueryLanguage QueryLanguage   `json:"query_language"`
	Options       json.RawMessage `json:"options"`
}

type rawOptionsV1 struct {
	Chart         *string       `json:"chart"`
	Limit         *int          `json:"limit"`
	Columns       []rawStringV1 `json:"columns"`
	GroupBy       *rawStringV1  `json:"group_by"`
	BarMode       *string       `json:"bar_mode"`
	BreakdownView *string       `json:"breakdown_view"`
}

type rawDashboardPanelsV2 struct {
	Version          int             `json:"version"`
	Features         []string        `json:"features"`
	CacheTTLSeconds  *int            `json:"cache_ttl_seconds,omitempty"`
	DefaultTimeRange json.RawMessage `json:"default_time_range,omitempty"`
	Tabs             []rawTabV2      `json:"tabs"`
	Panels           []rawPanelV2    `json:"panels"`
}

type rawTabV2 struct {
	ID     string        `json:"id"`
	Title  string        `json:"title"`
	Layout []rawLayoutV2 `json:"layout"`
}

type rawLayoutV2 struct {
	ID json.RawMessage `json:"id"`
	X  int             `json:"x"`
	Y  int             `json:"y"`
	W  int             `json:"w"`
	H  int             `json:"h"`
}

type rawPanelV2 struct {
	ID       json.RawMessage    `json:"id"`
	Kind     DashboardPanelKind `json:"kind"`
	Title    json.RawMessage    `json:"title"`
	TeamID   int                `json:"team_id"`
	SourceID int                `json:"source_id"`
	Query    rawQueryV2         `json:"query"`
	Viz      rawVizV2           `json:"viz"`
}

type rawQueryV2 struct {
	Mode           DashboardQueryMode `json:"mode"`
	Language       QueryLanguage      `json:"language"`
	Text           json.RawMessage    `json:"text"`
	GroupBy        json.RawMessage    `json:"group_by,omitempty"`
	Interval       string             `json:"interval,omitempty"`
	Limit          *int               `json:"limit,omitempty"`
	TranslateLimit *int               `json:"translate_limit,omitempty"`
}

type rawVizV2 struct {
	Type           DashboardVizType    `json:"type"`
	Mark           DashboardChartMark  `json:"mark,omitempty"`
	Stack          DashboardChartStack `json:"stack,omitempty"`
	Reduce         DashboardStatReduce `json:"reduce,omitempty"`
	Donut          *bool               `json:"donut,omitempty"`
	VisibleColumns []json.RawMessage   `json:"visible_columns,omitempty"`
}

var jsonNull = []byte("null")

// rawStringOrEmpty returns the raw string token, or `""` for an absent or null value.
func rawStringOrEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || bytes.Equal(raw, jsonNull) {
		return json.RawMessage(`""`)
	}
	return raw
}

// rawStringOrNone returns the raw string token, or nil for an absent or null value.
func rawStringOrNone(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || bytes.Equal(raw, jsonNull) {
		return nil
	}
	return raw
}

// decodeRawString reads the value of a raw string token.
func decodeRawString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// UpgradeDashboardPanels returns a schema 2 blob unchanged and converts a
// schema 1 blob to schema 2. The same input always gives the same bytes, and
// an upgraded blob has an empty features list. A blob that the schema 1
// validator rejects fails to upgrade.
func UpgradeDashboardPanels(raw json.RawMessage) (json.RawMessage, error) {
	schema, err := DashboardPanelsSchema(raw)
	if err != nil {
		return nil, err
	}
	switch schema {
	case DashboardPanelsVersion2:
		return raw, nil
	case DashboardPanelsVersion:
	default:
		return nil, fmt.Errorf("unsupported panels version %d, expected %d or %d", schema, DashboardPanelsVersion, DashboardPanelsVersion2)
	}
	if err := validateDashboardPanelsV1(raw); err != nil {
		return nil, err
	}
	var in rawDashboardPanelsV1
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("panels payload is not valid JSON: %w", err)
	}

	layout := make([]rawLayoutV2, len(in.Layout))
	for i, l := range in.Layout {
		layout[i] = rawLayoutV2{ID: json.RawMessage(l.ID), X: l.X, Y: l.Y, W: l.W, H: l.H}
	}
	panels := make([]rawPanelV2, len(in.Panels))
	for i := range in.Panels {
		p, err := upgradeDashboardPanelV1(&in.Panels[i])
		if err != nil {
			return nil, err
		}
		panels[i] = p
	}
	out := rawDashboardPanelsV2{
		Version:          DashboardPanelsVersion2,
		Features:         []string{},
		CacheTTLSeconds:  in.CacheTTLSeconds,
		DefaultTimeRange: rawStringOrNone(in.DefaultTimeRange),
		Tabs:             []rawTabV2{{ID: DashboardMainTabID, Title: DashboardMainTabTitle, Layout: layout}},
		Panels:           panels,
	}
	return encodeJSONNoEscape(out)
}

func upgradeDashboardPanelV1(p *rawPanelV1) (rawPanelV2, error) {
	var opts rawOptionsV1
	if len(p.Options) > 0 && !bytes.Equal(p.Options, jsonNull) {
		if err := json.Unmarshal(p.Options, &opts); err != nil {
			return rawPanelV2{}, fmt.Errorf("panel %s has invalid options: %w", p.ID, err)
		}
	}
	language := NormalizeQueryLanguage(p.QueryLanguage)
	out := rawPanelV2{
		ID:       json.RawMessage(p.ID),
		Kind:     DashboardPanelKindQuery,
		Title:    rawStringOrEmpty(json.RawMessage(p.Title)),
		TeamID:   p.TeamID,
		SourceID: p.SourceID,
		Query:    rawQueryV2{Mode: DashboardQueryModeCount, Language: language, Text: rawStringOrEmpty(json.RawMessage(p.Query)), Interval: DashboardQueryIntervalAuto},
	}
	switch DashboardPanelType(p.Type) {
	case DashboardPanelTimeseries:
		if groupBy := rawOptionalStringV1(opts.GroupBy); groupBy != nil && strings.TrimSpace(decodeRawString(groupBy)) != "" {
			out.Query.GroupBy = groupBy
		}
		out.Viz = upgradeChartViz(opts.Chart, opts.BarMode)
	case DashboardPanelStat:
		out.Viz = rawVizV2{Type: DashboardVizStat, Reduce: DashboardStatReduceSum}
	case DashboardPanelBreakdown:
		out.Query.GroupBy = rawOptionalStringV1(opts.GroupBy)
		if opts.BreakdownView != nil && *opts.BreakdownView == "donut" {
			donut := true
			out.Viz = rawVizV2{Type: DashboardVizPie, Donut: &donut}
		} else {
			out.Viz = rawVizV2{Type: DashboardVizToplist}
		}
	case DashboardPanelTable:
		limit := DashboardUpgradedTableLimit
		if opts.Limit != nil {
			limit = min(max(*opts.Limit, 1), MaxDashboardUpgradedTableLimit)
		}
		out.Query = rawQueryV2{Mode: DashboardQueryModeResult, Language: language, Text: out.Query.Text, Limit: &limit}
		out.Viz = rawVizV2{Type: DashboardVizTable, VisibleColumns: upgradeVisibleColumns(opts.Columns)}
	default:
		return rawPanelV2{}, fmt.Errorf("panel %s has invalid type %q", p.ID, p.Type)
	}
	// A count panel's options.limit was sent as the translate request's limit,
	// and only LogchefQL translates.
	if out.Query.Mode == DashboardQueryModeCount && language == QueryLanguageLogchefQL && opts.Limit != nil {
		out.Query.TranslateLimit = opts.Limit
	}
	return out, nil
}

// upgradeChartViz maps a schema 1 chart style and bar mode to a mark and a
// stack. A chart with no value draws a line; bars stack unless grouped; an
// area always stacks.
func upgradeChartViz(chart, barMode *string) rawVizV2 {
	viz := rawVizV2{Type: DashboardVizChart, Mark: DashboardChartMarkLine, Stack: DashboardChartStackNone}
	if chart == nil {
		return viz
	}
	switch DashboardPanelChart(*chart) {
	case DashboardPanelChartBars:
		viz.Mark, viz.Stack = DashboardChartMarkBar, DashboardChartStackStacked
		if barMode != nil && DashboardPanelBarMode(*barMode) == DashboardPanelBarModeGrouped {
			viz.Stack = DashboardChartStackNone
		}
	case DashboardPanelChartArea:
		viz.Mark, viz.Stack = DashboardChartMarkArea, DashboardChartStackStacked
	case DashboardPanelChartLine:
	}
	return viz
}

// upgradeVisibleColumns drops null and empty entries, keeps the first of each
// duplicate name, and returns nil when no name remains.
func upgradeVisibleColumns(columns []rawStringV1) []json.RawMessage {
	seen := make(map[string]struct{}, len(columns))
	var out []json.RawMessage
	for _, token := range columns {
		raw := rawStringOrNone(json.RawMessage(token))
		if raw == nil {
			continue
		}
		name := decodeRawString(raw)
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func rawOptionalStringV1(raw *rawStringV1) json.RawMessage {
	if raw == nil {
		return nil
	}
	return json.RawMessage(*raw)
}
