package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestRequestLogsExcludeCredentials(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	db, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: logger,
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "logging.db")},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cfg := &config.Config{
		Auth:   config.AuthConfig{APITokenSecret: authzTestTokenSecret},
		Shares: config.SharesConfig{DefaultTTL: time.Hour, MaxQueryTextBytes: 4096},
	}
	s := New(ServerOptions{Config: cfg, SQLite: db, Logger: logger, FS: http.Dir(t.TempDir())})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	s.app.Post("/logging-errors/:token", func(fiber.Ctx) error {
		return fiber.NewError(fiber.StatusInternalServerError, "test handler failure")
	})
	e := &collectionRouteEnv{t: t, s: s, cfg: cfg}
	user := mkTestUser(t, db, "logging@example.com", models.UserRoleMember)
	team, source := mkTestTeam(t, db, "logging", user)
	apiCredential := e.token(user, models.TokenScopeQuerySharesRead, models.TokenScopeQuerySharesWrite)
	createPath := fmt.Sprintf("/api/v1/teams/%d/sources/%d/query-shares", team.ID, source.ID)
	data := e.expect(apiCredential, http.MethodPost, createPath, `{"payload":{"mode":"sql","query":"SELECT 1"}}`, http.StatusCreated)
	var share models.QueryShareResponse
	if err := json.Unmarshal(data, &share); err != nil {
		t.Fatal(err)
	}
	if share.Token == "" {
		t.Fatal("create response has no share token")
	}
	e.expect(apiCredential, http.MethodGet, "/api/v1/query-shares/"+share.Token, "", http.StatusOK)
	e.expect(apiCredential, http.MethodGet, "/api/v1/query-shares%2F"+share.Token, "", http.StatusNotFound)
	const code = "secret-oauth-code"
	const state = "secret-oauth-state"
	e.expect("", http.MethodGet, "/api/v1/auth/callback?code="+code+"&state="+state, "", http.StatusInternalServerError)
	const errorToken = "secret-error-path-token"
	e.expect("", http.MethodPost, "/logging-errors/"+errorToken+"?code="+code+"&state="+state, "", http.StatusInternalServerError)

	for _, credential := range []string{share.Token, apiCredential, code, state, errorToken} {
		if strings.Contains(logs.String(), credential) {
			t.Error("log output contains a request credential")
		}
	}
	wantRoutes := map[string]int{
		"<unmatched>": http.StatusNotFound,
		"/api/v1/teams/:teamID/sources/:sourceID/query-shares": http.StatusCreated,
		"/api/v1/query-shares/:token":                          http.StatusOK,
		"/api/v1/auth/callback":                                http.StatusInternalServerError,
		"/logging-errors/:token":                               http.StatusInternalServerError,
	}
	foundError := false
	decoder := json.NewDecoder(strings.NewReader(logs.String()))
	for decoder.More() {
		var record struct {
			Message    string  `json:"msg"`
			Method     string  `json:"method"`
			Path       *string `json:"path"`
			Route      string  `json:"route"`
			Status     int     `json:"status"`
			DurationMS *int64  `json:"duration_ms"`
			TeamID     string  `json:"team_id"`
			SourceID   string  `json:"source_id"`
			Token      string  `json:"token"`
		}
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record.Route != "" && record.Path != nil {
			t.Error("log contains duplicate path attribute")
		}
		if record.Message == "request error" {
			foundError = true
			if record.Route != "/logging-errors/:token" {
				t.Error("error log does not use the route template")
			}
		}
		if record.Message != "http" {
			continue
		}
		wantStatus, ok := wantRoutes[record.Route]
		if !ok || record.Status != wantStatus || record.Method == "" || record.DurationMS == nil {
			t.Errorf("unexpected request log metadata for route %q", record.Route)
		}
		if record.Route == "/api/v1/teams/:teamID/sources/:sourceID/query-shares" {
			if record.TeamID != fmt.Sprint(team.ID) || record.SourceID != fmt.Sprint(source.ID) {
				t.Error("request log lacks team and source IDs")
			}
		}
		if record.Route == "/api/v1/query-shares/:token" && record.Token != share.Token[:8] {
			t.Error("share log lacks redacted token prefix")
		}
		delete(wantRoutes, record.Route)
	}
	if len(wantRoutes) != 0 || !foundError {
		t.Error("expected request or error logs are missing")
	}
}

func TestFailureLogsExcludeCredentials(t *testing.T) {
	tests := []struct {
		name    string
		message string
		handler func(*Server, fiber.Ctx) error
	}{
		{
			name:    "panic",
			message: "panic recovered",
			handler: func(_ *Server, _ fiber.Ctx) error {
				panic("test handler panic")
			},
		},
		{
			name:    "missing source authorization",
			message: "route reached handler without source authorization",
			handler: func(s *Server, c fiber.Ctx) error {
				s.authorizedSource(c)
				return nil
			},
		},
		{
			name:    "response write",
			message: "failed to write error response",
			handler: func(s *Server, c fiber.Ctx) error {
				s.sendLoadFailure(c, fiber.StatusInternalServerError, "test load failure", models.GeneralErrorType)
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, nil))
			app := fiber.New(fiber.Config{
				JSONEncoder: func(any) ([]byte, error) {
					return nil, fmt.Errorf("test response encoding failure")
				},
			})
			app.Use(recoverMiddleware(logger))
			app.Use(requestLogger(logger))
			s := &Server{log: logger}
			const route = "/logging-failures/:token"
			app.Get(route, func(c fiber.Ctx) error { return tt.handler(s, c) })

			const token = "secret-failure-path-token"
			const code = "secret-failure-query-code"
			path := "/logging-failures/" + token + "?code=" + code
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, path, http.NoBody))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
			}
			for _, credential := range []string{token, code} {
				if strings.Contains(logs.String(), credential) {
					t.Error("log output contains a request credential")
				}
			}
			found := false
			decoder := json.NewDecoder(strings.NewReader(logs.String()))
			for decoder.More() {
				var record struct {
					Message string  `json:"msg"`
					Path    *string `json:"path"`
					Route   string  `json:"route"`
				}
				if err := decoder.Decode(&record); err != nil {
					t.Fatal(err)
				}
				if record.Route != "" && record.Path != nil {
					t.Error("log contains duplicate path attribute")
				}
				if record.Message == tt.message {
					found = true
					if record.Route != route {
						t.Error("failure log does not use the route template")
					}
				}
			}
			if !found {
				t.Errorf("missing %q log", tt.message)
			}
		})
	}
}

func TestRequestLogsRetainRedactedRoutes(t *testing.T) {
	for i, tt := range []struct {
		path, wantRoute string
		status          int
	}{
		{"/api/v1/query-shares%2F12345678-secret-token", "<unmatched>", http.StatusNotFound},
		{"/api/v1/unknown/12345678-secret-token", "<unmatched>", http.StatusNotFound},
		{"/api/v1/unknown-path?code=secret-query", "<unmatched>", http.StatusNotFound},
		{"/api/v1/admin/settings?code=secret-query", "<unmatched>", http.StatusUnauthorized},
		{"/api/v1/query-shares/12345678-secret-token", "<unmatched>", http.StatusUnauthorized},
		{"/api/v1/query-shares/12345678-secret-token/typo?code=secret-query", "<unmatched>", http.StatusNotFound},
		{"/api/v1/QUERY-SHARES/12345678-secret-token/typo", "<unmatched>", http.StatusNotFound},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			s := New(ServerOptions{Config: &config.Config{}, SQLite: newServerTestDB(t), Logger: logger, FS: http.Dir(t.TempDir())})
			t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
			resp, err := s.app.Test(httptest.NewRequest(http.MethodGet, tt.path, http.NoBody))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
			}
			if strings.Contains(logs.String(), "12345678-secret-token") || strings.Contains(logs.String(), "secret-query") {
				t.Error("logs contain credentials")
			}
			found := false
			decoder := json.NewDecoder(strings.NewReader(logs.String()))
			for decoder.More() {
				var record struct {
					Message string  `json:"msg"`
					Path    *string `json:"path"`
					Route   string  `json:"route"`
				}
				if err := decoder.Decode(&record); err != nil {
					t.Fatal(err)
				}
				if record.Route != "" && record.Path != nil {
					t.Error("log contains duplicate path attribute")
				}
				if record.Route != "" && record.Route != tt.wantRoute {
					t.Errorf("%s log route = %q, want %q", record.Message, record.Route, tt.wantRoute)
				}
				if record.Message == "http" {
					found = true
					if record.Route != tt.wantRoute {
						t.Errorf("route = %q, want %q", record.Route, tt.wantRoute)
					}
				}
			}
			if !found {
				t.Fatal("no HTTP log")
			}
		})
	}
}

func TestAuthMiddlewareRedactsSessionCookies(t *testing.T) {
	for _, state := range []string{"missing", "expired", "database_error"} {
		t.Run(state, func(t *testing.T) {
			var logs bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			db, err := sqlite.New(context.Background(), sqlite.Options{Logger: logger, Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "sessions.db")}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			const cookie = "12345678-secret-session-cookie"
			if state == "expired" {
				user := mkTestUser(t, db, "session-logs@example.com", models.UserRoleMember)
				if err := db.CreateSession(context.Background(), &models.Session{ID: models.SessionID(cookie), UserID: user.ID, ExpiresAt: time.Now().Add(-time.Hour)}); err != nil {
					t.Fatal(err)
				}
			}
			s := New(ServerOptions{Config: &config.Config{}, SQLite: db, Logger: logger, FS: http.Dir(t.TempDir())})
			t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
			if state == "database_error" {
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			logs.Reset()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", http.NoBody)
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: cookie})
			resp, err := s.app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			want := http.StatusUnauthorized
			if state == "database_error" {
				want = http.StatusInternalServerError
			}
			if resp.StatusCode != want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, want)
			}
			if strings.Contains(logs.String(), cookie) {
				t.Error("auth logs contain session cookie")
			}
			if !strings.Contains(logs.String(), `"session_id":"12345678..."`) {
				t.Error("auth logs lack redacted session ID")
			}
		})
	}
}

func TestUnmatchedErrorHandlerLogsRetainRedactedRoute(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := New(ServerOptions{Config: &config.Config{}, SQLite: newServerTestDB(t), Logger: logger, FS: http.Dir(t.TempDir())})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	const secret = "unmatched-secret-path-segment"
	resp, err := s.app.Test(httptest.NewRequest(http.MethodPost, "/unknown/"+secret, http.NoBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
	if strings.Contains(logs.String(), secret) {
		t.Error("unmatched request secret appears in logs")
	}
	found := false
	decoder := json.NewDecoder(strings.NewReader(logs.String()))
	for decoder.More() {
		var record struct {
			Message string  `json:"msg"`
			Path    *string `json:"path"`
			Error   string  `json:"error"`
			Route   string  `json:"route"`
		}
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record.Route != "" && record.Path != nil {
			t.Error("log contains duplicate path attribute")
		}
		if record.Message == "request error" {
			found = true
			if record.Route != "<unmatched>" {
				t.Errorf("error log route = %q, want unmatched placeholder", record.Route)
			}
			if record.Error != "Method Not Allowed" {
				t.Errorf("error log message = %q, want Method Not Allowed", record.Error)
			}
		}
	}
	if !found {
		t.Fatal("missing error-handler log")
	}
}
