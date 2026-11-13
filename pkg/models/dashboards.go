package models

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Dashboard is a stored dashboard row. The panel layout, per-panel query and
// visualization live in one versioned JSON blob (PanelsJSON), validated by
// ValidateDashboardPanels before persistence. Responses are built from it by
// core.BuildDashboardResponse, never by marshaling it directly.
type Dashboard struct {
	ID          int             `db:"id"`
	Name        string          `db:"name"`
	Description string          `db:"description"`
	PanelsJSON  json.RawMessage `db:"panels_json"`
	CreatedBy   *UserID         `db:"created_by"`
	// Revision counts saves, from 1. Every update is a compare-and-swap on the
	// revision the client loaded, so two saves from one revision cannot both win.
	Revision int `db:"revision"`
	Timestamps
	// CreatedByName / CreatedByEmail identify the dashboard's creator for
	// display; empty for dashboards whose author was deleted (created_by NULL).
	CreatedByName  string `db:"-"`
	CreatedByEmail string `db:"-"`
	// Summary holds the derived list columns. Every write of PanelsJSON writes
	// them in the same statement, so blob and summary cannot disagree.
	Summary DashboardPanelsSummary `db:"-"`
}

// EmptyDashboardPanelsJSON is the empty schema 1 blob. It is the `panels` value
// of the stub that schema 1 clients get for a schema 2 row.
func EmptyDashboardPanelsJSON() json.RawMessage {
	return json.RawMessage(`{"version":1,"layout":[],"panels":[]}`)
}

// Dashboard name and description bounds, in Unicode code points. Rows from
// before the bound can be longer; the list truncates them and GET returns
// them whole.
const (
	MaxDashboardNameRunes        = 200
	MaxDashboardDescriptionRunes = 2000
)

// CreateDashboardRequest is the body for creating a dashboard.
type CreateDashboardRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Panels      json.RawMessage `json:"panels"`
}

// UpdateDashboardRequest is the body for replacing a dashboard's mutable fields.
type UpdateDashboardRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Panels      json.RawMessage `json:"panels"`
	// Revision is the dashboard revision the client loaded. It is required: a
	// body without it is refused, and a revision that no longer matches the
	// stored row is a conflict. The server ignores updated_at in the body.
	Revision *int `json:"revision"`
}

// Schema 1 panel-blob limits and allowed values, per the approved #56 design.
// Schema 1 validation does not change; schema 2 lives in dashboards_v2.go.
const (
	DashboardPanelsVersion = 1
	MaxDashboardPanels     = 24
	MaxDashboardPanelsSize = 100 * 1024 // 100 KB, raw JSON.
	MinDashboardPanelH     = 1
	MaxDashboardPanelH     = 6
	// DashboardGridColumns is the fixed width of the layout grid; a panel's
	// x+w must not exceed it (finding B3).
	DashboardGridColumns = 12
	// MaxDashboardPanelOptionLimit caps a panel option's row `limit`. It is
	// intentionally generous — the check exists to reject clearly-wrong values
	// (negative, zero, absurd) rather than to enforce a UI-render budget
	// (finding B8).
	MaxDashboardPanelOptionLimit = 100_000
	// MaxDashboardCacheTTLSeconds is the sanity bound on a dashboard's
	// cache_ttl_seconds blob field (1 day). The server also clamps to
	// dashboard_cache.max_ttl at request time; this is just a stored-value bound.
	MaxDashboardCacheTTLSeconds = 86_400
)

// DashboardPanelChart enumerates the chart styles a panel's options may request.
type DashboardPanelChart string

const (
	DashboardPanelChartBars DashboardPanelChart = "bars"
	DashboardPanelChartLine DashboardPanelChart = "line"
	DashboardPanelChartArea DashboardPanelChart = "area"
)

var validDashboardPanelCharts = map[DashboardPanelChart]struct{}{
	DashboardPanelChartBars: {},
	DashboardPanelChartLine: {},
	DashboardPanelChartArea: {},
}

// DashboardPanelBarMode specifies the bar render mode when options.chart is "bars".
type DashboardPanelBarMode string

const (
	DashboardPanelBarModeStacked DashboardPanelBarMode = "stacked"
	DashboardPanelBarModeGrouped DashboardPanelBarMode = "grouped"
)

var validDashboardPanelBarModes = map[DashboardPanelBarMode]struct{}{
	DashboardPanelBarModeStacked: {},
	DashboardPanelBarModeGrouped: {},
}

// DashboardPanelType enumerates the chart kinds a panel may render.
type DashboardPanelType string

const (
	DashboardPanelTimeseries DashboardPanelType = "timeseries"
	DashboardPanelStat       DashboardPanelType = "stat"
	DashboardPanelTable      DashboardPanelType = "table"
	DashboardPanelBreakdown  DashboardPanelType = "breakdown"
)

var validDashboardPanelTypes = map[DashboardPanelType]struct{}{
	DashboardPanelTimeseries: {},
	DashboardPanelStat:       {},
	DashboardPanelTable:      {},
	DashboardPanelBreakdown:  {},
}

// validDashboardPanelWidths is the set of allowed grid column spans (12-col grid).
var validDashboardPanelWidths = map[int]struct{}{
	2:  {},
	3:  {},
	4:  {},
	6:  {},
	8:  {},
	9:  {},
	12: {},
}

// dashboardPanelLayout is one entry in the grid layout: which panel (by id) sits
// where, and how large it is.
type dashboardPanelLayout struct {
	ID string `json:"id"`
	X  int    `json:"x"`
	Y  int    `json:"y"`
	W  int    `json:"w"`
	H  int    `json:"h"`
}

// dashboardPanel is one visualization: its source/team, query, and chart type.
type dashboardPanel struct {
	ID            string          `json:"id"`
	Title         string          `json:"title"`
	Type          string          `json:"type"`
	TeamID        int             `json:"team_id"`
	SourceID      int             `json:"source_id"`
	Query         string          `json:"query"`
	QueryLanguage QueryLanguage   `json:"query_language"`
	Options       json.RawMessage `json:"options,omitempty"`
	// Locked is a transient, response-only flag set by per-panel redaction
	// (RedactDashboardPanels): when true the viewer lacks access to this panel's
	// source, so Query/QueryLanguage/Options have been blanked in the response.
	// It is NEVER persisted — the stored blob is written from the raw request
	// bytes, never re-marshaled from this struct — and omitempty keeps it absent
	// for the common unredacted case.
	Locked bool `json:"locked,omitempty"`
}

// dashboardPanels is the top-level versioned blob stored in dashboards.panels_json.
type dashboardPanels struct {
	Version int                    `json:"version"`
	Layout  []dashboardPanelLayout `json:"layout"`
	Panels  []dashboardPanel       `json:"panels"`
	// CacheTTLSeconds is the dashboard-wide result-cache TTL. Absent => client
	// uses the default (600s); 0 => caching off; >0 => that TTL. The server
	// additionally clamps to dashboard_cache.max_ttl at request time.
	CacheTTLSeconds *int `json:"cache_ttl_seconds,omitempty"`
	// DefaultTimeRange is the relative range the dashboard opens with, in the
	// frontend's relative-time format ("15m", "6h", "7d", "today"). Absent =>
	// the viewer's current range is kept.
	DefaultTimeRange *string `json:"default_time_range,omitempty"`
}

// dashboardTimeRangePattern matches the relative ranges the frontend can parse:
// a count of minutes, hours, days or weeks, or a calendar day.
var dashboardTimeRangePattern = regexp.MustCompile(`^([1-9]\d{0,3}[mhdw]|today|yesterday)$`)

// dashboardPanelOptions is the validated contract for a panel's `options` blob
// (finding B8). Fields are pointers/slices so absent is distinguishable from a
// zero value, and unknown keys are ignored to stay forward-compatible. A field
// present with the wrong JSON shape (e.g. columns as a string) fails to
// unmarshal and is rejected.
type dashboardPanelOptions struct {
	Chart         *string  `json:"chart"`
	Limit         *int     `json:"limit"`
	Columns       []string `json:"columns"`
	GroupBy       *string  `json:"group_by"`
	BarMode       *string  `json:"bar_mode"`
	BreakdownView *string  `json:"breakdown_view"`
}

// DashboardPanelRef identifies the team and source a single panel targets. It
// is used by the authorization and dangling-reference checks in core.
type DashboardPanelRef struct {
	PanelID  string
	TeamID   int
	SourceID int
}

// DashboardPanelRefs extracts the (team_id, source_id) each query panel
// references from a raw panel blob of either schema. It parses leniently, so
// it also works on a blob that fails validation; it returns an error only when
// the blob is not JSON of the expected shape.
func DashboardPanelRefs(raw json.RawMessage) ([]DashboardPanelRef, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var blob struct {
		Panels []struct {
			ID       string             `json:"id"`
			Kind     DashboardPanelKind `json:"kind"`
			TeamID   int                `json:"team_id"`
			SourceID int                `json:"source_id"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(raw, &blob); err != nil {
		return nil, fmt.Errorf("panels payload is not valid JSON: %w", err)
	}
	refs := make([]DashboardPanelRef, 0, len(blob.Panels))
	for i := range blob.Panels {
		p := &blob.Panels[i]
		if p.Kind != "" && p.Kind != DashboardPanelKindQuery {
			continue
		}
		refs = append(refs, DashboardPanelRef{PanelID: p.ID, TeamID: p.TeamID, SourceID: p.SourceID})
	}
	return refs, nil
}

// RedactDashboardPanels returns a fresh COPY of the raw panel blob of either
// schema in which the panels named in lockedIDs have had their query blanked
// and Locked set to true. Id, title, team_id, source_id and the panel's type
// are kept so the frontend can render a "locked" placeholder in place, and
// the layout is left untouched.
//
// The input bytes are never mutated: this is response-shaping only. When there
// is nothing to redact (empty blob or empty lockedIDs), raw is returned verbatim.
func RedactDashboardPanels(raw json.RawMessage, lockedIDs map[string]struct{}) (json.RawMessage, error) {
	if len(raw) == 0 || len(lockedIDs) == 0 {
		return raw, nil
	}
	schema, err := dashboardPanelsSchema(raw)
	if err != nil {
		return nil, err
	}
	if schema == DashboardPanelsVersion2 {
		return redactDashboardPanelsV2(raw, lockedIDs)
	}
	var blob dashboardPanels
	if err := json.Unmarshal(raw, &blob); err != nil {
		return nil, fmt.Errorf("panels payload is not valid JSON: %w", err)
	}
	for i := range blob.Panels {
		if _, ok := lockedIDs[blob.Panels[i].ID]; !ok {
			continue
		}
		blob.Panels[i].Query = ""
		blob.Panels[i].QueryLanguage = ""
		blob.Panels[i].Options = nil
		blob.Panels[i].Locked = true
	}
	out, err := json.Marshal(blob)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal redacted panels: %w", err)
	}
	return out, nil
}

// ValidateDashboardPanels checks a raw panels_json blob and returns a
// descriptive error on the first violation. It is the single entry point for
// panel-blob validity across create, update and read, and dispatches on the
// blob's version: schema 1 rules below, schema 2 rules in dashboards_v2.go.
// A stored blob that fails it is corrupt.
func ValidateDashboardPanels(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("panels payload is required")
	}
	if len(raw) > MaxDashboardPanelsSizeV2 {
		return fmt.Errorf("panels payload is %d bytes, exceeds the %d byte limit", len(raw), MaxDashboardPanelsSizeV2)
	}
	schema, err := dashboardPanelsSchema(raw)
	if err != nil {
		return err
	}
	switch schema {
	case DashboardPanelsVersion:
		return validateDashboardPanelsV1(raw)
	case DashboardPanelsVersion2:
		return validateDashboardPanelsV2(raw)
	default:
		return fmt.Errorf("unsupported panels version %d, expected %d or %d", schema, DashboardPanelsVersion, DashboardPanelsVersion2)
	}
}

// dashboardPanelsSchema reads the blob's version with a lenient decode. It
// fails only when the blob is not a JSON object.
func dashboardPanelsSchema(raw json.RawMessage) (int, error) {
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return 0, fmt.Errorf("panels payload is not valid JSON: %w", err)
	}
	return probe.Version, nil
}

// validateDashboardPanelsV1 holds the schema 1 rules. They do not change.
func validateDashboardPanelsV1(raw json.RawMessage) error {
	if len(raw) > MaxDashboardPanelsSize {
		return fmt.Errorf("panels payload is %d bytes, exceeds the %d byte limit", len(raw), MaxDashboardPanelsSize)
	}

	var blob dashboardPanels
	if err := json.Unmarshal(raw, &blob); err != nil {
		return fmt.Errorf("panels payload is not valid JSON: %w", err)
	}

	if blob.Version != DashboardPanelsVersion {
		return fmt.Errorf("unsupported panels version %d, expected %d", blob.Version, DashboardPanelsVersion)
	}

	if err := validateDashboardSettings(blob.CacheTTLSeconds, blob.DefaultTimeRange); err != nil {
		return err
	}

	if len(blob.Panels) > MaxDashboardPanels {
		return fmt.Errorf("dashboard has %d panels, exceeds the max of %d", len(blob.Panels), MaxDashboardPanels)
	}

	panelIDs, err := validateDashboardPanelEntries(blob.Panels)
	if err != nil {
		return err
	}
	layoutIDs, err := validateDashboardLayoutEntries(blob.Layout)
	if err != nil {
		return err
	}
	return validateDashboardCoverage(panelIDs, layoutIDs)
}

// validateDashboardSettings checks the dashboard-wide settings that both
// schemas share.
func validateDashboardSettings(cacheTTLSeconds *int, defaultTimeRange *string) error {
	if cacheTTLSeconds != nil {
		if *cacheTTLSeconds < 0 || *cacheTTLSeconds > MaxDashboardCacheTTLSeconds {
			return fmt.Errorf("cache_ttl_seconds %d is out of range (allowed: 0-%d)", *cacheTTLSeconds, MaxDashboardCacheTTLSeconds)
		}
	}
	if defaultTimeRange != nil && !dashboardTimeRangePattern.MatchString(*defaultTimeRange) {
		return fmt.Errorf("default_time_range %q is not a relative range such as 15m, 6h, 7d or today", *defaultTimeRange)
	}
	return nil
}

// validateDashboardPanelEntries validates each panel and returns the set of
// panel ids.
func validateDashboardPanelEntries(panels []dashboardPanel) (map[string]struct{}, error) {
	panelIDs := make(map[string]struct{}, len(panels))
	for i := range panels {
		p := &panels[i]
		if p.ID == "" {
			return nil, fmt.Errorf("panel[%d] has an empty id", i)
		}
		if _, dup := panelIDs[p.ID]; dup {
			return nil, fmt.Errorf("duplicate panel id %q", p.ID)
		}
		panelIDs[p.ID] = struct{}{}

		if _, ok := validDashboardPanelTypes[DashboardPanelType(p.Type)]; !ok {
			return nil, fmt.Errorf("panel %q has invalid type %q", p.ID, p.Type)
		}
		if p.TeamID <= 0 {
			return nil, fmt.Errorf("panel %q has invalid team_id %d", p.ID, p.TeamID)
		}
		if p.SourceID <= 0 {
			return nil, fmt.Errorf("panel %q has invalid source_id %d", p.ID, p.SourceID)
		}
		if !NormalizeQueryLanguage(p.QueryLanguage).Valid() {
			return nil, fmt.Errorf("panel %q has invalid query_language %q", p.ID, p.QueryLanguage)
		}
		if err := validateDashboardPanelOptions(p.ID, DashboardPanelType(p.Type), p.Options); err != nil {
			return nil, err
		}
	}
	return panelIDs, nil
}

// validateDashboardLayoutEntries validates each layout rect (geometry, on-grid,
// no overlap) and returns the set of layout ids (finding B3).
func validateDashboardLayoutEntries(layout []dashboardPanelLayout) (map[string]struct{}, error) {
	layoutIDs := make(map[string]struct{}, len(layout))
	for i, l := range layout {
		if l.ID == "" {
			return nil, fmt.Errorf("layout[%d] has an empty id", i)
		}
		if _, dup := layoutIDs[l.ID]; dup {
			return nil, fmt.Errorf("duplicate layout id %q", l.ID)
		}
		layoutIDs[l.ID] = struct{}{}

		if _, ok := validDashboardPanelWidths[l.W]; !ok {
			return nil, fmt.Errorf("layout %q has invalid width %d (allowed: 2, 3, 4, 6, 8, 9, 12)", l.ID, l.W)
		}
		if l.H < MinDashboardPanelH || l.H > MaxDashboardPanelH {
			return nil, fmt.Errorf("layout %q has invalid height %d (allowed: %d-%d)", l.ID, l.H, MinDashboardPanelH, MaxDashboardPanelH)
		}
		// Position must be on-grid: non-negative and not spilling past the
		// right edge of the 12-column grid.
		if l.X < 0 || l.Y < 0 {
			return nil, fmt.Errorf("layout %q has negative position (x=%d, y=%d)", l.ID, l.X, l.Y)
		}
		if l.X+l.W > DashboardGridColumns {
			return nil, fmt.Errorf("layout %q spills off the grid: x+w = %d exceeds %d columns", l.ID, l.X+l.W, DashboardGridColumns)
		}
	}

	// No two panels may occupy overlapping grid cells. O(n^2) is fine given
	// MaxDashboardPanels.
	for i := range layout {
		for j := i + 1; j < len(layout); j++ {
			if layoutRectsOverlap(layout[i], layout[j]) {
				return nil, fmt.Errorf("layout %q and %q overlap", layout[i].ID, layout[j].ID)
			}
		}
	}
	return layoutIDs, nil
}

// validateDashboardCoverage requires layout and panels to be in one-to-one
// correspondence: every panel has exactly one layout entry and every layout id
// names a real panel (finding B3).
func validateDashboardCoverage(panelIDs, layoutIDs map[string]struct{}) error {
	for id := range panelIDs {
		if _, ok := layoutIDs[id]; !ok {
			return fmt.Errorf("panel %q has no layout entry", id)
		}
	}
	for id := range layoutIDs {
		if _, ok := panelIDs[id]; !ok {
			return fmt.Errorf("layout id %q does not reference any panel", id)
		}
	}
	return nil
}

// layoutRectsOverlap reports whether two grid rectangles share any cell.
func layoutRectsOverlap(a, b dashboardPanelLayout) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// validateDashboardPanelOptions enforces the panel `options` contract
// (finding B8). It is lenient about unknown keys for forward-compat but
// rejects clearly-wrong shapes and values.
func validateDashboardPanelOptions(panelID string, panelType DashboardPanelType, raw json.RawMessage) error {
	if len(raw) == 0 {
		if panelType == DashboardPanelBreakdown {
			return fmt.Errorf("panel %q requires a non-empty options.group_by for breakdown", panelID)
		}
		return nil
	}
	var opts dashboardPanelOptions
	if err := json.Unmarshal(raw, &opts); err != nil {
		return fmt.Errorf("panel %q has invalid options: %w", panelID, err)
	}
	if opts.Chart != nil && *opts.Chart != "" {
		if _, ok := validDashboardPanelCharts[DashboardPanelChart(*opts.Chart)]; !ok {
			return fmt.Errorf("panel %q has invalid options.chart %q (allowed: bars, line, area)", panelID, *opts.Chart)
		}
	}
	if opts.Limit != nil {
		if *opts.Limit <= 0 {
			return fmt.Errorf("panel %q has invalid options.limit %d (must be positive)", panelID, *opts.Limit)
		}
		if *opts.Limit > MaxDashboardPanelOptionLimit {
			return fmt.Errorf("panel %q has invalid options.limit %d (max %d)", panelID, *opts.Limit, MaxDashboardPanelOptionLimit)
		}
	}
	if opts.BarMode != nil && *opts.BarMode != "" {
		if _, ok := validDashboardPanelBarModes[DashboardPanelBarMode(*opts.BarMode)]; !ok {
			return fmt.Errorf("panel %q has invalid options.bar_mode %q (allowed: stacked, grouped)", panelID, *opts.BarMode)
		}
	}
	if err := validateDashboardBreakdownOptions(panelID, panelType, opts); err != nil {
		return err
	}
	// Columns and GroupBy shapes are enforced by the unmarshal above (a string
	// where an array is expected, or vice versa, fails to decode).
	return nil
}

func validateDashboardBreakdownOptions(panelID string, panelType DashboardPanelType, opts dashboardPanelOptions) error {
	if panelType == DashboardPanelBreakdown {
		if opts.GroupBy == nil || strings.TrimSpace(*opts.GroupBy) == "" {
			return fmt.Errorf("panel %q requires a non-empty options.group_by for breakdown", panelID)
		}
		if opts.BreakdownView != nil && *opts.BreakdownView != "horizontal-bars" && *opts.BreakdownView != "donut" {
			return fmt.Errorf("panel %q has invalid options.breakdown_view %q (allowed: horizontal-bars, donut)", panelID, *opts.BreakdownView)
		}
		return nil
	}
	if opts.BreakdownView != nil && *opts.BreakdownView != "" {
		return fmt.Errorf("panel %q has options.breakdown_view but is not a breakdown panel", panelID)
	}
	return nil
}
