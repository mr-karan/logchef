package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrDashboardNotFound is returned when a dashboard cannot be located.
	ErrDashboardNotFound = errors.New("dashboard not found")
	// ErrInvalidDashboard indicates the request payload failed validation.
	ErrInvalidDashboard = errors.New("invalid dashboard configuration")
	// ErrDashboardForbidden indicates the caller is not authorized to view or
	// mutate the dashboard (or a team/source its panels reference).
	ErrDashboardForbidden = errors.New("not authorized for this dashboard")
	// ErrDashboardConflict means another save moved the dashboard past the
	// revision the client loaded, so this save was not applied.
	ErrDashboardConflict = errors.New("dashboard was modified by someone else")
	// ErrDashboardRevisionRequired means the update body carries no revision,
	// so the server cannot tell which copy the client edited.
	ErrDashboardRevisionRequired = errors.New("dashboard revision is required")
	// ErrDashboardClientStale means a newer client saved the row: a schema 2
	// row got a schema 1 body, or the row uses a feature the writer did not
	// declare. The save would drop state the writer does not know.
	// DashboardClientStaleError carries the detail.
	ErrDashboardClientStale = errors.New("dashboard was saved by a newer client")
	// ErrDashboardListUnavailable means stale list summaries could not be
	// repaired in three passes because saves kept landing; retry shortly.
	ErrDashboardListUnavailable = errors.New("dashboard list is being repaired")
)

// DashboardClientStaleError refuses a save that would drop state its client
// does not know. Detail completes the sentence "A newer Logchef saved this
// dashboard, and <Detail>."
type DashboardClientStaleError struct {
	Detail string
}

func (e *DashboardClientStaleError) Error() string {
	return ErrDashboardClientStale.Error() + ": " + e.Detail
}

func (e *DashboardClientStaleError) Is(target error) bool {
	return target == ErrDashboardClientStale
}

const (
	// dashboardSummaryPageSize is the id-ordered batch of the stale scan.
	dashboardSummaryPageSize = 50
	// dashboardListRepairPasses bounds the repair-then-list loop.
	dashboardListRepairPasses = 3
)

// dashboardText trims and bounds a dashboard's name and description.
func dashboardText(rawName, rawDescription string) (name, description string, err error) {
	name = strings.TrimSpace(rawName)
	description = strings.TrimSpace(rawDescription)
	if name == "" {
		return "", "", fmt.Errorf("%w: name is required", ErrInvalidDashboard)
	}
	if n := utf8.RuneCountInString(name); n > models.MaxDashboardNameRunes {
		return "", "", fmt.Errorf("%w: name is %d characters, exceeds the %d character limit", ErrInvalidDashboard, n, models.MaxDashboardNameRunes)
	}
	if n := utf8.RuneCountInString(description); n > models.MaxDashboardDescriptionRunes {
		return "", "", fmt.Errorf("%w: description is %d characters, exceeds the %d character limit", ErrInvalidDashboard, n, models.MaxDashboardDescriptionRunes)
	}
	return name, description, nil
}

// prepareDashboardPanels validates a request blob and returns the bytes to
// store.
func prepareDashboardPanels(raw json.RawMessage) (json.RawMessage, error) {
	stored, err := models.CanonicalDashboardPanels(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidDashboard, err)
	}
	return stored, nil
}

// CreateDashboard validates and persists a new dashboard owned by the caller.
// It rejects panels that reference nonexistent teams/sources (B4) and, for
// non-admins, teams the caller does not belong to (B2).
func CreateDashboard(ctx context.Context, db store.StoreOps, log *slog.Logger, user *models.User, req *models.CreateDashboardRequest) (*models.Dashboard, error) {
	if req == nil || user == nil {
		return nil, ErrInvalidDashboard
	}
	name, description, err := dashboardText(req.Name, req.Description)
	if err != nil {
		return nil, err
	}
	stored, err := prepareDashboardPanels(req.Panels)
	if err != nil {
		return nil, err
	}
	if err := verifyDashboardPanelRefs(ctx, db, user, stored); err != nil {
		return nil, err
	}

	owner := user.ID
	dashboard := &models.Dashboard{
		Name:        name,
		Description: description,
		PanelsJSON:  stored,
		CreatedBy:   &owner,
	}
	if err := db.CreateDashboard(ctx, dashboard); err != nil {
		log.Error("failed to create dashboard", "error", err, "created_by", owner)
		return nil, fmt.Errorf("failed to create dashboard: %w", err)
	}
	log.Info("dashboard created", "dashboard_id", dashboard.ID, "created_by", owner)
	return dashboard, nil
}

// verifyDashboardPanelRefs enforces the panel team/source authorization and
// integrity rules shared by create and update:
//
//   - B4 (dangling refs): every panel's team_id and source_id must exist, and
//     the source must be linked to the team.
//   - B2 (cross-team access): a non-admin caller must be a member of every team
//     a panel references (membership + the team↔source link together imply the
//     caller can reach the source).
//
// Global admins bypass the membership check (B2) but not the existence check
// (B4) — a dangling reference is a data-integrity problem regardless of role.
func verifyDashboardPanelRefs(ctx context.Context, db store.StoreOps, user *models.User, raw json.RawMessage) error {
	refs, err := models.DashboardPanelRefs(raw)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidDashboard, err)
	}
	if len(refs) == 0 {
		return nil
	}

	admin := user.Role == models.UserRoleAdmin
	var userTeams map[models.TeamID]struct{}
	if !admin {
		userTeams, err = userTeamSet(ctx, db, user.ID)
		if err != nil {
			return err
		}
	}

	for _, ref := range refs {
		teamID := models.TeamID(ref.TeamID)
		sourceID := models.SourceID(ref.SourceID)

		if _, err := db.GetTeam(ctx, teamID); err != nil {
			if models.IsNotFound(err) {
				return fmt.Errorf("%w: panel %q references team %d which does not exist", ErrInvalidDashboard, ref.PanelID, ref.TeamID)
			}
			return fmt.Errorf("failed to verify team %d: %w", ref.TeamID, err)
		}
		if _, err := db.GetSource(ctx, sourceID); err != nil {
			if models.IsNotFound(err) {
				return fmt.Errorf("%w: panel %q references source %d which does not exist", ErrInvalidDashboard, ref.PanelID, ref.SourceID)
			}
			return fmt.Errorf("failed to verify source %d: %w", ref.SourceID, err)
		}
		linked, err := db.TeamHasSource(ctx, teamID, sourceID)
		if err != nil {
			return fmt.Errorf("failed to verify team/source link: %w", err)
		}
		if !linked {
			return fmt.Errorf("%w: panel %q references source %d which is not linked to team %d", ErrInvalidDashboard, ref.PanelID, ref.SourceID, ref.TeamID)
		}
		if !admin {
			if _, ok := userTeams[teamID]; !ok {
				return fmt.Errorf("%w: you are not a member of team %d referenced by panel %q", ErrDashboardForbidden, ref.TeamID, ref.PanelID)
			}
		}
	}
	return nil
}

func userTeamSet(ctx context.Context, db store.StoreOps, userID models.UserID) (map[models.TeamID]struct{}, error) {
	teams, err := db.ListUserTeams(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to load caller teams: %w", err)
	}
	set := make(map[models.TeamID]struct{}, len(teams))
	for _, t := range teams {
		set[t.ID] = struct{}{}
	}
	return set, nil
}

// GetDashboard retrieves a single stored dashboard row by id.
func GetDashboard(ctx context.Context, db store.StoreOps, log *slog.Logger, id int) (*models.Dashboard, error) {
	dashboard, err := db.GetDashboard(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrDashboardNotFound
		}
		log.Error("failed to get dashboard", "dashboard_id", id, "error", err)
		return nil, fmt.Errorf("failed to get dashboard: %w", err)
	}
	return dashboard, nil
}

// ListDashboards returns the summaries of the dashboards visible to user,
// newest-updated first, from the derived columns and never from a blob.
//
// Visibility (finding B1): a dashboard is visible to its creator, to global
// admins, and to members of any team its query panels reference. A corrupt
// row has no team ids, so only its creator and admins see it.
//
// A stale summary (an older writer, or an older derivation) is repaired before
// visibility applies. Saves that keep landing between the repair and the list
// query bound the loop; after three passes the list is unavailable.
func ListDashboards(ctx context.Context, db store.StoreOps, log *slog.Logger, user *models.User) ([]*models.DashboardSummary, error) {
	if user == nil {
		return nil, ErrDashboardForbidden
	}
	var rows []*models.DashboardSummary
	for range dashboardListRepairPasses {
		if _, err := BackfillDashboardSummaries(ctx, db, log); err != nil {
			return nil, err
		}
		listed, err := db.ListDashboardSummaries(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list dashboards: %w", err)
		}
		if !anySummaryStale(listed) {
			rows = listed
			break
		}
	}
	if rows == nil {
		return nil, ErrDashboardListUnavailable
	}

	admin := user.Role == models.UserRoleAdmin
	var userTeams map[models.TeamID]struct{}
	if !admin {
		var err error
		userTeams, err = userTeamSet(ctx, db, user.ID)
		if err != nil {
			return nil, err
		}
	}

	visible := make([]*models.DashboardSummary, 0, len(rows))
	for _, s := range rows {
		isOwner := s.CreatedBy != nil && *s.CreatedBy == user.ID
		allowed := admin || isOwner
		for _, teamID := range s.PanelTeamIDs {
			if allowed {
				break
			}
			_, allowed = userTeams[models.TeamID(teamID)]
		}
		if !allowed {
			continue
		}
		s.CanEdit = admin || isOwner
		visible = append(visible, s)
	}
	return visible, nil
}

func anySummaryStale(rows []*models.DashboardSummary) bool {
	for _, s := range rows {
		if s.Stale() {
			return true
		}
	}
	return false
}

// BackfillDashboardSummaries recomputes the derived columns of every stale
// dashboard row in id-ordered batches, conditionally on the revision read.
// It runs at startup after the migrations and before the list, is idempotent
// (a second run writes nothing) and reports how many rows it wrote. A row
// that fails its schema validator is marked corrupt and the run goes on.
func BackfillDashboardSummaries(ctx context.Context, db store.StoreOps, log *slog.Logger) (int, error) {
	repaired := 0
	afterID := 0
	for {
		page, err := db.ListStaleDashboardSummaryInputs(ctx, afterID, dashboardSummaryPageSize)
		if err != nil {
			return repaired, fmt.Errorf("failed to scan stale dashboard summaries: %w", err)
		}
		if len(page) == 0 {
			return repaired, nil
		}
		for _, row := range page {
			summary := models.SummarizeDashboardPanels(row.PanelsJSON)
			if summary.Corrupt {
				log.Warn("dashboard panel blob fails validation; marking it corrupt", "dashboard_id", row.ID, "revision", row.Revision)
			}
			n, err := db.RepairDashboardSummary(ctx, row.ID, row.Revision, summary)
			if err != nil {
				return repaired, fmt.Errorf("failed to repair dashboard %d summary: %w", row.ID, err)
			}
			repaired += int(n)
		}
		afterID = page[len(page)-1].ID
	}
}

// lockedDashboardPanels returns the ids of the query panels in panels whose
// source the viewer cannot reach. The creator and global admins see every
// panel in this release.
func lockedDashboardPanels(ctx context.Context, db store.StoreOps, log *slog.Logger, user *models.User, dashboard *models.Dashboard, panels json.RawMessage) (map[string]struct{}, error) {
	if user.Role == models.UserRoleAdmin || (dashboard.CreatedBy != nil && *dashboard.CreatedBy == user.ID) {
		return nil, nil
	}
	refs, err := models.DashboardPanelRefs(panels)
	if err != nil {
		return nil, fmt.Errorf("failed to read panel references: %w", err)
	}
	locked := make(map[string]struct{})
	for _, ref := range refs {
		hasAccess, err := UserHasAccessToTeamSource(ctx, db, log, user.ID, models.TeamID(ref.TeamID), models.SourceID(ref.SourceID))
		if err != nil {
			return nil, fmt.Errorf("failed to check panel source access: %w", err)
		}
		if !hasAccess {
			locked[ref.PanelID] = struct{}{}
		}
	}
	return locked, nil
}

// BuildDashboardResponse shapes a stored row for one caller and one client. It
// is the only path from a stored blob to a response: it serves the stub for a
// stored feature this server does not know, reports a corrupt blob with its
// raw bytes, upgrades a schema 1 blob for a schema 2 client, serves the stub
// to a schema 1 client on a schema 2 row, computes the caller's locked panels
// from their current source access, redacts them and sets can_edit. The
// caller has already passed UserCanViewDashboard.
func BuildDashboardResponse(ctx context.Context, db store.StoreOps, log *slog.Logger, user *models.User, client models.DashboardClient, dashboard *models.Dashboard) (*models.DashboardResponse, error) {
	resp := &models.DashboardResponse{
		ID:             dashboard.ID,
		Name:           dashboard.Name,
		Description:    dashboard.Description,
		CreatedBy:      dashboard.CreatedBy,
		CreatedByName:  dashboard.CreatedByName,
		CreatedByEmail: dashboard.CreatedByEmail,
		Revision:       dashboard.Revision,
		CanEdit:        UserCanEditDashboard(dashboard, user),
		Timestamps:     dashboard.Timestamps,
	}
	if len(models.DashboardPanelsUnknownFeatures(dashboard.PanelsJSON)) > 0 {
		return dashboardStub(resp), nil
	}
	if err := models.ValidateDashboardPanels(dashboard.PanelsJSON); err != nil {
		resp.Corrupt = true
		resp.RawPanelsJSON = string(dashboard.PanelsJSON)
		return resp, nil //nolint:nilerr // a corrupt blob is a response, not a failure
	}
	stored, _ := models.DashboardPanelsSchema(dashboard.PanelsJSON)
	panels := dashboard.PanelsJSON
	switch {
	case client.Schema == models.DashboardPanelsVersion && stored == models.DashboardPanelsVersion2:
		return dashboardStub(resp), nil
	case client.Schema == models.DashboardPanelsVersion2 && stored == models.DashboardPanelsVersion:
		upgraded, err := models.UpgradeDashboardPanels(panels)
		if err != nil {
			return nil, fmt.Errorf("failed to upgrade dashboard panels: %w", err)
		}
		panels = upgraded
	}
	locked, err := lockedDashboardPanels(ctx, db, log, user, dashboard, panels)
	if err != nil {
		return nil, err
	}
	redacted, err := models.RedactDashboardPanels(panels, locked)
	if err != nil {
		return nil, fmt.Errorf("failed to redact dashboard panels: %w", err)
	}
	resp.Panels = redacted
	return resp, nil
}

// dashboardStub fills the response a client gets for a blob it cannot read:
// the empty schema 1 blob, panels_corrupt, min_schema and no edit rights.
func dashboardStub(resp *models.DashboardResponse) *models.DashboardResponse {
	resp.Panels = models.EmptyDashboardPanelsJSON()
	resp.PanelsCorrupt = true
	resp.MinSchema = models.DashboardPanelsVersion2
	resp.CanEdit = false
	return resp
}

// UserCanViewDashboard reports whether user may view dashboard under the B1
// visibility model: creator, global admin, or a member of any team referenced
// by the panels. A corrupt row is visible only to its creator and admins.
func UserCanViewDashboard(ctx context.Context, db store.StoreOps, user *models.User, dashboard *models.Dashboard) (bool, error) {
	if user == nil || dashboard == nil {
		return false, nil
	}
	if user.Role == models.UserRoleAdmin {
		return true, nil
	}
	if dashboard.CreatedBy != nil && *dashboard.CreatedBy == user.ID {
		return true, nil
	}
	// A row with a feature this server does not know is readable enough for
	// visibility; any other row that fails validation is corrupt.
	if len(models.DashboardPanelsUnknownFeatures(dashboard.PanelsJSON)) == 0 {
		if err := models.ValidateDashboardPanels(dashboard.PanelsJSON); err != nil {
			return false, nil //nolint:nilerr // a corrupt blob grants no team-based visibility
		}
	}
	refs, err := models.DashboardPanelRefs(dashboard.PanelsJSON)
	if err != nil || len(refs) == 0 {
		return false, nil //nolint:nilerr // an unreadable blob grants no team-based visibility
	}
	userTeams, err := userTeamSet(ctx, db, user.ID)
	if err != nil {
		return false, err
	}
	for _, ref := range refs {
		if _, ok := userTeams[models.TeamID(ref.TeamID)]; ok {
			return true, nil
		}
	}
	return false, nil
}

// dashboardWriteContract applies the client schema contract to a save: a
// schema 2 row refuses a schema 1 body, and a row whose stored features the
// writer did not declare refuses the write. It reads the stored row and the
// headers, never the body's features list. A schema 1 or unreadable row takes
// any valid body.
func dashboardWriteContract(existing *models.Dashboard, client models.DashboardClient, body json.RawMessage) error {
	stored, err := models.DashboardPanelsSchema(existing.PanelsJSON)
	if err != nil || stored != models.DashboardPanelsVersion2 {
		return nil //nolint:nilerr // an unreadable row is repaired by any valid body
	}
	if bodySchema, _ := models.DashboardPanelsSchema(body); bodySchema != models.DashboardPanelsVersion2 {
		return &DashboardClientStaleError{Detail: "it was saved as panel schema 2 while this save is schema 1"}
	}
	var missing []string
	for _, name := range models.DashboardPanelsStoredFeatures(existing.PanelsJSON) {
		if !client.Supports(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return &DashboardClientStaleError{Detail: fmt.Sprintf("it uses %s, which this client does not declare", strings.Join(missing, ", "))}
	}
	return nil
}

// UpdateDashboard authorizes and saves against the client revision in one
// transaction, applying the client schema contract before the
// compare-and-swap so a stale write cannot slip in between a newer save and
// its commit.
func UpdateDashboard(ctx context.Context, db store.Store, log *slog.Logger, id int, user *models.User, client models.DashboardClient, req *models.UpdateDashboardRequest) (*models.Dashboard, error) {
	if req == nil || user == nil {
		return nil, ErrInvalidDashboard
	}
	if req.Revision == nil {
		return nil, ErrDashboardRevisionRequired
	}
	name, description, err := dashboardText(req.Name, req.Description)
	if err != nil {
		return nil, err
	}
	stored, err := prepareDashboardPanels(req.Panels)
	if err != nil {
		return nil, err
	}

	var updated *models.Dashboard
	err = db.WithTx(ctx, func(tx store.StoreOps) error {
		existing, err := tx.GetDashboard(ctx, id)
		if err != nil {
			if models.IsNotFound(err) {
				return ErrDashboardNotFound
			}
			return fmt.Errorf("failed to load dashboard: %w", err)
		}
		if !UserCanEditDashboard(existing, user) {
			return ErrDashboardForbidden
		}
		if err := dashboardWriteContract(existing, client, stored); err != nil {
			return err
		}
		if err := verifyDashboardPanelRefs(ctx, tx, user, stored); err != nil {
			return err
		}

		existing.Name = name
		existing.Description = description
		existing.PanelsJSON = stored
		existing.Revision = *req.Revision
		if err := tx.UpdateDashboard(ctx, existing); err != nil {
			switch {
			case models.IsConflict(err):
				return ErrDashboardConflict
			case models.IsNotFound(err):
				return ErrDashboardNotFound
			default:
				return fmt.Errorf("failed to update dashboard: %w", err)
			}
		}

		updated, err = tx.GetDashboard(ctx, id)
		if err != nil {
			return fmt.Errorf("failed to reload dashboard: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteDashboard removes a dashboard by id.
func DeleteDashboard(ctx context.Context, db store.StoreOps, log *slog.Logger, id int) error {
	if err := db.DeleteDashboard(ctx, id); err != nil {
		if models.IsNotFound(err) {
			return ErrDashboardNotFound
		}
		log.Error("failed to delete dashboard", "dashboard_id", id, "error", err)
		return fmt.Errorf("failed to delete dashboard: %w", err)
	}
	return nil
}

// UserCanEditDashboard returns true if the user is the creator or a global
// admin. Dashboards with a nil CreatedBy (author deleted) are editable only by
// global admins. Mirrors UserCanEditAlert.
func UserCanEditDashboard(dashboard *models.Dashboard, user *models.User) bool {
	if dashboard == nil || user == nil {
		return false
	}
	if user.Role == models.UserRoleAdmin {
		return true
	}
	return dashboard.CreatedBy != nil && *dashboard.CreatedBy == user.ID
}
