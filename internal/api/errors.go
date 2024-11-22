package api

import (
	"log/slog"

	"github.com/labstack/echo/v4"
)

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
