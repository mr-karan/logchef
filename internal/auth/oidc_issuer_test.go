package auth

import (
	"context"
	"errors"
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

	raw, err := fake.signIDToken(OIDCClaims{Email: "u@example.com", EmailVerified: new(true)})
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

// newCLITestProvider returns a provider for the fake IdP with browser client
// test-client and CLI client test-cli.
func newCLITestProvider(ctx context.Context, t *testing.T, fake *fakeOIDCServer, allowedIssuers ...string) *OIDCProvider {
	t.Helper()
	cfg := testOIDCConfig(fake)
	cfg.CLIClientID = "test-cli"
	cfg.AllowedIssuers = allowedIssuers
	p, err := NewOIDCProvider(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	return p
}

// TestVerifyCLIIDToken_SeparateAudiences: a token issued to cli_client_id
// passes only the CLI verifier, and a token issued to client_id passes only
// the browser verifier.
func TestVerifyCLIIDToken_SeparateAudiences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake := newFakeOIDCServer(t)
	p := newCLITestProvider(ctx, t, fake)
	claims := OIDCClaims{Email: "u@example.com", EmailVerified: new(true)}

	cliToken, err := fake.signIDTokenFor(fake.srv.URL, "test-cli", claims)
	if err != nil {
		t.Fatalf("sign CLI token: %v", err)
	}
	browserToken, err := fake.signIDTokenFor(fake.srv.URL, "test-client", claims)
	if err != nil {
		t.Fatalf("sign browser token: %v", err)
	}

	if _, err := p.VerifyCLIIDToken(ctx, cliToken); err != nil {
		t.Errorf("CLI verifier rejected aud=test-cli: %v", err)
	}
	if _, err := p.VerifyIDToken(ctx, cliToken); err == nil {
		t.Errorf("browser verifier accepted aud=test-cli")
	}
	if _, err := p.VerifyIDToken(ctx, browserToken); err != nil {
		t.Errorf("browser verifier rejected aud=test-client: %v", err)
	}
	if _, err := p.VerifyCLIIDToken(ctx, browserToken); err == nil {
		t.Errorf("CLI verifier accepted aud=test-client")
	}
}

// TestVerifyCLIIDToken_IssuerAllowList: allowed_issuers applies to the CLI
// verifier as it does to the browser verifier.
func TestVerifyCLIIDToken_IssuerAllowList(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake := newFakeOIDCServer(t)
	claims := OIDCClaims{Email: "u@example.com", EmailVerified: new(true)}

	const realmB = "https://realm-b.example"
	p := newCLITestProvider(ctx, t, fake, fake.srv.URL, realmB)
	for _, tc := range []struct {
		issuer string
		want   bool
	}{
		{fake.srv.URL, true},
		{realmB, true},
		{"https://realm-c.example", false},
	} {
		for _, aud := range []string{"test-client", "test-cli"} {
			raw, err := fake.signIDTokenFor(tc.issuer, aud, claims)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			verify := p.VerifyIDToken
			if aud == "test-cli" {
				verify = p.VerifyCLIIDToken
			}
			_, err = verify(ctx, raw)
			if tc.want && err != nil {
				t.Errorf("aud=%s iss=%s rejected: %v", aud, tc.issuer, err)
			}
			if !tc.want && !errors.Is(err, ErrOIDCInvalidToken) {
				t.Errorf("aud=%s iss=%s error = %v, want ErrOIDCInvalidToken", aud, tc.issuer, err)
			}
		}
	}

	// Without an allow-list, only the discovered issuer passes.
	strict := newCLITestProvider(ctx, t, fake)
	raw, err := fake.signIDTokenFor(realmB, "test-cli", claims)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := strict.VerifyCLIIDToken(ctx, raw); err == nil {
		t.Errorf("CLI verifier without allow-list accepted issuer %s", realmB)
	}
}

// TestVerifyCLIIDToken_NotConfigured: without cli_client_id there is no CLI
// verifier.
func TestVerifyCLIIDToken_NotConfigured(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fake := newFakeOIDCServer(t)
	p, err := NewOIDCProvider(ctx, testOIDCConfig(fake), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	raw, err := fake.signIDTokenFor(fake.srv.URL, "test-cli", OIDCClaims{Email: "u@example.com", EmailVerified: new(true)})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := p.VerifyCLIIDToken(ctx, raw); !errors.Is(err, ErrOIDCProviderNotConfigured) {
		t.Fatalf("error = %v, want ErrOIDCProviderNotConfigured", err)
	}
}
