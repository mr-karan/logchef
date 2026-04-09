package auth

import (
	"errors"
	"log/slog"
	"os"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestCheckEmailVerified(t *testing.T) {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	tests := []struct {
		name      string
		claims    OIDCClaims
		skipCheck bool
		wantErr   bool
	}{
		// Default behavior (skipCheck = false)
		{
			name:      "default: email_verified true allows login",
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: boolPtr(true)},
			skipCheck: false,
			wantErr:   false,
		},
		{
			name:      "default: email_verified explicitly false rejects",
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: boolPtr(false)},
			skipCheck: false,
			wantErr:   true,
		},
		{
			name:      "default: email_verified missing rejects",
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: nil},
			skipCheck: false,
			wantErr:   true,
		},

		// skip_email_verified_check = true
		{
			name:      "skip: email_verified true allows login",
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: boolPtr(true)},
			skipCheck: true,
			wantErr:   false,
		},
		{
			name:      "skip: email_verified explicitly false still rejects",
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: boolPtr(false)},
			skipCheck: true,
			wantErr:   true,
		},
		{
			name:      "skip: email_verified missing allows login",
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: nil},
			skipCheck: true,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckEmailVerified(tt.claims, tt.skipCheck, log, "test")
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckEmailVerified() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrOIDCEmailNotVerified) {
				t.Errorf("CheckEmailVerified() expected ErrOIDCEmailNotVerified, got %v", err)
			}
		})
	}
}
