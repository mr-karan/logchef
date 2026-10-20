package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

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
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: new(true)},
			skipCheck: false,
			wantErr:   false,
		},
		{
			name:      "default: email_verified explicitly false rejects",
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: new(false)},
			skipCheck: false,
			wantErr:   true,
		},
		{
			name:      "default: email_verified missing rejects",
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: nil},
			skipCheck: false,
			wantErr:   true,
		},

		// Cases with skip_email_verified_check enabled.
		{
			name:      "skip: email_verified true allows login",
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: new(true)},
			skipCheck: true,
			wantErr:   false,
		},
		{
			name:      "skip: email_verified explicitly false still rejects",
			claims:    OIDCClaims{Email: "a@b.com", EmailVerified: new(false)},
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

// TestDiscoverProvider covers #136: discovery retries while the IdP is down,
// gives up after maxWait, and fails fast on an issuer mismatch.
func TestDiscoverProvider(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	retry := discoveryRetry{
		attemptTimeout: time.Second,
		maxWait:        2 * time.Second,
		initialBackoff: 10 * time.Millisecond,
		maxBackoff:     50 * time.Millisecond,
	}

	// newIDP serves 503 for the first failFor requests, then a discovery
	// document advertising issuer (the server's own URL when empty).
	newIDP := func(t *testing.T, failFor int32, issuer string) (*httptest.Server, *atomic.Int32) {
		t.Helper()
		var hits atomic.Int32
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hits.Add(1) <= failFor {
				http.Error(w, "starting up", http.StatusServiceUnavailable)
				return
			}
			iss := issuer
			if iss == "" {
				iss = srv.URL
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                 iss,
				"authorization_endpoint": iss + "/auth",
				"token_endpoint":         iss + "/token",
				"jwks_uri":               iss + "/jwks",
			})
		}))
		t.Cleanup(srv.Close)
		return srv, &hits
	}

	t.Run("succeeds once the IdP comes up", func(t *testing.T) {
		t.Parallel()
		srv, hits := newIDP(t, 2, "")
		p, err := discoverProvider(context.Background(), srv.URL, retry, log)
		if err != nil {
			t.Fatalf("discoverProvider: %v", err)
		}
		if p.Endpoint().TokenURL != srv.URL+"/token" {
			t.Fatalf("token endpoint = %q, want %q", p.Endpoint().TokenURL, srv.URL+"/token")
		}
		if got := hits.Load(); got != 3 {
			t.Fatalf("discovery requests = %d, want 3", got)
		}
	})

	t.Run("gives up after maxWait", func(t *testing.T) {
		t.Parallel()
		srv, hits := newIDP(t, 1<<30, "")
		short := retry
		short.maxWait = 200 * time.Millisecond
		start := time.Now()
		_, err := discoverProvider(context.Background(), srv.URL, short, log)
		if err == nil {
			t.Fatal("expected error when the IdP never comes up")
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("discoverProvider took %v, want it bounded by maxWait", elapsed)
		}
		if hits.Load() < 2 {
			t.Fatalf("discovery requests = %d, want retries", hits.Load())
		}
	})

	t.Run("does not retry an issuer mismatch", func(t *testing.T) {
		t.Parallel()
		srv, hits := newIDP(t, 0, "https://other.example")
		_, err := discoverProvider(context.Background(), srv.URL, retry, log)
		if _, ok := errors.AsType[*oidc.IssuerMismatchError](err); !ok {
			t.Fatalf("err = %v, want IssuerMismatchError", err)
		}
		if got := hits.Load(); got != 1 {
			t.Fatalf("discovery requests = %d, want 1", got)
		}
	})
}
