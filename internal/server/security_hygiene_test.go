package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestSettingsSensitiveValuesByPrincipal(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.sqlite, "settings-admin@example.com", models.UserRoleAdmin)
	token := apiToken(t, s, admin, core.ReadOnlyTokenScopes()...)
	cookie := sessionCookie(t, s.sqlite, admin)
	ctx := context.Background()
	const secret = "test-smtp-secret-value"
	for _, setting := range []struct {
		key, value string
		sensitive  bool
	}{
		{"alerts.smtp_password", secret, true},
		{"alerts.smtp_host", "smtp.example.com", false},
		{"custom.secret", secret, true},
		{"custom.empty_secret", "", true},
	} {
		if err := s.sqlite.UpsertSetting(ctx, setting.key, setting.value, "string", "alerts", "", setting.sensitive); err != nil {
			t.Fatal(err)
		}
	}
	for _, method := range []string{"token", "session"} {
		for _, path := range []string{"/api/v1/admin/settings", "/api/v1/admin/settings/category/alerts", "/api/v1/admin/settings/alerts.smtp_password", "/api/v1/admin/settings/custom.secret", "/api/v1/admin/settings/custom.empty_secret", "/api/v1/admin/settings/alerts.smtp_host"} {
			t.Run(method+path, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
				if method == "token" {
					req.Header.Set("Authorization", "Bearer "+token)
				} else {
					req.AddCookie(cookie)
				}
				resp, err := s.app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want 200", resp.StatusCode)
				}
				var envelope struct {
					Data json.RawMessage `json:"data"`
				}
				if err := json.Unmarshal(body, &envelope); err != nil {
					t.Fatal(err)
				}
				var settings []SystemSettingResponse
				switch path {
				case "/api/v1/admin/settings":
					var categories []SettingsByCategoryResponse
					if err := json.Unmarshal(envelope.Data, &categories); err != nil {
						t.Fatal(err)
					}
					for _, category := range categories {
						settings = append(settings, category.Settings...)
					}
				case "/api/v1/admin/settings/category/alerts":
					if err := json.Unmarshal(envelope.Data, &settings); err != nil {
						t.Fatal(err)
					}
				default:
					var setting SystemSettingResponse
					if err := json.Unmarshal(envelope.Data, &setting); err != nil {
						t.Fatal(err)
					}
					settings = append(settings, setting)
				}
				if len(settings) == 0 {
					t.Fatal("no settings returned")
				}
				for _, setting := range settings {
					switch setting.Key {
					case "alerts.smtp_password", "custom.secret":
						want := secret
						if method == "token" {
							want = "********"
						}
						if setting.Value != want {
							t.Error("sensitive value did not match principal's reveal permission")
						}
					case "alerts.smtp_host":
						if setting.Value != "smtp.example.com" {
							t.Error("non-sensitive value was masked")
						}
					case "custom.empty_secret":
						if setting.Value != "" {
							t.Error("empty secret did not remain empty")
						}
					}
				}
				if method == "token" && bytes.Contains(body, []byte(secret)) {
					t.Error("API token response contains plaintext secret")
				}
			})
		}
	}
}

func TestSettingsRejectNonPositiveDurations(t *testing.T) {
	e := newCollectionRouteEnv(t)
	admin := mkTestUser(t, e.s.sqlite, "duration-admin@example.com", models.UserRoleAdmin)
	token := e.token(admin, models.TokenScopeSettingsWrite)
	keys := []string{"auth.session_duration", "auth.default_token_expiry", "alerts.evaluation_interval", "alerts.default_lookback", "alerts.request_timeout"}
	for _, key := range keys {
		for _, valueType := range []string{"duration", "string"} {
			for _, value := range []string{"-1h", "0s", "1h"} {
				t.Run(key+"/"+valueType+"/"+value, func(t *testing.T) {
					if err := e.s.sqlite.UpsertSetting(context.Background(), key, "2h", "duration", strings.Split(key, ".")[0], "", false); err != nil {
						t.Fatal(err)
					}
					body := fmt.Sprintf(`{"value":%q,"value_type":%q,"category":%q}`, value, valueType, strings.Split(key, ".")[0])
					want := http.StatusBadRequest
					if value == "1h" {
						want = http.StatusOK
					}
					e.expect(token, http.MethodPut, "/api/v1/admin/settings/"+key, body, want)
					saved, err := e.s.sqlite.GetSetting(context.Background(), key)
					if err != nil {
						t.Fatal(err)
					}
					expected := "2h"
					if want == http.StatusOK {
						expected = "1h"
					}
					if saved != expected {
						t.Errorf("stored value = %q, want %q", saved, expected)
					}
				})
			}
		}
	}
}

func TestManagedGuardsFailClosed(t *testing.T) {
	for _, kind := range []string{"source", "team"} {
		for _, state := range []string{"unmanaged", "managed", "database_error"} {
			t.Run(kind+"/"+state, func(t *testing.T) {
				db := newServerTestDB(t)
				owner := mkTestUser(t, db, "guard-owner@example.com", models.UserRoleMember)
				team, source := mkTestTeam(t, db, "guard-team", owner)
				s := &Server{sqlite: db, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
				id := int(team.ID)
				guard := s.requireTeamNotManaged
				path := "/:teamID"
				if kind == "source" {
					id = int(source.ID)
					guard = s.requireSourceNotManaged
					path = "/:sourceID"
				}
				if state == "managed" {
					var err error
					if kind == "source" {
						err = db.SetSourceManaged(context.Background(), source.ID, true, "")
					} else {
						err = db.SetTeamManaged(context.Background(), team.ID, true)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if state == "database_error" {
					if err := db.Close(); err != nil {
						t.Fatal(err)
					}
				}
				mutated := false
				app := fiber.New()
				app.Put(path, guard, func(c fiber.Ctx) error { mutated = true; return c.SendStatus(http.StatusNoContent) })
				resp, err := app.Test(httptest.NewRequest(http.MethodPut, fmt.Sprintf("/%d", id), http.NoBody))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				want := http.StatusNoContent
				if state == "managed" {
					want = http.StatusForbidden
				}
				if state == "database_error" {
					want = http.StatusInternalServerError
				}
				if resp.StatusCode != want {
					t.Errorf("status = %d, want %d", resp.StatusCode, want)
				}
				if mutated != (state == "unmanaged") {
					t.Errorf("mutation reached = %v in state %s", mutated, state)
				}
			})
		}
	}
}

func TestCredentialErrorsDoNotLeakInHandlerLogs(t *testing.T) {
	for _, kind := range []string{"session", "share"} {
		t.Run(kind, func(t *testing.T) {
			db := newServerTestDB(t)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			var logs bytes.Buffer
			s := &Server{sqlite: db, log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}
			app := fiber.New()
			const credential = "12345678-secret-credential-suffix"
			path := "/protected"
			if kind == "session" {
				app.Get(path, s.authenticateWithSession)
			} else {
				app.Get("/:token", func(c fiber.Ctx) error { c.Locals("user", &models.User{ID: 1}); return c.Next() }, s.handleGetQueryShare)
				path = "/" + credential
			}
			req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: credential})
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("status = %d, want 500", resp.StatusCode)
			}
			if logs.Len() == 0 {
				t.Fatal("error path did not log")
			}
			if strings.Contains(logs.String(), credential) {
				t.Error("handler log contains full credential")
			}
			if !strings.Contains(logs.String(), "12345678") {
				t.Error("handler log lacks credential prefix")
			}
		})
	}
}
