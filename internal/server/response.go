package server

import (
	"github.com/gofiber/fiber/v2"
	"github.com/mr-karan/logchef/pkg/models"
)

// Response represents a standard API response
type Response struct {
	Status    string      `json:"status"`
	Data      interface{} `json:"data,omitempty"`
	Message   string      `json:"message,omitempty"`
	ErrorType string      `json:"error_type,omitempty"`
}

// NewSuccessResponse creates a standard success response
func NewSuccessResponse(data interface{}) Response {
	return Response{
		Status: "success",
		Data:   data,
	}
}

// NewErrorResponse creates a standard error response
func NewErrorResponse(err interface{}, errorType models.ErrorType) Response {
	var errMsg string
	switch e := err.(type) {
	case error:
		errMsg = e.Error()
	case string:
		errMsg = e
	case models.ErrorResponse:
		return Response{
			Status:    "error",
			Message:   e.Message,
			ErrorType: string(e.Type),
		}
	default:
		errMsg = "unknown error"
	}

	if errorType == "" {
		errorType = models.GeneralErrorType
	}

	return Response{
		Status:    "error",
		Message:   errMsg,
		ErrorType: string(errorType),
	}
}

// SendSuccess is a convenience method to send a success response
func SendSuccess(c *fiber.Ctx, status int, data interface{}) error {
	return c.Status(status).JSON(NewSuccessResponse(data))
}

// SendError is a convenience method to send an error response
func SendError(c *fiber.Ctx, status int, err interface{}) error {
	return c.Status(status).JSON(NewErrorResponse(err, ""))
}

// SendErrorWithType is a convenience method to send an error response with a specific error type
func SendErrorWithType(c *fiber.Ctx, status int, err interface{}, errorType models.ErrorType) error {
	return c.Status(status).JSON(NewErrorResponse(err, errorType))
}
