package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mr-karan/logchef/pkg/models"
)

// Response defines the standard JSON structure for API responses.
type Response struct {
	Status    string      `json:"status"` // "success" or "error"
	Data      interface{} `json:"data,omitempty"`
	Message   string      `json:"message,omitempty"`
	ErrorType string      `json:"error_type,omitempty"` // Application-specific error type code.
}

// NewSuccessResponse creates a standard success response structure.
func NewSuccessResponse(data interface{}) Response {
	return Response{
		Status: "success",
		Data:   data,
	}
}

// NewErrorResponse creates a standard error response structure.
// It accepts various error types and maps them to a consistent JSON format.
func NewErrorResponse(err interface{}, errorType models.ErrorType) Response {
	var errMsg string
	// Handle different input error types gracefully.
	switch e := err.(type) {
	case error:
		errMsg = e.Error()
	case string:
		errMsg = e
	case models.ErrorResponse: // Allow passing pre-formatted ErrorResponse
		return Response{
			Status:    "error",
			Message:   e.Message,
			ErrorType: string(e.Type),
		}
	default:
		errMsg = "An unexpected error occurred" // Avoid exposing sensitive details.
	}

	// Default to GeneralErrorType if none is specified.
	if errorType == "" {
		errorType = models.GeneralErrorType
	}

	return Response{
		Status:    "error",
		Message:   errMsg,
		ErrorType: string(errorType),
	}
}

// SendSuccess is a helper function to easily send a successful JSON response
// with the given HTTP status code and data payload.
func SendSuccess(c *fiber.Ctx, status int, data interface{}) error {
	return c.Status(status).JSON(NewSuccessResponse(data))
}

// SendError is a helper function to easily send a JSON error response
// with the given HTTP status code and error message.
// It uses the GeneralErrorType by default.
func SendError(c *fiber.Ctx, status int, err interface{}) error {
	// Use default error type if none is specified.
	return c.Status(status).JSON(NewErrorResponse(err, ""))
}

// SendErrorWithType is a helper function to easily send a JSON error response
// with the given HTTP status code, error message, and a specific application error type.
func SendErrorWithType(c *fiber.Ctx, status int, err interface{}, errorType models.ErrorType) error {
	return c.Status(status).JSON(NewErrorResponse(err, errorType))
}
