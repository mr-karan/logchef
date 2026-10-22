package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
)

func TestCallbackIDPFailures(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, endpoint, code string
		status               int
		timeout              bool
	}{
		{name: "token 503", endpoint: "/token", status: http.StatusServiceUnavailable, code: "auth_error"},
		{name: "token timeout", endpoint: "/token", timeout: true, code: "auth_error"},
		{name: "JWKS timeout", endpoint: "/jwks", timeout: true, code: "auth_error"},
		{name: "JWKS 503", endpoint: "/jwks", status: http.StatusServiceUnavailable, code: "auth_error"},
		{name: "token rejection", endpoint: "/token", status: http.StatusBadRequest, code: "invalid_token"},
		{name: "bad signature", endpoint: "/jwks", code: "invalid_token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			release := make(chan struct{})
			var idp *httptest.Server
			idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == tc.endpoint {
					if tc.timeout {
						select {
						case <-release:
						case <-r.Context().Done():
						}
						return
					}
					if tc.status != 0 {
						w.WriteHeader(tc.status)
						_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
						return
					}
				}
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					_ = json.NewEncoder(w).Encode(map[string]string{"issuer": idp.URL, "jwks_uri": idp.URL + "/jwks"})
				case "/token":
					raw, err := jwt.Signed(signer).Claims(jwt.Claims{Issuer: idp.URL, Audience: jwt.Audience{"test-client"}, Expiry: jwt.NewNumericDate(time.Now().Add(time.Hour))}).Serialize()
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "token_type": "Bearer", "id_token": raw})
				case "/jwks":
					_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{})
				default:
					http.NotFound(w, r)
				}
			}))
			defer idp.Close()
			defer close(release)
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			provider, err := auth.NewOIDCProvider(t.Context(), &config.OIDCConfig{ProviderURL: idp.URL, TokenURL: idp.URL + "/token", ClientID: "test-client"}, log)
			if err != nil {
				t.Fatal(err)
			}
			logs.Reset()
			s := &Server{log: log, config: &config.Config{}, oidcProvider: provider}
			app := fiber.New()
			app.Get("/callback", func(c fiber.Ctx) error {
				ctx := c.Context()
				if tc.timeout {
					var cancel context.CancelFunc
					ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
					defer cancel()
				}
				c.SetContext(ctx)
				return s.handleCallback(c)
			})
			state := fmt.Sprintf("state.nonce.%d", time.Now().Unix())
			req := httptest.NewRequest(http.MethodGet, "/callback?code=code&state="+state, http.NoBody)
			req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
			req.AddCookie(&http.Cookie{Name: nonceCookieName, Value: "test-browser-secret"})
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if got, want := resp.Header.Get("Location"), "/auth/login?error="+tc.code; got != want {
				t.Errorf("redirect = %q, want %q", got, want)
			}
			level := "WARN"
			if tc.code == "auth_error" {
				level = "ERROR"
			}
			if strings.Count(logs.String(), "\n") != 1 || !strings.Contains(logs.String(), "level="+level) || !strings.Contains(logs.String(), "error_code="+tc.code) {
				t.Errorf("want one %s line with error_code=%s, got %s", level, tc.code, logs.String())
			}
		})
	}
}
