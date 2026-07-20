package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// Dashboard is a saved grid of visualization panels. The full panel layout,
// per-panel source/query/type and options live in a single versioned JSON blob
// (PanelsJSON), validated by ValidateDashboardPanels before persistence.
type Dashboard struct {
	ID          int             `json:"id" db:"id"`
	Name        string          `json:"name" db:"name"`
	Description string          `json:"description" db:"description"`
	PanelsJSON  json.RawMessage `json:"panels" db:"panels_json"`
	CreatedBy   *UserID         `json:"created_by,omitempty" db:"created_by"`
	Timestamps
	// CreatedByName / CreatedByEmail identify the dashboard's creator for
	// display. Populated on list (LEFT JOIN users); empty for dashboards whose
	// author was deleted (created_by NULL).
	CreatedByName  string `json:"created_by_name,omitempty" db:"-"`
	CreatedByEmail string `json:"created_by_email,omitempty" db:"-"`
	// CanEdit is a per-request UI authorization hint for the calling user
	// (creator or global admin). nil when not computed.
	CanEdit *bool `json:"can_edit,omitempty" db:"-"`
	// PanelsCorrupt is set on list when the stored panel blob failed
	// validation. To keep one bad row from breaking the whole list response
	// (finding B12), PanelsJSON is then replaced with an empty blob and this
	// flag is raised so the UI can surface a "needs repair" state.
	PanelsCorrupt bool `json:"panels_corrupt,omitempty" db:"-"`
}

// EmptyDashboardPanelsJSON is the minimal valid panel blob. It replaces a
// corrupt stored blob in list responses (finding B12) so the row still
// marshals cleanly.
func EmptyDashboardPanelsJSON() json.RawMessage {
	return json.RawMessage(`{"version":1,"layout":[],"panels":[]}`)
}

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
	// UpdatedAt is an optimistic-concurrency precondition (finding A3): the
	// updated_at the client loaded. When present, the update is rejected with a
	// conflict if the stored row has advanced past it. Zero value disables the
	// check (last-writer-wins), preserving the pre-A3 contract for older clients.
	UpdatedAt time.Time `json:"updated_at"`
}

// Dashboard panel-blob limits and allowed values, per the approved #56 design.
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

// DashboardPanelType enumerates the chart kinds a panel may render.
type DashboardPanelType string

const (
	DashboardPanelTimeseries DashboardPanelType = "timeseries"
	DashboardPanelStat       DashboardPanelType = "stat"
	DashboardPanelTable      DashboardPanelType = "table"
)

var validDashboardPanelTypes = map[DashboardPanelType]struct{}{
	DashboardPanelTimeseries: {},
	DashboardPanelStat:       {},
	DashboardPanelTable:      {},
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
}

// dashboardPanelOptions is the validated contract for a panel's `options` blob
// (finding B8). Fields are pointers/slices so absent is distinguishable from a
// zero value, and unknown keys are ignored to stay forward-compatible. A field
// present with the wrong JSON shape (e.g. columns as a string) fails to
// unmarshal and is rejected.
type dashboardPanelOptions struct {
	Chart   *string  `json:"chart"`
	Limit   *int     `json:"limit"`
	Columns []string `json:"columns"`
	GroupBy *string  `json:"group_by"`
}

// DashboardPanelRef identifies the team and source a single panel targets. It
// is used by the authorization and dangling-reference checks in core.
type DashboardPanelRef struct {
	PanelID  string
	TeamID   int
	SourceID int
}

// DashboardPanelRefs extracts the (team_id, source_id) each panel references
// from a raw panel blob. It returns an error if the blob is not parseable;
// callers that already ran ValidateDashboardPanels can rely on it succeeding.
func DashboardPanelRefs(raw json.RawMessage) ([]DashboardPanelRef, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var blob dashboardPanels
	if err := json.Unmarshal(raw, &blob); err != nil {
		return nil, fmt.Errorf("panels payload is not valid JSON: %w", err)
	}
	refs := make([]DashboardPanelRef, 0, len(blob.Panels))
	for i := range blob.Panels {
		p := &blob.Panels[i]
		refs = append(refs, DashboardPanelRef{PanelID: p.ID, TeamID: p.TeamID, SourceID: p.SourceID})
	}
	return refs, nil
}

// RedactDashboardPanels returns a fresh COPY of the raw panel blob in which the
// panels named in lockedIDs have had their sensitive fields (query text, query
// language, options) blanked and Locked set to true. The team_id, source_id,
// type and title are kept so the frontend can still render the panel's
// position and a "locked" placeholder, and the layout is left untouched.
//
// The input bytes are never mutated: this is response-shaping only. Redaction
// re-marshals from the parsed struct, so unknown/forward-compat keys on panels
// are dropped in the returned copy — acceptable because it is only used for the
// response, never written back to the store. When there is nothing to redact
// (empty blob or empty lockedIDs), the original raw is returned verbatim.
func RedactDashboardPanels(raw json.RawMessage, lockedIDs map[string]struct{}) (json.RawMessage, error) {
	if len(raw) == 0 || len(lockedIDs) == 0 {
		return raw, nil
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

// ValidateDashboardPanels checks a raw panels_json blob against the approved
// design's rules and returns a descriptive error on the first violation. It is
// the single source of truth for panel-blob validity across create and update.
func ValidateDashboardPanels(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("panels payload is required")
	}
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

	if blob.CacheTTLSeconds != nil {
		if *blob.CacheTTLSeconds < 0 || *blob.CacheTTLSeconds > MaxDashboardCacheTTLSeconds {
			return fmt.Errorf("cache_ttl_seconds %d is out of range (allowed: 0-%d)", *blob.CacheTTLSeconds, MaxDashboardCacheTTLSeconds)
		}
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
		if err := validateDashboardPanelOptions(p.ID, p.Options); err != nil {
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
	for i := 0; i < len(layout); i++ {
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
func validateDashboardPanelOptions(panelID string, raw json.RawMessage) error {
	if len(raw) == 0 {
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
	// Columns and GroupBy shapes are enforced by the unmarshal above (a string
	// where an array is expected, or vice versa, fails to decode).
	return nil
}
