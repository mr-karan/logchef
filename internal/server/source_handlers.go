package server

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// --- Admin Source Management Handlers ---

// handleListSources is an admin-only endpoint to list all configured sources.
// URL: GET /api/v1/admin/sources
// Requires: Admin privileges
func (s *Server) handleListSources(c *fiber.Ctx) error {
	// Use core function, passing required dependencies.
	sources, err := core.ListSources(c.Context(), s.sqlite, s.clickhouse, s.log)
	if err != nil {
		s.log.Error("failed to list sources via core function", slog.Any("error", err))
		return SendError(c, fiber.StatusInternalServerError, "Error listing sources")
	}

	s.log.Info("sources retrieved for admin list", "count", len(sources))

	// Log connection status of each source
	for _, src := range sources {
		s.log.Info("source connection status",
			"source_id", src.ID,
			"name", src.Name,
			"database", src.Connection.Database,
			"table", src.Connection.TableName,
			"is_connected", src.IsConnected)
	}

	// Convert sources to response objects to avoid exposing sensitive information.
	sourceResponses := make([]*models.SourceResponse, len(sources))
	for i, src := range sources {
		sourceResponses[i] = src.ToResponse()
		s.log.Debug("source response details",
			"source_id", src.ID,
			"name", src.Name,
			"is_connected", sourceResponses[i].IsConnected)
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

	// Set default timestamp field if not provided by user.
	if req.MetaTSField == "" {
		req.MetaTSField = "timestamp"
	}
	// Severity field is optional.

	// Call core function to validate, create table (if requested), save, and connect.
	createdSource, err := core.CreateSource(
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
	if err != nil {
		// Handle specific validation or creation errors from core.
		if validationErr, ok := err.(*core.ValidationError); ok {
			return SendErrorWithType(c, fiber.StatusBadRequest, validationErr.Error(), models.ValidationErrorType)
		}
		if errors.Is(err, core.ErrSourceAlreadyExists) {
			return SendErrorWithType(c, fiber.StatusConflict, err.Error(), models.ConflictErrorType)
		}

		s.log.Error("failed to create source via core function", slog.Any("error", err))
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

	// Call core function to remove from manager and delete from DB.
	if err := core.DeleteSource(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID); err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "Source not found")
		}
		s.log.Error("failed to delete source via core function", slog.Any("error", err), "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Error deleting source: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Source deleted successfully"})
}

// handleValidateSourceConnection validates ClickHouse connection details provided in the request body.
// URL: POST /api/v1/admin/sources/validate
// Requires: Admin privileges
func (s *Server) handleValidateSourceConnection(c *fiber.Ctx) error {
	var req models.ValidateConnectionRequest
	if err := c.BodyParser(&req); err != nil {
		s.log.Error("invalid request body for connection validation", "error", err)
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	s.log.Debug("validating connection request",
		"host", req.Host,
		"database", req.Database,
		"table", req.TableName,
		"timestamp_field", req.TimestampField,
		"severity_field", req.SeverityField,
	)

	var result *models.ConnectionValidationResult
	var coreErr error

	// Choose validation type based on whether column checks are requested.
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

	if coreErr != nil {
		// Handle specific validation errors.
		if validationErr, ok := coreErr.(*core.ValidationError); ok {
			s.log.Warn("connection validation failed", "error", validationErr.Message, "field", validationErr.Field)
			return SendErrorWithType(c, fiber.StatusBadRequest, validationErr.Error(), models.ValidationErrorType)
		}
		// Log other unexpected errors.
		s.log.Error("error validating connection via core function", "error", coreErr, "host", req.Host)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error validating connection: "+coreErr.Error(), models.ExternalServiceErrorType)
	}

	s.log.Debug("connection validation successful", "message", result.Message, "host", req.Host)
	return SendSuccess(c, fiber.StatusOK, result)
}

// --- User Source Access Handlers ---

// handleGetSource retrieves details for a specific source by ID.
// URL: GET /api/v1/sources/:sourceID
// Requires: User must have access to the source via team membership (checked by requireSourceAccess middleware).
func (s *Server) handleGetSource(c *fiber.Ctx) error {
	// Source ID access is validated by middleware requireSourceAccess or requireAdmin.
	sourceIDStr := c.Params("sourceID")
	if sourceIDStr == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Source ID is required", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	// Use core function to get detailed source info.
	src, err := core.GetSource(c.Context(), s.sqlite, s.clickhouse, s.log, sourceID)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Source not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get source via core function", slog.Any("error", err), "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error getting source", models.DatabaseErrorType)
	}

	// Convert to response object.
	return SendSuccess(c, fiber.StatusOK, src.ToResponse())
}

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
		s.log.Error("failed to get source before getting stats", slog.Any("error", err), "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Error getting source details")
	}

	// Get stats using the core function.
	stats, err := core.GetSourceStats(c.Context(), s.clickhouse, s.log, src)
	if err != nil {
		if errors.Is(err, core.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "Source not found when getting stats")
		}
		s.log.Error("failed to get source stats via core function", slog.Any("error", err), "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Error getting source stats: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, stats)
}

// handleListUserSources retrieves all sources accessible by the authenticated user.
// URL: GET /api/v1/sources
// Requires: User authentication
// This includes sources from all teams the user is a member of.
func (s *Server) handleListUserSources(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	if user == nil {
		// Should be caught by requireAuth middleware, but double-check.
		return SendErrorWithType(c, fiber.StatusUnauthorized, "User context not found", models.AuthenticationErrorType)
	}

	sources, err := core.ListSourcesForUser(c.Context(), s.sqlite, user.ID)
	if err != nil {
		s.log.Error("failed to list sources for user via core function", "error", err, "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to retrieve accessible sources", models.GeneralErrorType)
	}

	// Convert to response objects to hide sensitive connection details.
	sourceResponses := make([]*models.SourceResponse, 0, len(sources))
	for _, src := range sources {
		if src != nil { // Add nil check for safety
			sourceResponses = append(sourceResponses, src.ToResponse())
		}
	}

	return SendSuccess(c, fiber.StatusOK, sourceResponses)
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
