package server

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/audit"
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

// schemaAdviceScheduleBody is the body and the result of a schedule change.
type schemaAdviceScheduleBody struct {
	Enabled *bool `json:"enabled"`
}

// handleSetSchemaAdviceSchedule turns the weekly refresh of a source's
// report on or off.
// URL: PUT /api/v1/admin/sources/:sourceID/advisor/schedule
func (s *Server) handleSetSchemaAdviceSchedule(c fiber.Ctx) error {
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID", models.ValidationErrorType)
	}
	var req schemaAdviceScheduleBody
	if err := c.Bind().Body(&req); err != nil || req.Enabled == nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Request body must be {\"enabled\": true or false}", models.ValidationErrorType)
	}
	p := principalFromLocals(c)
	before, err := core.SetSchemaAdviceSchedule(c.Context(), s.store, s.datasources, p, sourceID, *req.Enabled)
	if err != nil {
		return s.sendSchemaAdviceError(c, p, sourceID, err)
	}
	auditSet(c, audit.SourceIDTarget(sourceID), audit.SchemaAdvisorScheduleFields(before), audit.SchemaAdvisorScheduleFields(*req.Enabled))
	return SendSuccess(c, fiber.StatusOK, req)
}

// schemaFindingDismissalRequest is the optional body of a dismissal.
type schemaFindingDismissalRequest struct {
	Reason string `json:"reason"`
}

// handleDismissSchemaFinding dismisses a finding of the stored report.
// URL: PUT /api/v1/admin/sources/:sourceID/advisor/findings/:findingID/dismissal
func (s *Server) handleDismissSchemaFinding(c fiber.Ctx) error {
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID", models.ValidationErrorType)
	}
	var req schemaFindingDismissalRequest
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
		}
	}
	p := principalFromLocals(c)
	before, after, rule, err := core.DismissSchemaFinding(c.Context(), s.store, s.datasources, p, sourceID, c.Params("findingID"), req.Reason)
	if err != nil {
		return s.sendSchemaAdviceError(c, p, sourceID, err)
	}
	var beforeFields *audit.Fields
	if before != nil {
		beforeFields = audit.SchemaAdvisorDismissalFields(before)
	}
	auditSet(c, audit.SourceIDTarget(sourceID), beforeFields, audit.SchemaAdvisorDismissalFields(after))
	auditDetail(c, "finding_id", after.FindingID)
	auditDetail(c, "rule", rule)
	return SendSuccess(c, fiber.StatusOK, after)
}

// handleRestoreSchemaFinding deletes the dismissal of a finding.
// URL: DELETE /api/v1/admin/sources/:sourceID/advisor/findings/:findingID/dismissal
func (s *Server) handleRestoreSchemaFinding(c fiber.Ctx) error {
	sourceID, err := core.ParseSourceID(c.Params("sourceID"))
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source ID", models.ValidationErrorType)
	}
	p := principalFromLocals(c)
	dismissal, err := core.RestoreSchemaFinding(c.Context(), s.store, s.datasources, p, sourceID, c.Params("findingID"))
	if err != nil {
		return s.sendSchemaAdviceError(c, p, sourceID, err)
	}
	auditSet(c, audit.SourceIDTarget(sourceID), audit.SchemaAdvisorDismissalFields(dismissal), nil)
	auditDetail(c, "finding_id", dismissal.FindingID)
	return c.SendStatus(fiber.StatusNoContent)
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
	case errors.Is(err, core.ErrSchemaFindingNotFound):
		return SendErrorWithType(c, fiber.StatusNotFound, "Finding not found", models.NotFoundErrorType)
	case errors.Is(err, datasource.ErrSchemaAdvisorBusy):
		return SendErrorWithType(c, fiber.StatusTooManyRequests, "Two schema analyses are already running, try again shortly", models.ValidationErrorType)
	case errors.Is(err, core.ErrSchemaAdviceRunFailed):
		s.log.Warn("schema analysis failed", "source_id", sourceID, "error", err)
		return SendErrorWithType(c, fiber.StatusBadGateway, "ClickHouse could not be analyzed; the stored report is kept", models.ExternalServiceErrorType)
	}
	s.log.Error("schema advice failed", "source_id", sourceID, "error", err)
	return SendErrorWithType(c, fiber.StatusInternalServerError, "Error reading schema advice", models.DatabaseErrorType)
}
