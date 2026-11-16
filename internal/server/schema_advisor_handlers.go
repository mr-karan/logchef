package server

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// schemaAdviceRefreshRequest is the optional body of a refresh.
type schemaAdviceRefreshRequest struct {
	WindowDays *int `json:"window_days"`
}

// handleGetSchemaAdvice returns the stored schema advisor report of a source.
// URL: GET /api/v1/admin/sources/:sourceID/advisor
func (s *Server) handleGetSchemaAdvice(c fiber.Ctx) error {
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID", models.ValidationErrorType)
	}
	p := principalFromLocals(c)
	advice, err := core.GetSchemaAdvice(c.Context(), s.store, s.datasources, p, sourceID)
	if err != nil {
		return s.sendSchemaAdviceError(c, p, sourceID, err)
	}
	return SendSuccess(c, fiber.StatusOK, advice)
}

// handleRefreshSchemaAdvice analyzes a source and returns the new report.
// URL: POST /api/v1/admin/sources/:sourceID/advisor/refresh
func (s *Server) handleRefreshSchemaAdvice(c fiber.Ctx) error {
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID", models.ValidationErrorType)
	}
	var req schemaAdviceRefreshRequest
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
		}
	}
	windowDays := core.DefaultSchemaAdviceWindowDays
	if req.WindowDays != nil {
		windowDays = *req.WindowDays
	}
	p := principalFromLocals(c)
	advice, err := core.RefreshSchemaAdvice(c.Context(), s.store, s.datasources, p, sourceID, windowDays)
	if err != nil {
		return s.sendSchemaAdviceError(c, p, sourceID, err)
	}
	return SendSuccess(c, fiber.StatusOK, advice)
}

func (s *Server) sendSchemaAdviceError(c fiber.Ctx, p access.Principal, sourceID models.SourceID, err error) error {
	if validationErr, ok := errors.AsType[*core.ValidationError](err); ok {
		return SendErrorWithType(c, fiber.StatusBadRequest, validationErr.Message, models.ValidationErrorType)
	}
	switch {
	case errors.Is(err, access.ErrGlobalAdminRequired):
		return SendErrorWithType(c, fiber.StatusForbidden, "Admin access required", models.AuthorizationErrorType)
	case errors.Is(err, access.ErrInsufficientScope):
		return sendInsufficientScope(c, p.User)
	case errors.Is(err, core.ErrSourceNotFound):
		return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
	case errors.Is(err, datasource.ErrSchemaAdvisorBusy):
		return SendErrorWithType(c, fiber.StatusTooManyRequests, "Two schema analyses are already running, try again shortly", models.ValidationErrorType)
	case errors.Is(err, core.ErrSchemaAdviceRunFailed):
		s.log.Warn("schema analysis failed", "source_id", sourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusBadGateway, "ClickHouse could not be analyzed; the stored report is kept", models.ExternalServiceErrorType)
	}
	s.log.Error("schema advice failed", "source_id", sourceID, "error", err)
	return SendErrorWithType(c, fiber.StatusInternalServerError, "Error reading schema advice", models.DatabaseErrorType)
}
