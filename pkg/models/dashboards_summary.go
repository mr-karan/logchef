package models

import (
	"encoding/json"
	"fmt"
)

// DashboardSummaryVersion is the derivation algorithm version that the
// derived columns were computed with. A row whose summary_version is lower,
// or whose summary_revision differs from its revision, is stale.
const DashboardSummaryVersion = 1

// DashboardPanelsSummary holds what the dashboard list needs from a blob. The
// dashboards table stores it in derived columns that every write of the blob
// also writes, so the list never reads a blob.
type DashboardPanelsSummary struct {
	PanelCount int
	TabCount   int
	// PanelTypes are the distinct visualization kinds, sorted. A schema 1 blob
	// reports the kinds that the upgrade gives its panels.
	PanelTypes []string
	// PanelTeamIDs are the distinct team ids of the query panels, sorted. The
	// visibility filter needs them.
	PanelTeamIDs []int
	// Corrupt is true when the blob fails its schema validator. Counts are then
	// 0 and the lists empty.
	Corrupt bool
}

// SummarizeDashboardPanels computes the derived columns from a blob of either
// schema. It upgrades a schema 1 blob in memory first, so both schemas give
// the same kinds. It never fails: a blob that fails validation gives the
// corrupt summary.
func SummarizeDashboardPanels(raw json.RawMessage) DashboardPanelsSummary {
	corrupt := DashboardPanelsSummary{PanelTypes: []string{}, PanelTeamIDs: []int{}, Corrupt: true}
	if err := ValidateDashboardPanels(raw); err != nil {
		return corrupt
	}
	upgraded, err := UpgradeDashboardPanels(raw)
	if err != nil {
		return corrupt
	}
	blob, err := decodeDashboardPanelsV2(upgraded)
	if err != nil {
		return corrupt
	}
	types := make([]string, 0, len(blob.Panels))
	teams := make([]int, 0, len(blob.Panels))
	for i := range blob.Panels {
		p := &blob.Panels[i]
		if p.Viz != nil {
			types = append(types, string(p.Viz.Type))
		}
		if p.Kind == DashboardPanelKindQuery {
			teams = append(teams, p.TeamID)
		}
	}
	return DashboardPanelsSummary{
		PanelCount:   len(blob.Panels),
		TabCount:     len(blob.Tabs),
		PanelTypes:   sortedDistinct(types),
		PanelTeamIDs: sortedDistinct(teams),
	}
}

// PanelTypesJSON is the panel_types column value.
func (s DashboardPanelsSummary) PanelTypesJSON() string {
	return mustJSONArray(s.PanelTypes)
}

// PanelTeamIDsJSON is the panel_team_ids column value.
func (s DashboardPanelsSummary) PanelTeamIDsJSON() string {
	return mustJSONArray(s.PanelTeamIDs)
}

func mustJSONArray[T any](items []T) string {
	if items == nil {
		items = []T{}
	}
	out, err := json.Marshal(items)
	if err != nil {
		panic(fmt.Sprintf("marshal summary column: %v", err))
	}
	return string(out)
}

// ParseDashboardSummaryColumns decodes the two JSON array columns of a list row.
func ParseDashboardSummaryColumns(panelTypes, panelTeamIDs string) (types []string, teams []int, err error) {
	types = []string{}
	teams = []int{}
	if err := json.Unmarshal([]byte(panelTypes), &types); err != nil {
		return nil, nil, fmt.Errorf("panel_types column %q: %w", panelTypes, err)
	}
	if err := json.Unmarshal([]byte(panelTeamIDs), &teams); err != nil {
		return nil, nil, fmt.Errorf("panel_team_ids column %q: %w", panelTeamIDs, err)
	}
	return types, teams, nil
}

// DashboardSummary is one row of the dashboard list: the summary fields that
// the API returns plus the derived columns that core needs to filter and
// repair the list. No stored blob.
type DashboardSummary struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// PanelCount and PanelTypes include locked panels; redaction keeps a locked
	// panel's type, so a summary shows nothing that the redacted blob hides.
	PanelCount     int      `json:"panel_count"`
	PanelTypes     []string `json:"panel_types"`
	TabCount       int      `json:"tab_count"`
	CreatedByName  string   `json:"created_by_name,omitempty"`
	CreatedByEmail string   `json:"created_by_email,omitempty"`
	Revision       int      `json:"revision"`
	CanEdit        bool     `json:"can_edit"`
	Corrupt        bool     `json:"corrupt"`
	Timestamps
	// Panels and PanelsCorrupt are the constant compatibility fields that a
	// schema 1 client gets: the empty schema 1 blob and true. They go away
	// with schema 1 write support.
	Panels        json.RawMessage `json:"panels,omitempty"`
	PanelsCorrupt bool            `json:"panels_corrupt,omitempty"`

	CreatedBy       *UserID `json:"-"`
	PanelTeamIDs    []int   `json:"-"`
	SummaryRevision int     `json:"-"`
	SummaryVersion  int     `json:"-"`
}

// Stale reports whether the derived columns were computed from another
// revision of the blob or by an older derivation.
func (s *DashboardSummary) Stale() bool {
	return s.SummaryRevision != s.Revision || s.SummaryVersion < DashboardSummaryVersion
}

// DashboardSummaryInput is a stale row as the backfill and list repair read
// it: just enough to recompute its summary conditionally on its revision.
type DashboardSummaryInput struct {
	ID         int
	Revision   int
	PanelsJSON json.RawMessage
}

// DashboardResponse is the one shape of a dashboard in GET, POST and PUT
// responses, built for one caller and one client schema by
// core.BuildDashboardResponse. A valid row carries Panels in the client's
// schema. A corrupt row carries Corrupt and RawPanelsJSON instead, for its
// creator or an admin only. A schema 1 client reading a schema 2 row gets the
// stub: the empty schema 1 blob, PanelsCorrupt, MinSchema and CanEdit false.
type DashboardResponse struct {
	ID             int             `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	CreatedBy      *UserID         `json:"created_by,omitempty"`
	CreatedByName  string          `json:"created_by_name,omitempty"`
	CreatedByEmail string          `json:"created_by_email,omitempty"`
	Revision       int             `json:"revision"`
	CanEdit        bool            `json:"can_edit"`
	Corrupt        bool            `json:"corrupt"`
	Panels         json.RawMessage `json:"panels,omitempty"`
	RawPanelsJSON  string          `json:"raw_panels_json,omitempty"`
	PanelsCorrupt  bool            `json:"panels_corrupt,omitempty"`
	MinSchema      int             `json:"min_schema,omitempty"`
	Timestamps
}
