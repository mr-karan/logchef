package auth

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

// TestVerifyIDToken_IssuerAllowList covers #91: issuer validation is enforced
// (no longer skipped). With no override the single discovered issuer is
// required; with an explicit allow-list only listed issuers are accepted.
func TestVerifyIDToken_IssuerAllowList(t *testing.T) {
	t.Parallel()
	fake := newFakeOIDCServer(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	raw, err := fake.signIDToken(OIDCClaims{Email: "u@example.com", EmailVerified: boolPtrAP(true)})
	if err != nil {
		t.Fatalf("signIDToken: %v", err)
	}

	t.Run("default accepts discovered issuer", func(t *testing.T) {
		t.Parallel()
		p, err := NewOIDCProvider(ctx, testOIDCConfig(fake), log)
		if err != nil {
			t.Fatalf("NewOIDCProvider: %v", err)
		}
		if _, err := p.VerifyIDToken(ctx, raw); err != nil {
			t.Fatalf("verify with discovered issuer: %v", err)
		}
	})

	t.Run("allow-list without token issuer rejects", func(t *testing.T) {
		t.Parallel()
		cfg := testOIDCConfig(fake)
		cfg.AllowedIssuers = []string{"https://wrong.example"}
		p, err := NewOIDCProvider(ctx, cfg, log)
		if err != nil {
			t.Fatalf("NewOIDCProvider: %v", err)
		}
		if _, err := p.VerifyIDToken(ctx, raw); err == nil {
			t.Fatalf("expected rejection for issuer not in allow-list")
		}
	})

	t.Run("allow-list including token issuer accepts", func(t *testing.T) {
		t.Parallel()
		cfg := testOIDCConfig(fake)
		cfg.AllowedIssuers = []string{"https://wrong.example", fake.srv.URL}
		p, err := NewOIDCProvider(ctx, cfg, log)
		if err != nil {
			t.Fatalf("NewOIDCProvider: %v", err)
		}
		if _, err := p.VerifyIDToken(ctx, raw); err != nil {
			t.Fatalf("verify with allow-listed issuer: %v", err)
		}
	})
}

func TestNormalizeIssuers(t *testing.T) {
	t.Parallel()
	got := normalizeIssuers([]string{" https://a ", "", "  ", "https://b"})
	if len(got) != 2 || got[0] != "https://a" || got[1] != "https://b" {
		t.Fatalf("normalizeIssuers = %#v, want [https://a https://b]", got)
	}
	if normalizeIssuers(nil) != nil {
		t.Fatalf("normalizeIssuers(nil) should be nil")
	}
}
