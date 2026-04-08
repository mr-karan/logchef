package server

import (
	"database/sql"
	"errors"
	"strconv"

	core "github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// handleListCurrentUserCollections retrieves all saved queries across all teams the user belongs to.
func (s *Server) handleListCurrentUserCollections(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	queries, err := s.sqlite.ListQueriesForUser(c.Context(), user.ID)
	if err != nil {
		s.log.Error("failed to list user collections", "error", err, "user_id", user.ID)
		return SendError(c, fiber.StatusInternalServerError, "Failed to list collections")
	}
	return SendSuccess(c, fiber.StatusOK, queries)
}

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

	queries, err := core.ListQueriesForTeamAndSource(c.Context(), s.sqlite, s.log, teamID, sourceID)
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
	var req models.CreateTeamQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	req.SourceID = sourceID

	if req.Name == "" || req.QueryContent == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Missing required fields (name, queryContent)", models.ValidationErrorType)
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
	createdQuery, err := core.CreateTeamSourceQuery(c.Context(), s.sqlite, s.datasources, s.log, teamID, sourceID, &req)

	if err != nil {
		s.log.Error("failed to create collection", "error", err, "team_id", teamID, "source_id", sourceID)
		if errors.Is(err, core.ErrQueryTypeRequired) ||
			errors.Is(err, core.ErrInvalidQueryType) ||
			errors.Is(err, core.ErrInvalidQueryContent) ||
			errors.Is(err, core.ErrUnsupportedSavedQueryDefinition) {
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

	query, err := core.GetTeamSourceQuery(c.Context(), s.sqlite, s.log, teamID, sourceID, collectionID)
	if err != nil {
		if errors.Is(err, core.ErrQueryNotFound) {
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
	var req models.UpdateTeamQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	updatedQuery, err := core.UpdateTeamSourceQuery(c.Context(), s.sqlite, s.datasources, s.log, teamID, sourceID, collectionID, &req)
	if err != nil {
		if errors.Is(err, core.ErrQueryNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Collection not found", models.NotFoundErrorType)
		}
		if errors.Is(err, core.ErrInvalidQueryType) ||
			errors.Is(err, core.ErrInvalidQueryContent) ||
			errors.Is(err, core.ErrUnsupportedSavedQueryDefinition) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		s.log.Error("failed to update collection", "error", err, "collection_id", collectionID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update collection", models.GeneralErrorType)
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
	query, err := core.GetTeamSourceQuery(c.Context(), s.sqlite, s.log, teamID, sourceID, collectionID)
	if err != nil {
		if errors.Is(err, core.ErrQueryNotFound) {
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
		"query_language": query.QueryLanguage,
		"editor_mode":   query.EditorMode,
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
