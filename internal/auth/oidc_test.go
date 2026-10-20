package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
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

// TestDiscoverProvider covers #136: discovery retries while the IdP is booting,
// gives up after maxWait, and fails fast when waiting cannot help.
func TestDiscoverProvider(t *testing.T) {
	t.Parallel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	retry := discoveryRetry{
		attemptTimeout: time.Second,
		maxWait:        2 * time.Second,
		initialBackoff: 10 * time.Millisecond,
		maxBackoff:     50 * time.Millisecond,
	}

	// newIDP answers the first failFor requests with failStatus (and a body
	// that is not discovery JSON), then serves a discovery document
	// advertising issuer (the server's own URL when empty).
	newIDP := func(t *testing.T, failFor int32, failStatus int, issuer string) (*httptest.Server, *atomic.Int32) {
		t.Helper()
		var hits atomic.Int32
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if hits.Add(1) <= failFor {
				w.WriteHeader(failStatus)
				_, _ = w.Write([]byte("<html>not discovery</html>"))
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

	for _, status := range []int{http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusNotFound, http.StatusTooManyRequests} {
		t.Run(fmt.Sprintf("retries %d until the IdP is up", status), func(t *testing.T) {
			t.Parallel()
			srv, hits := newIDP(t, 2, status, "")
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
	}

	permanent := []struct {
		name       string
		failStatus int
		issuer     string
	}{
		{name: "403", failStatus: http.StatusForbidden},
		{name: "invalid metadata", failStatus: http.StatusOK},
		{name: "issuer mismatch", issuer: "https://other.example"},
	}
	for _, tc := range permanent {
		t.Run("fails fast on "+tc.name, func(t *testing.T) {
			t.Parallel()
			failFor := int32(1 << 30)
			if tc.failStatus == 0 {
				failFor = 0
			}
			srv, hits := newIDP(t, failFor, tc.failStatus, tc.issuer)
			if _, err := discoverProvider(context.Background(), srv.URL, retry, log); err == nil {
				t.Fatal("expected error")
			}
			if got := hits.Load(); got != 1 {
				t.Fatalf("discovery requests = %d, want 1", got)
			}
		})
	}

	t.Run("fails fast on a malformed URL", func(t *testing.T) {
		t.Parallel()
		start := time.Now()
		if _, err := discoverProvider(context.Background(), "http://[::1", retry, log); err == nil {
			t.Fatal("expected error")
		}
		if elapsed := time.Since(start); elapsed > retry.initialBackoff {
			t.Fatalf("discoverProvider took %v, want no retry", elapsed)
		}
	})

	t.Run("gives up after maxWait", func(t *testing.T) {
		t.Parallel()
		srv, hits := newIDP(t, 1<<30, http.StatusServiceUnavailable, "")
		short := retry
		short.maxWait = 200 * time.Millisecond
		start := time.Now()
		if _, err := discoverProvider(context.Background(), srv.URL, short, log); err == nil {
			t.Fatal("expected error when the IdP never comes up")
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("discoverProvider took %v, want it bounded by maxWait", elapsed)
		}
		if hits.Load() < 2 {
			t.Fatalf("discovery requests = %d, want retries", hits.Load())
		}
	})
}
