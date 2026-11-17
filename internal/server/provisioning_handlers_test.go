package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

// Two sources whose names sanitize alike would export one variable for two
// header secrets. The export answers 409 and names both, not a bare 500.
func TestExportProvisioningSecretRefConflict(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.store, "export-admin@example.com", models.UserRoleAdmin)
	for _, src := range []struct{ name, account string }{{"prod-eu", "1"}, {"prod_eu", "2"}} {
		conn, err := json.Marshal(map[string]any{
			"base_url": "http://127.0.0.1:1",
			"tenant":   map[string]string{"account_id": src.account},
			"headers":  map[string]string{"X-Api-Key": "key-" + src.account},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.store.CreateSource(t.Context(), &models.Source{
			Name: src.name, SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: conn,
		}); err != nil {
			t.Fatal(err)
		}
	}

	resp := doAuthzRequest(t, s, http.MethodGet, "/api/v1/admin/provisioning/export", "", withSession(sessionCookie(t, s.store, admin)))
	if resp.status != http.StatusConflict {
		t.Fatalf("status = %d (%s), want 409", resp.status, resp.message)
	}
	for _, want := range []string{`"prod-eu"`, `"prod_eu"`, "LOGCHEF_SOURCE_PROD_EU_HEADER_X_API_KEY"} {
		if !strings.Contains(resp.message, want) {
			t.Errorf("message %q does not name %s", resp.message, want)
		}
	}
}
