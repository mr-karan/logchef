package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestExportJobHandlersRejectMissingJobs(t *testing.T) {
	s := newAuthzTestServer(t)
	w := newAuthzWorld(t, s.store)
	cookie := sessionCookie(t, s.store, w.member)

	for _, suffix := range []string{"", "/download"} {
		t.Run("missing export"+suffix, func(t *testing.T) {
			path := fmt.Sprintf("/api/v1/teams/%d/sources/%d/exports/missing%s", w.team, w.source, suffix)
			req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
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
			if resp.StatusCode != http.StatusNotFound || body.Status != "error" || body.ErrorType != string(models.NotFoundErrorType) {
				t.Errorf("status = %d, body = %+v, want 404 NotFoundError", resp.StatusCode, body)
			}
		})
	}
}
