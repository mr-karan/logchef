package server

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

// sendSilenceError maps a core silence error to its status.
func (s *Server) sendSilenceError(c fiber.Ctx, err error, action string) error {
	switch {
	case errors.Is(err, core.ErrInvalidSilence):
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	case errors.Is(err, access.ErrInsufficientScope):
		return SendErrorWithType(c, fiber.StatusForbidden, "This token lacks the alerts:write scope", models.AuthorizationErrorType)
	case errors.Is(err, core.ErrSourceAccessDenied):
		return SendErrorWithType(c, fiber.StatusForbidden, err.Error(), models.AuthorizationErrorType)
	case errors.Is(err, core.ErrSilenceForbidden):
		return SendErrorWithType(c, fiber.StatusForbidden, err.Error(), models.AuthorizationErrorType)
	case errors.Is(err, core.ErrSilenceNotFound):
		return SendErrorWithType(c, fiber.StatusNotFound, "Alert silence not found", models.NotFoundErrorType)
	default:
		s.log.Error("alert silence request failed", "action", action, "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to "+action+" alert silence", models.GeneralErrorType)
	}
}

func parseSilenceID(c fiber.Ctx) (models.AlertSilenceID, error) {
	id, err := parsePositiveIntParam(c, "silenceID")
	return models.AlertSilenceID(id), err
}

// handleListSilences lists the silences of one source, with active computed.
func (s *Server) handleListSilences(c fiber.Ctx) error {
	raw := c.Query("source_id")
	if raw == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "source_id is required", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(raw)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
	}
	list, err := core.ListSilencesForPrincipal(c.Context(), s.store, s.log, principalFromLocals(c), sourceID, time.Now())
	if err != nil {
		return s.sendSilenceError(c, err, "list")
	}
	return SendSuccess(c, fiber.StatusOK, list)
}

func (s *Server) handleCreateSilence(c fiber.Ctx) error {
	var req models.AlertSilenceRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	silence, err := core.CreateSilenceForPrincipal(c.Context(), s.store, s.log, principalFromLocals(c), &req, time.Now())
	if err != nil {
		return s.sendSilenceError(c, err, "create")
	}
	auditSet(c, audit.AlertSilenceTarget(silence), nil, audit.AlertSilenceFields(silence))
	return SendSuccess(c, fiber.StatusCreated, silence)
}

func (s *Server) handleUpdateSilence(c fiber.Ctx) error {
	id, err := parseSilenceID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	var req models.AlertSilenceRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	before, after, err := core.UpdateSilenceForPrincipal(c.Context(), s.store, s.log, principalFromLocals(c), id, &req, time.Now())
	if err != nil {
		return s.sendSilenceError(c, err, "update")
	}
	auditSet(c, audit.AlertSilenceTarget(after), audit.AlertSilenceFields(before), audit.AlertSilenceFields(after))
	return SendSuccess(c, fiber.StatusOK, after)
}

func (s *Server) handleDeleteSilence(c fiber.Ctx) error {
	id, err := parseSilenceID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	silence, err := core.DeleteSilenceForPrincipal(c.Context(), s.store, principalFromLocals(c), id)
	if err != nil {
		return s.sendSilenceError(c, err, "delete")
	}
	auditSet(c, audit.AlertSilenceTarget(silence), audit.AlertSilenceFields(silence), nil)
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Alert silence deleted"})
}
