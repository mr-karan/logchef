package models

import (
	"encoding/json"
	"fmt"
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
}

// Dashboard panel-blob limits and allowed values, per the approved #56 design.
const (
	DashboardPanelsVersion = 1
	MaxDashboardPanels     = 24
	MaxDashboardPanelsSize = 100 * 1024 // 100 KB, raw JSON.
	MinDashboardPanelH     = 1
	MaxDashboardPanelH     = 6
)

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
	3:  {},
	4:  {},
	6:  {},
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
}

// dashboardPanels is the top-level versioned blob stored in dashboards.panels_json.
type dashboardPanels struct {
	Version int                    `json:"version"`
	Layout  []dashboardPanelLayout `json:"layout"`
	Panels  []dashboardPanel       `json:"panels"`
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

	if len(blob.Panels) > MaxDashboardPanels {
		return fmt.Errorf("dashboard has %d panels, exceeds the max of %d", len(blob.Panels), MaxDashboardPanels)
	}

	panelIDs := make(map[string]struct{}, len(blob.Panels))
	for i, p := range blob.Panels {
		if p.ID == "" {
			return fmt.Errorf("panel[%d] has an empty id", i)
		}
		if _, dup := panelIDs[p.ID]; dup {
			return fmt.Errorf("duplicate panel id %q", p.ID)
		}
		panelIDs[p.ID] = struct{}{}

		if _, ok := validDashboardPanelTypes[DashboardPanelType(p.Type)]; !ok {
			return fmt.Errorf("panel %q has invalid type %q", p.ID, p.Type)
		}
		if p.TeamID <= 0 {
			return fmt.Errorf("panel %q has invalid team_id %d", p.ID, p.TeamID)
		}
		if p.SourceID <= 0 {
			return fmt.Errorf("panel %q has invalid source_id %d", p.ID, p.SourceID)
		}
		if !NormalizeQueryLanguage(p.QueryLanguage).Valid() {
			return fmt.Errorf("panel %q has invalid query_language %q", p.ID, p.QueryLanguage)
		}
	}

	layoutIDs := make(map[string]struct{}, len(blob.Layout))
	for i, l := range blob.Layout {
		if l.ID == "" {
			return fmt.Errorf("layout[%d] has an empty id", i)
		}
		if _, dup := layoutIDs[l.ID]; dup {
			return fmt.Errorf("duplicate layout id %q", l.ID)
		}
		layoutIDs[l.ID] = struct{}{}

		if _, ok := validDashboardPanelWidths[l.W]; !ok {
			return fmt.Errorf("layout %q has invalid width %d (allowed: 3, 4, 6, 12)", l.ID, l.W)
		}
		if l.H < MinDashboardPanelH || l.H > MaxDashboardPanelH {
			return fmt.Errorf("layout %q has invalid height %d (allowed: %d-%d)", l.ID, l.H, MinDashboardPanelH, MaxDashboardPanelH)
		}
	}

	return nil
}
