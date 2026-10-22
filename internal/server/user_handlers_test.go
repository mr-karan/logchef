package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestAdminAccountHandlersRejectMissingUsers(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.sqlite, "admin@example.com", models.UserRoleAdmin)
	cookie := sessionCookie(t, s.sqlite, admin)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/users/999999"},
		{http.MethodPut, "/users/999999"},
		{http.MethodDelete, "/users/999999"},
		{http.MethodGet, "/service-accounts/999999/tokens"},
		{http.MethodPost, "/service-accounts/999999/tokens"},
		{http.MethodDelete, "/service-accounts/999999/tokens/1"},
		{http.MethodGet, "/service-accounts/999999/teams"},
		{http.MethodPost, "/service-accounts/999999/teams"},
		{http.MethodDelete, "/service-accounts/999999/teams/1"},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/api/v1/admin"+tc.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(cookie)
			resp, err := s.app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer resp.Body.Close()
			var body Response
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.StatusCode != http.StatusNotFound || body.Status != "error" || strings.Contains(body.Message, "panic") {
				t.Errorf("status = %d, body = %+v, want 404 error without panic", resp.StatusCode, body)
			}
		})
	}
}
