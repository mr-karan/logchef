package server

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// --- Admin Source Management Handlers ---

// handleListSources is an admin-only endpoint to list all configured sources.
// URL: GET /api/v1/admin/sources
// Requires: Admin privileges
func (s *Server) handleListSources(c *fiber.Ctx) error {
	sources, err := core.ListSources(c.Context(), s.sqlite, s.backendRegistry, s.log)
	if err != nil {
		s.log.Error("failed to list sources", "error", err)
		return SendError(c, fiber.StatusInternalServerError, "Error listing sources")
	}

	sourceResponses := make([]*models.SourceResponse, len(sources))
	for i, src := range sources {
		sourceResponses[i] = src.ToResponse()
	}

	return SendSuccess(c, fiber.StatusOK, sourceResponses)
}

// handleCreateSource creates a new data source.
// URL: POST /api/v1/admin/sources
// Requires: Admin privileges
func (s *Server) handleCreateSource(c *fiber.Ctx) error {
	var req models.CreateSourceRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	if req.MetaTSField == "" {
		if req.BackendType == models.BackendVictoriaLogs {
			req.MetaTSField = "_time"
		} else {
			req.MetaTSField = "timestamp"
		}
	}

	var createdSource *models.Source
	var err error

	if req.BackendType == models.BackendVictoriaLogs {
		createdSource, err = core.CreateVictoriaLogsSource(
			c.Context(),
			s.sqlite,
			s.backendRegistry,
			s.log,
			req.Name,
			req.VictoriaLogsConnection,
			req.Description,
			req.MetaTSField,
			req.MetaSeverityField,
		)
	} else {
		createdSource, err = core.CreateSource(
			c.Context(),
			s.sqlite,
			s.clickhouse,
			s.log,
			req.Name,
			req.MetaIsAutoCreated,
			req.Connection,
			req.Description,
			req.TTLDays,
			req.MetaTSField,
			req.MetaSeverityField,
			req.Schema,
		)
	}

	if err != nil {
		if validationErr, ok := err.(*core.ValidationError); ok {
			return SendErrorWithType(c, fiber.StatusBadRequest, validationErr.Error(), models.ValidationErrorType)
		}
		if errors.Is(err, core.ErrSourceAlreadyExists) {
			return SendErrorWithType(c, fiber.StatusConflict, err.Error(), models.ConflictErrorType)
		}

		s.log.Error("failed to create source", "error", err)
		return SendErrorWithType(c, fiber.StatusInternalServerError, fmt.Sprintf("Error creating source: %v", err), models.DatabaseErrorType)
	}

	return SendSuccess(c, fiber.StatusCreated, createdSource.ToResponse())
}

// handleDeleteSource deletes a data source.
// URL: DELETE /api/v1/admin/sources/:sourceID
// Requires: Admin privileges
func (s *Server) handleDeleteSource(c *fiber.Ctx) error {
	sourceIDStr := c.Params("sourceID")
	if sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Source ID is required")
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	if err := core.DeleteSource(c.Context(), s.sqlite, s.backendRegistry, s.log, sourceID); err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "Source not found")
		}
		s.log.Error("failed to delete source", "error", err, "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Error deleting source: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Source deleted successfully"})
}

func (s *Server) handleValidateSourceConnection(c *fiber.Ctx) error {
	var req models.ValidateConnectionRequest
	if err := c.BodyParser(&req); err != nil {
		s.log.Warn("invalid connection validation request", "error", err)
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	var result *models.ConnectionValidationResult
	var coreErr error

	if req.BackendType == models.BackendVictoriaLogs {
		result, coreErr = core.ValidateVictoriaLogsConnection(
			c.Context(), s.backendRegistry, s.log, req.VictoriaLogsConnection,
		)
	} else {
		if req.TimestampField != "" {
			result, coreErr = core.ValidateConnectionWithColumns(
				c.Context(), s.clickhouse, s.log, req.ConnectionInfo,
				req.TimestampField, req.SeverityField,
			)
		} else {
			result, coreErr = core.ValidateConnection(
				c.Context(), s.clickhouse, s.log, req.ConnectionInfo,
			)
		}
	}

	if coreErr != nil {
		if validationErr, ok := coreErr.(*core.ValidationError); ok {
			s.log.Warn("connection validation failed", "error", validationErr.Message, "field", validationErr.Field)
			return SendErrorWithType(c, fiber.StatusBadRequest, validationErr.Error(), models.ValidationErrorType)
		}
		s.log.Error("connection validation error", "error", coreErr)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error validating connection: "+coreErr.Error(), models.ExternalServiceErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, result)
}

// --- User Source Access Handlers ---

// handleGetSourceStats retrieves table and column statistics for a specific source.
// URL: GET /api/v1/sources/:sourceID/stats
// Requires: User must have access to the source via team membership (checked by requireSourceAccess middleware).
func (s *Server) handleGetSourceStats(c *fiber.Ctx) error {
	// Source ID access validated by middleware.
	sourceIDStr := c.Params("sourceID")
	if sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Source ID is required")
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	// Get the source model first (needed by GetSourceStats).
	src, err := s.sqlite.GetSource(c.Context(), sourceID)
	if err != nil {
		if errors.Is(err, models.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			return SendError(c, fiber.StatusNotFound, "Source not found")
		}
		s.log.Error("failed to get source", "error", err, "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Error getting source details")
	}

	// Get stats using the core function.
	stats, err := core.GetSourceStats(c.Context(), s.clickhouse, s.log, src)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "Source not found when getting stats")
		}
		s.log.Error("failed to get source stats", "error", err, "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Error getting source stats: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, stats)
}

// handleGetTeamSourceStats handles GET /teams/:teamID/sources/:sourceID/stats
// Returns statistics for a specific source in the context of a team
func (s *Server) handleGetTeamSourceStats(c *fiber.Ctx) error {
	// We've already verified that:
	// 1. The user is a member of the team
	// 2. The team has access to the source

	// Extract source ID which is the only parameter we need for stats
	sourceIDStr := c.Params("sourceID")

	// Simply reuse the existing source stats handler
	// This is permissible because we've already verified all access controls
	_, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid source ID: "+err.Error())
	}

	// Get stats using the current implementation
	return s.handleGetSourceStats(c)
}
