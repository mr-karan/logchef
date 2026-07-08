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

// handleListDashboards lists all dashboards (visible to any authenticated user),
// newest-updated first, each annotated with the caller's edit permission.
func (s *Server) handleListDashboards(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	dashboards, err := core.ListDashboards(c.Context(), s.sqlite)
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

	dashboard, err := core.CreateDashboard(c.Context(), s.sqlite, s.log, user.ID, &req)
	if err != nil {
		if errors.Is(err, core.ErrInvalidDashboard) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		s.log.Error("failed to create dashboard", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create dashboard", models.GeneralErrorType)
	}
	setDashboardCanEdit(dashboard, user)
	return SendSuccess(c, fiber.StatusCreated, dashboard)
}

// handleGetDashboard returns a single dashboard (visible to any authenticated user).
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
	setDashboardCanEdit(dashboard, user)
	return SendSuccess(c, fiber.StatusOK, dashboard)
}

// handleUpdateDashboard updates a dashboard. Allowed only for the creator or a
// global admin.
func (s *Server) handleUpdateDashboard(c *fiber.Ctx) error {
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
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or a global admin can edit this dashboard", models.AuthorizationErrorType)
	}

	var req models.UpdateDashboardRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	updated, updateErr := core.UpdateDashboard(c.Context(), s.sqlite, s.log, id, &req)
	if updateErr != nil {
		switch {
		case errors.Is(updateErr, core.ErrInvalidDashboard):
			return SendErrorWithType(c, fiber.StatusBadRequest, updateErr.Error(), models.ValidationErrorType)
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
