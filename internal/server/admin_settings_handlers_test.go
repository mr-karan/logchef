package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestHandleTestWebhookHonorsAllowedHosts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name         string
		allowedHosts []string
		wantStatus   int
		wantRequests int32
	}{
		{name: "loopback denied by default", wantStatus: http.StatusBadGateway},
		{name: "unrelated allowlist does not permit loopback", allowedHosts: []string{"example.com"}, wantStatus: http.StatusBadGateway},
		{name: "allowlisted loopback permitted", allowedHosts: []string{"127.0.0.1"}, wantStatus: http.StatusOK, wantRequests: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var requests atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Method != http.MethodPost {
					t.Errorf("webhook method = %s, want POST", r.Method)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(receiver.Close)

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			db, err := sqlite.New(t.Context(), sqlite.Options{
				Logger: logger,
				Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "settings.db")},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })

			s := &Server{
				config: &config.Config{Alerts: config.AlertsConfig{WebhookAllowedHosts: tc.allowedHosts}},
				sqlite: db,
				log:    logger,
			}
			app := fiber.New()
			app.Post("/api/v1/admin/settings/test-webhook", func(c fiber.Ctx) error {
				c.Locals("user", &models.User{Role: models.UserRoleAdmin, Email: "admin@example.com"})
				return s.handleTestWebhook(c)
			})

			body, err := json.Marshal(TestWebhookRequest{WebhookURL: receiver.URL})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/settings/test-webhook", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			responseBody, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tc.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", resp.StatusCode, tc.wantStatus, responseBody)
			}
			if tc.wantRequests == 0 && !strings.Contains(string(responseBody), "not allowed") {
				t.Errorf("response does not report a blocked destination: %s", responseBody)
			}
			if got := requests.Load(); got != tc.wantRequests {
				t.Errorf("webhook requests = %d, want %d", got, tc.wantRequests)
			}
		})
	}
}
