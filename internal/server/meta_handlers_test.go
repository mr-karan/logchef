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

			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil))
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
