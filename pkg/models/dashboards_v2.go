package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// Schema 2 panel blob (RFC 0003 "Panel blob schema 2"). A panel is a query
// plus a visualization, tabs own the layouts, and the server writes the
// features list. The validator accepts exactly what this release renders:
// unknown keys, nulls and enum values from later phases are rejected, so a
// stored key never changes meaning when a later release starts to read it.
const (
	DashboardPanelsVersion2    = 2
	MaxDashboardPanelsSizeV2   = 512 * 1024
	MaxDashboardTabs           = 1 // 10 from phase 9 (tabs feature)
	MaxDashboardTabTitleRunes  = 60
	MaxDashboardQueryLimit     = 10_000
	MaxDashboardTranslateLimit = MaxDashboardPanelOptionLimit
	DashboardMainTabID         = "main"
	DashboardMainTabTitle      = "Main"
	DashboardQueryIntervalAuto = "auto"
	// DashboardUpgradedTableLimit is the row limit a schema 1 table ran with
	// when it set none, materialized by the upgrade.
	DashboardUpgradedTableLimit = 100
	// MaxDashboardUpgradedTableLimit is the clamp a schema 1 table limit got at
	// run time, materialized by the upgrade.
	MaxDashboardUpgradedTableLimit = 1000
)

// Request headers of the client schema contract.
const (
	DashboardSchemaHeader   = "X-Logchef-Dashboard-Schema"
	DashboardFeaturesHeader = "X-Logchef-Dashboard-Features"
)

type DashboardPanelKind string

const DashboardPanelKindQuery DashboardPanelKind = "query"

type DashboardQueryMode string

const (
	DashboardQueryModeCount  DashboardQueryMode = "count"
	DashboardQueryModeResult DashboardQueryMode = "result"
)

type DashboardVizType string

const (
	DashboardVizStat    DashboardVizType = "stat"
	DashboardVizChart   DashboardVizType = "chart"
	DashboardVizPie     DashboardVizType = "pie"
	DashboardVizToplist DashboardVizType = "toplist"
	DashboardVizTable   DashboardVizType = "table"
)

type DashboardChartMark string

const (
	DashboardChartMarkLine DashboardChartMark = "line"
	DashboardChartMarkArea DashboardChartMark = "area"
	DashboardChartMarkBar  DashboardChartMark = "bar"
)

type DashboardChartStack string

const (
	DashboardChartStackNone    DashboardChartStack = "none"
	DashboardChartStackStacked DashboardChartStack = "stacked"
)

type DashboardStatReduce string

const (
	DashboardStatReduceSum   DashboardStatReduce = "sum"
	DashboardStatReduceLast  DashboardStatReduce = "last"
	DashboardStatReduceFirst DashboardStatReduce = "first"
	DashboardStatReduceAvg   DashboardStatReduce = "avg"
	DashboardStatReduceMin   DashboardStatReduce = "min"
	DashboardStatReduceMax   DashboardStatReduce = "max"
)

var (
	validDashboardPanelKinds        = map[DashboardPanelKind]struct{}{DashboardPanelKindQuery: {}}
	validDashboardQueryModes        = map[DashboardQueryMode]struct{}{DashboardQueryModeCount: {}, DashboardQueryModeResult: {}}
	validDashboardQueryLanguages    = map[QueryLanguage]struct{}{QueryLanguageLogchefQL: {}, QueryLanguageClickHouseSQL: {}, QueryLanguageLogsQL: {}}
	validDashboardQueryIntervals    = map[string]struct{}{DashboardQueryIntervalAuto: {}}
	validDashboardVizTypes          = map[DashboardVizType]struct{}{DashboardVizStat: {}, DashboardVizChart: {}, DashboardVizPie: {}, DashboardVizToplist: {}, DashboardVizTable: {}}
	validDashboardChartMarks        = map[DashboardChartMark]struct{}{DashboardChartMarkLine: {}, DashboardChartMarkArea: {}, DashboardChartMarkBar: {}}
	validDashboardChartStacks       = map[DashboardChartStack]struct{}{DashboardChartStackNone: {}, DashboardChartStackStacked: {}}
	validDashboardStatReduces       = map[DashboardStatReduce]struct{}{DashboardStatReduceSum: {}, DashboardStatReduceLast: {}, DashboardStatReduceFirst: {}, DashboardStatReduceAvg: {}, DashboardStatReduceMin: {}, DashboardStatReduceMax: {}}
	validDashboardColumnKinds       = map[string]struct{}{"time": {}, "date": {}, "number": {}, "category": {}, "bool": {}, "other": {}}
	validDashboardColumnConfidences = map[string]struct{}{"confirmed": {}}
	validDashboardNumberNotations   = map[string]struct{}{"auto": {}, "standard": {}, "compact": {}}
	validDashboardNumberUnits       = map[string]struct{}{"none": {}, "percent": {}, "bytes": {}, "duration_ms": {}, "duration_s": {}}
)

// dashboardPanelsV2 is the schema 2 blob. Optional fields are pointers so
// absent is distinguishable from a zero value; a null is rejected separately.
type dashboardPanelsV2 struct {
	Version          int                `json:"version"`
	Features         []string           `json:"features"`
	CacheTTLSeconds  *int               `json:"cache_ttl_seconds,omitempty"`
	DefaultTimeRange *string            `json:"default_time_range,omitempty"`
	Tabs             []dashboardTabV2   `json:"tabs"`
	Panels           []dashboardPanelV2 `json:"panels"`
}

type dashboardTabV2 struct {
	ID     string                 `json:"id"`
	Title  string                 `json:"title"`
	Layout []dashboardPanelLayout `json:"layout"`
}

type dashboardPanelV2 struct {
	ID       string             `json:"id"`
	Kind     DashboardPanelKind `json:"kind"`
	Title    string             `json:"title"`
	TeamID   int                `json:"team_id,omitempty"`
	SourceID int                `json:"source_id,omitempty"`
	Query    *dashboardQueryV2  `json:"query,omitempty"`
	Viz      *dashboardVizV2    `json:"viz,omitempty"`
	// Locked is response-only: redaction sets it, and a request that carries it
	// is rejected.
	Locked *bool `json:"locked,omitempty"`
}

type dashboardQueryV2 struct {
	Mode           DashboardQueryMode `json:"mode"`
	Language       QueryLanguage      `json:"language"`
	Text           string             `json:"text"`
	GroupBy        *string            `json:"group_by,omitempty"`
	Interval       *string            `json:"interval,omitempty"`
	Limit          *int               `json:"limit,omitempty"`
	TranslateLimit *int               `json:"translate_limit,omitempty"`
}

type dashboardVizV2 struct {
	Value          *string                      `json:"value,omitempty"`
	OrderBy        *string                      `json:"order_by,omitempty"`
	Columns        map[string]dashboardColumnV2 `json:"columns,omitzero"`
	Format         *dashboardNumberFormatV2     `json:"format,omitempty"`
	Type           DashboardVizType             `json:"type"`
	Mark           *DashboardChartMark          `json:"mark,omitempty"`
	Stack          *DashboardChartStack         `json:"stack,omitempty"`
	Reduce         *DashboardStatReduce         `json:"reduce,omitempty"`
	Donut          *bool                        `json:"donut,omitempty"`
	VisibleColumns []string                     `json:"visible_columns,omitempty"`
}

type dashboardColumnV2 struct {
	Kind       string `json:"kind"`
	Confidence string `json:"confidence"`
}

type dashboardNumberFormatV2 struct {
	Notation *string  `json:"notation,omitempty"`
	Decimals *int     `json:"decimals,omitempty"`
	Grouping *bool    `json:"grouping,omitempty"`
	Scale    *float64 `json:"scale,omitempty"`
	Unit     *string  `json:"unit,omitempty"`
	Prefix   *string  `json:"prefix,omitempty"`
	Suffix   *string  `json:"suffix,omitempty"`
}

// DashboardFeature names a part of a schema 2 blob that an older writer would
// drop or misread (RFC 0003 "Client schema contract").
type DashboardFeature string

// dashboardFeatureBase marks a field or enum value of the base schema 2 that
// phase 3 ships, which every schema 2 writer understands.
const dashboardFeatureBase DashboardFeature = "base"

// dashboardFeatureRules compute the features a blob uses. Each phase that
// adds blob state adds its rule here, its field
// and enum entries to the origin tables below, and its name to the frontend's
// DASHBOARD_CLIENT_FEATURES. TestDashboardSchema2FeatureTableComplete keeps the
// three in step.
var dashboardFeatureRules = []struct {
	name DashboardFeature
	uses func(*dashboardPanelsV2) bool
}{
	{"result_frames", func(blob *dashboardPanelsV2) bool {
		for _, p := range blob.Panels {
			v := p.Viz
			if v != nil && ((p.Query != nil && p.Query.Mode == DashboardQueryModeResult && v.Type == DashboardVizStat) || (v.Reduce != nil && *v.Reduce != DashboardStatReduceSum) || v.OrderBy != nil || v.Columns != nil) {
				return true
			}
		}
		return false
	}},
	{"formats", func(blob *dashboardPanelsV2) bool {
		for _, p := range blob.Panels {
			if p.Viz != nil && p.Viz.Format != nil {
				return true
			}
		}
		return false
	}},
}

// dashboardSchema2FieldOrigins maps every JSON path of the schema 2 structs to
// the feature that an older writer would drop, or the base mark.
var dashboardSchema2FieldOrigins = map[string]DashboardFeature{
	"version":                       dashboardFeatureBase,
	"features":                      dashboardFeatureBase,
	"cache_ttl_seconds":             dashboardFeatureBase,
	"default_time_range":            dashboardFeatureBase,
	"tabs":                          dashboardFeatureBase,
	"tabs.id":                       dashboardFeatureBase,
	"tabs.title":                    dashboardFeatureBase,
	"tabs.layout":                   dashboardFeatureBase,
	"tabs.layout.id":                dashboardFeatureBase,
	"tabs.layout.x":                 dashboardFeatureBase,
	"tabs.layout.y":                 dashboardFeatureBase,
	"tabs.layout.w":                 dashboardFeatureBase,
	"tabs.layout.h":                 dashboardFeatureBase,
	"panels":                        dashboardFeatureBase,
	"panels.id":                     dashboardFeatureBase,
	"panels.kind":                   dashboardFeatureBase,
	"panels.title":                  dashboardFeatureBase,
	"panels.team_id":                dashboardFeatureBase,
	"panels.source_id":              dashboardFeatureBase,
	"panels.query":                  dashboardFeatureBase,
	"panels.query.mode":             dashboardFeatureBase,
	"panels.query.language":         dashboardFeatureBase,
	"panels.query.text":             dashboardFeatureBase,
	"panels.query.group_by":         dashboardFeatureBase,
	"panels.query.interval":         dashboardFeatureBase,
	"panels.query.limit":            dashboardFeatureBase,
	"panels.query.translate_limit":  dashboardFeatureBase,
	"panels.viz":                    dashboardFeatureBase,
	"panels.viz.type":               dashboardFeatureBase,
	"panels.viz.mark":               dashboardFeatureBase,
	"panels.viz.stack":              dashboardFeatureBase,
	"panels.viz.reduce":             dashboardFeatureBase,
	"panels.viz.donut":              dashboardFeatureBase,
	"panels.viz.visible_columns":    dashboardFeatureBase,
	"panels.locked":                 dashboardFeatureBase,
	"panels.viz.value":              "result_frames",
	"panels.viz.order_by":           "result_frames",
	"panels.viz.columns":            "result_frames",
	"panels.viz.columns.kind":       "result_frames",
	"panels.viz.columns.confidence": "result_frames",
	"panels.viz.format":             "formats",
	"panels.viz.format.notation":    "formats",
	"panels.viz.format.decimals":    "formats",
	"panels.viz.format.grouping":    "formats",
	"panels.viz.format.scale":       "formats",
	"panels.viz.format.unit":        "formats",
	"panels.viz.format.prefix":      "formats",
	"panels.viz.format.suffix":      "formats",
}

// dashboardSchema2EnumOrigins maps every accepted enum value, by field path, to
// the feature that introduces it or the base mark.
var dashboardSchema2EnumOrigins = map[string]map[string]DashboardFeature{
	"panels.kind":                   {string(DashboardPanelKindQuery): dashboardFeatureBase},
	"panels.query.mode":             {string(DashboardQueryModeCount): dashboardFeatureBase, string(DashboardQueryModeResult): dashboardFeatureBase},
	"panels.query.language":         {string(QueryLanguageLogchefQL): dashboardFeatureBase, string(QueryLanguageClickHouseSQL): dashboardFeatureBase, string(QueryLanguageLogsQL): dashboardFeatureBase},
	"panels.query.interval":         {DashboardQueryIntervalAuto: dashboardFeatureBase},
	"panels.viz.type":               {string(DashboardVizStat): dashboardFeatureBase, string(DashboardVizChart): dashboardFeatureBase, string(DashboardVizPie): dashboardFeatureBase, string(DashboardVizToplist): dashboardFeatureBase, string(DashboardVizTable): dashboardFeatureBase},
	"panels.viz.mark":               {string(DashboardChartMarkLine): dashboardFeatureBase, string(DashboardChartMarkArea): dashboardFeatureBase, string(DashboardChartMarkBar): dashboardFeatureBase},
	"panels.viz.stack":              {string(DashboardChartStackNone): dashboardFeatureBase, string(DashboardChartStackStacked): dashboardFeatureBase},
	"panels.viz.reduce":             {string(DashboardStatReduceSum): dashboardFeatureBase, "first": "result_frames", "last": "result_frames", "avg": "result_frames", "min": "result_frames", "max": "result_frames"},
	"panels.viz.columns.kind":       {"time": "result_frames", "date": "result_frames", "number": "result_frames", "category": "result_frames", "bool": "result_frames", "other": "result_frames"},
	"panels.viz.columns.confidence": {"confirmed": "result_frames"},
	"panels.viz.format.notation":    {"auto": "formats", "standard": "formats", "compact": "formats"},
	"panels.viz.format.unit":        {"none": "formats", "percent": "formats", "bytes": "formats", "duration_ms": "formats", "duration_s": "formats"},
}

// KnownDashboardFeatures lists the features this server computes, in the
// order of the phases that added them.
func KnownDashboardFeatures() []string {
	names := make([]string, 0, len(dashboardFeatureRules))
	for _, rule := range dashboardFeatureRules {
		names = append(names, string(rule.name))
	}
	return names
}

// KnownDashboardFeature reports whether name is a feature this server computes.
func KnownDashboardFeature(name string) bool {
	for _, rule := range dashboardFeatureRules {
		if string(rule.name) == name {
			return true
		}
	}
	return false
}

func dashboardFeaturesOf(blob *dashboardPanelsV2) []string {
	features := []string{}
	for _, rule := range dashboardFeatureRules {
		if rule.uses(blob) {
			features = append(features, string(rule.name))
		}
	}
	sort.Strings(features)
	return features
}

// DashboardFeatures is the pure function over a valid schema 2 blob that
// returns the sorted names of the features the blob uses. An upgraded schema 1
// blob uses none.
func DashboardFeatures(raw json.RawMessage) ([]string, error) {
	blob, err := decodeDashboardPanelsV2(raw)
	if err != nil {
		return nil, err
	}
	return dashboardFeaturesOf(blob), nil
}

// DashboardPanelsStoredFeatures reads the features list of a schema 2 blob
// without validating the rest of it, so the write path can refuse a stale
// client and the read path can detect a feature this server does not know.
// A schema 1 or unreadable blob has no features.
func DashboardPanelsStoredFeatures(raw json.RawMessage) []string {
	var probe struct {
		Version  int      `json:"version"`
		Features []string `json:"features"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil || probe.Version != DashboardPanelsVersion2 {
		return nil
	}
	return probe.Features
}

// DashboardPanelsUnknownFeatures lists the stored features of raw that this
// server does not know. A rolling deployment can store one; such a row is
// served as the stub and refuses writes.
func DashboardPanelsUnknownFeatures(raw json.RawMessage) []string {
	var unknown []string
	for _, name := range DashboardPanelsStoredFeatures(raw) {
		if !KnownDashboardFeature(name) {
			unknown = append(unknown, name)
		}
	}
	return unknown
}

// DashboardClient is what the two request headers say about the caller:
// the newest panel schema it reads, and the features its code supports.
type DashboardClient struct {
	Schema   int
	Features map[DashboardFeature]struct{}
}

// Supports reports whether the client declared the feature.
func (c DashboardClient) Supports(name string) bool {
	_, ok := c.Features[DashboardFeature(name)]
	return ok
}

// ErrDashboardSchemaHeader is the 400 for a schema header that is not 1 or 2.
var ErrDashboardSchemaHeader = errors.New(DashboardSchemaHeader + " must be 1 or 2")

// ParseDashboardClient parses the schema header (nil when the request has none)
// and the features header. An absent schema header or "1" is a schema 1
// client, "2" a schema 2 client, and every other value, the empty value
// included, is ErrDashboardSchemaHeader. Features are comma-separated,
// case-sensitive lowercase names; whitespace around a name is trimmed, blank
// entries, duplicates and unknown names are ignored. A schema 1 client
// declares no features.
func ParseDashboardClient(schema *string, features string) (DashboardClient, error) {
	client := DashboardClient{Schema: DashboardPanelsVersion, Features: map[DashboardFeature]struct{}{}}
	if schema != nil {
		switch strings.Trim(*schema, " \t") {
		case "1":
		case "2":
			client.Schema = DashboardPanelsVersion2
		default:
			return DashboardClient{}, ErrDashboardSchemaHeader
		}
	}
	if client.Schema == DashboardPanelsVersion {
		return client, nil
	}
	for entry := range strings.SplitSeq(features, ",") {
		name := strings.Trim(entry, " \t")
		if name == "" || !KnownDashboardFeature(name) {
			continue
		}
		client.Features[DashboardFeature(name)] = struct{}{}
	}
	return client, nil
}

// validateDashboardPanelsV2 holds the schema 2 rules.
func validateDashboardPanelsV2(raw json.RawMessage) error {
	blob, err := decodeDashboardPanelsV2(raw)
	if err != nil {
		return err
	}
	return validateDashboardPanelsV2Blob(blob)
}

// decodeDashboardPanelsV2 decodes a schema 2 blob strictly: an unknown key, a
// null anywhere, or trailing data is an error.
func decodeDashboardPanelsV2(raw json.RawMessage) (*dashboardPanelsV2, error) {
	if len(raw) > MaxDashboardPanelsSizeV2 {
		return nil, fmt.Errorf("panels payload is %d bytes, exceeds the %d byte limit", len(raw), MaxDashboardPanelsSizeV2)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var blob dashboardPanelsV2
	if err := dec.Decode(&blob); err != nil {
		return nil, fmt.Errorf("panels payload is not a valid schema 2 blob: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("panels payload has data after the blob")
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("panels payload is not valid JSON: %w", err)
	}
	if path, found := firstJSONNull(generic, ""); found {
		return nil, fmt.Errorf("panels payload has a null at %s; omit the key instead", path)
	}
	return &blob, nil
}

// firstJSONNull returns the path of the first null in a decoded JSON value, in
// key order so the report is stable.
func firstJSONNull(value any, path string) (string, bool) {
	switch v := value.(type) {
	case nil:
		if path == "" {
			return "the top level", true
		}
		return path, true
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := k
			if path != "" {
				child = path + "." + k
			}
			if p, found := firstJSONNull(v[k], child); found {
				return p, true
			}
		}
	case []any:
		for i, item := range v {
			if p, found := firstJSONNull(item, fmt.Sprintf("%s[%d]", path, i)); found {
				return p, true
			}
		}
	}
	return "", false
}

func validateDashboardPanelsV2Blob(blob *dashboardPanelsV2) error {
	if blob.Version != DashboardPanelsVersion2 {
		return fmt.Errorf("unsupported panels version %d, expected %d", blob.Version, DashboardPanelsVersion2)
	}
	if err := validateDashboardSettings(blob.CacheTTLSeconds, blob.DefaultTimeRange); err != nil {
		return err
	}
	layoutIDs, err := validateDashboardTabs(blob.Tabs)
	if err != nil {
		return err
	}
	if blob.Panels == nil {
		return fmt.Errorf("panels must be an array")
	}
	if len(blob.Panels) > MaxDashboardPanels {
		return fmt.Errorf("dashboard has %d panels, exceeds the max of %d", len(blob.Panels), MaxDashboardPanels)
	}
	panelIDs := make(map[string]struct{}, len(blob.Panels))
	for i := range blob.Panels {
		p := &blob.Panels[i]
		if p.ID == "" {
			return fmt.Errorf("panel[%d] has an empty id", i)
		}
		if _, dup := panelIDs[p.ID]; dup {
			return fmt.Errorf("duplicate panel id %q", p.ID)
		}
		panelIDs[p.ID] = struct{}{}
		if err := validateDashboardPanelV2(p); err != nil {
			return err
		}
	}
	return validateDashboardCoverage(panelIDs, layoutIDs)
}

// validateDashboardTabs checks the tabs and returns the set of layout ids
// across all of them.
func validateDashboardTabs(tabs []dashboardTabV2) (map[string]struct{}, error) {
	if len(tabs) == 0 {
		return nil, fmt.Errorf("tabs must hold at least one tab")
	}
	if len(tabs) > MaxDashboardTabs {
		return nil, fmt.Errorf("dashboard has %d tabs, exceeds the max of %d", len(tabs), MaxDashboardTabs)
	}
	tabIDs := make(map[string]struct{}, len(tabs))
	layoutIDs := make(map[string]struct{})
	for i := range tabs {
		tab := &tabs[i]
		if tab.ID == "" {
			return nil, fmt.Errorf("tabs[%d] has an empty id", i)
		}
		if _, dup := tabIDs[tab.ID]; dup {
			return nil, fmt.Errorf("duplicate tab id %q", tab.ID)
		}
		tabIDs[tab.ID] = struct{}{}
		if n := utf8.RuneCountInString(tab.Title); n < 1 || n > MaxDashboardTabTitleRunes {
			return nil, fmt.Errorf("tab %q has a title of %d characters (allowed: 1-%d)", tab.ID, n, MaxDashboardTabTitleRunes)
		}
		if tab.Layout == nil {
			return nil, fmt.Errorf("tab %q layout must be an array", tab.ID)
		}
		ids, err := validateDashboardLayoutEntries(tab.Layout)
		if err != nil {
			return nil, err
		}
		for id := range ids {
			if _, dup := layoutIDs[id]; dup {
				return nil, fmt.Errorf("layout id %q appears in more than one tab", id)
			}
			layoutIDs[id] = struct{}{}
		}
	}
	return layoutIDs, nil
}

func validateDashboardPanelV2(p *dashboardPanelV2) error {
	if _, ok := validDashboardPanelKinds[p.Kind]; !ok {
		return fmt.Errorf("panel %q has invalid kind %q (allowed: query)", p.ID, p.Kind)
	}
	if p.Locked != nil {
		return fmt.Errorf("panel %q carries locked, which only a response may", p.ID)
	}
	if p.TeamID <= 0 {
		return fmt.Errorf("panel %q has invalid team_id %d", p.ID, p.TeamID)
	}
	if p.SourceID <= 0 {
		return fmt.Errorf("panel %q has invalid source_id %d", p.ID, p.SourceID)
	}
	if p.Query == nil {
		return fmt.Errorf("panel %q has no query", p.ID)
	}
	if p.Viz == nil {
		return fmt.Errorf("panel %q has no viz", p.ID)
	}
	if err := validateDashboardQueryV2(p.ID, p.Query); err != nil {
		return err
	}
	return validateDashboardVizV2(p.ID, p.Query, p.Viz)
}

func validateDashboardQueryV2(panelID string, q *dashboardQueryV2) error {
	if _, ok := validDashboardQueryModes[q.Mode]; !ok {
		return fmt.Errorf("panel %q has invalid query.mode %q (allowed: count, result)", panelID, q.Mode)
	}
	if _, ok := validDashboardQueryLanguages[q.Language]; !ok {
		return fmt.Errorf("panel %q has invalid query.language %q (allowed: logchefql, clickhouse-sql, logsql)", panelID, q.Language)
	}
	count := q.Mode == DashboardQueryModeCount
	if q.GroupBy != nil {
		if !count {
			return fmt.Errorf("panel %q has query.group_by, which only count mode uses", panelID)
		}
		if strings.TrimSpace(*q.GroupBy) == "" {
			return fmt.Errorf("panel %q has a blank query.group_by; omit it instead", panelID)
		}
	}
	if q.Interval != nil {
		if !count {
			return fmt.Errorf("panel %q has query.interval, which only count mode uses", panelID)
		}
		if _, ok := validDashboardQueryIntervals[*q.Interval]; !ok {
			return fmt.Errorf("panel %q has invalid query.interval %q (allowed: auto)", panelID, *q.Interval)
		}
	}
	if q.Limit != nil {
		if count {
			return fmt.Errorf("panel %q has query.limit, which only result mode uses", panelID)
		}
		if *q.Limit < 1 || *q.Limit > MaxDashboardQueryLimit {
			return fmt.Errorf("panel %q has invalid query.limit %d (allowed: 1-%d)", panelID, *q.Limit, MaxDashboardQueryLimit)
		}
	}
	if q.TranslateLimit != nil {
		if !count || q.Language != QueryLanguageLogchefQL {
			return fmt.Errorf("panel %q has query.translate_limit, which only count mode with logchefql uses", panelID)
		}
		if *q.TranslateLimit < 1 || *q.TranslateLimit > MaxDashboardTranslateLimit {
			return fmt.Errorf("panel %q has invalid query.translate_limit %d (allowed: 1-%d)", panelID, *q.TranslateLimit, MaxDashboardTranslateLimit)
		}
	}
	return nil
}

func validateDashboardVizV2(panelID string, q *dashboardQueryV2, v *dashboardVizV2) error {
	if _, ok := validDashboardVizTypes[v.Type]; !ok {
		return fmt.Errorf("panel %q has invalid viz.type %q (allowed: stat, chart, pie, toplist, table)", panelID, v.Type)
	}
	table := v.Type == DashboardVizTable
	switch q.Mode {
	case DashboardQueryModeCount:
		if table {
			return fmt.Errorf("panel %q is a table in count mode; a table needs result mode", panelID)
		}
	case DashboardQueryModeResult:
		if !table && v.Type != DashboardVizStat {
			return fmt.Errorf("panel %q is a %s in result mode; only tables and stats run in result mode in this release", panelID, v.Type)
		}
		if v.Type == DashboardVizStat && v.Value == nil {
			return fmt.Errorf("panel %q needs viz.value", panelID)
		}
	}
	if (v.Type == DashboardVizPie || v.Type == DashboardVizToplist) && q.GroupBy == nil {
		return fmt.Errorf("panel %q is a %s and needs query.group_by", panelID, v.Type)
	}
	if q.Mode == DashboardQueryModeCount && (v.Value != nil || v.OrderBy != nil || v.Columns != nil) {
		return fmt.Errorf("panel %q has result roles or columns in count mode", panelID)
	}
	if err := validateDashboardFrameViz(panelID, v); err != nil {
		return err
	}
	return validateDashboardVizOptionsV2(panelID, v)
}

func validateDashboardFrameViz(id string, v *dashboardVizV2) error {
	for _, role := range []*string{v.Value, v.OrderBy} {
		if role == nil {
			continue
		}
		if v.Type != DashboardVizStat {
			return fmt.Errorf("panel %q has a role only a stat uses", id)
		}
		if n := utf8.RuneCountInString(*role); n < 1 || n > 128 {
			return fmt.Errorf("panel %q has an invalid role name", id)
		}
	}
	if len(v.Columns) > 100 {
		return fmt.Errorf("panel %q has more than 100 column kinds", id)
	}
	for name, c := range v.Columns {
		if n := utf8.RuneCountInString(name); n < 1 || n > 128 {
			return fmt.Errorf("panel %q has an invalid column name", id)
		}
		_, validKind := validDashboardColumnKinds[c.Kind]
		_, validConfidence := validDashboardColumnConfidences[c.Confidence]
		if !validKind || !validConfidence {
			return fmt.Errorf("panel %q has an invalid confirmed column kind", id)
		}
	}
	return validateDashboardNumberFormat(id, v)
}

func validateDashboardNumberFormat(id string, v *dashboardVizV2) error {
	f := v.Format
	if f == nil {
		return nil
	}
	if v.Type != DashboardVizStat {
		return fmt.Errorf("panel %q has a format only a stat uses in this release", id)
	}
	if f.Notation != nil {
		if _, ok := validDashboardNumberNotations[*f.Notation]; !ok {
			return fmt.Errorf("panel %q has invalid format.notation", id)
		}
	}
	if f.Unit != nil {
		if _, ok := validDashboardNumberUnits[*f.Unit]; !ok {
			return fmt.Errorf("panel %q has invalid format.unit", id)
		}
	}
	if f.Decimals != nil && (*f.Decimals < 0 || *f.Decimals > 10) {
		return fmt.Errorf("panel %q has invalid format.decimals", id)
	}
	if f.Scale != nil && (*f.Scale == 0 || math.IsNaN(*f.Scale) || math.IsInf(*f.Scale, 0)) {
		return fmt.Errorf("panel %q has invalid format.scale", id)
	}
	for _, affix := range []*string{f.Prefix, f.Suffix} {
		if affix != nil && utf8.RuneCountInString(*affix) > 16 {
			return fmt.Errorf("panel %q has a format affix longer than 16 characters", id)
		}
	}
	return nil
}

// validateDashboardVizOptionsV2 checks that each option sits on the one
// visualization that uses it and holds an accepted value.
func validateDashboardVizOptionsV2(panelID string, v *dashboardVizV2) error {
	if v.Mark != nil {
		if v.Type != DashboardVizChart {
			return fmt.Errorf("panel %q has viz.mark, which only a chart uses", panelID)
		}
		if _, ok := validDashboardChartMarks[*v.Mark]; !ok {
			return fmt.Errorf("panel %q has invalid viz.mark %q (allowed: line, area, bar)", panelID, *v.Mark)
		}
	}
	if v.Stack != nil {
		if v.Type != DashboardVizChart {
			return fmt.Errorf("panel %q has viz.stack, which only a chart uses", panelID)
		}
		if _, ok := validDashboardChartStacks[*v.Stack]; !ok {
			return fmt.Errorf("panel %q has invalid viz.stack %q (allowed: none, stacked)", panelID, *v.Stack)
		}
	}
	if v.Reduce != nil {
		if v.Type != DashboardVizStat {
			return fmt.Errorf("panel %q has viz.reduce, which only a stat uses", panelID)
		}
		if _, ok := validDashboardStatReduces[*v.Reduce]; !ok {
			return fmt.Errorf("panel %q has invalid viz.reduce %q (allowed: last, first, sum, avg, min, max)", panelID, *v.Reduce)
		}
	}
	if v.Donut != nil && v.Type != DashboardVizPie {
		return fmt.Errorf("panel %q has viz.donut, which only a pie uses", panelID)
	}
	if v.VisibleColumns != nil {
		if v.Type != DashboardVizTable {
			return fmt.Errorf("panel %q has viz.visible_columns, which only a table uses", panelID)
		}
		if len(v.VisibleColumns) == 0 {
			return fmt.Errorf("panel %q has an empty viz.visible_columns; omit it to show every column", panelID)
		}
		seen := make(map[string]struct{}, len(v.VisibleColumns))
		for _, name := range v.VisibleColumns {
			if name == "" {
				return fmt.Errorf("panel %q has an empty name in viz.visible_columns", panelID)
			}
			if _, dup := seen[name]; dup {
				return fmt.Errorf("panel %q lists column %q twice in viz.visible_columns", panelID, name)
			}
			seen[name] = struct{}{}
		}
	}
	return nil
}

// CanonicalDashboardPanels validates a request blob and returns the bytes to
// store: a schema 1 blob verbatim, or a schema 2 blob re-encoded with the
// features list that the server computed, so a client cannot understate what
// the blob uses.
func CanonicalDashboardPanels(raw json.RawMessage) (json.RawMessage, error) {
	if err := ValidateDashboardPanels(raw); err != nil {
		return nil, err
	}
	if schema, _ := DashboardPanelsSchema(raw); schema != DashboardPanelsVersion2 {
		return raw, nil
	}
	return canonicalDashboardPanelsV2(raw)
}

func canonicalDashboardPanelsV2(raw json.RawMessage) (json.RawMessage, error) {
	blob, err := decodeDashboardPanelsV2(raw)
	if err != nil {
		return nil, err
	}
	if err := validateDashboardPanelsV2Blob(blob); err != nil {
		return nil, err
	}
	blob.Features = dashboardFeaturesOf(blob)
	canonical, err := encodeJSONNoEscape(blob)
	if err != nil {
		return nil, err
	}
	if len(canonical) > MaxDashboardPanelsSizeV2 {
		return nil, fmt.Errorf("panels request is %d bytes; its stored form would be %d bytes after encoding, over the %d-byte limit", len(raw), len(canonical), MaxDashboardPanelsSizeV2)
	}
	if err := validateDashboardPanelsV2(canonical); err != nil {
		return nil, err
	}
	return canonical, nil
}

// redactDashboardPanelsV2 keeps id, kind, title, team_id, source_id and
// viz.type of a locked panel, drops its query and the rest of viz, and sets
// locked. Tab layouts stay.
func redactDashboardPanelsV2(raw json.RawMessage, lockedIDs map[string]struct{}) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var blob dashboardPanelsV2
	if err := dec.Decode(&blob); err != nil {
		return nil, fmt.Errorf("panels payload is not valid JSON: %w", err)
	}
	locked := true
	for i := range blob.Panels {
		p := &blob.Panels[i]
		if _, ok := lockedIDs[p.ID]; !ok {
			continue
		}
		p.Query = nil
		if p.Viz != nil {
			p.Viz = &dashboardVizV2{Type: p.Viz.Type}
		}
		p.Locked = &locked
	}
	if blob.Features == nil {
		blob.Features = []string{}
	}
	return encodeJSONNoEscape(blob)
}

// encodeJSONNoEscape marshals v without HTML escaping, so `<`, `>` and `&` in
// query text keep their bytes.
func encodeJSONNoEscape(v any) (json.RawMessage, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("failed to encode panels: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// sortedDistinct returns the distinct values of items in sorted order, never nil.
func sortedDistinct[T interface{ ~int | ~string }](items []T) []T {
	out := make([]T, 0, len(items))
	for _, item := range items {
		if !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	slices.Sort(out)
	return out
}
