package server

import (
	"errors"

	"github.com/mr-karan/logchef/pkg/models"

	"github.com/mr-karan/logchef/internal/source"

	"github.com/gofiber/fiber/v2"
)

// handleListSources handles GET /api/v1/sources
func (s *Server) handleListSources(c *fiber.Ctx) error {
	sources, err := s.sourceService.ListSources(c.Context())
	if err != nil {
		return SendError(c, fiber.StatusInternalServerError, "error listing sources: "+err.Error())
	}

	// Convert sources to response objects to avoid exposing sensitive information
	sourceResponses := make([]*models.SourceResponse, len(sources))
	for i, src := range sources {
		sourceResponses[i] = src.ToResponse()
	}

	return SendSuccess(c, fiber.StatusOK, sourceResponses)
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
		return SendError(c, fiber.StatusInternalServerError, "error getting source: "+err.Error())
	}

	// Convert to response object to avoid exposing sensitive information
	return SendSuccess(c, fiber.StatusOK, src.ToResponse())
}

// handleCreateSource handles POST /api/v1/sources
func (s *Server) handleCreateSource(c *fiber.Ctx) error {
	var req models.CreateSourceRequest
	if err := c.BodyParser(&req); err != nil {
		return SendError(c, fiber.StatusBadRequest, "invalid request body")
	}

	// Set default timestamp field if not provided
	if req.MetaTSField == "" {
		req.MetaTSField = "timestamp" // Default timestamp field
	}

	created, err := s.sourceService.CreateSource(c.Context(), req.MetaIsAutoCreated, req.Connection, req.Description, req.TTLDays, req.MetaTSField)
	if err != nil {
		var validationErr *source.ValidationError
		if errors.As(err, &validationErr) {
			return SendError(c, fiber.StatusBadRequest, validationErr.Error())
		}
		return SendError(c, fiber.StatusInternalServerError, "error creating source: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusCreated, created.ToResponse())
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
		return SendError(c, fiber.StatusInternalServerError, "error deleting source: "+err.Error())
	}

	return SendSuccess(c, fiber.StatusOK, fiber.Map{"message": "Source deleted successfully"})
}
