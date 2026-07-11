package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeOIDCServer is a minimal OIDC provider used to exercise HandleCallback
// end-to-end (discovery, JWKS, and token exchange) without a real IdP, per
// the spec's allowance for an httptest-based substitute for a full browser
// OIDC dance.
type fakeOIDCServer struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu     sync.Mutex
	tokens map[string]OIDCClaims // authorization code -> claims to embed in the id_token
}

func newFakeOIDCServer(t *testing.T) *fakeOIDCServer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}

	f := &fakeOIDCServer{key: key, tokens: make(map[string]OIDCClaims)}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                f.srv.URL,
			"authorization_endpoint":                f.srv.URL + "/auth",
			"token_endpoint":                        f.srv.URL + "/token",
			"jwks_uri":                              f.srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{{
				Key:       &f.key.PublicKey,
				KeyID:     "test-key",
				Algorithm: "RS256",
				Use:       "sig",
			}},
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		code := r.FormValue("code")

		f.mu.Lock()
		claims, ok := f.tokens[code]
		f.mu.Unlock()
		if !ok {
			http.Error(w, "unknown code", http.StatusBadRequest)
			return
		}

		idToken, err := f.signIDToken(claims)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fake-access-token",
			"token_type":   "Bearer",
			"id_token":     idToken,
		})
	})
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not used in tests", http.StatusNotImplemented)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// registerCode maps an authorization code to the claims the fake token
// endpoint will embed in the signed id_token it returns for that code.
func (f *fakeOIDCServer) registerCode(code string, claims OIDCClaims) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tokens[code] = claims
}

func (f *fakeOIDCServer) signIDToken(claims OIDCClaims) (string, error) {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithHeader("kid", "test-key"))
	if err != nil {
		return "", err
	}

	std := jwt.Claims{
		Issuer:   f.srv.URL,
		Subject:  claims.Email,
		Audience: jwt.Audience{"test-client"},
		Expiry:   jwt.NewNumericDate(time.Now().Add(time.Hour)),
		IssuedAt: jwt.NewNumericDate(time.Now()),
	}
	custom := map[string]any{
		"email": claims.Email,
		"name":  claims.Name,
	}
	if claims.EmailVerified != nil {
		custom["email_verified"] = *claims.EmailVerified
	}

	return jwt.Signed(signer).Claims(std).Claims(custom).Serialize()
}

func boolPtrAP(b bool) *bool { return &b }

func newAuthTestDB(t *testing.T) *sqlite.DB {
	t.Helper()
	db, err := sqlite.New(context.Background(), sqlite.Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "auth-test.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func testOIDCConfig(fake *fakeOIDCServer) *config.OIDCConfig {
	return &config.OIDCConfig{
		ProviderURL:  fake.srv.URL,
		AuthURL:      fake.srv.URL + "/auth",
		TokenURL:     fake.srv.URL + "/token",
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost/callback",
		Scopes:       []string{"openid", "email", "profile"},
	}
}

// TestHandleCallback_AutoProvisionsFirstLogin exercises the full HandleCallback
// path (token exchange, ID token verification, claims parsing) against a fake
// OIDC IdP: a first-time login from an allowed domain must create a member
// user and join the configured default teams.
func TestHandleCallback_AutoProvisionsFirstLogin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := discardLoggerAP()

	fake := newFakeOIDCServer(t)
	provider, err := NewOIDCProvider(ctx, testOIDCConfig(fake), log)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	db := newAuthTestDB(t)
	team, err := core.CreateTeam(ctx, db, log, "eng", "Engineering")
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	authCfg := &config.AuthConfig{
		SessionDuration:       time.Hour,
		MaxConcurrentSessions: 1,
		AutoProvision: config.AutoProvisionConfig{
			Enabled:        true,
			AllowedDomains: []string{"example.com"},
			DefaultTeamIDs: []int{int(team.ID)},
		},
	}

	fake.registerCode("code-first-login", OIDCClaims{
		Email:         "newhire@example.com",
		EmailVerified: boolPtrAP(true),
		Name:          "New Hire",
	})

	user, session, err := provider.HandleCallback(ctx, db, log, authCfg, "code-first-login", "state")
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if session == nil {
		t.Fatal("expected a session to be created")
	}
	if user.Email != "newhire@example.com" {
		t.Errorf("email = %q, want newhire@example.com", user.Email)
	}
	if user.Role != models.UserRoleMember {
		t.Errorf("role = %q, want member (never admin for auto-provisioned users)", user.Role)
	}
	if user.Status != models.UserStatusActive {
		t.Errorf("status = %q, want active", user.Status)
	}
	if managed, err := db.IsUserManaged(ctx, user.ID); err != nil || managed {
		t.Errorf("auto-provisioned user must be unmanaged (managed=%v err=%v)", managed, err)
	}

	member, err := db.GetTeamMember(ctx, team.ID, user.ID)
	if err != nil {
		t.Fatalf("GetTeamMember: %v", err)
	}
	if member == nil {
		t.Fatal("expected user to be added to the default team")
	}
	if member.Role != models.TeamRoleMember {
		t.Errorf("team role = %q, want member", member.Role)
	}
}

// TestHandleCallback_AutoProvisionDisabledRejects verifies that with
// auto_provision.enabled=false, an unknown user is rejected exactly as today
// (ErrUnauthorizedUser), never auto-created.
func TestHandleCallback_AutoProvisionDisabledRejects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := discardLoggerAP()

	fake := newFakeOIDCServer(t)
	provider, err := NewOIDCProvider(ctx, testOIDCConfig(fake), log)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	db := newAuthTestDB(t)
	authCfg := &config.AuthConfig{
		SessionDuration:       time.Hour,
		MaxConcurrentSessions: 1,
		AutoProvision: config.AutoProvisionConfig{
			Enabled:        false,
			AllowedDomains: []string{"example.com"},
		},
	}

	fake.registerCode("code-disabled", OIDCClaims{
		Email:         "someone@example.com",
		EmailVerified: boolPtrAP(true),
		Name:          "Someone",
	})

	_, _, err = provider.HandleCallback(ctx, db, log, authCfg, "code-disabled", "state")
	if !errors.Is(err, ErrUnauthorizedUser) {
		t.Fatalf("HandleCallback error = %v, want ErrUnauthorizedUser", err)
	}
	if _, getErr := db.GetUserByEmail(ctx, "someone@example.com"); !models.IsNotFound(getErr) {
		t.Errorf("user should not have been created, GetUserByEmail err = %v", getErr)
	}
}

// TestHandleCallback_AutoProvisionUnlistedDomainRejects verifies a domain not
// present in allowed_domains still fails as "user not found", even with
// auto-provisioning enabled for other domains.
func TestHandleCallback_AutoProvisionUnlistedDomainRejects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := discardLoggerAP()

	fake := newFakeOIDCServer(t)
	provider, err := NewOIDCProvider(ctx, testOIDCConfig(fake), log)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	db := newAuthTestDB(t)
	authCfg := &config.AuthConfig{
		SessionDuration:       time.Hour,
		MaxConcurrentSessions: 1,
		AutoProvision: config.AutoProvisionConfig{
			Enabled:        true,
			AllowedDomains: []string{"example.com"},
		},
	}

	fake.registerCode("code-unlisted-domain", OIDCClaims{
		Email:         "outsider@not-allowed.com",
		EmailVerified: boolPtrAP(true),
		Name:          "Outsider",
	})

	_, _, err = provider.HandleCallback(ctx, db, log, authCfg, "code-unlisted-domain", "state")
	if !errors.Is(err, ErrUnauthorizedUser) {
		t.Fatalf("HandleCallback error = %v, want ErrUnauthorizedUser", err)
	}
	if _, getErr := db.GetUserByEmail(ctx, "outsider@not-allowed.com"); !models.IsNotFound(getErr) {
		t.Errorf("user should not have been created, GetUserByEmail err = %v", getErr)
	}
}

// TestHandleCallback_ConcurrentFirstLoginsBothSucceed drives the full
// HandleCallback path concurrently for two distinct authorization codes that
// both resolve to the same brand-new email, mirroring two browser tabs
// racing through first login. Both must succeed and resolve to the same user.
func TestHandleCallback_ConcurrentFirstLoginsBothSucceed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	log := discardLoggerAP()

	fake := newFakeOIDCServer(t)
	provider, err := NewOIDCProvider(ctx, testOIDCConfig(fake), log)
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}

	db := newAuthTestDB(t)
	authCfg := &config.AuthConfig{
		SessionDuration:       time.Hour,
		MaxConcurrentSessions: 2,
		AutoProvision: config.AutoProvisionConfig{
			Enabled:        true,
			AllowedDomains: []string{"example.com"},
		},
	}

	claims := OIDCClaims{Email: "racer@example.com", EmailVerified: boolPtrAP(true), Name: "Racer"}
	fake.registerCode("code-race-1", claims)
	fake.registerCode("code-race-2", claims)

	var wg sync.WaitGroup
	users := make([]*models.User, 2)
	errs := make([]error, 2)
	codes := []string{"code-race-1", "code-race-2"}

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			users[i], _, errs[i] = provider.HandleCallback(ctx, db, log, authCfg, codes[i], "state")
		}(i)
	}
	wg.Wait()

	for i := 0; i < 2; i++ {
		if errs[i] != nil {
			t.Fatalf("HandleCallback[%d]: %v", i, errs[i])
		}
	}
	if users[0].ID != users[1].ID {
		t.Errorf("concurrent first logins resolved to different users: %d vs %d", users[0].ID, users[1].ID)
	}
}

func discardLoggerAP() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
