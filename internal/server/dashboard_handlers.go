package server

import (
	"errors"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

func parseDashboardID(c *fiber.Ctx) (int, error) {
	id, err := parsePositiveIntParam(c, "dashboardID")
	return int(id), err
}

// setDashboardCanEdit populates the per-request CanEdit UI hint for the caller.
func setDashboardCanEdit(dashboard *models.Dashboard, user *models.User) {
	canEdit := core.UserCanEditDashboard(dashboard, user)
	dashboard.CanEdit = &canEdit
}

// handleListDashboards lists the dashboards visible to the caller (finding B1:
// creator, global admin, or a member of a team referenced by the panels),
// newest-updated first, each annotated with the caller's edit permission.
func (s *Server) handleListDashboards(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	dashboards, err := core.ListDashboards(c.Context(), s.sqlite, s.log, user)
	if err != nil {
		s.log.Error("failed to list dashboards", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list dashboards", models.GeneralErrorType)
	}
	for _, d := range dashboards {
		setDashboardCanEdit(d, user)
	}
	return SendSuccess(c, fiber.StatusOK, dashboards)
}

// handleCreateDashboard creates a dashboard owned by the caller.
func (s *Server) handleCreateDashboard(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	var req models.CreateDashboardRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	dashboard, err := core.CreateDashboard(c.Context(), s.sqlite, s.log, user, &req)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrInvalidDashboard):
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		case errors.Is(err, core.ErrDashboardForbidden):
			return SendErrorWithType(c, fiber.StatusForbidden, err.Error(), models.AuthorizationErrorType)
		default:
			s.log.Error("failed to create dashboard", "error", err)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create dashboard", models.GeneralErrorType)
		}
	}
	setDashboardCanEdit(dashboard, user)
	return SendSuccess(c, fiber.StatusCreated, dashboard)
}

// handleGetDashboard returns a single dashboard. Visibility follows the B1
// model: creator, global admin, or a member of a team referenced by the panels;
// everyone else gets 403. For an any-team viewer, each panel targeting a source
// they cannot reach is redacted (query text blanked, locked flagged) before the
// response is sent — the creator and global admins see everything.
func (s *Server) handleGetDashboard(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	id, err := parseDashboardID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	dashboard, err := core.GetDashboard(c.Context(), s.sqlite, s.log, id)
	if err != nil {
		if errors.Is(err, core.ErrDashboardNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Dashboard not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load dashboard", models.GeneralErrorType)
	}

	canView, err := core.UserCanViewDashboard(c.Context(), s.sqlite, user, dashboard)
	if err != nil {
		s.log.Error("failed to authorize dashboard access", "dashboard_id", id, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load dashboard", models.GeneralErrorType)
	}
	if !canView {
		return SendErrorWithType(c, fiber.StatusForbidden, "You do not have access to this dashboard", models.AuthorizationErrorType)
	}

	// Per-panel redaction: blank the query text and flag `locked` for any panel
	// whose source this viewer cannot reach (response-only; the stored blob is
	// untouched). Creator and global admins are returned unredacted.
	if err := core.RedactDashboardPanelsForViewer(c.Context(), s.sqlite, s.log, user, dashboard); err != nil {
		s.log.Error("failed to redact dashboard panels", "dashboard_id", id, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load dashboard", models.GeneralErrorType)
	}

	setDashboardCanEdit(dashboard, user)
	return SendSuccess(c, fiber.StatusOK, dashboard)
}

// handleUpdateDashboard updates a dashboard. Editing is allowed only for the
// creator or a global admin; the new panels are additionally checked for
// team/source existence and (for non-admins) team membership (B2/B4). When the
// client sends the updated_at it loaded, a stale write is rejected with 409
// (A3).
func (s *Server) handleUpdateDashboard(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	id, err := parseDashboardID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	var req models.UpdateDashboardRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	updated, updateErr := core.UpdateDashboard(c.Context(), s.sqlite, s.log, id, user, &req)
	if updateErr != nil {
		switch {
		case errors.Is(updateErr, core.ErrInvalidDashboard):
			return SendErrorWithType(c, fiber.StatusBadRequest, updateErr.Error(), models.ValidationErrorType)
		case errors.Is(updateErr, core.ErrDashboardForbidden):
			return SendErrorWithType(c, fiber.StatusForbidden, updateErr.Error(), models.AuthorizationErrorType)
		case errors.Is(updateErr, core.ErrDashboardConflict):
			return SendErrorWithType(c, fiber.StatusConflict, "Dashboard was modified by someone else; reload and reapply your changes", models.ConflictErrorType)
		case errors.Is(updateErr, core.ErrDashboardNotFound):
			return SendErrorWithType(c, fiber.StatusNotFound, "Dashboard not found", models.NotFoundErrorType)
		default:
			s.log.Error("failed to update dashboard", "dashboard_id", id, "error", updateErr)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update dashboard", models.GeneralErrorType)
		}
	}
	setDashboardCanEdit(updated, user)
	return SendSuccess(c, fiber.StatusOK, updated)
}

// handleDeleteDashboard removes a dashboard (creator + global admin only).
func (s *Server) handleDeleteDashboard(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	id, err := parseDashboardID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	existing, err := core.GetDashboard(c.Context(), s.sqlite, s.log, id)
	if err != nil {
		if errors.Is(err, core.ErrDashboardNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Dashboard not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load dashboard", models.GeneralErrorType)
	}
	if !core.UserCanEditDashboard(existing, user) {
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or a global admin can delete this dashboard", models.AuthorizationErrorType)
	}

	if delErr := core.DeleteDashboard(c.Context(), s.sqlite, s.log, id); delErr != nil {
		if errors.Is(delErr, core.ErrDashboardNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Dashboard not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete dashboard", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Dashboard deleted"})
}
