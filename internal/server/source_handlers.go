package server

import (
	"errors"
	"fmt"

	"logchef/internal/source"
	"logchef/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// handleListSources handles GET /api/v1/sources
func (s *Server) handleListSources(c *fiber.Ctx) error {
	sources, err := s.sourceService.ListSources(c.Context())
	if err != nil {
		return fmt.Errorf("error listing sources: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, sources)
}

// handleGetSource handles GET /api/v1/sources/:id
func (s *Server) handleGetSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "source ID is required")
	}

	src, err := s.sourceService.GetSource(c.Context(), id)
	if err != nil {
		if errors.Is(err, source.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "source not found")
		}
		return fmt.Errorf("error getting source: %w", err)
	}

	// Uncomment if needed
	// if err := s.sourceService.ExploreSource(c.Context(), src); err != nil {
	//     s.log.Warn("error exploring source", "error", err)
	// }

	return SendSuccess(c, fiber.StatusOK, src)
}

// handleCreateSource handles POST /api/v1/sources
func (s *Server) handleCreateSource(c *fiber.Ctx) error {
	var req models.CreateSourceRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if req.Connection.Host == "" {
		return SendError(c, fiber.StatusBadRequest, "host is required")
	}
	if req.Connection.Database == "" {
		return SendError(c, fiber.StatusBadRequest, "database is required")
	}
	if req.Connection.TableName == "" {
		return SendError(c, fiber.StatusBadRequest, "table name is required")
	}
	if req.Description == "" {
		return SendError(c, fiber.StatusBadRequest, "description is required")
	}
	if req.MetaTSField == "" {
		req.MetaTSField = "timestamp" // Default timestamp field
	}

	created, err := s.sourceService.CreateSource(c.Context(), req.MetaIsAutoCreated, req.Connection, req.Description, req.TTLDays, req.MetaTSField)
	if err != nil {
		var validationErr *source.ValidationError
		if errors.As(err, &validationErr) {
			return SendError(c, fiber.StatusBadRequest, validationErr.Error())
		}
		return fmt.Errorf("error creating source: %w", err)
	}

	return SendSuccess(c, fiber.StatusCreated, created)
}

// handleDeleteSource handles DELETE /api/v1/sources/:id
func (s *Server) handleDeleteSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return SendError(c, fiber.StatusBadRequest, "source ID is required")
	}

	if err := s.sourceService.DeleteSource(c.Context(), id); err != nil {
		if errors.Is(err, source.ErrSourceNotFound) {
			return SendError(c, fiber.StatusNotFound, "source not found")
		}
		return fmt.Errorf("error deleting source: %w", err)
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Source deleted successfully"})
}
