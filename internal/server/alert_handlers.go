package server

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

func (s *Server) handleListAlerts(c *fiber.Ctx) error {
	teamID, sourceID, err := s.parseTeamAndSourceIDs(c)
	if err != nil {
		return err
	}

	alerts, err := core.ListAlertsByTeamSource(c.Context(), s.sqlite, teamID, sourceID)
	if err != nil {
		s.log.Error("failed to list alerts", "team_id", teamID, "source_id", sourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list alerts", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, alerts)
}

func (s *Server) handleCreateAlert(c *fiber.Ctx) error {
	teamID, sourceID, err := s.parseTeamAndSourceIDs(c)
	if err != nil {
		return err
	}

	var req models.CreateAlertRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	if req.QueryType == "" {
		req.QueryType = models.AlertQueryTypeSQL
	}
	if req.LookbackSeconds <= 0 {
		req.LookbackSeconds = int(s.config.Alerts.DefaultLookback.Seconds())
	}

	alert, err := core.CreateAlert(c.Context(), s.sqlite, s.log, teamID, sourceID, &req)
	if err != nil {
		if errors.Is(err, core.ErrInvalidAlertConfiguration) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		s.log.Error("failed to create alert", "team_id", teamID, "source_id", sourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create alert", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, alert)
}

func (s *Server) handleGetAlert(c *fiber.Ctx) error {
	teamID, sourceID, alertID, err := s.parseAlertIdentifiers(c)
	if err != nil {
		return err
	}

	alert, err := s.sqlite.GetAlertForTeamSource(c.Context(), teamID, sourceID, alertID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Alert not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get alert", "alert_id", alertID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to retrieve alert", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, alert)
}

func (s *Server) handleUpdateAlert(c *fiber.Ctx) error {
	teamID, sourceID, alertID, err := s.parseAlertIdentifiers(c)
	if err != nil {
		return err
	}

	var req models.UpdateAlertRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	updated, err := core.UpdateAlert(c.Context(), s.sqlite, s.log, teamID, sourceID, alertID, &req)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrInvalidAlertConfiguration):
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		case errors.Is(err, core.ErrAlertNotFound):
			return SendErrorWithType(c, fiber.StatusNotFound, "Alert not found", models.NotFoundErrorType)
		default:
			s.log.Error("failed to update alert", "alert_id", alertID, "error", err)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update alert", models.GeneralErrorType)
		}
	}
	return SendSuccess(c, fiber.StatusOK, updated)
}

func (s *Server) handleDeleteAlert(c *fiber.Ctx) error {
	teamID, sourceID, alertID, err := s.parseAlertIdentifiers(c)
	if err != nil {
		return err
	}

	if err := core.DeleteAlert(c.Context(), s.sqlite, s.log, teamID, sourceID, alertID); err != nil {
		if errors.Is(err, core.ErrAlertNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Alert not found", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete alert", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Alert deleted"})
}

func (s *Server) handleResolveAlert(c *fiber.Ctx) error {
	teamID, sourceID, alertID, err := s.parseAlertIdentifiers(c)
	if err != nil {
		return err
	}

	if _, err := s.sqlite.GetAlertForTeamSource(c.Context(), teamID, sourceID, alertID); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Alert not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to verify alert ownership", "alert_id", alertID, "team_id", teamID, "source_id", sourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to resolve alert", models.GeneralErrorType)
	}

	var req models.ResolveAlertRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	if err := core.ResolveAlert(c.Context(), s.sqlite, s.log, alertID, strings.TrimSpace(req.Message)); err != nil {
		if errors.Is(err, core.ErrAlertNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Alert is not active", models.NotFoundErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to resolve alert", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Alert resolved"})
}

func (s *Server) handleListAlertHistory(c *fiber.Ctx) error {
	teamID, sourceID, alertID, err := s.parseAlertIdentifiers(c)
	if err != nil {
		return err
	}

	if _, err := s.sqlite.GetAlertForTeamSource(c.Context(), teamID, sourceID, alertID); err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Alert not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to verify alert ownership", "alert_id", alertID, "team_id", teamID, "source_id", sourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list alert history", models.GeneralErrorType)
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

	history, err := core.ListAlertHistory(c.Context(), s.sqlite, alertID, limit)
	if err != nil {
		s.log.Error("failed to list alert history", "alert_id", alertID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list alert history", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, history)
}

func (s *Server) handleTestAlertQuery(c *fiber.Ctx) error {
	teamID, sourceID, err := s.parseTeamAndSourceIDs(c)
	if err != nil {
		return err
	}

	var req models.TestAlertQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	if req.QueryType == "" {
		req.QueryType = models.AlertQueryTypeSQL
	}
	if req.LookbackSeconds <= 0 {
		req.LookbackSeconds = int(s.config.Alerts.DefaultLookback.Seconds())
	}

	result, err := core.TestAlertQuery(c.Context(), s.sqlite, s.clickhouse, s.log, teamID, sourceID, &req)
	if err != nil {
		s.log.Error("failed to test alert query", "team_id", teamID, "source_id", sourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, err.Error(), models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

func (s *Server) parseTeamAndSourceIDs(c *fiber.Ctx) (models.TeamID, models.SourceID, error) {
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return 0, 0, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return 0, 0, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
	}
	return teamID, sourceID, nil
}

func (s *Server) parseAlertIdentifiers(c *fiber.Ctx) (models.TeamID, models.SourceID, models.AlertID, error) {
	teamID, sourceID, err := s.parseTeamAndSourceIDs(c)
	if err != nil {
		return 0, 0, 0, err
	}

	alertIDStr := c.Params("alertID")
	if alertIDStr == "" {
		return 0, 0, 0, SendErrorWithType(c, fiber.StatusBadRequest, "Alert ID is required", models.ValidationErrorType)
	}
	parsedID, err := strconv.ParseInt(alertIDStr, 10, 64)
	if err != nil {
		return 0, 0, 0, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid alert ID", models.ValidationErrorType)
	}
	return teamID, sourceID, models.AlertID(parsedID), nil
}
