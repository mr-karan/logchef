package server

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestOIDCNonceRoundTrip(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"matching", "mismatched claim", "missing claim", "missing cookie nonce", "swapped cookie nonce"} {
		t.Run(mode, func(t *testing.T) {
			var nonce string
			var exchanges atomic.Int32
			var idp *httptest.Server
			idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					_ = json.NewEncoder(w).Encode(map[string]string{"issuer": idp.URL, "jwks_uri": idp.URL + "/jwks"})
				case "/jwks":
					_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, Algorithm: "RS256", Use: "sig"}}})
				case "/token":
					exchanges.Add(1)
					claimNonce := nonce
					if mode == "mismatched claim" {
						claimNonce = "other-login"
					}
					claims := map[string]any{"email": "user@example.com", "name": "Test User", "email_verified": true}
					if mode != "missing claim" {
						claims["nonce"] = claimNonce
					}
					raw, err := jwt.Signed(signer).Claims(jwt.Claims{Issuer: idp.URL, Audience: jwt.Audience{"test-client"}, Expiry: jwt.NewNumericDate(time.Now().Add(time.Hour))}).Claims(claims).Serialize()
					if err != nil {
						t.Error(err)
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "token", "token_type": "Bearer", "id_token": raw})
				default:
					http.NotFound(w, r)
				}
			}))
			defer idp.Close()
			var logs bytes.Buffer
			log := slog.New(slog.NewTextHandler(&logs, nil))
			provider, err := auth.NewOIDCProvider(t.Context(), &config.OIDCConfig{ProviderURL: idp.URL, AuthURL: idp.URL + "/auth", TokenURL: idp.URL + "/token", ClientID: "test-client"}, log)
			if err != nil {
				t.Fatal(err)
			}
			db := newServerTestDB(t)
			s := &Server{log: log, sqlite: db, oidcProvider: provider, config: &config.Config{Auth: config.AuthConfig{
				SessionDuration: time.Hour, MaxConcurrentSessions: 5,
				AutoProvision: config.AutoProvisionConfig{Enabled: true, AllowedDomains: []string{"example.com"}},
			}}}
			app := fiber.New()
			app.Get("/login", s.handleLogin)
			app.Get("/callback", s.handleCallback)
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/login", http.NoBody), fiber.TestConfig{Timeout: 10 * time.Second, FailOnTimeout: true})
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			authURL, err := url.Parse(resp.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			state := authURL.Query().Get("state")
			nonce = authURL.Query().Get("nonce")
			if nonce == "" || state == "" || strings.Contains(state, nonce) {
				t.Fatal("authorization request must carry independent state and nonce values")
			}
			var stateCookie *http.Cookie
			for _, cookie := range resp.Cookies() {
				if cookie.Name == stateCookieName {
					stateCookie = cookie
				}
			}
			if stateCookie == nil || stateCookie.Value != state+":"+nonce || !stateCookie.HttpOnly || stateCookie.SameSite != http.SameSiteLaxMode {
				t.Fatal("login did not store state and nonce together in the HTTP-only cookie")
			}
			switch mode {
			case "missing cookie nonce":
				stateCookie.Value = state
			case "swapped cookie nonce":
				stateCookie.Value = state + ":another-login"
			}
			logs.Reset()
			req := httptest.NewRequest(http.MethodGet, "/callback?code=code&state="+url.QueryEscape(state), http.NoBody)
			req.AddCookie(stateCookie)
			resp, err = app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second, FailOnTimeout: true})
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusTemporaryRedirect {
				t.Fatalf("callback status = %d, want 307", resp.StatusCode)
			}
			var sessionCookie *http.Cookie
			for _, cookie := range resp.Cookies() {
				if cookie.Name == sessionCookieName {
					sessionCookie = cookie
				}
			}
			if mode == "matching" {
				if sessionCookie == nil || resp.Header.Get("Location") != defaultPostLoginPath {
					t.Fatal("matching nonce did not complete login")
				}
				if _, err := db.GetSession(t.Context(), models.SessionID(sessionCookie.Value)); err != nil {
					t.Fatalf("login session was not persisted: %v", err)
				}
				return
			}
			if sessionCookie != nil || resp.Header.Get("Location") != "/auth/login?error=session_expired" {
				t.Fatal("nonce failure did not use state-failure handling")
			}
			if _, err := db.GetUserByEmail(t.Context(), "user@example.com"); !models.IsNotFound(err) {
				t.Error("nonce failure provisioned a user")
			}
			if strings.Count(logs.String(), "\n") != 1 || !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "error_code=session_expired") {
				t.Errorf("want one state-failure warning, got %s", logs.String())
			}
			if mode == "missing cookie nonce" && exchanges.Load() != 0 {
				t.Error("missing cookie nonce reached the token endpoint")
			}
		})
	}
}
