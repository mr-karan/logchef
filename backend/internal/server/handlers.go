package server

import (
	"fmt"
	"net/http"
	"time"

	"backend-v2/pkg/models"

	"github.com/gofiber/fiber/v2"
)

// Response represents a standard API response
type Response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
}

// handleHealth handles the health check endpoint
func (s *Server) handleHealth(c *fiber.Ctx) error {
	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"status": "ok",
			"time":   time.Now(),
		},
	})
}

// handleListSources handles listing all sources
func (s *Server) handleListSources(c *fiber.Ctx) error {
	sources, err := s.svc.ListSources(c.Context())
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "Failed to list sources",
			},
		})
	}

	// Ensure we return an empty array instead of null
	if sources == nil {
		sources = make([]*models.Source, 0)
	}

	return c.JSON(Response{
		Status: "success",
		Data:   sources,
	})
}

// handleGetSource handles getting a single source
func (s *Server) handleGetSource(c *fiber.Ctx) error {
	id := c.Params("id")
	source, err := s.svc.GetSource(c.Context(), id)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "Failed to get source",
			},
		})
	}
	if source == nil {
		return c.Status(http.StatusNotFound).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "Source not found",
			},
		})
	}

	return c.JSON(Response{
		Status: "success",
		Data:   source,
	})
}

// handleCreateSource handles creating a new source
func (s *Server) handleCreateSource(c *fiber.Ctx) error {
	var source struct {
		TableName   string `json:"table_name"`
		SchemaType  string `json:"schema_type"`
		DSN         string `json:"dsn"`
		Description string `json:"description"`
		TTLDays     int    `json:"ttl_days"`
	}

	if err := c.BodyParser(&source); err != nil {
		return c.Status(http.StatusBadRequest).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "Invalid request body",
			},
		})
	}

	created, err := s.svc.CreateSource(c.Context(), source.TableName, source.SchemaType, source.DSN, source.Description, source.TTLDays)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": fmt.Sprintf("Failed to create source: %v", err),
			},
		})
	}

	return c.Status(http.StatusCreated).JSON(Response{
		Status: "success",
		Data:   created,
	})
}

// handleDeleteSource handles deleting a source
func (s *Server) handleDeleteSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if err := s.svc.DeleteSource(c.Context(), id); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "Failed to delete source",
			},
		})
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"message": "Source deleted successfully",
		},
	})
}
