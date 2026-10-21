package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
)

func TestCallbackBoundsUnresponsiveIDP(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint string
		deadline       time.Duration
	}{
		{"token timeout", "token", 0}, {"jwks timeout", "jwks", 0},
		{"token cancellation", "token", 100 * time.Millisecond}, {"jwks cancellation", "jwks", 100 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeOIDCServer(t)
			raw, err := fake.signIDToken(OIDCClaims{Email: "user@example.com", EmailVerified: new(true)})
			if err != nil {
				t.Fatal(err)
			}
			release := make(chan struct{})
			reached := make(chan struct{}, 4)
			stopped := make(chan struct{}, 4)
			var stub *httptest.Server
			stub = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]string{"issuer": stub.URL, "jwks_uri": stub.URL + "/jwks"})
				case "/token":
					if err := r.ParseForm(); err != nil {
						t.Error(err)
						return
					}
					if tc.endpoint == "jwks" {
						w.Header().Set("Content-Type", "application/json")
						_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "token_type": "Bearer", "id_token": raw})
						return
					}
					fallthrough
				default:
					reached <- struct{}{}
					select {
					case <-release:
					case <-r.Context().Done():
						stopped <- struct{}{}
					}
				}
			}))
			defer stub.Close()
			defer close(release)
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			cfg := testOIDCConfig(fake)
			cfg.ProviderURL = stub.URL
			cfg.AllowedIssuers = []string{fake.srv.URL}
			cfg.TokenURL = stub.URL + "/token"
			p, err := NewOIDCProvider(t.Context(), cfg, log)
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if tc.deadline > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.deadline)
				defer cancel()
			}
			result := make(chan error, 1)
			go func() {
				_, _, err := p.HandleCallback(ctx, nil, log, &config.AuthConfig{}, "code", "state")
				result <- err
			}()
			select {
			case <-reached:
			case <-time.After(time.Second):
				t.Fatal("IdP endpoint was not reached")
			}
			select {
			case err := <-result:
				if !errors.Is(err, ErrOIDCInvalidToken) {
					t.Fatalf("callback error = %v", err)
				}
			case <-time.After(12 * time.Second):
				t.Fatal("callback did not return within 12 seconds")
			}
			if tc.deadline > 0 {
				select {
				case <-stopped:
				case <-time.After(time.Second):
					t.Fatal("outbound IdP request ignored callback cancellation")
				}
			}
		})
	}
}
