package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/config"
)

func TestHandleLocalLoginGating(t *testing.T) {
	t.Parallel()

	newApp := func(enabled bool) *fiber.App {
		s := &Server{config: &config.Config{Auth: config.AuthConfig{Local: config.LocalAuthConfig{Enabled: enabled}}}}
		app := fiber.New()
		app.Post("/login", s.handleLocalLogin)
		return app
	}

	t.Run("disabled returns 404", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"a@example.com","password":"x-y-z-12345"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := newApp(false).Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("missing fields returns 400", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"a@example.com"}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err := newApp(true).Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status = %d, want 400", resp.StatusCode)
		}
	})
}
