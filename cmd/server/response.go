package main

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/mr-karan/logchef/internal/errors"
)

// Error types
const (
	ErrorTypeValidation   = "ValidationError"
	ErrorTypeNotFound     = "NotFoundError"
	ErrorTypeGeneral      = "GeneralException"
	ErrorTypeUnauthorized = "UnauthorizedError"
	ErrorTypeBadRequest   = "BadRequestError"
	ErrorTypeConflict     = "ConflictError"
)

// Response represents the standard API response envelope
type Response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
}

// ErrorResponse represents the error response envelope
type ErrorResponse struct {
	Status    string      `json:"status"`
	Message   string      `json:"message"`
	ErrorType string      `json:"error_type"`
	Data      interface{} `json:"data,omitempty"`
}

// NewResponse creates a success response
func NewResponse(data interface{}) Response {
	return Response{
		Status: "success",
		Data:   data,
	}
}

// NewErrorResponse creates an error response
func NewErrorResponse(message string, errorType string, data interface{}) ErrorResponse {
	return ErrorResponse{
		Status:    "error",
		Message:   message,
		ErrorType: errorType,
		Data:      data,
	}
}

// HandleError logs the error and returns an appropriate HTTP response
func HandleError(c echo.Context, err error, status int, userMsg string) error {
	// Log the actual error with details
	slog.Error("api error",
		"error", err,
		"status", status,
		"path", c.Path(),
		"method", c.Request().Method,
	)

	// Return a standardized error response
	return c.JSON(status, NewErrorResponse(userMsg, ErrorTypeGeneral, nil))
}

// HandleValidationError handles validation errors with appropriate status code and message
func HandleValidationError(c echo.Context, err error, userMsg string, validationErrors interface{}) error {
	if validErr, ok := err.(*errors.ValidationError); ok {
		// Log the validation error with details
		slog.Error("validation error",
			"error", err,
			"path", c.Path(),
			"method", c.Request().Method,
			"field", validErr.Field,
			"validation_errors", validationErrors,
		)

		// Return a validation error response with details
		return c.JSON(http.StatusUnprocessableEntity, NewErrorResponse(userMsg, ErrorTypeValidation, map[string]interface{}{
			"field":   validErr.Field,
			"message": validErr.Message,
		}))
	}

	// Fallback to generic validation error
	return c.JSON(http.StatusUnprocessableEntity, NewErrorResponse(userMsg, ErrorTypeValidation, validationErrors))
}

// HandleConflictError handles conflict errors with appropriate status code and message
func HandleConflictError(c echo.Context, err error, userMsg string) error {
	// Log the conflict error with details
	slog.Error("conflict error",
		"error", err,
		"path", c.Path(),
		"method", c.Request().Method,
	)

	// Return a conflict error response
	return c.JSON(http.StatusConflict, NewErrorResponse(userMsg, ErrorTypeConflict, nil))
}

// Move response related code here...
