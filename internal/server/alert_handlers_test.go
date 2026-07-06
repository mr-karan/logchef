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

func TestValidateAlertsEnabled(t *testing.T) {
	t.Parallel()

	t.Run("enabled returns nil", func(t *testing.T) {
		t.Parallel()
		s := &Server{config: &config.Config{Alerts: config.AlertsConfig{Enabled: true}}}
		if got := s.validateAlertsEnabled(); got != nil {
			t.Fatalf("validateAlertsEnabled() = non-nil (%T), want nil", got)
		}
	})

	t.Run("disabled returns 503 responder", func(t *testing.T) {
		t.Parallel()
		s := &Server{config: &config.Config{Alerts: config.AlertsConfig{Enabled: false}}}
		responder := s.validateAlertsEnabled()
		if responder == nil {
			t.Fatalf("validateAlertsEnabled() = nil, want non-nil responder")
		}

		app := fiber.New()
		app.Get("/probe", func(c *fiber.Ctx) error {
			return responder(c)
		})

		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/probe", nil))
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer resp.Body.Close()

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
