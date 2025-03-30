package server

import (
	"errors"
	"log/slog"
	"strconv"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
	"github.com/mr-karan/logchef/internal/saved_queries"
)

// handleListTeamSourceQueries handles GET /api/v1/teams/:teamID/sources/:sourceID/queries
func (s *Server) handleListTeamSourceQueries(c *fiber.Ctx) error {
	// Get team ID and source ID from params
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	if teamIDStr == "" || sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID and Source ID are required")
	}

	// Convert string IDs to proper types
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
	}
	sourceID := models.SourceID(sourceIDInt)

	// List queries for this team and source - permissions already checked by middleware
	queries, err := s.savedQueryService.ListQueriesForTeamAndSource(c.Context(), teamID, sourceID)
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "Failed to list queries")
	}

	return SendSuccess(c, fiber.StatusOK, queries)
}

// handleCreateTeamSourceQuery handles POST /api/v1/teams/:teamID/sources/:sourceID/queries
func (s *Server) handleCreateTeamSourceQuery(c *fiber.Ctx) error {
	// Get team ID and source ID from params
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	if teamIDStr == "" || sourceIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID and Source ID are required")
	}

	// Convert string IDs to proper types
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
	}
	sourceID := models.SourceID(sourceIDInt)

	// Parse request body
	var req struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		QueryType    string `json:"query_type"`
		QueryContent string `json:"query_content"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Get user from context for ownership
	user := c.Locals("user").(*models.User)

	// Create query - permissions already checked by middleware
	createdQuery, err := s.savedQueryService.CreateTeamSourceQuery(
		c.Context(),
		teamID,
		sourceID,
		req.Name,
		req.Description,
		req.QueryContent,
		req.QueryType,
		user.ID,
	)
	if err != nil {
		// Log the actual error
		s.log.Error("failed to create team source query", slog.Any("error", err), slog.Int("teamID", int(teamID)), slog.Int("sourceID", int(sourceID)))

		// Check for specific validation error
		if errors.Is(err, saved_queries.ErrQueryTypeRequired) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}

		// Return generic server error for other issues
		return SendError(c, fiber.StatusInternalServerError, "Failed to create query")
	}

	return SendSuccess(c, fiber.StatusCreated, createdQuery)
}

// handleGetTeamSourceQuery handles GET /api/v1/teams/:teamID/sources/:sourceID/queries/:queryID
func (s *Server) handleGetTeamSourceQuery(c *fiber.Ctx) error {
	// Get team ID, source ID and query ID from params
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	queryIDStr := c.Params("queryID")
	if teamIDStr == "" || sourceIDStr == "" || queryIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID, Source ID, and Query ID are required")
	}

	// Convert string IDs to proper types
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
	}
	sourceID := models.SourceID(sourceIDInt)

	queryIDInt, err := strconv.Atoi(queryIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid query ID: "+err.Error())
	}
	queryID := int(queryIDInt)

	// Get query for this team and source - permissions already checked by middleware
	query, err := s.savedQueryService.GetTeamSourceQuery(c.Context(), teamID, sourceID, queryID)
	if err != nil {
		if errors.Is(err, saved_queries.ErrQueryNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Query not found", models.NotFoundErrorType)
		}
		return SendError(c, fiber.StatusInternalServerError, "Failed to get query: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, query)
}

// handleUpdateTeamSourceQuery handles PUT /api/v1/teams/:teamID/sources/:sourceID/queries/:queryID
func (s *Server) handleUpdateTeamSourceQuery(c *fiber.Ctx) error {
	// Get team ID, source ID and query ID from params
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	queryIDStr := c.Params("queryID")
	if teamIDStr == "" || sourceIDStr == "" || queryIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID, Source ID, and Query ID are required")
	}

	// Convert string IDs to proper types
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
	}
	sourceID := models.SourceID(sourceIDInt)

	queryIDInt, err := strconv.Atoi(queryIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid query ID: "+err.Error())
	}
	queryID := int(queryIDInt)

	// Parse request body
	var req struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		QueryType    string `json:"query_type"`
		QueryContent string `json:"query_content"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Get user from context for potential ownership/permissions check (if needed by service)
	// user := c.Locals("user").(*models.User)

	// Update query - permissions already checked by middleware
	updatedQuery, err := s.savedQueryService.UpdateTeamSourceQuery(
		c.Context(),
		teamID,
		sourceID,
		queryID,
		req.Name,
		req.Description,
		req.QueryContent,
		req.QueryType,
		// user.ID, // Pass user ID if needed for authorization in service layer
	)
	if err != nil {
		if errors.Is(err, saved_queries.ErrQueryNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Query not found", models.NotFoundErrorType)
		}
		return SendError(c, fiber.StatusInternalServerError, "Failed to update query: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, updatedQuery)
}

// handleDeleteTeamSourceQuery handles DELETE /api/v1/teams/:teamID/sources/:sourceID/queries/:queryID
func (s *Server) handleDeleteTeamSourceQuery(c *fiber.Ctx) error {
	// Get team ID, source ID and query ID from params
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	queryIDStr := c.Params("queryID")
	if teamIDStr == "" || sourceIDStr == "" || queryIDStr == "" {
		return SendError(c, fiber.StatusBadRequest, "Team ID, Source ID, and Query ID are required")
	}

	// Convert string IDs to proper types
	teamIDInt, err := strconv.Atoi(teamIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid team ID: "+err.Error())
	}
	teamID := models.TeamID(teamIDInt)

	sourceIDInt, err := strconv.Atoi(sourceIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid source ID: "+err.Error())
	}
	sourceID := models.SourceID(sourceIDInt)

	queryIDInt, err := strconv.Atoi(queryIDStr)
	if err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid query ID: "+err.Error())
	}
	queryID := int(queryIDInt)

	// Delete query - permissions already checked by middleware
	err = s.savedQueryService.DeleteTeamSourceQuery(c.Context(), teamID, sourceID, queryID)
	if err != nil {
		if errors.Is(err, saved_queries.ErrQueryNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Query not found", models.NotFoundErrorType)
		}
		return SendError(c, fiber.StatusInternalServerError, "Failed to delete query: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"message": "Query deleted successfully",
	})
}
