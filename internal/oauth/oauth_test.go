package oauth

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zitadel/oidc/v3/pkg/op"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func testConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{PublicURL: "https://logchef.test"},
		Auth: config.AuthConfig{
			APITokenSecret: testSecret,
			OAuth: config.OAuthConfig{
				Enabled: true,
				Clients: []config.OAuthClientConfig{{ID: "chatgpt", Name: "ChatGPT", RedirectURIs: []string{"https://chatgpt.example/cb"}}},
			},
		},
	}
}

func openDB(t *testing.T, path string) *sqlite.DB {
	t.Helper()
	db, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: path},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTestServer(t *testing.T, db *sqlite.DB) *Server {
	t.Helper()
	s, err := New(testConfig(), db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// issueToken creates a user and a grant through the store and issues an
// access token the way the storage adapter does. It returns the encrypted
// bearer value and its plaintext "<id>:<subject>".
func issueToken(t *testing.T, s *Server, scopes []models.TokenScope, resource string) (token, plain string) {
	t.Helper()
	ctx := context.Background()
	user := &models.User{Email: "u" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com", Status: models.UserStatusActive, AccountType: models.UserAccountTypeHuman, Role: models.UserRoleMember}
	if err := s.db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	req := &models.OAuthAuthRequest{
		ID: models.OAuthAuthRequestID(randomID()), ClientID: "chatgpt", RedirectURI: "https://chatgpt.example/cb",
		Resource: resource, Scopes: scopes, CodeChallenge: strings.Repeat("a", 43), State: "s",
		ExpiresAt: now.Add(time.Minute), CreatedAt: now,
	}
	if err := s.db.CreateOAuthAuthRequest(ctx, req); err != nil {
		t.Fatal(err)
	}
	grant, err := s.db.ApproveOAuthAuthRequest(ctx, req.ID, user.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	st := &storage{server: s}
	tokenID, issue, _ := st.newIssue(scopeStrings(scopes, false), now, false)
	if err := s.db.IssueOAuthTokens(ctx, grant.ID, issue, now); err != nil {
		t.Fatal(err)
	}
	plain = tokenID + ":" + strconv.Itoa(int(user.ID))
	token, err = s.crypto.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	return token, plain
}

// F3: the provider and the resource server use AES-GCM only. A token in the
// legacy AES-CFB format, built from a real live token ID and the real key, is
// rejected by both.
func TestLegacyCFBTokensRejected(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, openDB(t, filepath.Join(t.TempDir(), "o.db")))
	token, plain := issueToken(t, s, []models.TokenScope{models.TokenScopeLogsRead}, s.mcpResource)
	if _, err := s.AuthenticateAccessToken(context.Background(), token, ResourceMCP); err != nil {
		t.Fatalf("GCM token rejected: %v", err)
	}
	key, err := deriveKey(testSecret, cryptoKeyLabel)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := op.NewAESCrypto(key).Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.provider.Crypto().Decrypt(legacy); err == nil {
		t.Fatal("provider crypto decrypted a legacy AES-CFB value")
	}
	if _, err := s.AuthenticateAccessToken(context.Background(), legacy, ResourceMCP); !errors.Is(err, ErrInvalidAccessToken) {
		t.Fatalf("legacy token: err = %v, want ErrInvalidAccessToken", err)
	}
	if got, err := s.provider.Crypto().Decrypt(token); err != nil || got != plain {
		t.Fatalf("provider crypto cannot read its own GCM token: %q %v", got, err)
	}
}

// Keys come from api_token_secret through HKDF, so a second process (another
// replica, or the same one after a restart) accepts tokens the first issued.
// A different secret does not.
func TestKeysDeriveFromSecret(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "o.db")
	first := newTestServer(t, openDB(t, path))
	token, _ := issueToken(t, first, []models.TokenScope{models.TokenScopeLogsRead}, first.apiResource)
	second := newTestServer(t, openDB(t, path))
	if _, err := second.AuthenticateAccessToken(context.Background(), token, ResourceAPI); err != nil {
		t.Fatalf("second process rejected the token: %v", err)
	}
	cfg := testConfig()
	cfg.Auth.APITokenSecret = strings.Repeat("z", 32)
	other, err := New(cfg, openDB(t, path), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.AuthenticateAccessToken(context.Background(), token, ResourceAPI); err == nil {
		t.Fatal("a server with another secret accepted the token")
	}
	hashKey, _ := deriveKey(testSecret, hashKeyLabel)
	cryptoKey, _ := deriveKey(testSecret, cryptoKeyLabel)
	if hashKey == cryptoKey {
		t.Fatal("hash and crypto keys are equal")
	}
}

// The scopes and expiry carried in the token ID are covered by the stored
// HMAC: changing them makes the token unknown.
func TestAccessTokenIDTamperRejected(t *testing.T) {
	t.Parallel()
	s := newTestServer(t, openDB(t, filepath.Join(t.TempDir(), "o.db")))
	_, plain := issueToken(t, s, []models.TokenScope{models.TokenScopeProfileRead, models.TokenScopeLogsRead}, s.apiResource)
	tokenID, subject, _ := strings.Cut(plain, ":")
	parts := strings.Split(tokenID, ".")

	widened := parts[0] + "." + strconv.FormatUint(uint64(scopeMask(scopeStrings(ReadScopes, false))), 10) + "." + parts[2]
	later := parts[0] + "." + parts[1] + "." + strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	for name, id := range map[string]string{"widened scopes": widened, "later expiry": later, "other subject": tokenID} {
		sub := subject
		if name == "other subject" {
			sub = "999999"
		}
		forged, err := s.crypto.Encrypt(id + ":" + sub)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.AuthenticateAccessToken(context.Background(), forged, ResourceAPI); !errors.Is(err, ErrInvalidAccessToken) {
			t.Errorf("%s: err = %v, want ErrInvalidAccessToken", name, err)
		}
	}

	token, err := s.crypto.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.AuthenticateAccessToken(context.Background(), token, ResourceAPI)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Principal.Scopes(), []models.TokenScope{models.TokenScopeProfileRead, models.TokenScopeLogsRead}) {
		t.Fatalf("scopes = %v", got.Principal.Scopes())
	}
	if got.Principal.Require(models.TokenScopeSourcesRead) == nil {
		t.Fatal("principal holds a scope the token does not carry")
	}
}

func TestParseScopes(t *testing.T) {
	t.Parallel()
	scopes, offline, err := parseScopes([]string{"logs:read", "offline_access", "profile:read", "logs:read"})
	if err != nil || !offline || !slices.Equal(scopes, []models.TokenScope{models.TokenScopeProfileRead, models.TokenScopeLogsRead}) {
		t.Fatalf("parseScopes = %v %v %v", scopes, offline, err)
	}
	for _, bad := range [][]string{nil, {"offline_access"}, {"openid"}, {"logs:read", "logs:write"}, {"*"}, {"tokens:read"}} {
		if _, _, err := parseScopes(bad); err == nil {
			t.Errorf("parseScopes(%v) accepted", bad)
		}
	}
	for mask := range uint(1 << len(ReadScopes)) {
		if got := scopeMask(scopeStrings(scopesFromMask(mask), false)); got != mask {
			t.Fatalf("mask %d round-trips to %d", mask, got)
		}
	}
}

func TestNativeRedirectPolicy(t *testing.T) {
	t.Parallel()
	clients := newClients(config.OAuthConfig{}, "https://logchef.test")
	for uri, want := range map[string]bool{
		"http://127.0.0.1:8080/callback":    true,
		"http://127.0.0.1/callback":         true,
		"http://[::1]:8080/callback":        true,
		"http://localhost:8080/callback":    true,
		"http://localhost/callback":         true,
		"http://LOCALHOST:8080/callback":    false,
		"http://localhost.example/callback": false,
		"http://localhost:8787/callback/ab": false,
		"https://127.0.0.1:8080/callback":   false,
		"http://127.0.0.1:8080/callback/":   false,
		"http://127.0.0.1:8080/Callback":    false,
		"http://127.0.0.1:8080/%63allback":  false,
		"http://127.0.0.1:8080/callback?":   false,
		"http://127.0.0.1:8080/callback#":   false,
		"http://127.0.0.1:abc/callback":     false,
		"http://127.0.0.1:/callback":        false,
		"http://[::1/callback":              false,
		"http://0x7f.0.0.1:80/callback":     false,
		"http://127.1:80/callback":          false,
		"HTTP://127.0.0.1:80/callback":      false,
	} {
		for _, id := range []models.OAuthClientID{config.OAuthCLIClientID, config.OAuthMCPClientID} {
			if got := clients[id].redirectAllowed(uri); got != want {
				t.Errorf("%s redirectAllowed(%q) = %v, want %v", id, uri, got, want)
			}
		}
	}
}
