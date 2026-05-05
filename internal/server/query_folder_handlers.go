package server

import (
	"database/sql"
	"errors"
	"strconv"

	core "github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

func (s *Server) handleListQueryFolders(c *fiber.Ctx) error {
	teamID, err := parseTeamIDParam(c)
	if err != nil {
		return err
	}

	folders, err := s.sqlite.ListQueryFolders(c.Context(), teamID)
	if err != nil {
		s.log.Error("failed to list query folders", "error", err, "team_id", teamID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list folders", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, folders)
}

func (s *Server) handleCreateQueryFolder(c *fiber.Ctx) error {
	teamID, err := parseTeamIDParam(c)
	if err != nil {
		return err
	}
	user := c.Locals("user").(*models.User)

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Color       string `json:"color"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	folder, err := core.CreateQueryFolder(c.Context(), s.sqlite, s.log, teamID, req.Name, req.Description, req.Color, user.ID)
	if err != nil {
		if errors.Is(err, core.ErrQueryFolderNameRequired) || errors.Is(err, core.ErrQueryFolderColorInvalid) {
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		}
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to create folder", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusCreated, folder)
}

func (s *Server) handleGetQueryFolder(c *fiber.Ctx) error {
	teamID, folderID, err := parseTeamAndFolderParams(c)
	if err != nil {
		return err
	}

	folder, err := s.sqlite.GetQueryFolder(c.Context(), teamID, folderID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Folder not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to get query folder", "error", err, "team_id", teamID, "folder_id", folderID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to retrieve folder", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, folder)
}

func (s *Server) handleUpdateQueryFolder(c *fiber.Ctx) error {
	teamID, folderID, err := parseTeamAndFolderParams(c)
	if err != nil {
		return err
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Color       string `json:"color"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	folder, err := core.UpdateQueryFolder(c.Context(), s.sqlite, s.log, teamID, folderID, req.Name, req.Description, req.Color)
	if err != nil {
		switch {
		case errors.Is(err, core.ErrQueryFolderNameRequired), errors.Is(err, core.ErrQueryFolderColorInvalid):
			return SendErrorWithType(c, fiber.StatusBadRequest, err.Error(), models.ValidationErrorType)
		case errors.Is(err, core.ErrQueryFolderNotFound), errors.Is(err, sql.ErrNoRows):
			return SendErrorWithType(c, fiber.StatusNotFound, "Folder not found", models.NotFoundErrorType)
		default:
			return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update folder", models.GeneralErrorType)
		}
	}
	return SendSuccess(c, fiber.StatusOK, folder)
}

func (s *Server) handleDeleteQueryFolder(c *fiber.Ctx) error {
	teamID, folderID, err := parseTeamAndFolderParams(c)
	if err != nil {
		return err
	}

	if err := s.sqlite.DeleteQueryFolder(c.Context(), teamID, folderID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Folder not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to delete query folder", "error", err, "team_id", teamID, "folder_id", folderID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to delete folder", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Folder deleted successfully"})
}

func (s *Server) handleListQueryFolderCollections(c *fiber.Ctx) error {
	teamID, folderID, err := parseTeamAndFolderParams(c)
	if err != nil {
		return err
	}

	queries, err := s.sqlite.ListQueriesByFolder(c.Context(), teamID, folderID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return SendErrorWithType(c, fiber.StatusNotFound, "Folder not found", models.NotFoundErrorType)
		}
		s.log.Error("failed to list folder collections", "error", err, "team_id", teamID, "folder_id", folderID)
		return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to list folder collections", models.GeneralErrorType)
	}
	return SendSuccess(c, fiber.StatusOK, queries)
}

func (s *Server) handleAddQueryToFolder(c *fiber.Ctx) error {
	teamID, folderID, collectionID, err := parseTeamFolderCollectionParams(c)
	if err != nil {
		return err
	}
	user := c.Locals("user").(*models.User)

	if err := s.sqlite.AddQueriesToFolder(c.Context(), teamID, folderID, []int{collectionID}, &user.ID); err != nil {
		return s.sendFolderMembershipError(c, err)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Collection added to folder"})
}

func (s *Server) handleRemoveQueryFromFolder(c *fiber.Ctx) error {
	teamID, folderID, collectionID, err := parseTeamFolderCollectionParams(c)
	if err != nil {
		return err
	}

	if err := s.sqlite.RemoveQueryFromFolder(c.Context(), teamID, folderID, collectionID); err != nil {
		return s.sendFolderMembershipError(c, err)
	}
	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Collection removed from folder"})
}

func (s *Server) handleBulkUpdateQueryFolderCollections(c *fiber.Ctx) error {
	teamID, folderID, err := parseTeamAndFolderParams(c)
	if err != nil {
		return err
	}
	user := c.Locals("user").(*models.User)

	var req struct {
		Add    []int `json:"add"`
		Remove []int `json:"remove"`
	}
	if err := c.BodyParser(&req); err != nil {
		return SendErrorWithType(c, fiber.StatusBadRequest, "Invalid request body", models.ValidationErrorType)
	}

	if len(req.Add) > 0 {
		if err := s.sqlite.AddQueriesToFolder(c.Context(), teamID, folderID, req.Add, &user.ID); err != nil {
			return s.sendFolderMembershipError(c, err)
		}
	}
	for _, queryID := range req.Remove {
		if err := s.sqlite.RemoveQueryFromFolder(c.Context(), teamID, folderID, queryID); err != nil {
			return s.sendFolderMembershipError(c, err)
		}
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Folder collections updated"})
}

func (s *Server) sendFolderMembershipError(c *fiber.Ctx, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return SendErrorWithType(c, fiber.StatusNotFound, "Folder or collection not found", models.NotFoundErrorType)
	}
	s.log.Error("failed to update folder membership", "error", err)
	return SendErrorWithType(c, fiber.StatusInternalServerError, "Failed to update folder membership", models.GeneralErrorType)
}

func parseTeamIDParam(c *fiber.Ctx) (models.TeamID, error) {
	teamID, err := core.ParseTeamID(c.Params("teamID"))
	if err != nil {
		return 0, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid team_id parameter", models.ValidationErrorType)
	}
	return teamID, nil
}

func parseTeamAndFolderParams(c *fiber.Ctx) (models.TeamID, int, error) {
	teamID, err := parseTeamIDParam(c)
	if err != nil {
		return 0, 0, err
	}
	folderID, err := strconv.Atoi(c.Params("folderID"))
	if err != nil || folderID <= 0 {
		return 0, 0, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid folder_id parameter", models.ValidationErrorType)
	}
	return teamID, folderID, nil
}

func parseTeamFolderCollectionParams(c *fiber.Ctx) (models.TeamID, int, int, error) {
	teamID, folderID, err := parseTeamAndFolderParams(c)
	if err != nil {
		return 0, 0, 0, err
	}
	collectionID, err := strconv.Atoi(c.Params("collectionID"))
	if err != nil || collectionID <= 0 {
		return 0, 0, 0, SendErrorWithType(c, fiber.StatusBadRequest, "Invalid collection_id parameter", models.ValidationErrorType)
	}
	return teamID, folderID, collectionID, nil
}
