package server

import (
	"fmt"
	"net/http"
	"time"

	"backend-v2/internal/service"
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
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to list sources: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"sources": sources,
		},
	})
}

// handleGetSource handles getting a single source
func (s *Server) handleGetSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "source id is required")
	}

	source, err := s.svc.GetSource(c.Context(), id)
	if err != nil {
		if err == service.ErrSourceNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("source %s not found", id))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to get source: %v", err))
	}

	if source == nil {
		return fiber.NewError(http.StatusNotFound, fmt.Sprintf("source %s not found", id))
	}

	// Get schema information
	if err := s.svc.ExploreSource(c.Context(), source); err != nil {
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to get schema: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"source": source,
		},
	})
}

// CreateSourceRequest represents a request to create a new source
type CreateSourceRequest struct {
	SchemaType  string                `json:"schema_type"`
	Connection  models.ConnectionInfo `json:"connection"`
	Description string                `json:"description"`
	TTLDays     int                   `json:"ttl_days"`
}

// handleCreateSource handles creating a new source
func (s *Server) handleCreateSource(c *fiber.Ctx) error {
	var req CreateSourceRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	if err := service.ValidateCreateSourceRequest(req.SchemaType, req.Connection, req.Description, req.TTLDays); err != nil {
		return fiber.NewError(http.StatusBadRequest, err.Error())
	}

	created, err := s.svc.CreateSource(c.Context(), req.SchemaType, req.Connection, req.Description, req.TTLDays)
	if err != nil {
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to create source: %v", err))
	}

	return c.Status(http.StatusCreated).JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"source": created,
		},
	})
}

// handleDeleteSource handles deleting a source
func (s *Server) handleDeleteSource(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "source id is required")
	}

	if err := s.svc.DeleteSource(c.Context(), id); err != nil {
		if err == service.ErrSourceNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("source %s not found", id))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to delete source: %v", err))
	}

	return c.JSON(Response{
		Status: "success",
		Data: fiber.Map{
			"message": "Source deleted successfully",
		},
	})
}

// LogQueryResponse represents the response structure for log queries
type LogQueryResponse struct {
	Logs   interface{}            `json:"logs"`
	Stats  interface{}            `json:"stats"`
	Params LogQueryResponseParams `json:"params"`
}

// LogQueryResponseParams represents the query parameters used in the response
type LogQueryResponseParams struct {
	SourceID       string               `json:"source_id"`
	FilterGroups   []models.FilterGroup `json:"filter_groups"`
	Limit          int                  `json:"limit"`
	StartTimestamp int64                `json:"start_timestamp"`
	EndTimestamp   int64                `json:"end_timestamp"`
	Sort           *models.SortOptions  `json:"sort"`
}

// handleQueryLogs handles POST requests to search/query logs from a source
func (s *Server) handleQueryLogs(c *fiber.Ctx) error {
	id := c.Params("id")
	if id == "" {
		return fiber.NewError(http.StatusBadRequest, "source id is required")
	}

	var req models.LogQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, fmt.Sprintf("invalid request body: %v", err))
	}

	// Set default mode to filters for backward compatibility
	if req.Mode == "" {
		req.Mode = models.QueryModeFilters
	}

	if err := service.ValidateLogQueryRequest(&req); err != nil {
		return fiber.NewError(http.StatusBadRequest, err.Error())
	}

	// Additional validation for timestamps
	if req.StartTimestamp > req.EndTimestamp {
		return fiber.NewError(http.StatusBadRequest, "start_timestamp cannot be greater than end_timestamp")
	}

	// Set default limit if not provided
	if req.Limit == 0 {
		req.Limit = 100 // default limit
	}

	// Convert to query params
	params := clickhouse.LogQueryParams{
		Limit:        req.Limit,
		StartTime:    time.UnixMilli(req.StartTimestamp).UTC(),
		EndTime:      time.UnixMilli(req.EndTimestamp).UTC(),
		FilterGroups: req.FilterGroups,
		Sort:         req.Sort,
		Mode:         req.Mode,
		RawSQL:       req.RawSQL,
		LogChefQL:    req.LogChefQL,
	}

	// Set default sort if not provided
	if req.Sort == nil {
		params.Sort = &models.SortOptions{
			Field: "timestamp",
			Order: models.SortOrderDesc,
		}
	} else {
		params.Sort = req.Sort
	}

	fmt.Println("filter groups", req.FilterGroups)

	// Query logs
	result, err := s.svc.QueryLogs(c.Context(), id, params)
	if err != nil {
		if err == service.ErrSourceNotFound {
			return fiber.NewError(http.StatusNotFound, fmt.Sprintf("source %s not found", id))
		}
		return fiber.NewError(http.StatusInternalServerError, fmt.Sprintf("failed to query logs: %v", err))
	}

	response := LogQueryResponse{
		Logs:  result.Data,
		Stats: result.Stats,
		Params: LogQueryResponseParams{
			SourceID:       id,
			FilterGroups:   req.FilterGroups,
			Limit:          req.Limit,
			StartTimestamp: req.StartTimestamp,
			EndTimestamp:   req.EndTimestamp,
			Sort:           req.Sort,
		},
	}

	return c.JSON(Response{
		Status: "success",
		Data:   response,
	})
}
