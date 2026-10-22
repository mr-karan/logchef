package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestHandleCallbackEmailNormalization(t *testing.T) {
	for _, tc := range []struct {
		name, email string
		rejected    bool
	}{
		{"lowercase grows beyond limit", strings.Repeat("Ⱥ", 121) + "@example.com", true},
		{"lowercase grows within limit", strings.Repeat("Ⱥ", 80) + "@example.com", false},
		{"padded short address", strings.Repeat(" ", 254) + "User@Example.com ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeOIDCServer(t)
			fake.registerCode("code", OIDCClaims{Email: tc.email, Name: "Test User", EmailVerified: new(true)}, "nonce")
			log := discardLoggerAP()
			provider, err := NewOIDCProvider(t.Context(), testOIDCConfig(fake), log)
			if err != nil {
				t.Fatal(err)
			}
			db := newAuthTestDB(t)
			cfg := &config.AuthConfig{
				SessionDuration: time.Hour, MaxConcurrentSessions: 5,
				AutoProvision: config.AutoProvisionConfig{Enabled: true, AllowedDomains: []string{"example.com"}},
			}
			user, session, err := provider.HandleCallback(t.Context(), db, log, cfg, "code", "nonce", "test-pkce-verifier")
			if tc.rejected {
				if err == nil || user != nil || session != nil {
					t.Fatalf("oversized email accepted: user=%t session=%t err=%v", user != nil, session != nil, err)
				}
				if _, err := db.GetUserByEmail(t.Context(), tc.email); !models.IsNotFound(err) {
					t.Fatalf("rejected email lookup error = %v, want not found", err)
				}
				return
			}
			if err != nil || user == nil || session == nil {
				t.Fatalf("normalized email did not complete login: %v", err)
			}
			stored, err := db.GetUser(t.Context(), user.ID)
			if err != nil {
				t.Fatal(err)
			}
			if want := strings.ToLower(strings.TrimSpace(tc.email)); stored.Email != want {
				t.Fatalf("stored email = %q, want %q", stored.Email, want)
			}
			if _, err := db.GetSession(t.Context(), session.ID); err != nil {
				t.Fatalf("login session was not persisted: %v", err)
			}
		})
	}
}
