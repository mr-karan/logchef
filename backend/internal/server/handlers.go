package server

import (
	"fmt"
	"net/http"
	"time"

	"backend-v2/pkg/clickhouse"
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
	if err := s.svc.ExploreSource(c.Context(), source); err != nil {
		return c.Status(http.StatusInternalServerError).JSON(Response{
			Status: "error",
			Data: fiber.Map{
				"error": fmt.Sprintf("Failed to get schema: %v", err),
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

// QueryLogsRequest represents the request parameters for querying logs
type QueryLogsRequest struct {
	StartTimestamp int64 `json:"start_timestamp" query:"start_timestamp"`
	EndTimestamp   int64 `json:"end_timestamp" query:"end_timestamp"`
	Limit          int   `json:"limit" query:"limit"`
}

// handleQueryLogs handles requests to query logs from a source
func (s *Server) handleQueryLogs(c *fiber.Ctx) error {
	sourceID := c.Params("id")
	if sourceID == "" {
		return fiber.NewError(http.StatusBadRequest, "source id is required")
	}

	// Parse query parameters
	var req QueryLogsRequest
	if err := c.QueryParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid query parameters: %v", err))
	}

	// Validate and set defaults
	if req.Limit <= 0 {
		req.Limit = 100 // default limit
	}
	if req.Limit > 1000 {
		req.Limit = 1000 // max limit
	}

	// Validate timestamps
	if req.StartTimestamp > 0 && req.EndTimestamp > 0 && req.StartTimestamp > req.EndTimestamp {
		return fiber.NewError(http.StatusBadRequest, "start_timestamp cannot be greater than end_timestamp")
	}

	// Convert timestamps to time.Time
	params := clickhouse.LogQueryParams{
		Limit: req.Limit,
	}
	if req.StartTimestamp > 0 {
		params.StartTime = time.UnixMilli(req.StartTimestamp).UTC()
	}
	if req.EndTimestamp > 0 {
		params.EndTime = time.UnixMilli(req.EndTimestamp).UTC()
	}

	// Query logs
	result, err := s.svc.QueryLogs(c.Context(), sourceID, params)
	if err != nil {
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("error querying logs: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"logs":  result.Data,
			"stats": result.Stats,
			"params": fiber.Map{
				"source_id":       sourceID,
				"limit":           req.Limit,
				"start_timestamp": req.StartTimestamp,
				"end_timestamp":   req.EndTimestamp,
			},
		},
	})
}
