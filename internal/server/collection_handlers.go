package server

import (
	"errors"
	"strconv"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v3"
)

func parseCollectionID(c fiber.Ctx) (int, error) {
	id, err := parsePositiveIntParam(c, "collectionID")
	return int(id), err
}

func mapCollectionError(c fiber.Ctx, err error) (handled bool, sendErr error) {
	switch {
	case errors.Is(err, core.ErrCollectionNotFound):
		return true, SendErrorWithType(c, fiber.StatusNotFound, "Collection not found", models.NotFoundErrorType)
	case errors.Is(err, core.ErrCollectionForbidden):
		return true, SendErrorWithType(c, fiber.StatusForbidden, "Only collection owners can perform this action", models.AuthorizationErrorType)
	case errors.Is(err, core.ErrPersonalCollectionImmutable):
		return true, SendErrorWithType(c, fiber.StatusBadRequest, "Personal collections cannot be modified or deleted", models.ValidationErrorType)
	case errors.Is(err, core.ErrInvalidCollectionRole):
		return true, SendErrorWithType(c, fiber.StatusBadRequest, "Role must be 'owner', 'editor', or 'member'", models.ValidationErrorType)
	case errors.Is(err, core.ErrCollectionTeamNotMember):
		return true, SendErrorWithType(c, fiber.StatusForbidden, err.Error(), models.AuthorizationErrorType)
	case errors.Is(err, core.ErrTeamNotFound):
		return true, SendErrorWithType(c, fiber.StatusNotFound, "Team not found", models.NotFoundErrorType)
	case errors.Is(err, core.ErrLastOwnerRemoval):
		return true, SendErrorWithType(c, fiber.StatusConflict, err.Error(), models.ValidationErrorType)
	case errors.Is(err, core.ErrQueryNotFound):
		return true, SendErrorWithType(c, fiber.StatusNotFound, "Saved query not found", models.NotFoundErrorType)
	}
	return false, nil
}

// handleListCollections returns the caller's collections (auto-creates personal).
func (s *Server) handleListCollections(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	collections, err := core.ListCollectionsForUser(c.Context(), s.sqlite, s.log, user)
	if err != nil {
		s.log.Error("failed to list collections", "error", err, "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list collections", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, collections)
}

// handleCreateCollection creates a shared collection. Requires team admin or global admin.
func (s *Server) handleCreateCollection(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	var req models.CreateCollectionRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	collection, err := core.CreateCollection(c.Context(), s.sqlite, s.log, req.Name, req.Description, user.ID)
	if err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		s.log.Error("failed to create collection", "error", err, "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, err.Error(), models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, collection)
}

// handleGetCollection returns a single collection (member-only, admin bypass).
func (s *Server) handleGetCollection(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	collection, _, err := core.GetCollectionForUser(c.Context(), s.sqlite, s.log, id, user.ID)
	if err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load collection", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, collection)
}

// handleUpdateCollection updates name/description. Requires team admin or global admin.
func (s *Server) handleUpdateCollection(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	var req models.UpdateCollectionRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	updated, err := core.UpdateCollection(c.Context(), s.sqlite, s.log, id, user.ID, req.Name, req.Description)
	if err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		s.log.Error("failed to update collection", "error", err, "collection_id", id)
		return SendErrorWithType(c, fiber.StatusInternalServerError, err.Error(), models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, updated)
}

// handleDeleteCollection removes a collection. Requires team admin or global admin.
func (s *Server) handleDeleteCollection(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	if err := core.DeleteCollection(c.Context(), s.sqlite, s.log, id, user.ID); err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		s.log.Error("failed to delete collection", "error", err, "collection_id", id)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete collection", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Collection deleted"})
}

// handleListCollectionMembers returns members of a collection.
func (s *Server) handleListCollectionMembers(c fiber.Ctx) error {
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	members, err := core.ListCollectionMembers(c.Context(), s.sqlite, s.log, id, principalFromLocals(c))
	if err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list members", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, members)
}

// handleAddCollectionMember invites a user. Requires team admin or global admin.
func (s *Server) handleAddCollectionMember(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	var req models.AddCollectionMemberRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if err := core.AddCollectionMember(c.Context(), s.sqlite, s.log, id, user.ID, req.UserID, req.Role); err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, fiber.Map{"message": "Member added"})
}

// handleRemoveCollectionMember drops a member.
func (s *Server) handleRemoveCollectionMember(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	userIDStr := c.Params("userID")
	userIDNum, err := strconv.Atoi(userIDStr)
	if err != nil || userIDNum <= 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid user id", models.ValidationErrorType)
	}
	if err := core.RemoveCollectionMember(c.Context(), s.sqlite, s.log, id, user.ID, models.UserID(userIDNum)); err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Member removed"})
}

// handleListCollectionTeams returns the teams a collection is shared with.
func (s *Server) handleListCollectionTeams(c fiber.Ctx) error {
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	teams, err := core.ListCollectionTeams(c.Context(), s.sqlite, s.log, id, principalFromLocals(c))
	if err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list teams", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, teams)
}

// handleAddCollectionTeam shares a collection with a team.
func (s *Server) handleAddCollectionTeam(c fiber.Ctx) error {
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	var req models.AddCollectionTeamRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if req.TeamID <= 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team id", models.ValidationErrorType)
	}
	if err := core.AddCollectionTeam(c.Context(), s.sqlite, s.log, id, principalFromLocals(c), req.TeamID); err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		s.log.Error("failed to add collection team", "error", err, "collection_id", id, "team_id", req.TeamID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to share collection with team", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, fiber.Map{"message": "Team added"})
}

// handleRemoveCollectionTeam removes a team share.
func (s *Server) handleRemoveCollectionTeam(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	teamID, err := parsePositiveIntParam(c, "teamID")
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team id", models.ValidationErrorType)
	}
	if err := core.RemoveCollectionTeam(c.Context(), s.sqlite, s.log, id, user.ID, models.TeamID(teamID)); err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		s.log.Error("failed to remove collection team", "error", err, "collection_id", id, "team_id", teamID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to remove team", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Team removed"})
}

// handleListCollectionItems returns items with the runnable flag.
func (s *Server) handleListCollectionItems(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	items, err := core.ListCollectionItems(c.Context(), s.sqlite, s.log, id, user.ID)
	if err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list items", models.GeneralErrorType)
	}
	// Per-item edit/delete hints for the Library UI.
	for i := range items {
		s.enrichSavedQueryPermissions(c, &items[i].Query, user)
	}
	return SendSuccess(c, fiber.StatusOK, items)
}

// handleAddCollectionItem links a saved query.
func (s *Server) handleAddCollectionItem(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	var req models.AddCollectionItemRequest
	if err := c.Bind().Body(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if err := core.AddCollectionItem(c.Context(), s.sqlite, s.log, id, user.ID, req.SavedQueryID, req.SortOrder); err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, fiber.Map{"message": "Item added"})
}

// handleRemoveCollectionItem unlinks a saved query.
func (s *Server) handleRemoveCollectionItem(c fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	queryIDStr := c.Params("queryID")
	queryIDNum, err := strconv.Atoi(queryIDStr)
	if err != nil || queryIDNum <= 0 {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid query id", models.ValidationErrorType)
	}
	if err := core.RemoveCollectionItem(c.Context(), s.sqlite, s.log, id, user.ID, queryIDNum); err != nil {
		if handled, sendErr := mapCollectionError(c, err); handled {
			return sendErr
		}
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Item removed"})
}
