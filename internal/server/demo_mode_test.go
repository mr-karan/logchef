package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestEnforceDemoReadOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "read remains available", method: http.MethodGet, path: "/api/v1/dashboards", want: http.StatusNoContent},
		{name: "local login remains available", method: http.MethodPost, path: "/api/v1/auth/local/login", want: http.StatusNoContent},
		{name: "logout remains available", method: http.MethodPost, path: "/api/v1/auth/logout", want: http.StatusNoContent},
		{name: "log query remains available", method: http.MethodPost, path: "/api/v1/teams/1/sources/5/logs/query", want: http.StatusNoContent},
		{name: "query cancellation remains available", method: http.MethodPost, path: "/api/v1/teams/1/sources/5/logs/query/abc/cancel", want: http.StatusNoContent},
		{name: "logchefql translation remains available", method: http.MethodPost, path: "/api/v1/teams/1/sources/5/logchefql/translate", want: http.StatusNoContent},
		{name: "dashboard create is blocked", method: http.MethodPost, path: "/api/v1/dashboards", want: http.StatusForbidden},
		{name: "preference update is blocked", method: http.MethodPut, path: "/api/v1/me/preferences", want: http.StatusForbidden},
		{name: "source validation is blocked", method: http.MethodPost, path: "/api/v1/admin/sources/validate", want: http.StatusForbidden},
		{name: "exports stay blocked", method: http.MethodPost, path: "/api/v1/teams/1/sources/5/logs/export", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &Server{config: &config.Config{Demo: config.DemoConfig{ReadOnly: true}}}
			app := fiber.New()
			app.Use(s.enforceDemoReadOnly)
			app.Add(tt.method, "/*", func(c *fiber.Ctx) error { return c.SendStatus(http.StatusNoContent) })

			resp, err := app.Test(httptest.NewRequest(tt.method, tt.path, http.NoBody))
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.want)
			}
			if tt.want == http.StatusForbidden {
				var body Response
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				if body.ErrorType != "DEMO_INSTANCE" || body.Message != publicDemoReadOnlyMessage {
					t.Fatalf("unexpected response: %+v", body)
				}
			}
		})
	}
}

func TestEnforceDemoReadOnlyDisabled(t *testing.T) {
	t.Parallel()

	s := &Server{config: &config.Config{}}
	app := fiber.New()
	app.Use(s.enforceDemoReadOnly)
	app.Post("/api/v1/dashboards", func(c *fiber.Ctx) error { return c.SendStatus(http.StatusNoContent) })

	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/v1/dashboards", http.NoBody))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestEnforceDemoReadOnlyAllowsTrustedProvisioning(t *testing.T) {
	t.Parallel()

	s := &Server{config: &config.Config{Demo: config.DemoConfig{
		ReadOnly:          true,
		ProvisioningToken: "private-bootstrap-token",
	}}}
	app := fiber.New()
	app.Use(s.enforceDemoReadOnly)
	app.Post("/api/v1/dashboards", func(c *fiber.Ctx) error { return c.SendStatus(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboards", http.NoBody)
	req.Header.Set(demoProvisioningTokenHeader, "private-bootstrap-token")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestEnforceDemoReadOnlyRejectsWrongProvisioningToken(t *testing.T) {
	t.Parallel()

	s := &Server{config: &config.Config{Demo: config.DemoConfig{
		ReadOnly:          true,
		ProvisioningToken: "private-bootstrap-token",
	}}}
	app := fiber.New()
	app.Use(s.enforceDemoReadOnly)
	app.Post("/api/v1/dashboards", func(c *fiber.Ctx) error { return c.SendStatus(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/dashboards", http.NoBody)
	req.Header.Set(demoProvisioningTokenHeader, "wrong-token")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestDemoReadOnlyDoesNotExposeSharedQueryHistory(t *testing.T) {
	t.Parallel()

	s := &Server{config: &config.Config{Demo: config.DemoConfig{ReadOnly: true}}}
	app := fiber.New()
	app.Get("/api/v1/me/query-history", func(c *fiber.Ctx) error {
		c.Locals("user", &models.User{ID: 1})
		return s.handleListQueryHistory(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/me/query-history", http.NoBody))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var envelope struct {
		Data []models.QueryHistory `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(envelope.Data) != 0 {
		t.Fatalf("history = %+v, want empty", envelope.Data)
	}
}
