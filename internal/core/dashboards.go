package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

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
	// ErrDashboardConflict indicates an optimistic-concurrency precondition
	// failed: the stored dashboard changed since the client loaded it (A3).
	ErrDashboardConflict = errors.New("dashboard was modified by someone else")
)

// CreateDashboard validates and persists a new dashboard owned by the caller.
// It rejects panels that reference nonexistent teams/sources (B4) and, for
// non-admins, teams the caller does not belong to (B2).
func CreateDashboard(ctx context.Context, db store.StoreOps, log *slog.Logger, user *models.User, req *models.CreateDashboardRequest) (*models.Dashboard, error) {
	if req == nil || user == nil {
		return nil, ErrInvalidDashboard
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidDashboard)
	}
	if err := models.ValidateDashboardPanels(req.Panels); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDashboard, err)
	}
	if err := verifyDashboardPanelRefs(ctx, db, user, req.Panels); err != nil {
		return nil, err
	}

	owner := user.ID
	dashboard := &models.Dashboard{
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		PanelsJSON:  req.Panels,
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
		return fmt.Errorf("%w: %s", ErrInvalidDashboard, err)
	}
	if len(refs) == 0 {
		return nil
	}

	admin := user.Role == models.UserRoleAdmin
	var userTeams map[models.TeamID]struct{}
	if !admin {
		teams, err := db.ListUserTeams(ctx, user.ID)
		if err != nil {
			return fmt.Errorf("failed to load caller teams: %w", err)
		}
		userTeams = make(map[models.TeamID]struct{}, len(teams))
		for _, t := range teams {
			userTeams[t.ID] = struct{}{}
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

// GetDashboard retrieves a single dashboard by id.
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

// ListDashboards returns the dashboards visible to user, newest-updated first.
//
// Visibility (finding B1): a dashboard is visible to its creator, to global
// admins, and to members of ANY team referenced by the dashboard's panels.
// Dashboards have no team column of their own today, so visibility is derived
// from the panel blob. See UserCanViewDashboard for the shared rule.
//
// A row whose stored panel blob is unparseable (finding B12) is not dropped
// from the whole response: it is only surfaced to its creator/admins (its team
// references cannot be derived), and when surfaced its blob is replaced with an
// empty one and PanelsCorrupt is set so a single bad row can't break the list.
func ListDashboards(ctx context.Context, db store.StoreOps, log *slog.Logger, user *models.User) ([]*models.Dashboard, error) {
	if user == nil {
		return nil, ErrDashboardForbidden
	}
	dashboards, err := db.ListDashboards(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list dashboards: %w", err)
	}

	admin := user.Role == models.UserRoleAdmin
	var userTeams map[models.TeamID]struct{}
	if !admin {
		teams, err := db.ListUserTeams(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to load caller teams: %w", err)
		}
		userTeams = make(map[models.TeamID]struct{}, len(teams))
		for _, t := range teams {
			userTeams[t.ID] = struct{}{}
		}
	}

	visible := make([]*models.Dashboard, 0, len(dashboards))
	for _, d := range dashboards {
		isOwner := d.CreatedBy != nil && *d.CreatedBy == user.ID
		refs, refErr := models.DashboardPanelRefs(d.PanelsJSON)
		corrupt := refErr != nil

		allowed := admin || isOwner
		if !allowed && !corrupt {
			for _, ref := range refs {
				if _, ok := userTeams[models.TeamID(ref.TeamID)]; ok {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			continue
		}
		if corrupt {
			d.PanelsCorrupt = true
			d.PanelsJSON = models.EmptyDashboardPanelsJSON()
		} else if err := RedactDashboardPanelsForViewer(ctx, db, log, user, d); err != nil {
			return nil, err
		}
		visible = append(visible, d)
	}
	return visible, nil
}

// RedactDashboardPanelsForViewer rewrites dashboard.PanelsJSON for the RESPONSE
// so that panels whose source the viewer cannot reach are stripped of their
// query text (and query language / options) and flagged locked. It layers on
// top of the any-team visibility gate (UserCanViewDashboard, finding B1): a
// viewer who can see the dashboard because they belong to ONE of its panels'
// teams must still not receive the sensitive metadata of panels targeting
// sources they cannot reach.
//
// The creator and global admins see everything unredacted. The stored blob is
// never mutated — redaction is applied to a fresh copy assigned to the passed
// in-memory dashboard only (mirroring how the corrupt-row path swaps PanelsJSON
// on the loaded struct without writing back).
func RedactDashboardPanelsForViewer(ctx context.Context, db store.StoreOps, log *slog.Logger, user *models.User, dashboard *models.Dashboard) error {
	if user == nil || dashboard == nil {
		return nil
	}
	// Creator and global admins get the full, unredacted blob.
	if user.Role == models.UserRoleAdmin {
		return nil
	}
	if dashboard.CreatedBy != nil && *dashboard.CreatedBy == user.ID {
		return nil
	}

	refs, err := models.DashboardPanelRefs(dashboard.PanelsJSON)
	if err != nil || len(refs) == 0 {
		// An unparseable or empty blob has nothing to redact here; corrupt rows
		// are handled separately on the list path.
		return nil
	}

	locked := make(map[string]struct{})
	for _, ref := range refs {
		hasAccess, err := UserHasAccessToTeamSource(ctx, db, log, user.ID, models.TeamID(ref.TeamID), models.SourceID(ref.SourceID))
		if err != nil {
			return fmt.Errorf("failed to check panel source access: %w", err)
		}
		if !hasAccess {
			locked[ref.PanelID] = struct{}{}
		}
	}
	if len(locked) == 0 {
		return nil
	}

	redacted, err := models.RedactDashboardPanels(dashboard.PanelsJSON, locked)
	if err != nil {
		return fmt.Errorf("failed to redact dashboard panels: %w", err)
	}
	dashboard.PanelsJSON = redacted
	return nil
}

// UserCanViewDashboard reports whether user may view dashboard under the B1
// visibility model: creator, global admin, or a member of any team referenced
// by the panels. A dashboard whose panel blob is unparseable is visible only
// to its creator/admins (its teams cannot be derived).
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
	refs, err := models.DashboardPanelRefs(dashboard.PanelsJSON)
	if err != nil || len(refs) == 0 {
		return false, nil
	}
	teams, err := db.ListUserTeams(ctx, user.ID)
	if err != nil {
		return false, fmt.Errorf("failed to load caller teams: %w", err)
	}
	userTeams := make(map[models.TeamID]struct{}, len(teams))
	for _, t := range teams {
		userTeams[t.ID] = struct{}{}
	}
	for _, ref := range refs {
		if _, ok := userTeams[models.TeamID(ref.TeamID)]; ok {
			return true, nil
		}
	}
	return false, nil
}

// UpdateDashboard validates and persists changes to an existing dashboard.
// Authorization mirrors the create path plus an edit gate and an
// optimistic-concurrency precondition:
//
//   - only the creator or a global admin may edit (existing behavior);
//   - A3: if req.UpdatedAt is set and the stored row has advanced past it, the
//     update is rejected with ErrDashboardConflict;
//   - B2/B4: the new panels are re-checked for team/source existence and, for
//     non-admins, team membership.
func UpdateDashboard(ctx context.Context, db store.StoreOps, log *slog.Logger, id int, user *models.User, req *models.UpdateDashboardRequest) (*models.Dashboard, error) {
	if req == nil || user == nil {
		return nil, ErrInvalidDashboard
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalidDashboard)
	}
	if err := models.ValidateDashboardPanels(req.Panels); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidDashboard, err)
	}

	existing, err := db.GetDashboard(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrDashboardNotFound
		}
		log.Error("failed to load dashboard for update", "dashboard_id", id, "error", err)
		return nil, fmt.Errorf("failed to load dashboard: %w", err)
	}

	if !UserCanEditDashboard(existing, user) {
		return nil, ErrDashboardForbidden
	}

	// A3: reject a stale write. Only enforced when the client supplies a
	// precondition, so pre-A3 clients keep working (last-writer-wins).
	if !req.UpdatedAt.IsZero() && existing.UpdatedAt.After(req.UpdatedAt) {
		return nil, ErrDashboardConflict
	}

	if err := verifyDashboardPanelRefs(ctx, db, user, req.Panels); err != nil {
		return nil, err
	}

	existing.Name = name
	existing.Description = strings.TrimSpace(req.Description)
	existing.PanelsJSON = req.Panels
	if err := db.UpdateDashboard(ctx, existing); err != nil {
		if models.IsNotFound(err) {
			return nil, ErrDashboardNotFound
		}
		log.Error("failed to update dashboard", "dashboard_id", id, "error", err)
		return nil, fmt.Errorf("failed to update dashboard: %w", err)
	}

	updated, err := db.GetDashboard(ctx, id)
	if err != nil {
		if models.IsNotFound(err) {
			return nil, ErrDashboardNotFound
		}
		return nil, fmt.Errorf("failed to reload dashboard: %w", err)
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
