package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/mr-karan/logchef/internal/config"
)

func TestHandleGetMetaAlertsEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		enabled bool
	}{
		{name: "alerts enabled", enabled: true},
		{name: "alerts disabled", enabled: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			s := &Server{
				version: "test",
				config: &config.Config{
					Server: config.ServerConfig{HTTPServerTimeout: 30 * time.Second},
					Alerts: config.AlertsConfig{Enabled: tc.enabled},
				},
			}

			app := fiber.New()
			app.Get("/api/v1/meta", s.handleGetMeta)

			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/meta", http.NoBody))
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

			// Assert the raw JSON key so the field name in the wire format
			// is validated, not just the Go struct decoding.
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Fatalf("unmarshal envelope: %v (body=%q)", err, body)
			}
			dataRaw, ok := raw["data"]
			if !ok {
				t.Fatalf("response missing data field: %q", body)
			}
			var data map[string]json.RawMessage
			if err := json.Unmarshal(dataRaw, &data); err != nil {
				t.Fatalf("unmarshal data: %v (data=%q)", err, dataRaw)
			}
			ae, ok := data["alerts_enabled"]
			if !ok {
				t.Fatalf("data missing alerts_enabled field: %q", dataRaw)
			}
			var got bool
			if err := json.Unmarshal(ae, &got); err != nil {
				t.Fatalf("unmarshal alerts_enabled: %v", err)
			}
			if got != tc.enabled {
				t.Fatalf("alerts_enabled = %v, want %v", got, tc.enabled)
			}
		})
	}
}

func TestHandleGetMetaAdvertisesDemoReadOnly(t *testing.T) {
	t.Parallel()

	s := &Server{
		version: "test",
		config: &config.Config{
			Server: config.ServerConfig{HTTPServerTimeout: 30 * time.Second},
			Demo:   config.DemoConfig{ReadOnly: true},
		},
	}
	app := fiber.New()
	app.Get("/api/v1/meta", s.handleGetMeta)

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/meta", http.NoBody))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer resp.Body.Close()

	var envelope struct {
		Data MetaResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !envelope.Data.DemoReadOnly {
		t.Fatal("demo_read_only = false, want true")
	}
}

func TestHandleGetMetaDemoLoginCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		demo        config.DemoConfig
		local       config.LocalAuthConfig
		wantExposed bool
	}{
		{
			name:        "explicit read-only local demo",
			demo:        config.DemoConfig{ReadOnly: true, ShowLoginCredentials: true},
			local:       config.LocalAuthConfig{Enabled: true, AdminEmail: "demo@example.com", AdminPassword: "demo-password"},
			wantExposed: true,
		},
		{
			name:  "opt-in disabled",
			demo:  config.DemoConfig{ReadOnly: true},
			local: config.LocalAuthConfig{Enabled: true, AdminEmail: "demo@example.com", AdminPassword: "demo-password"},
		},
		{
			name:  "read-only disabled",
			demo:  config.DemoConfig{ShowLoginCredentials: true},
			local: config.LocalAuthConfig{Enabled: true, AdminEmail: "demo@example.com", AdminPassword: "demo-password"},
		},
		{
			name:  "local auth disabled",
			demo:  config.DemoConfig{ReadOnly: true, ShowLoginCredentials: true},
			local: config.LocalAuthConfig{AdminEmail: "demo@example.com", AdminPassword: "demo-password"},
		},
		{
			name:  "credentials incomplete",
			demo:  config.DemoConfig{ReadOnly: true, ShowLoginCredentials: true},
			local: config.LocalAuthConfig{Enabled: true, AdminEmail: "demo@example.com"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := &Server{
				version: "test",
				config: &config.Config{
					Server: config.ServerConfig{HTTPServerTimeout: 30 * time.Second},
					Demo:   tt.demo,
					Auth:   config.AuthConfig{Local: tt.local},
				},
			}
			app := fiber.New()
			app.Get("/api/v1/meta", s.handleGetMeta)

			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/meta", http.NoBody))
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			if got := resp.Header.Get(fiber.HeaderCacheControl); got != "no-store, private" {
				t.Fatalf("Cache-Control = %q, want %q", got, "no-store, private")
			}

			var envelope struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			raw, exposed := envelope.Data["demo_login_credentials"]
			if exposed != tt.wantExposed {
				t.Fatalf("demo_login_credentials exposed = %v, want %v", exposed, tt.wantExposed)
			}
			if !tt.wantExposed {
				return
			}

			var got DemoLoginCredentials
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("decode demo_login_credentials: %v", err)
			}
			if got.Email != tt.local.AdminEmail || got.Password != tt.local.AdminPassword {
				t.Fatalf("demo_login_credentials = %#v, want configured local credentials", got)
			}
		})
	}
}
