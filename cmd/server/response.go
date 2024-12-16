package main

import (
	"log/slog"

	"github.com/labstack/echo/v4"
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
	return c.JSON(status, NewErrorResponse(userMsg, "GeneralException", nil))
}

// Move response related code here...
