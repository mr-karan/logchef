package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/alerts"
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
		malformed    bool
	}{
		{name: "malformed status error bounded", allowedHosts: []string{"127.0.0.1"}, wantStatus: http.StatusBadGateway, wantRequests: 1, malformed: true},
		{name: "loopback denied by default", wantStatus: http.StatusBadGateway},
		{name: "unrelated allowlist does not permit loopback", allowedHosts: []string{"example.com"}, wantStatus: http.StatusBadGateway},
		{name: "allowlisted loopback permitted", allowedHosts: []string{"127.0.0.1"}, wantStatus: http.StatusOK, wantRequests: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var requests atomic.Int32
			var connections atomic.Int32
			receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if tc.malformed {
					connection, buffer, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer connection.Close()
					_, _ = buffer.WriteString("HTTP/1.1 " + strings.Repeat("x", 10000) + "\r\n\r\n")
					_ = buffer.Flush()
					return
				}
				if r.Method != http.MethodPost {
					t.Errorf("webhook method = %s, want POST", r.Method)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			receiver.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateNew {
					connections.Add(1)
				}
			}
			receiver.Start()
			t.Cleanup(receiver.Close)

			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, nil))
			db, err := sqlite.New(t.Context(), sqlite.Options{
				Logger: logger,
				Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "settings.db")},
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })

			allowlist, err := config.ParseWebhookAllowlist(tc.allowedHosts)
			if err != nil {
				t.Fatal(err)
			}
			s := &Server{
				webhookSender: alerts.NewDynamicWebhookSender(db, alerts.WebhookSenderOptions{Logger: logger, AllowedHosts: allowlist}),
				sqlite:        db,
				log:           logger,
			}
			app := fiber.New()
			app.Post("/api/v1/admin/settings/test-webhook", func(c fiber.Ctx) error {
				c.Locals("user", &models.User{Role: models.UserRoleAdmin, Email: "admin@example.com"})
				return s.handleTestWebhook(c)
			})

			for range 2 {
				body, err := json.Marshal(TestWebhookRequest{WebhookURL: strings.Replace(receiver.URL, "://", "://user:secret-userinfo@", 1) + "/receiver?token=secret-query"})
				if err != nil {
					t.Fatal(err)
				}
				req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/settings/test-webhook", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				responseBody, err := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if tc.malformed && (len(responseBody) > 1024 || strings.Contains(string(responseBody), strings.Repeat("x", 100))) {
					t.Errorf("API response retained malformed-status parser text, length %d", len(responseBody))
				}
				if resp.StatusCode != tc.wantStatus {
					t.Errorf("status = %d, want %d; body = %s", resp.StatusCode, tc.wantStatus, responseBody)
				}
				if tc.wantRequests == 0 && strings.Contains(string(responseBody), "127.0.0.1") {
					t.Errorf("API response leaked resolved IP: %s", responseBody)
				}
				if tc.wantRequests == 0 && !strings.Contains(string(responseBody), "not allowed") {
					t.Errorf("response does not report a blocked destination: %s", responseBody)
				}
			}
			if tc.wantRequests == 0 {
				if got := strings.Count(logs.String(), `msg="webhook destination blocked by policy"`); got != 2 {
					t.Errorf("denial logs = %d, want one per request", got)
				}
				if !strings.Contains(logs.String(), `level=WARN`) || !strings.Contains(logs.String(), `rule="Test Alert"`) || !strings.Contains(logs.String(), `resolved_ip=127.0.0.1`) {
					t.Errorf("missing policy denial details in server logs: %s", logs.String())
				}
			}
			if strings.Contains(logs.String(), "secret-userinfo") || strings.Contains(logs.String(), "secret-query") || strings.Contains(logs.String(), "/receiver") {
				t.Errorf("webhook URL credentials leaked in logs: %s", logs.String())
			}
			if tc.wantStatus == http.StatusOK && connections.Load() != 1 {
				t.Errorf("admin notifications used %d connections, want one shared connection", connections.Load())
			}
			if got := requests.Load(); got != 2*tc.wantRequests {
				t.Errorf("webhook requests = %d, want %d", got, 2*tc.wantRequests)
			}
		})
	}
}

func TestHandleTestWebhookTransportErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"TLS", "TLS verification failed"},
		{"refused", "connection refused"},
		{"timeout", "request timed out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.name == "timeout" {
					<-release
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			if tc.name == "TLS" {
				receiver.StartTLS()
			} else {
				receiver.Start()
			}
			defer receiver.Close()
			defer close(release)
			if tc.name == "refused" {
				receiver.Close()
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			db, err := sqlite.New(t.Context(), sqlite.Options{Logger: logger, Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "settings.db")}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			allowlist, err := config.ParseWebhookAllowlist([]string{"127.0.0.1"})
			if err != nil {
				t.Fatal(err)
			}
			timeout := "1s"
			if tc.name == "timeout" {
				timeout = "30ms"
			}
			if err := db.UpsertSetting(t.Context(), "alerts.request_timeout", timeout, "string", "alerts", "", false); err != nil {
				t.Fatal(err)
			}
			s := &Server{webhookSender: alerts.NewDynamicWebhookSender(db, alerts.WebhookSenderOptions{Logger: logger, AllowedHosts: allowlist}), sqlite: db, log: logger}
			app := fiber.New()
			app.Post("/test-webhook", func(c fiber.Ctx) error {
				c.Locals("user", &models.User{Role: models.UserRoleAdmin, Email: "admin@example.com"})
				return s.handleTestWebhook(c)
			})
			body, err := json.Marshal(TestWebhookRequest{WebhookURL: receiver.URL})
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/test-webhook", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			response, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			responseBody, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != http.StatusBadGateway || !strings.Contains(string(responseBody), tc.want) || strings.Contains(string(responseBody), "127.0.0.1") {
				t.Fatalf("API error = %s, want fixed %q without destination", responseBody, tc.want)
			}
		})
	}
}
