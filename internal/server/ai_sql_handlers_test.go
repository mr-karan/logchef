package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestGenerateAISQLRejectsUnavailableSourceProvider(t *testing.T) {
	s := newAuthzTestServer(t)
	s.config.AI.Enabled = true
	s.config.AI.Provider = "bedrock"
	w := newAuthzWorld(t, s.sqlite)

	path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/generate-sql", w.team, w.source)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"natural_language_query":"show logs"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie(t, s.sqlite, w.member))
	resp, err := s.app.Test(req)
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
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d, body = %+v", resp.StatusCode, http.StatusInternalServerError, body)
	}
	if body.Status != "error" || body.ErrorType != string(models.DatabaseErrorType) {
		t.Errorf("body = %+v, want error with type %s", body, models.DatabaseErrorType)
	}
}
