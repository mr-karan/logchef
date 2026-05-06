package server

import (
	"errors"
	"strconv"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

func parseCollectionID(c *fiber.Ctx) (int, error) {
	idStr := c.Params("collectionID")
	if idStr == "" {
		return 0, errors.New("missing collection id")
	}
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid collection id")
	}
	return id, nil
}

func mapCollectionError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, core.ErrCollectionNotFound):
		return SendErrorWithType(c, fiber.StatusNotFound, "Collection not found", models.NotFoundErrorType)
	case errors.Is(err, core.ErrCollectionForbidden):
		return SendErrorWithType(c, fiber.StatusForbidden, "Only collection owners can perform this action", models.AuthorizationErrorType)
	case errors.Is(err, core.ErrPersonalCollectionImmutable):
		return SendErrorWithType(c, fiber.StatusBadRequest, "Personal collections cannot be modified or deleted", models.ValidationErrorType)
	case errors.Is(err, core.ErrInvalidCollectionRole):
		return SendErrorWithType(c, fiber.StatusBadRequest, "Role must be 'owner' or 'member'", models.ValidationErrorType)
	case errors.Is(err, core.ErrQueryNotFound):
		return SendErrorWithType(c, fiber.StatusNotFound, "Saved query not found", models.NotFoundErrorType)
	}
	return nil
}

// handleListCollections returns the caller's collections (auto-creates personal).
func (s *Server) handleListCollections(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	collections, err := core.ListCollectionsForUser(c.Context(), s.sqlite, s.log, user)
	if err != nil {
		s.log.Error("failed to list collections", "error", err, "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list collections", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, collections)
}

// handleCreateCollection creates a shared collection.
func (s *Server) handleCreateCollection(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)

	var req models.CreateCollectionRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	collection, err := core.CreateCollection(c.Context(), s.sqlite, s.log, req.Name, req.Description, user.ID)
	if err != nil {
		if mapped := mapCollectionError(c, err); mapped != nil {
			return mapped
		}
		s.log.Error("failed to create collection", "error", err, "user_id", user.ID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, err.Error(), models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, collection)
}

// handleGetCollection returns a single collection (member-only, admin bypass).
func (s *Server) handleGetCollection(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	collection, _, err := core.GetCollectionForUser(c.Context(), s.sqlite, s.log, id, user.ID, user.Role == models.UserRoleAdmin)
	if err != nil {
		if mapped := mapCollectionError(c, err); mapped != nil {
			return mapped
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to load collection", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, collection)
}

// handleUpdateCollection updates name/description.
func (s *Server) handleUpdateCollection(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	var req models.UpdateCollectionRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	updated, err := core.UpdateCollection(c.Context(), s.sqlite, s.log, id, user.ID, user.Role == models.UserRoleAdmin, req.Name, req.Description)
	if err != nil {
		if mapped := mapCollectionError(c, err); mapped != nil {
			return mapped
		}
		s.log.Error("failed to update collection", "error", err, "collection_id", id)
		return SendErrorWithType(c, fiber.StatusInternalServerError, err.Error(), models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, updated)
}

// handleDeleteCollection removes a collection.
func (s *Server) handleDeleteCollection(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}

	if err := core.DeleteCollection(c.Context(), s.sqlite, s.log, id, user.ID, user.Role == models.UserRoleAdmin); err != nil {
		if mapped := mapCollectionError(c, err); mapped != nil {
			return mapped
		}
		s.log.Error("failed to delete collection", "error", err, "collection_id", id)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete collection", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Collection deleted"})
}

// handleListCollectionMembers returns members of a collection.
func (s *Server) handleListCollectionMembers(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	members, err := core.ListCollectionMembers(c.Context(), s.sqlite, s.log, id, user.ID, user.Role == models.UserRoleAdmin)
	if err != nil {
		if mapped := mapCollectionError(c, err); mapped != nil {
			return mapped
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list members", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, members)
}

// handleAddCollectionMember invites a user.
func (s *Server) handleAddCollectionMember(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	var req models.AddCollectionMemberRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if err := core.AddCollectionMember(c.Context(), s.sqlite, s.log, id, user.ID, user.Role == models.UserRoleAdmin, req.UserID, req.Role); err != nil {
		if mapped := mapCollectionError(c, err); mapped != nil {
			return mapped
		}
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, fiber.Map{"message": "Member added"})
}

// handleRemoveCollectionMember drops a member.
func (s *Server) handleRemoveCollectionMember(c *fiber.Ctx) error {
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
	if err := core.RemoveCollectionMember(c.Context(), s.sqlite, s.log, id, user.ID, user.Role == models.UserRoleAdmin, models.UserID(userIDNum)); err != nil {
		if mapped := mapCollectionError(c, err); mapped != nil {
			return mapped
		}
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Member removed"})
}

// handleListCollectionItems returns items with the runnable flag.
func (s *Server) handleListCollectionItems(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	items, err := core.ListCollectionItems(c.Context(), s.sqlite, s.log, id, user.ID, user.Role == models.UserRoleAdmin)
	if err != nil {
		if mapped := mapCollectionError(c, err); mapped != nil {
			return mapped
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list items", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, items)
}

// handleAddCollectionItem links a saved query.
func (s *Server) handleAddCollectionItem(c *fiber.Ctx) error {
	user := c.Locals("user").(*models.User)
	id, err := parseCollectionID(c)
	if err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	var req models.AddCollectionItemRequest
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}
	if err := core.AddCollectionItem(c.Context(), s.sqlite, s.log, id, user.ID, user.Role == models.UserRoleAdmin, req.SavedQueryID, req.SortOrder); err != nil {
		if mapped := mapCollectionError(c, err); mapped != nil {
			return mapped
		}
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, fiber.Map{"message": "Item added"})
}

// handleRemoveCollectionItem unlinks a saved query.
func (s *Server) handleRemoveCollectionItem(c *fiber.Ctx) error {
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
	if err := core.RemoveCollectionItem(c.Context(), s.sqlite, s.log, id, user.ID, user.Role == models.UserRoleAdmin, queryIDNum); err != nil {
		if mapped := mapCollectionError(c, err); mapped != nil {
			return mapped
		}
		return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Item removed"})
}
