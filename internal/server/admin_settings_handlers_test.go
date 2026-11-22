package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestSettingsBaseURLsRequireAbsoluteHTTPURL(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.store, "url-admin@example.com", models.UserRoleAdmin)
	auth := withSession(sessionCookie(t, s.store, admin))
	for _, key := range []string{"server.frontend_url", "alerts.frontend_url", "alerts.external_url", "ai.base_url"} {
		t.Run(key, func(t *testing.T) {
			category, _, _ := strings.Cut(key, ".")
			for _, tc := range []struct {
				name   string
				value  string
				status int
			}{
				{"relative path", "relative/path", http.StatusBadRequest},
				{"protocol relative", "//evil.example", http.StatusBadRequest},
				{"javascript scheme", "javascript:alert(1)", http.StatusBadRequest},
				{"missing host", "https://", http.StatusBadRequest},
				{"localhost", "http://localhost:5173", http.StatusOK},
				{"base path", "https://logs.example.com/base", http.StatusOK},
				{"empty", "", http.StatusOK},
			} {
				t.Run(tc.name, func(t *testing.T) {
					const original = "https://original.example.com"
					if err := s.store.UpsertSetting(t.Context(), key, original, "string", category, "", false); err != nil {
						t.Fatal(err)
					}
					body := fmt.Sprintf(`{"value":%q,"value_type":"string","category":%q}`, tc.value, category)
					resp := doAuthzRequest(t, s, http.MethodPut, "/api/v1/admin/settings/"+key, body, auth)
					if resp.status != tc.status {
						t.Errorf("PUT %q status = %d, want %d", tc.value, resp.status, tc.status)
					}
					want := original
					if tc.status == http.StatusOK {
						want = tc.value
					}
					saved, err := s.store.GetSetting(t.Context(), key)
					if err != nil {
						t.Fatal(err)
					}
					if saved != want {
						t.Errorf("stored value = %q, want %q", saved, want)
					}
				})
			}
		})
	}
}

// TestSettingsEmailAddressesMustParse: the From and Reply-To settings take a
// bare address or one with a display name, and refuse a value the mailer
// could not send with.
func TestSettingsEmailAddressesMustParse(t *testing.T) {
	s := newAuthzTestServer(t)
	admin := mkTestUser(t, s.store, "mail-admin@example.com", models.UserRoleAdmin)
	auth := withSession(sessionCookie(t, s.store, admin))
	for _, key := range []string{"alerts.smtp_from", "alerts.smtp_reply_to"} {
		for _, tc := range []struct {
			value  string
			status int
		}{
			{"logchef@shop.example", http.StatusOK},
			{"Logchef <logchef@shop.example>", http.StatusOK},
			{"", http.StatusOK},
			{"Logchef <logchef@shop.example", http.StatusBadRequest},
			{"logchef@", http.StatusBadRequest},
			{"not an address", http.StatusBadRequest},
		} {
			body := fmt.Sprintf(`{"value":%q,"value_type":"string","category":"alerts"}`, tc.value)
			resp := doAuthzRequest(t, s, http.MethodPut, "/api/v1/admin/settings/"+key, body, auth)
			if resp.status != tc.status {
				t.Errorf("PUT %s %q status = %d, want %d", key, tc.value, resp.status, tc.status)
			}
		}
	}
}
