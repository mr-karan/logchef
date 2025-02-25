package server

import (
	"github.com/gofiber/fiber/v2"
)

// Response represents a standard API response
type Response struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
}

// NewSuccessResponse creates a standard success response
func NewSuccessResponse(data interface{}) Response {
	return Response{
		Status: "success",
		Data:   data,
	}
}

// NewErrorResponse creates a standard error response
func NewErrorResponse(err interface{}) Response {
	var errMsg string
	switch e := err.(type) {
	case error:
		errMsg = e.Error()
	case string:
		errMsg = e
	default:
		errMsg = "unknown error"
	}

	return Response{
		Status: "error",
		Data: fiber.Map{
			"error": errMsg,
		},
	}
}

// SendSuccess is a convenience method to send a success response
func SendSuccess(c *fiber.Ctx, status int, data interface{}) error {
	return c.Status(status).JSON(NewSuccessResponse(data))
}

// SendError is a convenience method to send an error response
func SendError(c *fiber.Ctx, status int, err interface{}) error {
	return c.Status(status).JSON(NewErrorResponse(err))
}
