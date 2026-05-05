package server

import (
	"errors"
	"strconv"

	core "github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// parseSavedQueryID extracts and validates the :queryID URL parameter.
func parseSavedQueryID(c *fiber.Ctx) (int, error) {
	idStr := c.Params("queryID")
	if idStr == "" {
		return 0, errors.New("missing query id")
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid query id")
	}
	return id, nil
}

// loadSavedQueryWithVisibility fetches a saved query and verifies the caller
// has visibility (source access via any team). Returns the query, the caller,
// and a Fiber response if either lookup or authorization fails.
func (s *Server) loadSavedQueryWithVisibility(c *fiber.Ctx) (*models.SavedQuery, *models.User, error) {
	user, ok := c.Locals("user").(*models.User)
	if !ok || user == nil {
		return nil, nil, SendErrorWithType(c, fiber.StatusUnauthorized, "Authentication context missing", models.AuthenticationErrorType)
	}

	queryID, err := parseSavedQueryID(c)
	if err != nil {
		return nil, nil, SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	query, err := core.GetSavedQuery(c.Context(), s.sqlite, s.log, queryID)
	if err != nil {
		if errors.Is(err, core.ErrQueryNotFound) {
			return nil, nil, SendErrorWithType(c, fiber.StatusNotFound, "Saved query not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to load saved query", "error", err, "query_id", queryID)
		return nil, nil, SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load saved query", models.GeneralErrorType)
	}

	if user.Role != models.UserRoleAdmin {
		hasAccess, accessErr := s.sqlite.UserHasSourceAccess(c.Context(), user.ID, query.SourceID)
		if accessErr != nil {
			s.log.Error("failed to check source access for saved query", "error", accessErr, "user_id", user.ID, "source_id", query.SourceID)
			return nil, nil, SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify access", models.GeneralErrorType)
		}
		if !hasAccess {
			return nil, nil, SendErrorWithType(c, fiber.StatusNotFound, "Saved query not found", models.NotFoundErrorType)
		}
	}

	return query, user, nil
}

// handleListSavedQueries lists saved queries the caller can see. Optional ?source_id filter.
func (s *Server) handleListSavedQueries(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	if sourceParam := c.Query("source_id"); sourceParam != "" {
		sourceID, err := core.ParseSourceID(sourceParam)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid source_id parameter", models.ValidationErrorType)
		}
		queries, err := core.ListSavedQueriesForUserBySource(c.Context(), s.sqlite, s.log, user.ID, sourceID)
		if err != nil {
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list saved queries", models.GeneralErrorType)
		}
		return SendSuccess(c, fiber.StatusOK, queries)
	}

	queries, err := core.ListSavedQueriesForUser(c.Context(), s.sqlite, s.log, user.ID)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list saved queries", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, queries)
}

// handleCreateSavedQuery creates a new saved query bound to the supplied source.
// The caller must have source access via any of their teams; the resulting query
// is owned by the caller (created_by = user.ID).
func (s *Server) handleCreateSavedQuery(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	var req models.CreateSavedQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if req.Name == "" || req.SourceID == 0 || req.QueryType == "" || req.QueryContent == "" {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Missing required fields (name, source_id, query_type, query_content)", models.ValidationErrorType)
	}

	if user.Role != models.UserRoleAdmin {
		hasAccess, err := s.sqlite.UserHasSourceAccess(c.Context(), user.ID, req.SourceID)
		if err != nil {
			s.log.Error("failed to check source access for saved query create", "error", err, "user_id", user.ID, "source_id", req.SourceID)
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to verify access", models.GeneralErrorType)
		}
		if !hasAccess {
			return SendErrorWithType(c, fiber.StatusForbidden, "No team you belong to has access to this source", models.AuthorizationErrorType)
		}
	}

	created, err := core.CreateSavedQuery(c.Context(), s.sqlite, s.log, req.SourceID, req.Name, req.Description, req.QueryContent, string(req.QueryType), user.ID)
	if err != nil {
		if errors.Is(err, core.ErrQueryTypeRequired) || errors.Is(err, core.ErrInvalidQueryType) || errors.Is(err, core.ErrInvalidQueryContent) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create saved query", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, created)
}

// handleGetSavedQuery returns a single saved query.
func (s *Server) handleGetSavedQuery(c *fiber.Ctx) error {
	query, _, err := s.loadSavedQueryWithVisibility(c)
	if err != nil {
		return err
	}
	return SendSuccess(c, fiber.StatusOK, query)
}

// handleUpdateSavedQuery updates a saved query. Allowed only for the creator
// or a global admin; legacy queries (created_by IS NULL) require global admin.
func (s *Server) handleUpdateSavedQuery(c *fiber.Ctx) error {
	query, user, err := s.loadSavedQueryWithVisibility(c)
	if err != nil {
		return err
	}
	if !core.UserCanEditSavedQuery(query, user) {
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or a global admin can edit this query", models.AuthorizationErrorType)
	}

	var req struct {
		Name         *string `json:"name"`
		Description  *string `json:"description"`
		QueryType    *string `json:"query_type"`
		QueryContent *string `json:"query_content"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	name := query.Name
	if req.Name != nil {
		name = *req.Name
	}
	description := query.Description
	if req.Description != nil {
		description = *req.Description
	}
	queryType := string(query.QueryType)
	if req.QueryType != nil {
		queryType = *req.QueryType
	}
	queryContent := query.QueryContent
	if req.QueryContent != nil {
		queryContent = *req.QueryContent
	}

	updated, updateErr := core.UpdateSavedQuery(c.Context(), s.sqlite, s.log, query.ID, name, description, queryContent, queryType)
	if updateErr != nil {
		if errors.Is(updateErr, core.ErrQueryNotFound) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Saved query not found", models.NotFoundErrorType)
		}
		if errors.Is(updateErr, core.ErrInvalidQueryType) || errors.Is(updateErr, core.ErrInvalidQueryContent) {
			return SendErrorWithType(c, fiber.StatusBadRequest, updateErr.Error(), models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update saved query", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, updated)
}

// handleDeleteSavedQuery removes a saved query (creator + global admin only).
func (s *Server) handleDeleteSavedQuery(c *fiber.Ctx) error {
	query, user, err := s.loadSavedQueryWithVisibility(c)
	if err != nil {
		return err
	}
	if !core.UserCanEditSavedQuery(query, user) {
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or a global admin can delete this query", models.AuthorizationErrorType)
	}

	if delErr := core.DeleteSavedQuery(c.Context(), s.sqlite, s.log, query.ID); delErr != nil {
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete saved query", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Saved query deleted successfully"})
}

// handleToggleSavedQueryBookmark flips the bookmark flag (creator + global admin only).
func (s *Server) handleToggleSavedQueryBookmark(c *fiber.Ctx) error {
	query, user, err := s.loadSavedQueryWithVisibility(c)
	if err != nil {
		return err
	}
	if !core.UserCanEditSavedQuery(query, user) {
		return SendErrorWithType(c, fiber.StatusForbidden, "Only the creator or a global admin can bookmark this query", models.AuthorizationErrorType)
	}

	newStatus, toggleErr := core.ToggleSavedQueryBookmark(c.Context(), s.sqlite, s.log, query.ID)
	if toggleErr != nil {
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to toggle bookmark", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"is_bookmarked": newStatus,
		"message":       "Bookmark status toggled successfully",
	})
}

// handleResolveSavedQuery returns the query plus enough metadata for the explorer
// to hydrate without round-tripping through URL params.
func (s *Server) handleResolveSavedQuery(c *fiber.Ctx) error {
	query, _, err := s.loadSavedQueryWithVisibility(c)
	if err != nil {
		return err
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{
		"id":            query.ID,
		"source_id":     query.SourceID,
		"name":          query.Name,
		"description":   query.Description,
		"query_type":    query.QueryType,
		"query_content": query.QueryContent,
		"is_bookmarked": query.IsBookmarked,
		"created_by":    query.CreatedBy,
		"created_at":    query.CreatedAt,
		"updated_at":    query.UpdatedAt,
	})
}
