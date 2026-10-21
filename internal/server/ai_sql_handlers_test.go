package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	recovermw "github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestGenerateAISQLAuthorization(t *testing.T) {
	s := newAuthzTestServer(t)
	s.config.AI.Enabled = true
	s.config.AI.Provider = "bedrock"
	w := newAuthzWorld(t, s.sqlite)

	for _, tc := range []struct {
		name string
		user *models.User
		team models.TeamID
		want int
	}{
		{"member", w.member, w.team, http.StatusBadRequest},
		{"admin outside team", w.admin, w.team, http.StatusBadRequest},
		{"outsider", w.outsider, w.team, http.StatusForbidden},
		{"admin with unlinked source", w.admin, w.bareTeam, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/generate-sql", tc.team, w.source)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(sessionCookie(t, s.sqlite, tc.user))
			resp, err := s.app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d, body = %s", resp.StatusCode, tc.want, body)
			}
			if tc.want == http.StatusBadRequest && !strings.Contains(string(body), "Natural language query is required") {
				t.Errorf("handler did not reach request validation: %s", body)
			}
		})
	}
}

func TestGenerateAISQLRejectsInvalidSource(t *testing.T) {
	s := newAuthzTestServer(t)
	s.config.AI.Enabled = true
	s.config.AI.Provider = "bedrock"
	w := newAuthzWorld(t, s.sqlite)
	app := fiber.New()
	app.Use(recovermw.New())
	withUser(app, http.MethodPost, "/teams/:teamID/sources/:sourceID/generate-sql", w.member, s.handleGenerateAISQL)

	for _, tc := range []struct {
		name      string
		team      string
		src       string
		want      int
		errorType models.ErrorType
	}{
		{"invalid source ID", fmt.Sprint(w.team), "invalid", http.StatusBadRequest, models.ValidationErrorType},
		{"invalid team ID", "invalid", fmt.Sprint(w.source), http.StatusBadRequest, models.ValidationErrorType},
		{"missing source", fmt.Sprint(w.team), "999999", http.StatusNotFound, models.NotFoundErrorType},
		{"unavailable source provider", fmt.Sprint(w.team), fmt.Sprint(w.source), http.StatusInternalServerError, models.DatabaseErrorType},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := fmt.Sprintf("/teams/%s/sources/%s/generate-sql", tc.team, tc.src)
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"natural_language_query":"show logs"}`))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			var body Response
			raw, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode response: %v, body = %s", err, raw)
			}
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d, body = %+v", resp.StatusCode, tc.want, body)
			}
			if body.Status != "error" || body.ErrorType != string(tc.errorType) {
				t.Errorf("body = %+v, want error with type %s", body, tc.errorType)
			}
		})
	}
}
