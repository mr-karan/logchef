package server

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestCallbackSessionFailureLogsAtBoundary(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	path := filepath.Join(t.TempDir(), "callback.db")
	db, err := sqlite.New(t.Context(), sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	user := &models.User{Email: "user@example.com", FullName: "Test User", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := db.CreateUser(t.Context(), user); err != nil {
		t.Fatal(err)
	}
	rawDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()
	if _, err := rawDB.ExecContext(t.Context(), `CREATE TRIGGER fail_session BEFORE INSERT ON sessions BEGIN SELECT RAISE(ABORT, 'session insert failed'); END`); err != nil {
		t.Fatal(err)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]string{"issuer": idp.URL, "jwks_uri": idp.URL + "/jwks"})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			raw, err := jwt.Signed(signer).Claims(jwt.Claims{Issuer: idp.URL, Audience: jwt.Audience{"test-client"}, Expiry: jwt.NewNumericDate(time.Now().Add(time.Hour))}).Claims(auth.OIDCClaims{Email: user.Email, EmailVerified: new(true)}).Serialize()
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
	provider, err := auth.NewOIDCProvider(t.Context(), &config.OIDCConfig{ProviderURL: idp.URL, TokenURL: idp.URL + "/token", ClientID: "test-client"}, log)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{log: log, config: &config.Config{Auth: config.AuthConfig{SessionDuration: time.Hour, MaxConcurrentSessions: 5}}, sqlite: db, oidcProvider: provider}
	app := fiber.New()
	app.Get("/callback", s.handleCallback)
	state := fmt.Sprintf("state.nonce.%d", time.Now().Unix())
	req := httptest.NewRequest(http.MethodGet, "/callback?code=code&state="+state, http.NoBody)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Location"); got != "/auth/login?error=auth_error" {
		t.Fatalf("redirect = %q", got)
	}
	if got := strings.Count(logs.String(), "error_code=auth_error"); got != 1 {
		t.Fatalf("callback boundary logged %d failures, want one", got)
	}
	for line := range strings.SplitSeq(logs.String(), "\n") {
		if strings.Contains(line, "error_code=auth_error") {
			if !strings.Contains(line, "level=ERROR") || !strings.Contains(line, "email="+user.Email) || !strings.Contains(line, "session insert failed") {
				t.Fatalf("callback boundary lost failure context: %s", line)
			}
		}
	}
	if got := strings.Count(logs.String(), "level=ERROR"); got != 2 {
		t.Fatalf("session insert failure emitted %d ERROR lines, want 2", got)
	}
}
