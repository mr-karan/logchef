package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v3"
)

// TestAlertTimeout is the maximum time to wait for an ad-hoc "test alert
// query" run against the configured datasource before aborting. This is
// separate from the 90s deadline the scheduled alert evaluator applies to
// EvaluateAlert.
const TestAlertTimeout = 30 * time.Second

// requireAlertsEnabled is route-group middleware that short-circuits with 503
// when the alerts subsystem is disabled in config, and otherwise passes the
// request through. The flag is read from the config snapshot at request time;
// toggling alerts.enabled requires a server restart. Mounted once on the
// /alerts group so every alert endpoint is gated in a single place rather than
// each handler re-checking the flag.
func (s *Server) requireAlertsEnabled(c fiber.Ctx) error {
	if !s.config.Alerts.Enabled {
		return SendErrorWithType(c, http.StatusServiceUnavailable,
			"Alerting is disabled on this server. Set alerts.enabled = true (or LOGCHEF_ALERTS__ENABLED=true) and restart to enable.",
			models.GeneralErrorType)
	}
	return c.Next()
}

func parseAlertID(c fiber.Ctx) (models.AlertID, error) {
	id, err := parsePositiveIntParam(c, "alertID")
	return models.AlertID(id), err
}

// loadAlertWithVisibility fetches an alert and verifies the caller has source
// access via any team. On failure it writes the error response and returns
// false; the caller must then return nil.
func (s *Server) loadAlertWithVisibility(c fiber.Ctx) (*models.Alert, *models.User, bool) {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		s.sendLoadFailure(c, fiber.StatusUnauthorized, "Authentication context missing", models.AuthenticationErrorType)
		return nil, nil, false
	}

	alertID, err := parseAlertID(c)
	if err != nil {
		s.sendLoadFailure(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		return nil, nil, false
	}

	alert, err := core.GetAlertForPrincipal(c.RequestCtx(), s.sqlite, s.log, principalFromLocals(c), alertID)
	if err != nil {
		if errors.Is(err, core.ErrAlertNotFound) || models.IsNotFound(err) {
			s.sendLoadFailure(c, fiber.StatusNotFound, "Alert not found", models.NotFoundErrorType)
			return nil, nil, false
		}
		if errors.Is(err, core.ErrAccessCheck) {
			s.log.Error("failed to check source access for alert", "error", err, "user_id", user.ID, "alert_id", alertID)
			s.sendLoadFailure(c, fiber.StatusInternalServerError, "Failed to verify access", models.GeneralErrorType)
			return nil, nil, false
		}
		s.log.Error("failed to load alert", "error", err, "alert_id", alertID)
		s.sendLoadFailure(c, fiber.StatusInternalServerError, "Failed to load alert", models.GeneralErrorType)
		return nil, nil, false
	}

	return alert, user, true
}

// handleListAlerts lists alerts the caller can see. Optional ?source_id filter.
func (s *Server) handleListAlerts(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	if sourceParam := c.Query("source_id"); sourceParam != "" {
		sourceID, err := core.ParseSourceID(sourceParam)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
		}
		alerts, err := core.ListAlertsBySourceForPrincipal(c.RequestCtx(), s.sqlite, principalFromLocals(c), sourceID)
		if err != nil {
			if errors.Is(err, core.ErrSourceAccessDenied) {
				return SendErrorWithType(c, fiber.StatusForbidden, "No team you belong to has access to this source", models.AuthorizationErrorType)
			}
			if errors.Is(err, core.ErrAccessCheck) {
				return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify access", models.GeneralErrorType)
			}
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list alerts", models.GeneralErrorType)
		}
		return SendSuccess(c, fiber.StatusOK, alerts)
	}

	alerts, err := core.ListAlertsForUser(c.RequestCtx(), s.sqlite, user.ID)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list alerts", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, alerts)
}

// handleCreateAlert creates a new alert against the source in the request body.
// The caller must have source access; the resulting alert is owned by the caller.
func (s *Server) handleCreateAlert(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	var req models.CreateAlertRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	if req.SourceID == 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "source_id is required", models.ValidationErrorType)
	}
	if req.LookbackSeconds <= 0 {
		req.LookbackSeconds = int(s.config.Alerts.DefaultLookback.Seconds())
	}

	hasAccess, err := s.sqlite.UserHasSourceAccess(c.RequestCtx(), user.ID, req.SourceID)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify access", models.GeneralErrorType)
	}
	if !hasAccess {
		return SendErrorWithType(c, fiber.StatusForbidden, "No team you belong to has access to this source", models.AuthorizationErrorType)
	}

	alert, err := core.CreateAlert(c.RequestCtx(), s.sqlite, s.datasources, s.log, req.SourceID, user.ID, &req)
	if err != nil {
		if errors.Is(err, core.ErrInvalidAlertConfiguration) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		s.log.Error("failed to create alert", "source_id", req.SourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create alert", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, alert)
}

// handleGetAlert returns a single alert.
func (s *Server) handleGetAlert(c fiber.Ctx) error {
	alert, _, ok := s.loadAlertWithVisibility(c)
	if !ok {
		return nil
	}
	return SendSuccess(c, fiber.StatusOK, alert)
}

// handleUpdateAlert updates an alert. Allowed only for the creator or a global admin.
func (s *Server) handleUpdateAlert(c fiber.Ctx) error {
	alert, user, ok := s.loadAlertWithVisibility(c)
	if !ok {
		return nil
	}
	if !core.UserCanEditAlert(alert, user) {
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or a global admin can edit this alert", models.AuthorizationErrorType)
	}

	var req models.UpdateAlertRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	updated, updateErr := core.UpdateAlert(c.RequestCtx(), s.sqlite, s.datasources, s.log, alert.ID, &req)
	if updateErr != nil {
		switch {
		case errors.Is(updateErr, core.ErrInvalidAlertConfiguration):
			return SendErrorWithType(c, fiber.StatusBadRequest, updateErr.Error(), models.ValidationErrorType)
		case errors.Is(updateErr, core.ErrAlertNotFound):
			return SendErrorWithType(c, fiber.StatusNotFound, "Alert not found", models.NotFoundErrorType)
		default:
			s.log.Error("failed to update alert", "alert_id", alert.ID, "error", updateErr)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update alert", models.GeneralErrorType)
		}
	}
	return SendSuccess(c, fiber.StatusOK, updated)
}

// handleDeleteAlert removes an alert (creator + global admin only).
func (s *Server) handleDeleteAlert(c fiber.Ctx) error {
	alert, user, ok := s.loadAlertWithVisibility(c)
	if !ok {
		return nil
	}
	if !core.UserCanEditAlert(alert, user) {
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or a global admin can delete this alert", models.AuthorizationErrorType)
	}

	if delErr := core.DeleteAlert(c.RequestCtx(), s.sqlite, s.log, alert.ID); delErr != nil {
		if errors.Is(delErr, core.ErrAlertNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Alert not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete alert", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Alert deleted"})
}

// handleResolveAlert manually resolves the most recent triggered history entry.
func (s *Server) handleResolveAlert(c fiber.Ctx) error {
	alert, user, ok := s.loadAlertWithVisibility(c)
	if !ok {
		return nil
	}
	if !core.UserCanEditAlert(alert, user) {
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or a global admin can resolve this alert", models.AuthorizationErrorType)
	}

	var req models.ResolveAlertRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	if s.alertsManager != nil {
		if err := s.alertsManager.ManualResolve(c.RequestCtx(), alert.ID, strings.TrimSpace(req.Message)); err != nil {
			if strings.Contains(err.Error(), "no active alert") {
				return SendErrorWithType(c, fiber.StatusNotFound, "Alert is not active", models.NotFoundErrorType)
			}
			s.log.Error("failed to manually resolve alert", "alert_id", alert.ID, "error", err)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to resolve alert", models.GeneralErrorType)
		}
	} else {
		if err := core.ResolveAlert(c.RequestCtx(), s.sqlite, s.log, alert.ID, strings.TrimSpace(req.Message)); err != nil {
			if errors.Is(err, core.ErrAlertNotFound) {
				return SendErrorWithType(c, fiber.StatusNotFound, "Alert is not active", models.NotFoundErrorType)
			}
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to resolve alert", models.GeneralErrorType)
		}
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Alert resolved"})
}

// handleListAlertHistory returns recent history entries for an alert.
func (s *Server) handleListAlertHistory(c fiber.Ctx) error {
	alert, _, ok := s.loadAlertWithVisibility(c)
	if !ok {
		return nil
	}

	limit := s.config.Alerts.HistoryLimit
	if limit <= 0 {
		limit = models.DefaultAlertHistoryLimit
	}
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			if parsed < limit {
				limit = parsed
			}
		}
	}

	history, err := core.ListAlertHistory(c.RequestCtx(), s.sqlite, alert.ID, limit)
	if err != nil {
		s.log.Error("failed to list alert history", "alert_id", alert.ID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list alert history", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, history)
}

// handleTestAlertQuery executes a test query against the source in the request body.
func (s *Server) handleTestAlertQuery(c fiber.Ctx) error {
	var req struct {
		SourceID models.SourceID `json:"source_id"`
		models.TestAlertQueryRequest
	}
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if req.SourceID == 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "source_id is required", models.ValidationErrorType)
	}
	if req.LookbackSeconds <= 0 {
		req.LookbackSeconds = int(s.config.Alerts.DefaultLookback.Seconds())
	}

	// Bound by TestAlertTimeout so a slow/misbehaving datasource can't hang
	// the request indefinitely.
	ctx, cancel := context.WithTimeout(c.RequestCtx(), TestAlertTimeout)
	defer cancel()

	p := principalFromLocals(c)
	result, err := core.TestAlertQueryForPrincipal(ctx, s.sqlite, s.datasources, p, req.SourceID, &req.TestAlertQueryRequest)
	if err != nil {
		// The source check runs before the query, so its errors are mapped
		// before the timeout checks, exactly as when the handler did it.
		if errors.Is(err, core.ErrSourceAccessDenied) {
			return SendErrorWithType(c, fiber.StatusForbidden, "No team you belong to has access to this source", models.AuthorizationErrorType)
		}
		if errors.Is(err, core.ErrAccessCheck) {
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify access", models.GeneralErrorType)
		}
		if errors.Is(err, access.ErrInsufficientScope) {
			return sendInsufficientScope(c, p.User)
		}
		if ctx.Err() == context.Canceled {
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request cancelled", models.ExternalServiceErrorType)
		}
		if ctx.Err() == context.DeadlineExceeded {
			s.log.Warn("test alert query timed out", "source_id", req.SourceID, "timeout", TestAlertTimeout)
			return SendErrorWithType(c, fiber.StatusRequestTimeout, "Request timed out", models.ExternalServiceErrorType)
		}
		if errors.Is(err, datasource.ErrOperationNotSupported) {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Alert evaluation is not supported for this source type yet", models.ValidationErrorType)
		}
		if errors.Is(err, core.ErrInvalidAlertConfiguration) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		s.log.Error("failed to test alert query", "source_id", req.SourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, err.Error(), models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}
