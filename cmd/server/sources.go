package main

import (
	"database/sql"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/mr-karan/logchef/internal/errors"
	"github.com/mr-karan/logchef/internal/sources"
)

type SourceHandler struct {
	service *sources.Service
}

func NewSourceHandler(service *sources.Service) *SourceHandler {
	return &SourceHandler{
		service: service,
	}
}

func (h *SourceHandler) Create(c echo.Context) error {
	var req sources.CreateSourceRequest
	if err := c.Bind(&req); err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid JSON payload")
	}

	source, err := h.service.Create(c.Request().Context(), req)
	if err != nil {
		if errors.IsValidationError(err) {
			return HandleValidationError(c, err, "Invalid source configuration", nil)
		}
		if errors.IsConflictError(err) {
			return HandleConflictError(c, err, "Source already exists")
		}
		if err == sql.ErrNoRows {
			return HandleError(c, err, http.StatusNotFound, "Source not found")
		}
		return HandleError(c, err, http.StatusInternalServerError, "Failed to create source")
	}

	return c.JSON(http.StatusCreated, NewResponse(source))
}

func (h *SourceHandler) Get(c echo.Context) error {
	id := c.Param("id")
	source, err := h.service.Get(c.Request().Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			return HandleError(c, err, http.StatusNotFound, "Source not found")
		}
		return HandleError(c, err, http.StatusInternalServerError, "Failed to fetch source")
	}
	return c.JSON(http.StatusOK, NewResponse(source))
}

func (h *SourceHandler) Update(c echo.Context) error {
	id := c.Param("id")

	var req sources.UpdateSourceRequest
	if err := c.Bind(&req); err != nil {
		return HandleError(c, err, http.StatusBadRequest, "Invalid request payload")
	}

	if err := h.service.Update(c.Request().Context(), id, req); err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to update source")
	}

	return c.JSON(http.StatusOK, NewResponse(map[string]interface{}{
		"message":  "TTL updated successfully",
		"ttl_days": req.TTLDays,
	}))
}

func (h *SourceHandler) List(c echo.Context) error {
	sources, err := h.service.List(c.Request().Context())
	if err != nil {
		return HandleError(c, err, http.StatusInternalServerError, "Failed to list sources")
	}

	return c.JSON(http.StatusOK, NewResponse(sources))
}

func (h *SourceHandler) Delete(c echo.Context) error {
	id := c.Param("id")

	if err := h.service.Delete(c.Request().Context(), id); err != nil {
		if err == sql.ErrNoRows {
			return HandleError(c, err, http.StatusNotFound, "Source not found")
		}
		return HandleError(c, err, http.StatusInternalServerError, "Failed to delete source")
	}

	return c.JSON(http.StatusOK, NewResponse(map[string]string{
		"message": "Source deleted successfully",
	}))
}
