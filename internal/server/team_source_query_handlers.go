package server

import (
	"database/sql"
	"errors"
	"strconv"

	core "github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// handleListTeamCollections retrieves all saved queries (collections) for a team across all sources.
func (s *Server) handleListTeamCollections(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamID")

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}

	queries, err := s.sqlite.ListQueriesByTeam(c.Context(), teamID)
	if err != nil {
		s.log.Error("failed to list team collections", "error", err, "team_id", teamID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to list collections")
	}
	return SendSuccess(c, fiber.StatusOK, queries)
}

// handleListTeamSourceCollections retrieves saved queries (collections) for a specific team and source.
func (s *Server) handleListTeamSourceCollections(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
	}

	// LINTER_ISSUE: Linter incorrectly reports core.ListQueriesByTeamAndSource as undefined.
	// Using direct sqlite call as workaround.
	queries, err := s.sqlite.ListQueriesByTeamAndSource(c.Context(), teamID, sourceID)
	if err != nil {
		s.log.Error("failed to list collections", "error", err, "team_id", teamID, "source_id", sourceID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to list collections")
	}
	return SendSuccess(c, fiber.StatusOK, queries)
}

// handleCreateTeamSourceCollection creates a new saved query (collection) for a specific team and source.
// Assumes requireAuth, requireTeamMember, and requireCollectionManagement middleware have run.
func (s *Server) handleCreateTeamSourceCollection(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
	}

	// Parse request body.
	var req struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		QueryType    string `json:"query_type"`
		QueryContent string `json:"query_content"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Validate required fields.
	if req.Name == "" || req.QueryType == "" || req.QueryContent == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Missing required fields (name, queryType, queryContent)", models.ValidationErrorType)
	}

	// Authorization Check: Ensure the team actually has access to the source.
	hasAccess, err := core.TeamHasSourceAccess(c.Context(), s.sqlite, teamID, sourceID)
	if err != nil {
		s.log.Error("error checking team source access for collection create", "error", err, "team_id", teamID, "source_id", sourceID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Error checking permissions", models.GeneralErrorType)
	}
	if !hasAccess {
		return SendErrorWithType(c, fiber.StatusForbidden, "Specified team does not have access to the specified source", models.AuthorizationErrorType)
	}

	// Create query using core function.
	createdQuery, err := core.CreateTeamSourceQuery(
		c.Context(),
		s.sqlite,
		s.log,
		teamID,
		sourceID,
		req.Name,
		req.Description,
		req.QueryContent,
		req.QueryType,
		// Pass creator ID if core function supports it in the future.
	)

	if err != nil {
		s.log.Error("failed to create collection", "error", err, "team_id", teamID, "source_id", sourceID)
		if errors.Is(err, core.ErrQueryTypeRequired) || errors.Is(err, core.ErrInvalidQueryType) || errors.Is(err, core.ErrInvalidQueryContent) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create collection", models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusCreated, createdQuery)
}

// handleGetTeamSourceCollection retrieves a specific saved query (collection) belonging to a team/source.
// Assumes requireAuth and requireTeamMember middleware have run.
func (s *Server) handleGetTeamSourceCollection(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	collectionIDStr := c.Params("collectionID")
	if collectionIDStr == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Collection ID is required", models.ValidationErrorType)
	}

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
	}
	collectionID, err := strconv.Atoi(collectionIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid Collection ID format", models.ValidationErrorType)
	}

	// Call the sqlite method directly for now due to linter issues with core.handleNotFoundError
	query, err := s.sqlite.GetTeamSourceQuery(c.Context(), teamID, sourceID, collectionID)
	if err != nil {
		// LINTER_ISSUE: Cannot reliably check core.ErrQueryNotFound due to potential resolution issues.
		// Checking for sql.ErrNoRows from the underlying db call instead.
		if errors.Is(err, sql.ErrNoRows) { // Check for underlying DB error
			return SendErrorWithType(c, fiber.StatusNotFound, "Collection not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get collection", "error", err, "collection_id", collectionID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to retrieve collection", models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, query)
}

// handleUpdateTeamSourceCollection updates a saved query (collection).
// Assumes requireAuth, requireTeamMember, and requireCollectionManagement middleware have run.
func (s *Server) handleUpdateTeamSourceCollection(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	collectionIDStr := c.Params("collectionID")
	if collectionIDStr == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Collection ID is required", models.ValidationErrorType)
	}

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
	}
	collectionID, err := strconv.Atoi(collectionIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid Collection ID format", models.ValidationErrorType)
	}

	// Parse request body.
	var req struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		QueryType    *string `json:"query_type"`
		QueryContent *string `json:"query_content"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	// Prepare update data - pass empty strings for omitted fields.
	name := ""
	if req.Name != nil {
		name = *req.Name
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}
	queryType := ""
	if req.QueryType != nil {
		queryType = *req.QueryType
	}
	queryContent := ""
	if req.QueryContent != nil {
		queryContent = *req.QueryContent
	}

	// Validate Content and Type before sending to DB.
	if queryType != "" && models.SavedQueryType(queryType) != models.SavedQueryTypeLogchefQL && models.SavedQueryType(queryType) != models.SavedQueryTypeSQL {
		return SendErrorWithType(c, fiber.StatusBadRequest, core.ErrInvalidQueryType.Error(), models.ValidationErrorType)
	}
	if queryContent != "" {
		if err := core.ValidateSavedQueryContent(queryContent); err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
	}

	// Call sqlite update function.
	// Middleware ensures user has appropriate team admin rights.
	err = s.sqlite.UpdateTeamSourceQuery(
		c.Context(),
		teamID,
		sourceID,
		collectionID,
		name, description, queryType, queryContent,
	)
	if err != nil {
		// Check if the underlying error was potentially ErrNoRows.
		if errors.Is(err, sql.ErrNoRows) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Collection not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to update collection", "error", err, "collection_id", collectionID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update collection", models.GeneralErrorType)
	}

	// Fetch and return updated query.
	updatedQuery, err := s.sqlite.GetTeamSourceQuery(c.Context(), teamID, sourceID, collectionID)
	if err != nil {
		s.log.Error("failed to fetch updated collection", "error", err, "collection_id", collectionID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Collection updated but failed to retrieve latest state", models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, updatedQuery)
}

// handleDeleteTeamSourceCollection deletes a saved query (collection).
// Assumes requireAuth, requireTeamMember, and requireCollectionManagement middleware have run.
func (s *Server) handleDeleteTeamSourceCollection(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	collectionIDStr := c.Params("collectionID")
	if collectionIDStr == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Collection ID is required", models.ValidationErrorType)
	}

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
	}
	collectionID, err := strconv.Atoi(collectionIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid Collection ID format", models.ValidationErrorType)
	}

	// Call sqlite delete function.
	// Middleware ensures user has appropriate team admin rights.
	err = s.sqlite.DeleteTeamSourceQuery(c.Context(), teamID, sourceID, collectionID)
	if err != nil {
		// DELETE often doesn't return ErrNoRows.
		if errors.Is(err, sql.ErrNoRows) { // Check just in case.
			return SendErrorWithType(c, fiber.StatusNotFound, "Collection not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to delete collection", "error", err, "collection_id", collectionID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete collection", models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Collection deleted successfully"})
}

// handleResolveQuery resolves a saved query (collection) and returns its full details
// in a format suitable for hydrating the log explorer. This endpoint allows the frontend
// to load a saved query by ID without including all query parameters in the URL.
// Assumes requireAuth and requireTeamMember middleware have run.
func (s *Server) handleResolveQuery(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	collectionIDStr := c.Params("collectionID")
	if collectionIDStr == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Collection ID is required", models.ValidationErrorType)
	}

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
	}
	collectionID, err := strconv.Atoi(collectionIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid Collection ID format", models.ValidationErrorType)
	}

	// Get the saved query from database
	query, err := s.sqlite.GetTeamSourceQuery(c.Context(), teamID, sourceID, collectionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Collection not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get collection", "error", err, "collection_id", collectionID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to retrieve collection", models.GeneralErrorType)
	}

	// Return the full query details for hydration
	// The frontend will use this to populate the explorer state
	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"id":            query.ID,
		"team_id":       query.TeamID,
		"source_id":     query.SourceID,
		"name":          query.Name,
		"description":   query.Description,
		"query_type":    query.QueryType,
		"query_content": query.QueryContent,
		"is_bookmarked": query.IsBookmarked,
		"created_at":    query.CreatedAt,
		"updated_at":    query.UpdatedAt,
	})
}

// handleToggleQueryBookmark toggles the bookmark status of a saved query (collection).
// Assumes requireAuth, requireTeamMember, and requireCollectionManagement middleware have run.
func (s *Server) handleToggleQueryBookmark(c *fiber.Ctx) error {
	teamIDStr := c.Params("teamID")
	sourceIDStr := c.Params("sourceID")
	collectionIDStr := c.Params("collectionID")
	if collectionIDStr == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Collection ID is required", models.ValidationErrorType)
	}

	teamID, err := core.ParseTeamID(teamIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}
	sourceID, err := core.ParseSourceID(sourceIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
	}
	collectionID, err := strconv.Atoi(collectionIDStr)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid Collection ID format", models.ValidationErrorType)
	}

	// Toggle the bookmark status.
	newStatus, err := s.sqlite.ToggleQueryBookmark(c.Context(), teamID, sourceID, collectionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Collection not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to toggle bookmark", "error", err, "collection_id", collectionID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to toggle bookmark", models.GeneralErrorType)
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"is_bookmarked": newStatus,
		"message":       "Bookmark status toggled successfully",
	})
}
