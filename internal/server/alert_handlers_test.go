package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/config"
)

func TestRequireAlertsEnabled(t *testing.T) {
	t.Parallel()

	t.Run("enabled calls next", func(t *testing.T) {
		t.Parallel()
		s := &Server{config: &config.Config{Alerts: config.AlertsConfig{Enabled: true}}}

		app := fiber.New()
		app.Get("/probe", s.requireAlertsEnabled, func(c *fiber.Ctx) error {
			return c.SendString("reached")
		})

		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/probe", nil))
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(body) != "reached" {
			t.Fatalf("body = %q, want the downstream handler to run", body)
		}
	})

	t.Run("disabled short-circuits with 503", func(t *testing.T) {
		t.Parallel()
		s := &Server{config: &config.Config{Alerts: config.AlertsConfig{Enabled: false}}}

		reached := false
		app := fiber.New()
		app.Get("/probe", s.requireAlertsEnabled, func(c *fiber.Ctx) error {
			reached = true
			return c.SendString("reached")
		})

		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/probe", nil))
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer resp.Body.Close()

		if reached {
			t.Fatal("downstream handler ran, want it short-circuited by the middleware")
		}
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		var envelope struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			t.Fatalf("unmarshal body: %v (body=%q)", err, body)
		}
		if envelope.Status != "error" {
			t.Fatalf("status = %q, want %q", envelope.Status, "error")
		}
		if !strings.Contains(envelope.Message, "alerts.enabled") {
			t.Fatalf("message = %q, want it to reference alerts.enabled", envelope.Message)
		}
		if !strings.Contains(envelope.Message, "LOGCHEF_ALERTS__ENABLED") {
			t.Fatalf("message = %q, want it to reference LOGCHEF_ALERTS__ENABLED", envelope.Message)
		}
	})
}
