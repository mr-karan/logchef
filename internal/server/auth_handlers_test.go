package server

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// newCLITokenIDP starts an IdP that serves discovery and JWKS, and returns
// a function that signs an otherwise valid ID token for aud.
func newCLITokenIDP(t *testing.T) (idp *httptest.Server, sign func(aud string) string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k", Algorithm: "RS256", Use: "sig"}}})
	})

	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "k"))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	sign = func(aud string) string {
		now := time.Now()
		raw, err := jwt.Signed(signer).Claims(map[string]any{
			"iss":            srv.URL,
			"sub":            "cli-user",
			"aud":            aud,
			"iat":            now.Unix(),
			"exp":            now.Add(time.Hour).Unix(),
			"email":          "cli@example.com",
			"email_verified": true,
		}).Serialize()
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return raw
	}
	return srv, sign
}

func newCLITokenApp(t *testing.T, idp *httptest.Server, cliClientID string) *fiber.App {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		OIDC: config.OIDCConfig{
			ProviderURL: idp.URL,
			AuthURL:     idp.URL + "/auth",
			TokenURL:    idp.URL + "/token",
			ClientID:    "logchef",
			CLIClientID: cliClientID,
			RedirectURL: "http://localhost/callback",
		},
		Auth: config.AuthConfig{APITokenSecret: "test-secret"},
	}
	provider, err := auth.NewOIDCProvider(context.Background(), &cfg.OIDC, log)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	db := newServerTestDB(t)
	if _, err := core.CreateUser(context.Background(), db, log, "cli@example.com", "CLI User", models.UserRoleMember, models.UserStatusActive); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	s := New(ServerOptions{
		Config:       cfg,
		SQLite:       db,
		OIDCProvider: provider,
		FS:           http.FS(fstest.MapFS{"index.html": {Data: []byte(`<base href="/" />`)}}),
		Logger:       log,
	})
	t.Cleanup(func() { _ = s.Shutdown(context.Background()) })
	return s.app
}

func postCLIToken(t *testing.T, app *fiber.App, idToken string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/cli/token", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+idToken)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// TestCLITokenExchange_UsesCLIAudience: /cli/token accepts an ID token issued
// to cli_client_id and rejects one issued to the browser client_id.
func TestCLITokenExchange_UsesCLIAudience(t *testing.T) {
	t.Parallel()
	idp, sign := newCLITokenIDP(t)
	app := newCLITokenApp(t, idp, "logchef-cli")

	if got := postCLIToken(t, app, sign("logchef-cli")); got != http.StatusOK {
		t.Errorf("logchef-cli token: status = %d, want 200", got)
	}
	if got := postCLIToken(t, app, sign("logchef")); got != http.StatusUnauthorized {
		t.Errorf("logchef token: status = %d, want 401", got)
	}
}

// TestCLITokenExchange_WithoutCLIClientID: without cli_client_id, /cli/token
// returns the status it returns when no OIDC provider exists.
func TestCLITokenExchange_WithoutCLIClientID(t *testing.T) {
	t.Parallel()
	idp, sign := newCLITokenIDP(t)
	app := newCLITokenApp(t, idp, "")

	if got := postCLIToken(t, app, sign("logchef")); got != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", got)
	}
}
