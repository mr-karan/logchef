package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestRequireTokenScopeRejectsMissingScope(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	app.Post("/saved", func(c *fiber.Ctx) error {
		c.Locals("auth_method", "token")
		c.Locals("user", &models.User{ID: 1, Email: "svc@example.com", Role: models.UserRoleMember})
		c.Locals("api_token", &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeLogsRead}})
		return c.Next()
	}, s.requireTokenScope(models.TokenScopeSavedQueriesWrite), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/saved", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusForbidden)
	}
}

func TestRequireTokenScopeAllowsMatchingScope(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	app.Post("/logs", func(c *fiber.Ctx) error {
		c.Locals("auth_method", "token")
		c.Locals("api_token", &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeLogsRead}})
		return c.Next()
	}, s.requireTokenScope(models.TokenScopeLogsRead), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/logs", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
}

func TestRequireTokenScopeSkipsSessions(t *testing.T) {
	t.Parallel()
	app := fiber.New()
	s := &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	app.Post("/saved", func(c *fiber.Ctx) error {
		c.Locals("auth_method", "session")
		return c.Next()
	}, s.requireTokenScope(models.TokenScopeSavedQueriesWrite), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/saved", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusNoContent)
	}
}
