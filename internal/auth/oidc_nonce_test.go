package auth

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestHandleCallbackNonce(t *testing.T) {
	for _, tc := range []struct {
		name, expected, claim string
		valid                 bool
	}{
		{"matching", "login-nonce", "login-nonce", true},
		{"mismatched", "login-nonce", "another-login", false},
		{"missing claim", "login-nonce", "", false},
		{"missing expected nonce", "", "login-nonce", false},
		{"both missing", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeOIDCServer(t)
			const email = "nonce-user@example.com"
			fake.registerCode("code", OIDCClaims{Email: email, Name: "Test User", EmailVerified: new(true)}, tc.claim)
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			provider, err := NewOIDCProvider(t.Context(), testOIDCConfig(fake), log)
			if err != nil {
				t.Fatal(err)
			}
			logs.Reset()
			db := newAuthTestDB(t)
			cfg := &config.AuthConfig{
				SessionDuration: time.Hour, MaxConcurrentSessions: 5,
				AutoProvision: config.AutoProvisionConfig{Enabled: true, AllowedDomains: []string{"example.com"}},
			}
			user, session, err := provider.HandleCallback(t.Context(), db, log, cfg, "code", tc.expected)
			if tc.valid {
				if err != nil || user == nil || session == nil {
					t.Fatalf("matching nonce did not create a session: %v", err)
				}
				if _, err := db.GetSession(t.Context(), session.ID); err != nil {
					t.Fatalf("session was not persisted: %v", err)
				}
				return
			}
			if !errors.Is(err, ErrOIDCInvalidNonce) || user != nil || session != nil {
				t.Errorf("nonce failure accepted: user=%v session=%v err=%v", user != nil, session != nil, err)
			}
			if _, err := db.GetUserByEmail(t.Context(), email); !models.IsNotFound(err) {
				t.Error("nonce failure reached user provisioning")
			}
			if logs.Len() != 0 {
				t.Error("provider logged the nonce failure before the HTTP boundary")
			}
		})
	}
}
