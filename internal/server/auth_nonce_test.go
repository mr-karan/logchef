package server

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
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
	type loginCookies struct {
		state, nonce, verifier *http.Cookie
		callbackState          string
	}
	for _, tc := range []struct {
		name                                 string
		insecureCookies                      bool
		anotherLogin                         bool
		mutate                               func(current, other *loginCookies)
		claimNonce                           func(string) string
		wantLogin, wantExchange, wantCleared bool
		errorCode                            string
	}{
		{name: "matching", wantLogin: true, wantExchange: true, wantCleared: true},
		{name: "matching insecure cookies", insecureCookies: true, wantLogin: true, wantExchange: true, wantCleared: true},
		{name: "mismatched claim", claimNonce: func(string) string { return "other-login" }, wantExchange: true, wantCleared: true},
		{name: "missing claim", claimNonce: func(string) string { return "" }, wantExchange: true, wantCleared: true},
		{name: "missing state cookie", mutate: func(current, _ *loginCookies) { current.state = nil }},
		{name: "swapped state cookie", anotherLogin: true, mutate: func(current, other *loginCookies) { current.state = other.state }},
		{name: "swapped login code", anotherLogin: true, mutate: func(current, other *loginCookies) {
			current.state, current.nonce = other.state, other.nonce
			current.callbackState = other.callbackState
		}, wantExchange: true, wantCleared: true},
		{name: "missing nonce cookie", mutate: func(current, _ *loginCookies) { current.nonce = nil }, wantCleared: true},
		{name: "swapped nonce cookie", mutate: func(current, _ *loginCookies) { current.nonce.Value = "other-browser-secret" }, wantExchange: true, wantCleared: true},
		{name: "URL nonce as cookie", mutate: func(current, _ *loginCookies) { current.nonce.Value = nonceFromSecret(current.nonce.Value) }, wantExchange: true, wantCleared: true},
		{name: "missing verifier cookie", mutate: func(current, _ *loginCookies) { current.verifier = nil }, wantCleared: true},
		{name: "empty verifier cookie", mutate: func(current, _ *loginCookies) { current.verifier.Value = "" }, wantCleared: true},
		{name: "swapped verifier cookie", anotherLogin: true, mutate: func(current, other *loginCookies) { current.verifier = other.verifier }, wantExchange: true, wantCleared: true, errorCode: "invalid_token"},
		{name: "URL challenge as verifier", mutate: func(current, _ *loginCookies) { current.verifier.Value = nonceFromSecret(current.verifier.Value) }, wantExchange: true, wantCleared: true, errorCode: "invalid_token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var nonce, challenge string
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
					if err := r.ParseForm(); err != nil {
						t.Error(err)
					}
					digest := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
					if base64.RawURLEncoding.EncodeToString(digest[:]) != challenge {
						w.WriteHeader(http.StatusBadRequest)
						_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
						return
					}
					claimNonce := nonce
					if tc.claimNonce != nil {
						claimNonce = tc.claimNonce(nonce)
					}
					claims := map[string]any{"email": "user@example.com", "name": "Test User", "email_verified": true}
					if claimNonce != "" {
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
			s := &Server{log: log, store: db, oidcProvider: provider, config: &config.Config{Auth: config.AuthConfig{
				SessionDuration: time.Hour, MaxConcurrentSessions: 5,
				AutoProvision: config.AutoProvisionConfig{Enabled: true, AllowedDomains: []string{"example.com"}},
			}}}
			s.config.Server.SecureCookie = new(!tc.insecureCookies)
			app := fiber.New()
			app.Get("/login", s.handleLogin)
			app.Get("/callback", s.handleCallback)
			startLogin := func() (loginCookies, string, string) {
				t.Helper()
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
				nonce := authURL.Query().Get("nonce")
				if nonce == "" || state == "" || strings.Contains(state, nonce) {
					t.Fatal("authorization request must carry distinct state and nonce values")
				}
				login := loginCookies{callbackState: state}
				for _, cookie := range resp.Cookies() {
					switch cookie.Name {
					case stateCookieName:
						login.state = cookie
					case nonceCookieName:
						login.nonce = cookie
					case verifierCookieName:
						login.verifier = cookie
					}
				}
				if login.state == nil || login.state.Value != state || !login.state.HttpOnly || login.state.SameSite != http.SameSiteLaxMode || login.state.Secure != s.config.Server.IsSecureCookie() {
					t.Fatal("login did not preserve the state-only HTTP-only cookie")
				}
				for _, cookie := range []*http.Cookie{login.nonce, login.verifier} {
					if cookie == nil || !cookie.HttpOnly || cookie.SameSite != login.state.SameSite || cookie.Secure != login.state.Secure || cookie.Path != login.state.Path || cookie.Domain != login.state.Domain || cookie.MaxAge != login.state.MaxAge || !cookie.Expires.Equal(login.state.Expires) {
						t.Fatal("login secret cookies must share the state cookie protections and lifetime")
					}
					secretBytes, err := base64.RawURLEncoding.DecodeString(cookie.Value)
					if err != nil || len(secretBytes) != 32 {
						t.Fatal("login secret cookie must encode 32 random bytes")
					}
				}
				digest := sha256.Sum256([]byte(login.nonce.Value))
				if nonce != base64.RawURLEncoding.EncodeToString(digest[:]) || strings.Contains(authURL.String(), login.nonce.Value) {
					t.Fatal("authorization URL must carry only the hash of the nonce secret")
				}
				challenge := sha256.Sum256([]byte(login.verifier.Value))
				if authURL.Query().Get("code_challenge_method") != "S256" || authURL.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(challenge[:]) || strings.Contains(authURL.String(), login.verifier.Value) {
					t.Fatal("authorization URL must carry only the S256 challenge of the verifier")
				}
				return login, nonce, authURL.Query().Get("code_challenge")
			}
			login, nonceParam, challengeParam := startLogin()
			nonce, challenge = nonceParam, challengeParam
			stateCookie := *login.state
			var other loginCookies
			if tc.anotherLogin {
				other, _, _ = startLogin()
				if other.verifier.Value == login.verifier.Value {
					t.Fatal("each login must have a fresh verifier")
				}
			}
			if tc.mutate != nil {
				tc.mutate(&login, &other)
			}
			logs.Reset()
			req := httptest.NewRequest(http.MethodGet, "/callback?code=code&state="+url.QueryEscape(login.callbackState), http.NoBody)
			for _, cookie := range []*http.Cookie{login.state, login.nonce, login.verifier} {
				if cookie != nil {
					req.AddCookie(cookie)
				}
			}
			resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second, FailOnTimeout: true})
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
			for _, name := range []string{stateCookieName, nonceCookieName, verifierCookieName} {
				var cleared bool
				for _, cookie := range resp.Cookies() {
					if cookie.Name == name && cookie.Value == "" && cookie.Expires.Before(time.Now()) && cookie.HttpOnly && cookie.SameSite == stateCookie.SameSite && cookie.Secure == stateCookie.Secure && cookie.Path == stateCookie.Path && cookie.Domain == stateCookie.Domain {
						cleared = true
					}
				}
				if cleared != tc.wantCleared {
					t.Errorf("callback cleared %s = %t, want %t", name, cleared, tc.wantCleared)
				}
			}
			if got := exchanges.Load() > 0; got != tc.wantExchange {
				t.Errorf("token exchange = %t, want %t", got, tc.wantExchange)
			}
			if tc.wantLogin {
				if sessionCookie == nil || resp.Header.Get("Location") != defaultPostLoginPath {
					t.Fatal("matching nonce did not complete login")
				}
				if _, err := db.GetSession(t.Context(), models.SessionID(sessionCookie.Value)); err != nil {
					t.Fatalf("login session was not persisted: %v", err)
				}
				return
			}
			errorCode := tc.errorCode
			if errorCode == "" {
				errorCode = "session_expired"
			}
			if sessionCookie != nil || resp.Header.Get("Location") != "/auth/login?error="+errorCode {
				t.Fatalf("login failure redirect = %q, want %s without a session", resp.Header.Get("Location"), errorCode)
			}
			if _, err := db.GetUserByEmail(t.Context(), "user@example.com"); !models.IsNotFound(err) {
				t.Error("nonce failure provisioned a user")
			}
			if strings.Count(logs.String(), "\n") != 1 || !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "error_code="+errorCode) {
				t.Errorf("want one login-failure warning, got %s", logs.String())
			}
		})
	}
}
