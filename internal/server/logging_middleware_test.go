package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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
		"/api/v1/teams/:teamID/sources/:sourceID/query-shares": http.StatusCreated,
		"/api/v1/query-shares/:token":                          http.StatusOK,
		"/api/v1/auth/callback":                                http.StatusInternalServerError,
		"/logging-errors/:token":                               http.StatusInternalServerError,
	}
	foundError := false
	decoder := json.NewDecoder(strings.NewReader(logs.String()))
	for decoder.More() {
		var record struct {
			Message    string `json:"msg"`
			Method     string `json:"method"`
			Path       string `json:"path"`
			Status     int    `json:"status"`
			DurationMS *int64 `json:"duration_ms"`
			TeamID     string `json:"team_id"`
			SourceID   string `json:"source_id"`
		}
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		if record.Message == "request error" {
			foundError = true
			if record.Path != "/logging-errors/:token" {
				t.Error("error log does not use the route template")
			}
		}
		if record.Message != "http" {
			continue
		}
		wantStatus, ok := wantRoutes[record.Path]
		if !ok || record.Status != wantStatus || record.Method == "" || record.DurationMS == nil {
			t.Errorf("unexpected request log metadata for route %q", record.Path)
		}
		if record.Path == "/api/v1/teams/:teamID/sources/:sourceID/query-shares" {
			if record.TeamID != fmt.Sprint(team.ID) || record.SourceID != fmt.Sprint(source.ID) {
				t.Error("request log lacks team and source IDs")
			}
		}
		delete(wantRoutes, record.Path)
	}
	if len(wantRoutes) != 0 || !foundError {
		t.Error("expected request or error logs are missing")
	}
}
