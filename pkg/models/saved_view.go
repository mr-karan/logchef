package models

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
)

// SavedViewID identifies a saved Explorer view.
type SavedViewID int

// SavedViewScope says who owns a saved view and who can see it.
type SavedViewScope string

const (
	// SavedViewScopePersonal views belong to one user.
	SavedViewScopePersonal SavedViewScope = "personal"
	// SavedViewScopeTeam views belong to one team.
	SavedViewScopeTeam SavedViewScope = "team"
	// SavedViewScopeSource views are visible to every team that reaches the source.
	SavedViewScopeSource SavedViewScope = "source"
)

// Valid reports whether s is one of the three scopes.
func (s SavedViewScope) Valid() bool {
	switch s {
	case SavedViewScopePersonal, SavedViewScopeTeam, SavedViewScopeSource:
		return true
	}
	return false
}

// Saved view definition bounds.
const (
	SavedViewDefinitionVersion  = 1
	MaxSavedViewDefinitionBytes = 65536
	MaxSavedViewColumns         = 200
	MaxSavedViewColumnNameBytes = 512
	MinSavedViewColumnWidth     = 50
	MaxSavedViewColumnWidth     = 3000
	MaxSavedViewSortEntries     = 3
	MaxSavedViewFilterBytes     = 8192
)

// ErrInvalidSavedViewDefinition wraps every definition rule failure.
var ErrInvalidSavedViewDefinition = errors.New("invalid saved view definition")

// savedViewTimeRangePattern is the relative-range rule that saved queries use.
var savedViewTimeRangePattern = regexp.MustCompile(`^\d+[smhdw]$`)

// SavedViewColumn is one visible table column. A nil Width means automatic.
type SavedViewColumn struct {
	Name  string `json:"name"`
	Width *int   `json:"width,omitempty"`
}

// SavedViewSort is one sort entry. It orders the loaded rows in the browser
// and never changes the query's ORDER BY.
type SavedViewSort struct {
	Column string `json:"column"`
	Desc   bool   `json:"desc"`
}

// SavedViewDefinition is the stored Explorer setup. An absent part says
// nothing about that part of the Explorer.
type SavedViewDefinition struct {
	Version     int                    `json:"version"`
	Columns     []SavedViewColumn      `json:"columns,omitempty"`
	Sort        []SavedViewSort        `json:"sort,omitempty"`
	DisplayMode *DisplayModePreference `json:"display_mode,omitempty"`
	Filter      *string                `json:"filter,omitempty"`
	TimeRange   *string                `json:"time_range,omitempty"`
}

// ParseSavedViewDefinition decodes raw strictly and validates it. An unknown
// key or data after the object is an error. Every entry point (API,
// provisioning, share payload) parses through this function.
func ParseSavedViewDefinition(raw []byte) (SavedViewDefinition, error) {
	var def SavedViewDefinition
	if len(raw) > MaxSavedViewDefinitionBytes {
		return def, fmt.Errorf("%w: definition is %d bytes, the limit is %d", ErrInvalidSavedViewDefinition, len(raw), MaxSavedViewDefinitionBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&def); err != nil {
		return SavedViewDefinition{}, fmt.Errorf("%w: %w", ErrInvalidSavedViewDefinition, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return SavedViewDefinition{}, fmt.Errorf("%w: data after the definition object", ErrInvalidSavedViewDefinition)
	}
	if err := def.Validate(); err != nil {
		return SavedViewDefinition{}, err
	}
	return def, nil
}

// ParseQueryShareLayout parses the layout of a share payload. A share's
// query and time range travel in the payload itself, so a layout that sets
// filter or time_range is rejected.
func ParseQueryShareLayout(raw []byte) (SavedViewDefinition, error) {
	def, err := ParseSavedViewDefinition(raw)
	if err != nil {
		return SavedViewDefinition{}, err
	}
	if def.Filter != nil || def.TimeRange != nil {
		return SavedViewDefinition{}, invalidSavedViewDefinition("a share layout cannot set filter or time_range; the share's query and time range hold them")
	}
	return def, nil
}

// Validate checks every definition rule except the LogchefQL filter check,
// which needs the logchefql package and runs in core.
func (d SavedViewDefinition) Validate() error {
	if d.Version != SavedViewDefinitionVersion {
		return invalidSavedViewDefinition("version must be %d", SavedViewDefinitionVersion)
	}
	if err := validateSavedViewColumns(d.Columns); err != nil {
		return err
	}
	if err := validateSavedViewSort(d.Sort); err != nil {
		return err
	}
	if d.DisplayMode != nil {
		switch *d.DisplayMode {
		case DisplayModeTable, DisplayModeCompact, DisplayModeJSON:
		default:
			return invalidSavedViewDefinition("display_mode must be %q, %q or %q", DisplayModeTable, DisplayModeCompact, DisplayModeJSON)
		}
	}
	if d.Filter != nil && len(*d.Filter) > MaxSavedViewFilterBytes {
		return invalidSavedViewDefinition("filter is %d bytes, the limit is %d", len(*d.Filter), MaxSavedViewFilterBytes)
	}
	if d.TimeRange != nil && !savedViewTimeRangePattern.MatchString(*d.TimeRange) {
		return invalidSavedViewDefinition("time_range must be a relative range such as 15m, 1h or 7d")
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		return invalidSavedViewDefinition("%w", err)
	}
	if len(encoded) > MaxSavedViewDefinitionBytes {
		return invalidSavedViewDefinition("definition is %d bytes, the limit is %d", len(encoded), MaxSavedViewDefinitionBytes)
	}
	return nil
}

func invalidSavedViewDefinition(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidSavedViewDefinition}, args...)...)
}

func validateSavedViewColumns(columns []SavedViewColumn) error {
	if columns != nil && len(columns) == 0 {
		return invalidSavedViewDefinition("columns must have at least one entry; omit it to keep the table's columns")
	}
	if len(columns) > MaxSavedViewColumns {
		return invalidSavedViewDefinition("columns has %d entries, the limit is %d", len(columns), MaxSavedViewColumns)
	}
	seen := make(map[string]struct{}, len(columns))
	for i, col := range columns {
		if err := validateSavedViewColumnName(col.Name); err != nil {
			return invalidSavedViewDefinition("columns[%d].name %w", i, err)
		}
		if _, dup := seen[col.Name]; dup {
			return invalidSavedViewDefinition("column %q appears more than once", col.Name)
		}
		seen[col.Name] = struct{}{}
		if col.Width != nil && (*col.Width < MinSavedViewColumnWidth || *col.Width > MaxSavedViewColumnWidth) {
			return invalidSavedViewDefinition("columns[%d].width must be from %d to %d", i, MinSavedViewColumnWidth, MaxSavedViewColumnWidth)
		}
	}
	return nil
}

func validateSavedViewSort(sort []SavedViewSort) error {
	if sort != nil && len(sort) == 0 {
		return invalidSavedViewDefinition("sort must have at least one entry; omit it to keep the table's sort")
	}
	if len(sort) > MaxSavedViewSortEntries {
		return invalidSavedViewDefinition("sort has %d entries, the limit is %d", len(sort), MaxSavedViewSortEntries)
	}
	for i, s := range sort {
		if err := validateSavedViewColumnName(s.Column); err != nil {
			return invalidSavedViewDefinition("sort[%d].column %w", i, err)
		}
	}
	return nil
}

func validateSavedViewColumnName(name string) error {
	if name == "" {
		return errors.New("is empty")
	}
	if len(name) > MaxSavedViewColumnNameBytes {
		return fmt.Errorf("is %d bytes, the limit is %d", len(name), MaxSavedViewColumnNameBytes)
	}
	return nil
}

// SavedView is a named Explorer setup for one source. UserID is set only for
// a personal view and TeamID only for a team view.
type SavedView struct {
	ID          SavedViewID         `json:"id"`
	SourceID    SourceID            `json:"source_id"`
	Scope       SavedViewScope      `json:"scope"`
	UserID      *UserID             `json:"user_id,omitempty"`
	TeamID      *TeamID             `json:"team_id,omitempty"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Definition  SavedViewDefinition `json:"definition"`
	IsDefault   bool                `json:"is_default"`
	// Managed views are owned by provisioning.
	Managed bool `json:"managed"`
	// Revision counts saves, from 1. Every update is a compare-and-swap on
	// the revision the caller loaded.
	Revision  int     `json:"revision"`
	CreatedBy *UserID `json:"created_by,omitempty"`
	Timestamps
	// CanEdit and CanSetDefault are per-request hints for the caller,
	// filled by core.
	CanEdit       bool `json:"can_edit"`
	CanSetDefault bool `json:"can_set_default"`
}

// ScopeKey returns the scope, owner and source that the view's name and
// default flag are unique in.
func (v *SavedView) ScopeKey() SavedViewScopeKey {
	return SavedViewScopeKey{SourceID: v.SourceID, Scope: v.Scope, UserID: v.UserID, TeamID: v.TeamID}
}

// SavedViewScopeKey names one scope of one source. UserID is set only for
// the personal scope and TeamID only for the team scope.
type SavedViewScopeKey struct {
	SourceID SourceID
	Scope    SavedViewScope
	UserID   *UserID
	TeamID   *TeamID
}

// ClearedSavedViewDefault is the view that lost the default flag when another
// view of its scope took it.
type ClearedSavedViewDefault struct {
	ID      SavedViewID
	Managed bool
}

// Saved view text and count bounds.
const (
	MaxSavedViewNameRunes        = 200
	MaxSavedViewDescriptionRunes = 2000
	MaxSavedViewsPerScope        = 50
)

// CreateSavedViewRequest is the body of the create route. The owner comes
// from the caller and the path, never from the body.
type CreateSavedViewRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Scope       SavedViewScope  `json:"scope"`
	Definition  json.RawMessage `json:"definition"`
	IsDefault   bool            `json:"is_default"`
}

// UpdateSavedViewRequest is the body of the update route. Revision is the
// revision the caller loaded; a nil Revision is rejected.
type UpdateSavedViewRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Definition  json.RawMessage `json:"definition"`
	Revision    *int            `json:"revision"`
}

// SavedViewList is the list response: the views the caller can see for one
// team and source, and the view the default rules pick.
type SavedViewList struct {
	Views         []*SavedView `json:"views"`
	DefaultViewID *SavedViewID `json:"default_view_id"`
}
