package auth

import (
	"errors"
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
		{"padded short address", strings.Repeat(" ", 254) + "User@Example.com ", true},
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
					t.Fatalf("rejected email accepted: user=%t session=%t err=%v", user != nil, session != nil, err)
				}
				if _, err := db.GetUserByEmail(t.Context(), models.NormalizeEmail(tc.email)); !models.IsNotFound(err) {
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

func TestHandleCallbackRejectsPaddedEmail(t *testing.T) {
	for _, whitespace := range []struct {
		name, value string
	}{
		{"ASCII space", " "},
		{"non-breaking space", "\u00a0"},
		{"line separator", "\u2028"},
		{"ideographic space", "\u3000"},
	} {
		for _, position := range []string{"leading", "trailing"} {
			for _, account := range []string{"new", "existing"} {
				t.Run(whitespace.name+"/"+position+"/"+account, func(t *testing.T) {
					t.Parallel()
					email := "User@Example.com"
					if position == "leading" {
						email = whitespace.value + email
					} else {
						email += whitespace.value
					}
					fake := newFakeOIDCServer(t)
					fake.registerCode("code", OIDCClaims{Email: email, Name: "Test User", EmailVerified: new(true)}, "nonce")
					log := discardLoggerAP()
					provider, err := NewOIDCProvider(t.Context(), testOIDCConfig(fake), log)
					if err != nil {
						t.Fatal(err)
					}
					db := newAuthTestDB(t)
					if account == "existing" {
						user := &models.User{Email: "user@example.com", FullName: "Test User", Role: models.UserRoleMember, Status: models.UserStatusActive}
						if err := db.CreateUser(t.Context(), user); err != nil {
							t.Fatal(err)
						}
					}
					cfg := &config.AuthConfig{
						SessionDuration: time.Hour, MaxConcurrentSessions: 5,
						AutoProvision: config.AutoProvisionConfig{Enabled: true, AllowedDomains: []string{"example.com"}},
					}
					user, session, err := provider.HandleCallback(t.Context(), db, log, cfg, "code", "nonce", "test-pkce-verifier")
					if !errors.Is(err, ErrUnauthorizedUser) || user != nil || session != nil {
						t.Fatalf("padded claim accepted: user=%t session=%t err=%v, want unauthorized", user != nil, session != nil, err)
					}
					stored, err := db.GetUserByEmail(t.Context(), "user@example.com")
					if account == "new" {
						if !models.IsNotFound(err) {
							t.Fatalf("rejected claim created an account: %v", err)
						}
					} else {
						if err != nil {
							t.Fatal(err)
						}
						if stored.LastLoginAt != nil {
							t.Fatal("rejected claim updated last login")
						}
						count, err := db.CountUserSessions(t.Context(), stored.ID)
						if err != nil || count != 0 {
							t.Fatalf("rejected claim persisted sessions: count=%d err=%v", count, err)
						}
					}
				})
			}
		}
	}
}
