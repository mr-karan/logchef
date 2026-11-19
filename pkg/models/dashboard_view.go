package models

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// DashboardView is the read-only part of a dashboard blob that a scheduled
// report runs (RFC 0008): the default range, and the tabs with their query
// panels in reading order.
type DashboardView struct {
	// DefaultTimeRange is "" when the dashboard sets none.
	DefaultTimeRange string
	Tabs             []DashboardViewTab
}

type DashboardViewTab struct {
	ID    string
	Title string
	// Panels are in reading order: row, then column.
	Panels []DashboardViewPanel
}

// DashboardViewPanel is one query panel. Optional blob fields read as their
// zero value when absent.
type DashboardViewPanel struct {
	ID       string
	Title    string
	TeamID   TeamID
	SourceID SourceID
	Query    DashboardViewQuery
	Viz      DashboardViewViz
}

type DashboardViewQuery struct {
	Mode     DashboardQueryMode
	Language QueryLanguage
	Text     string
	GroupBy  string
	// Interval is "" or "auto" for the automatic window.
	Interval       string
	Limit          int
	TranslateLimit int
	// Range replaces the dashboard range for this panel.
	Range string
}

type DashboardViewViz struct {
	Type    DashboardVizType
	Value   string
	OrderBy string
	Reduce  DashboardStatReduce
	Format  DashboardNumberFormat
	// X, Y and Series are a chart's roles in result mode, and XScale its
	// axis: "time", "category", or "auto" or "" for the x column's kind.
	X      string
	Y      []string
	Series string
	XScale string
	// Label is a pie's or toplist's label role in result mode.
	Label string
	// MaxRows is a toplist's row cap, 0 when unset.
	MaxRows int
	// MinSlicePercent folds a pie's smaller slices, 0 when unset.
	MinSlicePercent float64
	// Columns holds the confirmed kind of a column by name.
	Columns        map[string]string
	VisibleColumns []string
}

// DashboardNumberFormat is viz.format of RFC 0003. Unset strings are "".
type DashboardNumberFormat struct {
	Notation string
	Decimals *int
	Grouping *bool
	Scale    *float64
	Unit     string
	Prefix   string
	Suffix   string
}

// ReadDashboardView upgrades a schema 1 blob, validates it and returns its
// view. A blob that uses a feature this server does not know, or that fails
// validation, is an error.
func ReadDashboardView(raw json.RawMessage) (*DashboardView, error) {
	if unknown := DashboardPanelsUnknownFeatures(raw); len(unknown) > 0 {
		return nil, fmt.Errorf("dashboard uses features this server does not know: %s", strings.Join(unknown, ", "))
	}
	upgraded, err := UpgradeDashboardPanels(raw)
	if err != nil {
		return nil, err
	}
	blob, err := decodeDashboardPanelsV2(upgraded)
	if err != nil {
		return nil, err
	}
	if err := validateDashboardPanelsV2Blob(blob); err != nil {
		return nil, err
	}
	panels := make(map[string]*dashboardPanelV2, len(blob.Panels))
	for i := range blob.Panels {
		panels[blob.Panels[i].ID] = &blob.Panels[i]
	}
	view := &DashboardView{Tabs: make([]DashboardViewTab, len(blob.Tabs))}
	if blob.DefaultTimeRange != nil {
		view.DefaultTimeRange = *blob.DefaultTimeRange
	}
	for i, tab := range blob.Tabs {
		layout := slices.Clone(tab.Layout)
		slices.SortStableFunc(layout, func(a, b dashboardPanelLayout) int {
			return cmp.Or(cmp.Compare(a.Y, b.Y), cmp.Compare(a.X, b.X))
		})
		view.Tabs[i] = DashboardViewTab{ID: tab.ID, Title: tab.Title}
		for _, l := range layout {
			if p := panels[l.ID]; p != nil {
				view.Tabs[i].Panels = append(view.Tabs[i].Panels, dashboardViewPanel(p))
			}
		}
	}
	return view, nil
}

func dashboardViewPanel(p *dashboardPanelV2) DashboardViewPanel {
	q, v := p.Query, p.Viz
	out := DashboardViewPanel{
		ID: p.ID, Title: p.Title, TeamID: TeamID(p.TeamID), SourceID: SourceID(p.SourceID),
		Query: DashboardViewQuery{
			Mode: q.Mode, Language: q.Language, Text: q.Text,
			GroupBy: deref(q.GroupBy), Interval: deref(q.Interval), Range: deref(q.Range),
			Limit: deref(q.Limit), TranslateLimit: deref(q.TranslateLimit),
		},
		Viz: DashboardViewViz{
			Type: v.Type, Value: deref(v.Value), OrderBy: deref(v.OrderBy), Reduce: deref(v.Reduce),
			X: deref(v.X), Y: slices.Clone(v.Y), Series: deref(v.Series), XScale: deref(v.XScale),
			Label: deref(v.Label), MaxRows: deref(v.MaxRows), MinSlicePercent: deref(v.MinSlicePercent),
			VisibleColumns: slices.Clone(v.VisibleColumns),
		},
	}
	if len(v.Columns) > 0 {
		out.Viz.Columns = make(map[string]string, len(v.Columns))
		for name, c := range v.Columns {
			out.Viz.Columns[name] = c.Kind
		}
	}
	if f := v.Format; f != nil {
		out.Viz.Format = DashboardNumberFormat{
			Notation: deref(f.Notation), Decimals: f.Decimals, Grouping: f.Grouping, Scale: f.Scale,
			Unit: deref(f.Unit), Prefix: deref(f.Prefix), Suffix: deref(f.Suffix),
		}
	}
	return out
}

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
