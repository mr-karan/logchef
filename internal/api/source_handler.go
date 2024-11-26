package api

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/db"
	"github.com/mr-karan/logchef/internal/models"
)

type SourceHandler struct {
	sourceRepo *models.SourceRepository
	clickhouse *db.Clickhouse
}

// // parseDSN converts a DSN string into ClickhouseConfig
// func parseDSN(dsn string) config.ClickhouseConfig {
// 	// TODO: Implement proper DSN parsing
// 	// For now, just log the DSN and return default config
// 	slog.Info("Received DSN", "dsn", dsn)
// 	return config.ClickhouseConfig{
// 		Host:        "localhost",
// 		Port:        9000,
// 		Database:    "default",
// 		Username:    "default",
// 		Password:    "",
// 		DialTimeout: 10 * time.Second,
// 	}
// }

// parseDSN converts a DSN string into ClickhouseConfig
func parseDSN(dsn string) config.ClickhouseConfig {
	parsedURL, err := url.Parse(dsn)
	if err != nil {
		slog.Error("Failed to parse DSN", "dsn", dsn, "error", err)
		return config.ClickhouseConfig{}
	}

	port, _ := strconv.Atoi(parsedURL.Port())
	queryParams := parsedURL.Query()

	dialTimeout, _ := time.ParseDuration(queryParams.Get("dial_timeout"))

	return config.ClickhouseConfig{
		Host:        parsedURL.Hostname(),
		Port:        port,
		Database:    strings.TrimPrefix(parsedURL.Path, "/"),
		Username:    queryParams.Get("username"),
		Password:    queryParams.Get("password"),
		DialTimeout: dialTimeout,
	}
}

func NewSourceHandler(sourceRepo *models.SourceRepository, clickhouse *db.Clickhouse) *SourceHandler {
	return &SourceHandler{
		sourceRepo: sourceRepo,
		clickhouse: clickhouse,
	}
}

type CreateSourceRequest struct {
	Name       string `json:"name"`
	SchemaType string `json:"schema_type"`
	TTLDays    int    `json:"ttl_days"`
	DSN        string `json:"dsn"`
}

func (h *SourceHandler) Create(c echo.Context) error {
	var req CreateSourceRequest
	if err := c.Bind(&req); err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid JSON payload")
	}

	if req.TTLDays == 0 {
		req.TTLDays = 90 // default TTL
	}

	// Validate mandatory fields
	if req.Name == "" {
		return HandleError(c, fmt.Errorf("name cannot be empty"), http.StatusBadRequest, "Name cannot be empty")
	}
	if req.SchemaType == "" {
		return HandleError(c, fmt.Errorf("schema type cannot be empty"), http.StatusBadRequest, "Schema type cannot be empty")
	}
	if req.DSN == "" {
		return HandleError(c, fmt.Errorf("dsn cannot be empty"), http.StatusBadRequest, "DSN cannot be empty")
	}

	// Check if source with name already exists
	existingSource, err := h.sourceRepo.GetByName(req.Name)
	if err != nil && err != sql.ErrNoRows {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to check existing source")
	}
	if existingSource != nil {
		return HandleError(c, fmt.Errorf("source with name '%s' already exists", req.Name), http.StatusConflict, fmt.Sprintf("Source with name '%s' already exists", req.Name))
	}

	// Validate name format
	if len(req.Name) < 4 || len(req.Name) > 30 {
		return HandleError(c, fmt.Errorf("name length invalid"), http.StatusBadRequest, "Name must be between 4 and 30 characters")
	}
	for _, char := range req.Name {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_') {
			return HandleError(c, fmt.Errorf("invalid name format"), http.StatusBadRequest, "Name must contain only alphanumeric characters and underscores")
		}
	}

	source := &models.Source{
		ID:         uuid.New().String(),
		Name:       req.Name,
		TableName:  req.Name,
		SchemaType: req.SchemaType,
		DSN:        req.DSN,
	}

	// Parse DSN into ClickhouseConfig
	cfg := parseDSN(req.DSN)

	// Create table in Clickhouse
	if err := h.clickhouse.CreateSourceTable(source.ID, cfg, source.TableName, req.TTLDays); err != nil {
		return HandleError(c, fmt.Errorf("failed to create source with DSN %s: %w", req.DSN, err), http.StatusInternalServerError, fmt.Errorf("Failed to create source: %w", err).Error())
	}

	// Store metadata in SQLite
	if err := h.sourceRepo.Create(source); err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to create source")
	}

	// Initialize connection in the pool
	if err := h.clickhouse.GetPool().AddConnection(source.ID, cfg); err != nil {
		// Cleanup the created source if connection fails
		_ = h.sourceRepo.Delete(source.ID)
		return HandleError(c, fmt.Errorf("failed to initialize connection: %w", err), http.StatusInternalServerError, "Failed to initialize connection")
	}

	return c.JSON(http.StatusCreated, NewResponse(source))
}

func (h *SourceHandler) Get(c echo.Context) error {
	id := c.Param("id")
	source, err := h.sourceRepo.Get(id)
	if err != nil {
		if err == sql.ErrNoRows {
			return HandleError(c, err, http.StatusNotFound, "Source not found")
		}
		return HandleError(c, err, http.StatusInternalServerError, "Failed to fetch source")
	}
	return c.JSON(http.StatusOK, NewResponse(source))
}

type UpdateSourceRequest struct {
	TTLDays int `json:"ttl_days"`
}

func (h *SourceHandler) Update(c echo.Context) error {
	id := c.Param("id")

	// First check if source exists
	source, err := h.sourceRepo.Get(id)
	if err != nil {
		if err == sql.ErrNoRows {
			return HandleError(c, err, http.StatusNotFound, "Source not found")
		}
		return HandleError(c, err, http.StatusInternalServerError, "Failed to fetch source")
	}

	var req UpdateSourceRequest
	if err := c.Bind(&req); err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid request payload")
	}

	// Validate TTL
	if req.TTLDays <= 0 {
		return HandleError(c, fmt.Errorf("invalid TTL days: %d", req.TTLDays), http.StatusBadRequest, "TTL days must be greater than 0")
	}

	// Update TTL in Clickhouse
	if err := h.clickhouse.UpdateTableTTL(source.ID, source.TableName, req.TTLDays); err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to update TTL")
	}

	return c.JSON(http.StatusOK, NewResponse(map[string]interface{}{
		"message":  "TTL updated successfully",
		"ttl_days": req.TTLDays,
	}))
}

func (h *SourceHandler) List(c echo.Context) error {
	sources, err := h.sourceRepo.List()
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to list sources")
	}

	// Ensure we return empty array instead of null
	if sources == nil {
		sources = []*models.Source{}
	}

	// Initialize connections for all sources
	for _, source := range sources {
		cfg := parseDSN(source.DSN)
		// Try to add connection if not exists
		if err := h.clickhouse.GetPool().AddConnection(source.ID, cfg); err != nil {
			slog.Error("failed to initialize connection for source",
				"source_id", source.ID,
				"error", err,
			)
		}
	}

	return c.JSON(http.StatusOK, NewResponse(sources))
}

func (h *SourceHandler) Delete(c echo.Context) error {
	id := c.Param("id")

	// First get the source to check if it exists and get table name
	source, err := h.sourceRepo.Get(id)
	if err != nil {
		if err == sql.ErrNoRows {
			return HandleError(c, err, http.StatusNotFound, "Source not found")
		}
		return HandleError(c, err, http.StatusInternalServerError, "Failed to fetch source")
	}

	// Delete metadata from SQLite
	if err := h.sourceRepo.Delete(id); err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to delete source")
	}

	// Drop table in Clickhouse
	if err := h.clickhouse.DropSourceTable(source.ID, source.TableName); err != nil {
		// Log error but don't return it since metadata is already deleted
		slog.Error("failed to drop clickhouse table",
			"error", err,
			"source_id", source.ID,
			"table", source.TableName,
		)
	}

	return c.JSON(http.StatusOK, NewResponse(map[string]string{
		"message": "Source deleted successfully",
	}))
}
