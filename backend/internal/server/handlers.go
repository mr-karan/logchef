package server

import (
	"fmt"
	"net/http"
	"strconv"
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

	// Get schema information
	columns, err := s.svc.ExploreSource(c.Context(), source)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": fmt.Sprintf("Failed to get schema: %v", err),
			},
		})
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"source":  source,
			"columns": columns,
		},
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

// handleQueryLogs handles querying logs from a source
func (s *Server) handleQueryLogs(c *fiber.Ctx) error {
	sourceID := c.Query("source")
	if sourceID == "" {
		return c.Status(http.StatusBadRequest).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": "source parameter is required",
			},
		})
	}

	// Parse pagination parameters
	limit := 100 // default limit
	offset := 0  // default offset

	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			offset = parsedOffset
		}
	}

	logs, err := s.svc.QueryLogs(c.Context(), sourceID, limit, offset)
	if err != nil {
		return c.Status(http.StatusInternalServerError).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": fmt.Sprintf("Failed to query logs: %v", err),
			},
		})
	}

	params := make(map[string]string)
	params["source"] = sourceID
	params["limit"] = strconv.Itoa(limit)
	params["offset"] = strconv.Itoa(offset)

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"logs":   logs,
			"params": params,
		},
	})
}

// RegisterRoutes registers all routes for the server
func (s *Server) RegisterRoutes(app *fiber.App) {
	app.Get("/health", s.handleHealth)

	// Sources routes
	app.Get("/sources", s.handleListSources)
	app.Get("/sources/:id", s.handleGetSource)
	app.Post("/sources", s.handleCreateSource)
	app.Delete("/sources/:id", s.handleDeleteSource)

	// Query routes
	app.Get("/query/logs", s.handleQueryLogs)
}
