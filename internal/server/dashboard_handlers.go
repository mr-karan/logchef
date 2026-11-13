package server

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v3"
)

func parseDashboardID(c fiber.Ctx) (int, error) {
	id, err := parsePositiveIntParam(c, "dashboardID")
	return int(id), err
}

// dashboardClient reads the client schema contract headers. The schema header
// must be distinguished absent from empty, so it comes from the header map.
func dashboardClient(c fiber.Ctx) (models.DashboardClient, error) {
	var schema *string
	for key, values := range c.GetReqHeaders() {
		if strings.EqualFold(key, models.DashboardSchemaHeader) {
			joined := strings.Join(values, ",")
			schema = &joined
		}
	}
	return models.ParseDashboardClient(schema, c.Get(models.DashboardFeaturesHeader))
}

// sendDashboard builds and sends the one response shape for a stored row.
func (s *Server) sendDashboard(c fiber.Ctx, status int, user *models.User, client models.DashboardClient, dashboard *models.Dashboard) error {
	resp, err := core.BuildDashboardResponse(c.Context(), s.store, s.log, user, client, dashboard)
	if err != nil {
		s.log.Error("failed to build dashboard response", "dashboard_id", dashboard.ID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load dashboard", models.GeneralErrorType)
	}
	return SendSuccess(c, status, resp)
}

// handleListDashboards lists the summaries of the dashboards visible to the
// caller (finding B1: creator, global admin, or a member of a team referenced
// by the panels), newest-updated first. No stored blob is returned. A schema 1
// client gets the constant compatibility fields on every summary.
func (s *Server) handleListDashboards(c fiber.Ctx) error {
	user := currentUser(c)
	client, err := dashboardClient(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	summaries, err := core.ListDashboards(c.Context(), s.store, s.log, user)
	if err != nil {
		if errors.Is(err, core.ErrDashboardListUnavailable) {
			c.Set(fiber.HeaderRetryAfter, "1")
			return SendErrorWithType(c, fiber.StatusServiceUnavailable, "The dashboard list is being repaired; retry shortly", models.GeneralErrorType)
		}
		s.log.Error("failed to list dashboards", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list dashboards", models.GeneralErrorType)
	}
	if client.Schema == models.DashboardPanelsVersion {
		for _, summary := range summaries {
			summary.Panels = models.EmptyDashboardPanelsJSON()
			summary.PanelsCorrupt = true
		}
	}
	return SendSuccess(c, fiber.StatusOK, summaries)
}

// handleCreateDashboard creates a dashboard owned by the caller.
func (s *Server) handleCreateDashboard(c fiber.Ctx) error {
	user := currentUser(c)
	client, err := dashboardClient(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	var req models.CreateDashboardRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	dashboard, err := core.CreateDashboard(c.Context(), s.store, s.log, user, &req)
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
	return s.sendDashboard(c, fiber.StatusCreated, user, client, dashboard)
}

// handleGetDashboard returns a single dashboard. Visibility follows the B1
// model: creator, global admin, or a member of a team referenced by the panels;
// everyone else gets 403, as does anyone but the creator or an admin for a
// corrupt row. The response is built in the client's schema with each panel
// the viewer cannot reach redacted.
func (s *Server) handleGetDashboard(c fiber.Ctx) error {
	user := currentUser(c)
	client, err := dashboardClient(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	id, err := parseDashboardID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	dashboard, err := core.GetDashboard(c.Context(), s.store, s.log, id)
	if err != nil {
		if errors.Is(err, core.ErrDashboardNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Dashboard not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load dashboard", models.GeneralErrorType)
	}

	canView, err := core.UserCanViewDashboard(c.Context(), s.store, user, dashboard)
	if err != nil {
		s.log.Error("failed to authorize dashboard access", "dashboard_id", id, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load dashboard", models.GeneralErrorType)
	}
	if !canView {
		return SendErrorWithType(c, fiber.StatusForbidden, "You do not have access to this dashboard", models.AuthorizationErrorType)
	}
	return s.sendDashboard(c, fiber.StatusOK, user, client, dashboard)
}

// handleUpdateDashboard updates a dashboard. Editing is allowed only for the
// creator or a global admin; the new panels are additionally checked for
// team/source existence and (for non-admins) team membership (B2/B4). The body
// must carry the revision the client loaded: without it the answer is 428,
// and when another save already moved the revision it is 409. A save that
// would drop state its client does not know is 409 as well.
func (s *Server) handleUpdateDashboard(c fiber.Ctx) error {
	user := currentUser(c)
	client, err := dashboardClient(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	id, err := parseDashboardID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	var req models.UpdateDashboardRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	updated, updateErr := core.UpdateDashboard(c.Context(), s.store, s.log, id, user, client, &req)
	if updateErr != nil {
		var stale *core.DashboardClientStaleError
		switch {
		case errors.Is(updateErr, core.ErrInvalidDashboard):
			return SendErrorWithType(c, fiber.StatusBadRequest, updateErr.Error(), models.ValidationErrorType)
		case errors.Is(updateErr, core.ErrDashboardForbidden):
			return SendErrorWithType(c, fiber.StatusForbidden, updateErr.Error(), models.AuthorizationErrorType)
		case errors.Is(updateErr, core.ErrDashboardRevisionRequired):
			return SendErrorWithType(c, fiber.StatusPreconditionRequired, "This save does not say which revision of the dashboard it changes. Reload the page and apply your changes again.", models.ValidationErrorType)
		case errors.As(updateErr, &stale):
			return SendErrorWithType(c, fiber.StatusConflict, fmt.Sprintf("A newer Logchef saved this dashboard, and %s. Reload the page to continue.", stale.Detail), models.ConflictErrorType)
		case errors.Is(updateErr, core.ErrDashboardConflict):
			return SendErrorWithType(c, fiber.StatusConflict, "Dashboard was modified by someone else; reload and reapply your changes", models.ConflictErrorType)
		case errors.Is(updateErr, core.ErrDashboardNotFound):
			return SendErrorWithType(c, fiber.StatusNotFound, "Dashboard not found", models.NotFoundErrorType)
		default:
			s.log.Error("failed to update dashboard", "dashboard_id", id, "error", updateErr)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update dashboard", models.GeneralErrorType)
		}
	}
	return s.sendDashboard(c, fiber.StatusOK, user, client, updated)
}

// handleDeleteDashboard removes a dashboard (creator + global admin only).
func (s *Server) handleDeleteDashboard(c fiber.Ctx) error {
	user := currentUser(c)

	id, err := parseDashboardID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	existing, err := core.GetDashboard(c.Context(), s.store, s.log, id)
	if err != nil {
		if errors.Is(err, core.ErrDashboardNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Dashboard not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load dashboard", models.GeneralErrorType)
	}
	if !core.UserCanEditDashboard(existing, user) {
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or a global admin can delete this dashboard", models.AuthorizationErrorType)
	}

	if delErr := core.DeleteDashboard(c.Context(), s.store, s.log, id); delErr != nil {
		if errors.Is(delErr, core.ErrDashboardNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Dashboard not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete dashboard", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Dashboard deleted"})
}
