package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store/sqlite"
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
		for _, state := range []string{"unmanaged", "managed", "database_error", "missing"} {
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
				if state == "missing" {
					id = 999999
				}
				mutated := false
				app := fiber.New()
				app.Put(path, guard, func(c fiber.Ctx) error {
					if state == "missing" {
						return c.SendStatus(http.StatusNotFound)
					}
					mutated = true
					return c.SendStatus(http.StatusNoContent)
				})
				resp, err := app.Test(httptest.NewRequest(http.MethodPut, fmt.Sprintf("/%d", id), http.NoBody))
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				want := http.StatusNoContent
				if state == "missing" {
					want = http.StatusNotFound
				}
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

func TestSettingsPreserveSensitivityOnUpdate(t *testing.T) {
	e := newCollectionRouteEnv(t)
	admin := mkTestUser(t, e.s.sqlite, "sensitive-admin@example.com", models.UserRoleAdmin)
	token := e.token(admin, models.TokenScopeSettingsRead)
	cookie := sessionCookie(t, e.s.sqlite, admin)
	for _, key := range []string{"ai.api_key", "alerts.smtp_password", "custom.secret"} {
		t.Run(key, func(t *testing.T) {
			if key == "custom.secret" {
				if err := e.s.sqlite.UpsertSetting(context.Background(), key, "old-secret", "string", "ai", "", true); err != nil {
					t.Fatal(err)
				}
			}
			for _, sensitivity := range []string{"", `,"is_sensitive":false`} {
				req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/settings/"+key, strings.NewReader(`{"value":"new-secret-value","value_type":"string","category":"ai"`+sensitivity+`}`))
				req.Header.Set("Content-Type", "application/json")
				req.AddCookie(cookie)
				resp, err := e.s.app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("session update status = %d, want 200", resp.StatusCode)
				}
				body := e.expect(token, http.MethodGet, "/api/v1/admin/settings/"+key, "", http.StatusOK)
				if strings.Contains(string(body), "new-secret-value") || !strings.Contains(string(body), "********") {
					t.Error("token can read updated secret")
				}
				settings, err := e.s.sqlite.ListSettings(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				for _, setting := range settings {
					if setting.Key == key && !setting.IsSensitive {
						t.Error("updated setting lost sensitivity")
					}
				}
			}
		})
	}
}

func TestTestEmailRequiresBrowserSession(t *testing.T) {
	e := newCollectionRouteEnv(t)
	e.cfg.Alerts.Enabled = true
	admin := mkTestUser(t, e.s.sqlite, "email-admin@example.com", models.UserRoleAdmin)
	token := e.token(admin, models.TokenScopeSettingsWrite)
	e.expect(token, http.MethodPost, "/api/v1/admin/settings/test-email", `{}`, http.StatusUnauthorized)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/settings/test-email", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie(t, e.s.sqlite, admin))
	resp, err := e.s.app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("session status = %d, want unconfigured SMTP 400", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("SMTP is not configured")) {
		t.Fatal("browser session did not reach SMTP configuration check")
	}
}

func TestMissingManagedResourcesReturnNotFound(t *testing.T) {
	s := newAuthzTestServer(t)
	e := &collectionRouteEnv{t: t, s: s, cfg: s.config}
	admin := mkTestUser(t, e.s.sqlite, "missing-admin@example.com", models.UserRoleAdmin)
	token := e.token(admin, models.TokenScopeTeamsWrite, models.TokenScopeSourcesWrite)
	for _, path := range []string{"/api/v1/admin/teams/999999", "/api/v1/admin/sources/999999"} {
		e.expect(token, http.MethodDelete, path, "", http.StatusNotFound)
		if strings.Contains(path, "/sources/") {
			e.expect(token, http.MethodPut, path, `{}`, http.StatusNotFound)
		}
	}
}

func TestLegacySettingsRemainSensitive(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := sqlite.New(ctx, sqlite.Options{Logger: logger, Config: config.SQLiteConfig{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	for _, key := range []string{"ai.api_key", "alerts.smtp_password"} {
		if _, err := raw.ExecContext(ctx, `INSERT INTO system_settings (key, value, value_type, category, is_sensitive) VALUES (?, 'legacy-secret-value', 'string', ?, 0) ON CONFLICT(key) DO UPDATE SET value = excluded.value, is_sensitive = 0`, key, strings.Split(key, ".")[0]); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Auth: config.AuthConfig{APITokenSecret: authzTestTokenSecret}}
	s := New(ServerOptions{Config: cfg, SQLite: db, Logger: logger, FS: http.Dir(t.TempDir())})
	t.Cleanup(func() { _ = s.Shutdown(ctx) })
	e := &collectionRouteEnv{t: t, s: s, cfg: cfg}
	admin := mkTestUser(t, db, "legacy-admin@example.com", models.UserRoleAdmin)
	token := e.token(admin, models.TokenScopeSettingsRead)
	for _, path := range []string{"/api/v1/admin/settings", "/api/v1/admin/settings/category/ai", "/api/v1/admin/settings/category/alerts", "/api/v1/admin/settings/ai.api_key", "/api/v1/admin/settings/alerts.smtp_password"} {
		body := e.expect(token, http.MethodGet, path, "", http.StatusOK)
		if strings.Contains(string(body), "legacy-secret-value") || !strings.Contains(string(body), "********") {
			t.Errorf("legacy secret exposed at %s", path)
		}
	}
}

func TestSettingsSecretDestinationsRequireSession(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.sqlite, "destination-admin@example.com", models.UserRoleAdmin)
	token := apiToken(t, s, admin, models.TokenScopeSettingsWrite)
	cookie := sessionCookie(t, s.sqlite, admin)
	for _, setting := range []struct{ key, value, valueType string }{
		{"alerts.smtp_host", "smtp.example.com", "string"},
		{"alerts.smtp_port", "465", "number"},
		{"alerts.smtp_security", "tls", "string"},
		{"alerts.tls_insecure_skip_verify", "true", "boolean"},
		{"ai.base_url", "https://ai.example.com", "string"},
		{"server.frontend_url", "https://frontend.example.com", "string"},
		{"ai.api_key", "fixture-key", "string"},
		{"custom.secret", "fixture-key", "string"},
	} {
		t.Run(setting.key, func(t *testing.T) {
			ctx := context.Background()
			category, _, _ := strings.Cut(setting.key, ".")
			if category == "custom" {
				category = "ai"
			}
			if err := s.sqlite.UpsertSetting(ctx, setting.key, "original", setting.valueType, category, "", setting.key == "custom.secret"); err != nil {
				t.Fatal(err)
			}
			body := fmt.Sprintf(`{"value":%q,"value_type":%q,"category":%q}`, setting.value, setting.valueType, category)
			for _, principal := range []string{"token", "session"} {
				for _, method := range []string{http.MethodPut, http.MethodDelete} {
					req := httptest.NewRequest(method, "/api/v1/admin/settings/"+setting.key, strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					if principal == "token" {
						req.Header.Set("Authorization", "Bearer "+token)
					} else {
						req.AddCookie(cookie)
					}
					resp, err := s.app.Test(req)
					if err != nil {
						t.Fatal(err)
					}
					resp.Body.Close()
					want := http.StatusForbidden
					if principal == "session" {
						want = http.StatusOK
					}
					if resp.StatusCode != want {
						t.Errorf("%s %s status = %d, want %d", principal, method, resp.StatusCode, want)
					}
					saved, err := s.sqlite.GetSetting(ctx, setting.key)
					if principal == "token" && (err != nil || saved != "original") {
						t.Error("token modified session-only setting")
					}
					if principal == "session" && method == http.MethodDelete && err == nil {
						t.Error("session delete was not persisted")
					}
					if principal == "session" && method == http.MethodPut && (err != nil || saved != setting.value) {
						t.Error("session update was not persisted")
					}
				}
			}
		})
	}
}
