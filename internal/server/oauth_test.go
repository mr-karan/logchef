package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/oauth"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	testIssuer      = "https://logchef.test"
	testAPIResource = testIssuer + "/api"
	testMCPResource = testIssuer + "/mcp"
	testWebClient   = "chatgpt"
	testWebRedirect = "https://chatgpt.example/connector/oauth/abc"
	testCLIRedirect = "http://127.0.0.1:49152/callback"
	testAllScopes   = "profile:read teams:read sources:read logs:read saved_queries:read collections:read alerts:read offline_access"
)

type oauthEnv struct {
	t       *testing.T
	db      *sqlite.DB
	srv     *Server
	oauth   *oauth.Server
	cfg     *config.Config
	user    *models.User
	session string
}

func testOAuthConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{PublicURL: testIssuer},
		Query: config.QueryConfig{
			DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 30, DefaultPreviewLimit: 100, MaxPreviewLimit: 1000,
			MaxResponseBytes: 1 << 20, MaxConcurrentPerUser: 3, MaxConcurrentGlobal: 30, MCPCallTimeoutSeconds: 30,
		},
		Auth: config.AuthConfig{
			APITokenSecret:     "0123456789abcdef0123456789abcdef",
			DefaultTokenExpiry: time.Hour,
			OAuth: config.OAuthConfig{
				Enabled: true,
				Clients: []config.OAuthClientConfig{{ID: testWebClient, Name: "ChatGPT", RedirectURIs: []string{testWebRedirect}}},
			},
		},
	}
}

func newServerForTest(t *testing.T, cfg *config.Config, db *sqlite.DB, oauthServer *oauth.Server) *Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	datasources := datasource.NewService(db, log)
	datasources.Register(datasource.NewClickHouseProvider(clickhouse.NewManager(log), log))
	datasources.Register(victorialogs.NewProvider(log))
	srv := New(ServerOptions{
		Config:      cfg,
		SQLite:      db,
		Datasources: datasources,
		OAuth:       oauthServer,
		FS:          http.FS(fstest.MapFS{"index.html": {Data: []byte(`<!doctype html><html><head><base href="/" /></head><body>spa</body></html>`)}}),
		Logger:      log,
	})
	t.Cleanup(func() {
		if err := srv.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return srv
}

func newOAuthEnv(t *testing.T) *oauthEnv {
	t.Helper()
	return newOAuthEnvWithConfig(t, testOAuthConfig(), models.UserRoleMember)
}

func newOAuthEnvWithConfig(t *testing.T, cfg *config.Config, role models.UserRole) *oauthEnv {
	t.Helper()
	db := newServerTestDB(t)
	oauthServer, err := oauth.New(cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("oauth.New: %v", err)
	}
	env := &oauthEnv{t: t, db: db, oauth: oauthServer, cfg: cfg, srv: newServerForTest(t, cfg, db, oauthServer)}
	env.user, env.session = env.newSessionUserWithRole("user@example.com", role)
	return env
}

func (e *oauthEnv) newSessionUser(email string) (user *models.User, sessionID string) {
	e.t.Helper()
	return e.newSessionUserWithRole(email, models.UserRoleMember)
}

func (e *oauthEnv) newSessionUserWithRole(email string, role models.UserRole) (user *models.User, sessionID string) {
	e.t.Helper()
	ctx := context.Background()
	user = &models.User{Email: email, FullName: "Customer A", Role: role, Status: models.UserStatusActive, AccountType: models.UserAccountTypeHuman}
	if err := e.db.CreateUser(ctx, user); err != nil {
		e.t.Fatalf("CreateUser: %v", err)
	}
	sessionID = "session-" + email
	if err := e.db.CreateSession(ctx, &models.Session{ID: models.SessionID(sessionID), UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		e.t.Fatalf("CreateSession: %v", err)
	}
	return user, sessionID
}

// testResponse is a fully read response, so tests never hold an open body.
type testResponse struct {
	StatusCode int
	Header     http.Header
	Body       *bytes.Reader
}

func testRequest(t *testing.T, app *fiber.App, req *http.Request) *testResponse {
	t.Helper()
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("app.Test %s %s: %v", req.Method, req.URL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading %s %s: %v", req.Method, req.URL, err)
	}
	return &testResponse{StatusCode: resp.StatusCode, Header: resp.Header, Body: bytes.NewReader(body)}
}

func (e *oauthEnv) do(req *http.Request) *testResponse {
	e.t.Helper()
	return testRequest(e.t, e.srv.app, req)
}

type pkcePair struct{ verifier, challenge string }

func newPKCE() pkcePair {
	b := make([]byte, 32)
	rand.Read(b)
	verifier := base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	return pkcePair{verifier: verifier, challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
}

func webAuthorizeParams(p pkcePair) url.Values {
	return url.Values{
		"client_id":             {testWebClient},
		"redirect_uri":          {testWebRedirect},
		"response_type":         {"code"},
		"scope":                 {testAllScopes},
		"state":                 {"state-123"},
		"resource":              {testMCPResource},
		"code_challenge":        {p.challenge},
		"code_challenge_method": {"S256"},
	}
}

func cliAuthorizeParams(p pkcePair) url.Values {
	v := webAuthorizeParams(p)
	v.Set("client_id", config.OAuthCLIClientID)
	v.Set("redirect_uri", testCLIRedirect)
	v.Set("resource", testAPIResource)
	return v
}

func (e *oauthEnv) authorize(params url.Values) *testResponse {
	return e.do(httptest.NewRequest(http.MethodGet, testIssuer+oauth.AuthorizePath+"?"+params.Encode(), http.NoBody))
}

// startRequest runs /oauth/authorize and returns the pending request ID.
func (e *oauthEnv) startRequest(params url.Values) string {
	e.t.Helper()
	resp := e.authorize(params)
	if resp.StatusCode != http.StatusFound {
		body, _ := io.ReadAll(resp.Body)
		e.t.Fatalf("authorize status %d: %s", resp.StatusCode, body)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		e.t.Fatal(err)
	}
	if loc.Scheme+"://"+loc.Host+loc.Path != testIssuer+oauth.ConsentPath {
		e.t.Fatalf("authorize redirected to %q, want the consent page", loc)
	}
	id := loc.Query().Get("request")
	if len(id) != 43 {
		e.t.Fatalf("request id %q is not 256 bits", id)
	}
	return id
}

func (e *oauthEnv) consentRequest(method, path, session, body string, headers map[string]string) *testResponse {
	req := httptest.NewRequest(method, testIssuer+path, strings.NewReader(body))
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return e.do(req)
}

var sameOriginJSON = map[string]string{"Origin": testIssuer, "Content-Type": "application/json"}

// decide posts a consent decision and returns the redirect URL.
func (e *oauthEnv) decide(id, session string, approve bool) *url.URL {
	e.t.Helper()
	body := `{"approve":false}`
	if approve {
		body = `{"approve":true}`
	}
	resp := e.consentRequest(http.MethodPost, "/api/v1/oauth/requests/"+id+"/decision", session, body, sameOriginJSON)
	var out struct {
		Data struct {
			RedirectURL string `json:"redirect_url"`
		} `json:"data"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK {
		e.t.Fatalf("decision status %d, message %q, err %v", resp.StatusCode, out.Message, err)
	}
	u, err := url.Parse(out.Data.RedirectURL)
	if err != nil {
		e.t.Fatal(err)
	}
	return u
}

// code runs authorize and approval and returns the authorization code.
func (e *oauthEnv) code(params url.Values) string {
	e.t.Helper()
	redirect := e.decide(e.startRequest(params), e.session, true)
	if redirect.Query().Get("code") == "" {
		e.t.Fatalf("approval redirect %q has no code", redirect)
	}
	return redirect.Query().Get("code")
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
}

func (e *oauthEnv) postToken(form url.Values) (int, tokenResponse) {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, testIssuer+oauth.TokenPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := e.do(req)
	var out tokenResponse
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func codeExchangeForm(params url.Values, code, verifier string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {params.Get("client_id")},
		"redirect_uri":  {params.Get("redirect_uri")},
		"code":          {code},
		"code_verifier": {verifier},
		"resource":      {params.Get("resource")},
	}
}

func refreshForm(params url.Values, refresh string) url.Values {
	return url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {params.Get("client_id")},
		"refresh_token": {refresh},
		"resource":      {params.Get("resource")},
	}
}

// tokens runs the full flow and returns the token response.
func (e *oauthEnv) tokens(params url.Values, p pkcePair) tokenResponse {
	e.t.Helper()
	status, out := e.postToken(codeExchangeForm(params, e.code(params), p.verifier))
	if status != http.StatusOK || out.AccessToken == "" {
		e.t.Fatalf("code exchange status %d error %q", status, out.Error)
	}
	return out
}

func (e *oauthEnv) apiGet(path, bearer string) *testResponse {
	req := httptest.NewRequest(http.MethodGet, testIssuer+path, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+bearer)
	return e.do(req)
}

func noRedirect(t *testing.T, resp *testResponse) {
	t.Helper()
	if resp.StatusCode != http.StatusBadRequest || resp.Header.Get("Location") != "" {
		t.Fatalf("status %d Location %q, want 400 without a redirect", resp.StatusCode, resp.Header.Get("Location"))
	}
}

func errorRedirect(t *testing.T, resp *testResponse, wantError string) {
	t.Helper()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status %d, want 302 error redirect", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(loc.String(), testWebRedirect) && !strings.HasPrefix(loc.String(), testCLIRedirect) {
		t.Fatalf("error redirect to %q", loc)
	}
	q := loc.Query()
	if q.Get("error") != wantError || q.Get("iss") != testIssuer || q.Get("state") != "state-123" {
		t.Fatalf("error redirect query = %v, want error=%s with iss and state", q, wantError)
	}
}

// T-OA-1 (authorization side): wrong client and unregistered redirect never
// redirect; plain or missing PKCE and wrong or missing resource are errors.
func TestOAuthAuthorizeRejects(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()

	t.Run("unknown client", func(t *testing.T) {
		v := webAuthorizeParams(p)
		v.Set("client_id", "evil")
		noRedirect(t, e.authorize(v))
	})
	t.Run("unregistered redirect", func(t *testing.T) {
		v := webAuthorizeParams(p)
		v.Set("redirect_uri", "https://attacker.example/cb")
		noRedirect(t, e.authorize(v))
	})
	t.Run("redirect prefix of registered", func(t *testing.T) {
		v := webAuthorizeParams(p)
		v.Set("redirect_uri", testWebRedirect+"/extra")
		noRedirect(t, e.authorize(v))
	})
	t.Run("repeated client_id", func(t *testing.T) {
		v := webAuthorizeParams(p)
		v.Add("client_id", testWebClient)
		noRedirect(t, e.authorize(v))
	})

	for _, tc := range []struct {
		name, field, value, wantError string
	}{
		{"plain PKCE", "code_challenge_method", "plain", "invalid_request"},
		{"missing PKCE method", "code_challenge_method", "", "invalid_request"},
		{"missing challenge", "code_challenge", "", "invalid_request"},
		{"short challenge", "code_challenge", "abc", "invalid_request"},
		{"missing resource", "resource", "", "invalid_target"},
		{"wrong resource", "resource", "https://other.example/mcp", "invalid_target"},
		{"web client asks for API resource", "resource", testAPIResource, "invalid_target"},
		{"token response type", "response_type", "token", "unsupported_response_type"},
		{"form_post response mode", "response_mode", "form_post", "invalid_request"},
		{"request object", "request", "eyJ.e30.", "request_not_supported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := webAuthorizeParams(p)
			if tc.value == "" {
				v.Del(tc.field)
			} else {
				v.Set(tc.field, tc.value)
			}
			errorRedirect(t, e.authorize(v), tc.wantError)
		})
	}
	t.Run("repeated resource", func(t *testing.T) {
		v := webAuthorizeParams(p)
		v.Add("resource", testMCPResource)
		errorRedirect(t, e.authorize(v), "invalid_request")
	})
	t.Run("missing state", func(t *testing.T) {
		v := webAuthorizeParams(p)
		v.Del("state")
		resp := e.authorize(v)
		loc, _ := url.Parse(resp.Header.Get("Location"))
		if resp.StatusCode != http.StatusFound || loc.Query().Get("error") != "invalid_request" || loc.Query().Get("iss") != testIssuer {
			t.Fatalf("status %d Location %q, want invalid_request with iss", resp.StatusCode, loc)
		}
	})
}

// T-OA-1 (token side): every binding failure is rejected, does not burn the
// code, and the correct exchange then succeeds exactly once. The replay
// revokes the grant (planner section 3.6).
func TestOAuthCodeExchangeBinding(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	params := webAuthorizeParams(p)
	code := e.code(params)

	for _, tc := range []struct {
		name   string
		mutate func(url.Values)
	}{
		{"wrong verifier", func(f url.Values) { f.Set("code_verifier", newPKCE().verifier) }},
		{"missing verifier", func(f url.Values) { f.Del("code_verifier") }},
		{"wrong client", func(f url.Values) { f.Set("client_id", config.OAuthCLIClientID); f.Set("resource", testAPIResource) }},
		{"unknown client", func(f url.Values) { f.Set("client_id", "evil") }},
		{"wrong redirect", func(f url.Values) { f.Set("redirect_uri", "https://attacker.example/cb") }},
		{"wrong resource", func(f url.Values) { f.Set("resource", testAPIResource) }},
		{"missing resource", func(f url.Values) { f.Del("resource") }},
		{"client secret", func(f url.Values) { f.Set("client_secret", "x") }},
		{"query parameters", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := codeExchangeForm(params, code, p.verifier)
			if tc.mutate == nil {
				req := httptest.NewRequest(http.MethodPost, testIssuer+oauth.TokenPath+"?code="+url.QueryEscape(code), strings.NewReader(form.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				if resp := e.do(req); resp.StatusCode == http.StatusOK {
					t.Fatal("query parameters accepted")
				}
				return
			}
			tc.mutate(form)
			if status, _ := e.postToken(form); status == http.StatusOK {
				t.Fatal("bad exchange accepted")
			}
		})
	}

	status, first := e.postToken(codeExchangeForm(params, code, p.verifier))
	if status != http.StatusOK || first.AccessToken == "" || first.RefreshToken == "" {
		t.Fatalf("correct exchange status %d error %q", status, first.Error)
	}
	if resp := e.apiGet("/api/v1/me", first.AccessToken); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("MCP token at /api status %d, want 401", resp.StatusCode)
	}
	status, replay := e.postToken(codeExchangeForm(params, code, p.verifier))
	if status != http.StatusBadRequest || replay.Error != "invalid_grant" {
		t.Fatalf("replayed code status %d error %q, want 400 invalid_grant", status, replay.Error)
	}
	if status, out := e.postToken(refreshForm(params, first.RefreshToken)); status == http.StatusOK {
		t.Fatalf("refresh after code replay succeeded (%q); the grant was not revoked", out.Error)
	}
	if _, err := e.oauth.AuthenticateAccessToken(context.Background(), first.AccessToken, oauth.ResourceMCP); err == nil {
		t.Fatal("access token still valid after code replay")
	}
}

// T-OA-2: iss on success and error redirects; metadata issuer equals the PRM
// authorization server byte for byte.
func TestOAuthIssuerIdentification(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()

	redirect := e.decide(e.startRequest(webAuthorizeParams(p)), e.session, true)
	if redirect.Query().Get("iss") != testIssuer || redirect.Query().Get("state") != "state-123" {
		t.Fatalf("success redirect %q lacks exact iss or state", redirect)
	}
	if !strings.HasPrefix(redirect.String(), testWebRedirect+"?") {
		t.Fatalf("success redirect %q is not the registered URI", redirect)
	}
	v := webAuthorizeParams(p)
	v.Set("scope", "logs:write")
	errorRedirect(t, e.authorize(v), "invalid_scope")

	// ZITADEL itself rejects prompt=none combined with another prompt. Its
	// error redirect must also carry iss (adapter A2).
	v = webAuthorizeParams(p)
	v.Set("prompt", "none login")
	errorRedirect(t, e.authorize(v), "invalid_request")

	var as oauth.AuthorizationServerMetadata
	resp := e.do(httptest.NewRequest(http.MethodGet, testIssuer+oauth.AuthorizationServerMetadataPath, http.NoBody))
	if err := json.NewDecoder(resp.Body).Decode(&as); err != nil {
		t.Fatal(err)
	}
	var prm oauth.ProtectedResourceMetadata
	resp = e.do(httptest.NewRequest(http.MethodGet, testIssuer+oauth.ProtectedResourceMetadataPath, http.NoBody))
	if err := json.NewDecoder(resp.Body).Decode(&prm); err != nil {
		t.Fatal(err)
	}
	if as.Issuer != testIssuer || len(prm.AuthorizationServers) != 1 || prm.AuthorizationServers[0] != as.Issuer {
		t.Fatalf("issuer %q, PRM authorization_servers %q", as.Issuer, prm.AuthorizationServers)
	}
	if !as.AuthorizationResponseIssParameterSupported || prm.Resource != testMCPResource {
		t.Fatalf("metadata %+v PRM %+v", as, prm)
	}
	if slices.Contains(prm.ScopesSupported, "offline_access") || len(prm.ScopesSupported) != 7 {
		t.Fatalf("PRM scopes %v, want the seven read scopes only", prm.ScopesSupported)
	}
}

// T-OA-3: write, wildcard, admin and OIDC scopes get invalid_scope before
// consent.
func TestOAuthScopePolicy(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	for _, scope := range []string{"logs:write", "*", "tokens:read", "users:read", "dashboards:read", "openid", "email", "profile", "offline_access", "", "logs:read openid"} {
		t.Run(scope, func(t *testing.T) {
			v := webAuthorizeParams(p)
			v.Set("scope", scope)
			errorRedirect(t, e.authorize(v), "invalid_scope")
		})
	}
}

// T-OA-4: the native client accepts 127.0.0.1 and [::1] on any port and
// nothing else.
func TestOAuthNativeRedirectPolicy(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	for _, redirect := range []string{"http://127.0.0.1:49152/callback", "http://127.0.0.1:1/callback", "http://[::1]:65535/callback", "http://127.0.0.1/callback", "http://localhost:49152/callback", "http://localhost:8787/callback"} {
		t.Run("accept "+redirect, func(t *testing.T) {
			v := cliAuthorizeParams(p)
			v.Set("redirect_uri", redirect)
			e.startRequest(v)
		})
	}
	for _, redirect := range []string{
		"https://localhost:49152/callback",
		"http://localhost:49152/callback/3f2a9c",
		"https://127.0.0.1:49152/callback",
		"http://127.0.0.1:49152/callback/extra",
		"http://127.0.0.1:49152/other",
		"http://127.0.0.1:49152/callback?x=1",
		"http://127.0.0.1:49152/callback?",
		"http://127.0.0.1:49152/callback#frag",
		"http://user@127.0.0.1:49152/callback",
		"http://127.0.0.2:49152/callback",
		"http://[::1]x/callback",
		"http://127.0.0.1:0/callback",
		"http://127.0.0.1:99999/callback",
		"http://127.0.0.1.attacker.example/callback",
		testWebRedirect,
	} {
		t.Run("reject "+redirect, func(t *testing.T) {
			v := cliAuthorizeParams(p)
			v.Set("redirect_uri", redirect)
			noRedirect(t, e.authorize(v))
		})
	}
	t.Run("CLI flow completes for the API resource", func(t *testing.T) {
		params := cliAuthorizeParams(p)
		tokens := e.tokens(params, p)
		if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusOK {
			t.Fatalf("/me with CLI token status %d", resp.StatusCode)
		}
	})
}

// T-OA-5: consent is session-only, same-origin, JSON, and single-decision.
func TestOAuthConsentCSRF(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	id := e.startRequest(webAuthorizeParams(p))
	path := "/api/v1/oauth/requests/" + id + "/decision"
	approve := `{"approve":true}`

	pat, err := core.CreateAPIToken(context.Background(), e.db, slog.New(slog.NewTextHandler(io.Discard, nil)), &e.cfg.Auth, e.user.ID, "pat", nil, []models.TokenScope{models.TokenScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	bearer := map[string]string{"Origin": testIssuer, "Content-Type": "application/json", "Authorization": "Bearer " + pat.Token}

	for _, tc := range []struct {
		name    string
		session string
		body    string
		headers map[string]string
		want    int
	}{
		{"no session", "", approve, sameOriginJSON, http.StatusUnauthorized},
		{"bearer PAT", "", approve, bearer, http.StatusUnauthorized},
		{"bearer PAT with session", e.session, approve, bearer, http.StatusUnauthorized},
		{"foreign origin", e.session, approve, map[string]string{"Origin": "https://attacker.example", "Content-Type": "application/json"}, http.StatusForbidden},
		{"origin with other port", e.session, approve, map[string]string{"Origin": testIssuer + ":8443", "Content-Type": "application/json"}, http.StatusForbidden},
		{"missing origin", e.session, approve, map[string]string{"Content-Type": "application/json"}, http.StatusForbidden},
		{"form content type", e.session, "approve=true", map[string]string{"Origin": testIssuer, "Content-Type": "application/x-www-form-urlencoded"}, http.StatusUnsupportedMediaType},
		{"text content type", e.session, approve, map[string]string{"Origin": testIssuer, "Content-Type": "text/plain"}, http.StatusUnsupportedMediaType},
		{"missing approve", e.session, `{}`, sameOriginJSON, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := e.consentRequest(http.MethodPost, path, tc.session, tc.body, tc.headers)
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}

	if resp := e.consentRequest(http.MethodGet, "/api/v1/oauth/requests/"+id, "", "", map[string]string{"Authorization": "Bearer " + pat.Token}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET with bearer status %d, want 401", resp.StatusCode)
	}
	resp := e.consentRequest(http.MethodGet, "/api/v1/oauth/requests/"+id, e.session, "", nil)
	var got struct {
		Data struct {
			Client       oauth.ClientInfo  `json:"client"`
			ResourceKind string            `json:"resource_kind"`
			Instance     string            `json:"instance"`
			RedirectURI  string            `json:"redirect_uri"`
			Scopes       []oauth.ScopeInfo `json:"scopes"`
			User         struct {
				Email string `json:"email"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status %d err %v", resp.StatusCode, err)
	}
	if got.Data.Client.Name != "ChatGPT" || got.Data.ResourceKind != "mcp" || got.Data.Instance != testIssuer ||
		got.Data.RedirectURI != testWebRedirect || len(got.Data.Scopes) != 7 || got.Data.User.Email != e.user.Email {
		t.Fatalf("consent request = %+v", got.Data)
	}

	e.decide(id, e.session, true)
	if resp := e.consentRequest(http.MethodPost, path, e.session, approve, sameOriginJSON); resp.StatusCode != http.StatusConflict {
		t.Fatalf("second decision status %d, want 409", resp.StatusCode)
	}
	if resp := e.consentRequest(http.MethodGet, "/api/v1/oauth/requests/"+id, e.session, "", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET after decision status %d, want 404", resp.StatusCode)
	}
	if resp := e.consentRequest(http.MethodGet, "/api/v1/oauth/requests/unknown", e.session, "", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("GET unknown status %d, want 404", resp.StatusCode)
	}
}

// The first decision binds the user: a second user cannot redeem or change it.
func TestOAuthDecisionBindsFirstUser(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	params := webAuthorizeParams(p)
	id := e.startRequest(params)
	other, otherSession := e.newSessionUser("other@example.com")
	redirect := e.decide(id, otherSession, true)
	status, tokens := e.postToken(codeExchangeForm(params, redirect.Query().Get("code"), p.verifier))
	if status != http.StatusOK {
		t.Fatalf("exchange status %d", status)
	}
	token, err := e.oauth.AuthenticateAccessToken(context.Background(), tokens.AccessToken, oauth.ResourceMCP)
	if err != nil || token.Principal.User.ID != other.ID {
		t.Fatalf("token principal = %+v, %v; want the approving user", token, err)
	}
	if resp := e.consentRequest(http.MethodPost, "/api/v1/oauth/requests/"+id+"/decision", e.session, `{"approve":false}`, sameOriginJSON); resp.StatusCode != http.StatusConflict {
		t.Fatalf("second user's decision status %d, want 409", resp.StatusCode)
	}
}

// T-OA-6: deny redirects with access_denied, state and iss, and no code.
func TestOAuthDeny(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	redirect := e.decide(e.startRequest(webAuthorizeParams(newPKCE())), e.session, false)
	q := redirect.Query()
	if q.Get("error") != "access_denied" || q.Get("iss") != testIssuer || q.Get("state") != "state-123" || q.Has("code") {
		t.Fatalf("deny redirect %q", redirect)
	}
	if apps := e.connectedApps(e.session); len(apps) != 0 {
		t.Fatalf("deny created grants %+v", apps)
	}
}

// T-OA-8 and T-OA-11: audiences are separate, and an ID token is never a
// bearer token.
func TestOAuthAudienceSeparation(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	ctx := context.Background()
	webPKCE := newPKCE()
	mcp := e.tokens(webAuthorizeParams(webPKCE), webPKCE)
	if resp := e.apiGet("/api/v1/me", mcp.AccessToken); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("MCP token at /api/v1/me status %d, want 401", resp.StatusCode)
	}
	if _, err := e.oauth.AuthenticateAccessToken(ctx, mcp.AccessToken, oauth.ResourceMCP); err != nil {
		t.Fatalf("MCP token rejected for /mcp: %v", err)
	}

	cliPKCE := newPKCE()
	api := e.tokens(cliAuthorizeParams(cliPKCE), cliPKCE)
	if resp := e.apiGet("/api/v1/me", api.AccessToken); resp.StatusCode != http.StatusOK {
		t.Fatalf("API token at /api/v1/me status %d", resp.StatusCode)
	}
	if _, err := e.oauth.AuthenticateAccessToken(ctx, api.AccessToken, oauth.ResourceMCP); err == nil {
		t.Fatal("API-audience token accepted for /mcp")
	}
	if api.IDToken == "" {
		t.Fatal("expected ZITADEL to emit an ID token")
	}
	resp := e.apiGet("/api/v1/me", api.IDToken)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "invalid_token") {
		t.Fatalf("ID token at /api status %d WWW-Authenticate %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	for _, resource := range []oauth.Resource{oauth.ResourceAPI, oauth.ResourceMCP} {
		if _, err := e.oauth.AuthenticateAccessToken(ctx, api.IDToken, resource); err == nil {
			t.Fatalf("ID token accepted for resource %d", resource)
		}
		if _, err := e.oauth.AuthenticateAccessToken(ctx, api.RefreshToken, resource); err == nil {
			t.Fatalf("refresh token accepted for resource %d", resource)
		}
	}
}

// T-OA-8 (MCP side) is asserted through AuthenticateAccessToken in
// TestOAuthAudienceSeparation and TestOAuthCodeExchangeBinding; /mcp itself
// is mounted in phase 4.

func (e *oauthEnv) connectedApps(session string) []oauth.ConnectedApp {
	e.t.Helper()
	resp := e.consentRequest(http.MethodGet, "/api/v1/me/connected-apps", session, "", nil)
	var out struct {
		Data []oauth.ConnectedApp `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK {
		e.t.Fatalf("connected apps status %d err %v", resp.StatusCode, err)
	}
	return out.Data
}

// T-OA-9: revoking a grant stops the next access-token use and the next
// refresh, through Connected apps and through /oauth/revoke.
func TestOAuthRevocation(t *testing.T) {
	t.Parallel()
	t.Run("connected apps", func(t *testing.T) {
		t.Parallel()
		e := newOAuthEnv(t)
		p := newPKCE()
		params := cliAuthorizeParams(p)
		tokens := e.tokens(params, p)
		apps := e.connectedApps(e.session)
		if len(apps) != 1 || apps[0].Client.ID != config.OAuthCLIClientID || apps[0].ResourceKind != "api" || !apps[0].OfflineAccess {
			t.Fatalf("connected apps = %+v", apps)
		}
		path := "/api/v1/me/connected-apps/" + strconv.Itoa(int(apps[0].ID))
		if resp := e.consentRequest(http.MethodDelete, path, e.session, "", map[string]string{"Origin": "https://attacker.example"}); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("cross-origin revoke status %d, want 403", resp.StatusCode)
		}
		if resp := e.consentRequest(http.MethodGet, "/api/v1/me/connected-apps", "", "", map[string]string{"Authorization": "Bearer " + tokens.AccessToken}); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("OAuth token listing connected apps status %d, want 401", resp.StatusCode)
		}
		_, otherSession := e.newSessionUser("other@example.com")
		if resp := e.consentRequest(http.MethodDelete, path, otherSession, "", map[string]string{"Origin": testIssuer}); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("other user's revoke status %d, want 404", resp.StatusCode)
		}
		if resp := e.consentRequest(http.MethodDelete, path, e.session, "", map[string]string{"Origin": testIssuer}); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("revoke status %d, want 204", resp.StatusCode)
		}
		if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("/me after revoke status %d, want 401", resp.StatusCode)
		}
		if status, out := e.postToken(refreshForm(params, tokens.RefreshToken)); status != http.StatusBadRequest || out.Error != "invalid_grant" {
			t.Fatalf("refresh after revoke status %d error %q", status, out.Error)
		}
		if apps := e.connectedApps(e.session); len(apps) != 0 {
			t.Fatalf("revoked grant still listed: %+v", apps)
		}
	})
	for _, hint := range []string{"refresh_token", "access_token", ""} {
		t.Run("revocation endpoint "+hint, func(t *testing.T) {
			t.Parallel()
			e := newOAuthEnv(t)
			p := newPKCE()
			params := cliAuthorizeParams(p)
			tokens := e.tokens(params, p)
			token := tokens.RefreshToken
			if hint == "access_token" {
				token = tokens.AccessToken
			}
			form := url.Values{"client_id": {config.OAuthCLIClientID}, "token": {token}}
			if hint != "" {
				form.Set("token_type_hint", hint)
			}
			req := httptest.NewRequest(http.MethodPost, testIssuer+oauth.RevokePath, strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if resp := e.do(req); resp.StatusCode != http.StatusOK {
				t.Fatalf("revoke status %d", resp.StatusCode)
			}
			if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("/me after revoke status %d", resp.StatusCode)
			}
			if status, _ := e.postToken(refreshForm(params, tokens.RefreshToken)); status != http.StatusBadRequest {
				t.Fatalf("refresh after revoke status %d", status)
			}
		})
	}
	t.Run("other client cannot revoke", func(t *testing.T) {
		t.Parallel()
		e := newOAuthEnv(t)
		p := newPKCE()
		tokens := e.tokens(cliAuthorizeParams(p), p)
		form := url.Values{"client_id": {testWebClient}, "token": {tokens.RefreshToken}}
		req := httptest.NewRequest(http.MethodPost, testIssuer+oauth.RevokePath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		e.do(req)
		if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusOK {
			t.Fatalf("another client revoked the grant: /me status %d", resp.StatusCode)
		}
	})
}

// T-OA-10: deactivating the user makes the next call fail.
func TestOAuthDeactivatedUser(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	tokens := e.tokens(cliAuthorizeParams(p), p)
	if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusOK {
		t.Fatalf("/me status %d", resp.StatusCode)
	}
	if err := core.UpdateUser(context.Background(), e.db, slog.New(slog.NewTextHandler(io.Discard, nil)), e.user.ID, models.User{Status: models.UserStatusInactive}); err != nil {
		t.Fatal(err)
	}
	if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/me after deactivation status %d, want 401", resp.StatusCode)
	}
}

// T-OA-12: metadata lists only implemented grants and endpoints, and the
// ZITADEL routes Logchef does not use are unreachable.
func TestOAuthMetadataAdvertisesOnlyImplemented(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	resp := e.do(httptest.NewRequest(http.MethodGet, testIssuer+oauth.AuthorizationServerMetadataPath, http.NoBody))
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"registration_endpoint", "client_id_metadata_document_supported", "jwks_uri", "userinfo_endpoint", "device_authorization_endpoint", "introspection_endpoint", "end_session_endpoint", "id_token_signing_alg_values_supported", "subject_types_supported", "claims_supported"} {
		if _, ok := raw[field]; ok {
			t.Errorf("metadata advertises %s", field)
		}
	}
	md := e.oauth.Metadata()
	if !slices.Equal(md.GrantTypesSupported, []string{"authorization_code", "refresh_token"}) ||
		!slices.Equal(md.CodeChallengeMethodsSupported, []string{"S256"}) ||
		!slices.Equal(md.TokenEndpointAuthMethodsSupported, []string{"none"}) ||
		!slices.Equal(md.ResponseTypesSupported, []string{"code"}) ||
		slices.Contains(md.ScopesSupported, "openid") || !slices.Contains(md.ScopesSupported, "offline_access") {
		t.Fatalf("metadata = %+v", md)
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/.well-known/openid-configuration"},
		{http.MethodPost, "/oauth/introspect"},
		{http.MethodGet, "/userinfo"},
		{http.MethodGet, "/keys"},
		{http.MethodGet, "/end_session"},
		{http.MethodPost, "/device_authorization"},
		{http.MethodGet, "/oauth/authorize/callback?id=x"},
		{http.MethodGet, oauth.TokenPath},
	} {
		resp := e.do(httptest.NewRequest(tc.method, testIssuer+tc.path, http.NoBody))
		if strings.Contains(resp.Header.Get("Content-Type"), "application/json") && resp.StatusCode == http.StatusOK {
			t.Errorf("%s %s answered JSON 200", tc.method, tc.path)
		}
		if resp.StatusCode == http.StatusFound {
			t.Errorf("%s %s redirected", tc.method, tc.path)
		}
	}
	for _, grant := range []string{"client_credentials", "urn:ietf:params:oauth:grant-type:jwt-bearer", "urn:ietf:params:oauth:grant-type:token-exchange", "urn:ietf:params:oauth:grant-type:device_code", "password"} {
		status, out := e.postToken(url.Values{"grant_type": {grant}, "client_id": {config.OAuthCLIClientID}, "resource": {testAPIResource}})
		if status != http.StatusBadRequest || out.Error != "unsupported_grant_type" {
			t.Errorf("grant %s status %d error %q", grant, status, out.Error)
		}
	}
}

// T-OA-14: refresh rotates, rejects a different resource and broader scopes,
// accepts narrower scopes and enforces them on the new access token. A
// replayed refresh token revokes the grant.
func TestOAuthRefresh(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	params := cliAuthorizeParams(p)
	params.Set("scope", "logs:read profile:read offline_access")
	tokens := e.tokens(params, p)

	bad := refreshForm(params, tokens.RefreshToken)
	bad.Set("resource", testMCPResource)
	if status, out := e.postToken(bad); status == http.StatusOK || out.Error != "invalid_target" {
		t.Fatalf("different resource status %d error %q", status, out.Error)
	}
	bad = refreshForm(params, tokens.RefreshToken)
	bad.Del("resource")
	if status, _ := e.postToken(bad); status == http.StatusOK {
		t.Fatal("refresh without resource accepted")
	}
	bad = refreshForm(params, tokens.RefreshToken)
	bad.Set("scope", "logs:read profile:read sources:read")
	if status, out := e.postToken(bad); status == http.StatusOK || out.Error != "invalid_scope" {
		t.Fatalf("broader scope status %d error %q", status, out.Error)
	}

	narrow := refreshForm(params, tokens.RefreshToken)
	narrow.Set("scope", "profile:read")
	status, narrowed := e.postToken(narrow)
	if status != http.StatusOK || narrowed.RefreshToken == "" || narrowed.RefreshToken == tokens.RefreshToken {
		t.Fatalf("narrow refresh status %d error %q", status, narrowed.Error)
	}
	if narrowed.Scope != "profile:read" {
		t.Fatalf("narrowed scope response %q", narrowed.Scope)
	}
	resp := e.apiGet("/api/v1/me", narrowed.AccessToken)
	var me struct {
		Data struct {
			AuthMethod string `json:"auth_method"`
			Auth       struct {
				Method    string              `json:"method"`
				Scopes    []models.TokenScope `json:"scopes"`
				ExpiresAt time.Time           `json:"expires_at"`
				ClientID  string              `json:"client_id"`
			} `json:"auth"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("/me status %d err %v", resp.StatusCode, err)
	}
	if me.Data.AuthMethod != "oauth" || me.Data.Auth.Method != "oauth" || me.Data.Auth.ClientID != config.OAuthCLIClientID ||
		!slices.Equal(me.Data.Auth.Scopes, []models.TokenScope{models.TokenScopeProfileRead}) ||
		me.Data.Auth.ExpiresAt.Before(time.Now()) || me.Data.Auth.ExpiresAt.After(time.Now().Add(11*time.Minute)) {
		t.Fatalf("/me auth = %+v", me.Data)
	}
	resp = e.apiGet("/api/v1/me/query-history", narrowed.AccessToken)
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(resp.Header.Get("WWW-Authenticate"), "insufficient_scope") {
		t.Fatalf("narrowed token on logs:read route status %d WWW-Authenticate %q", resp.StatusCode, resp.Header.Get("WWW-Authenticate"))
	}
	if resp := e.apiGet("/api/v1/me/query-history", tokens.AccessToken); resp.StatusCode != http.StatusOK {
		t.Fatalf("original token on logs:read route status %d", resp.StatusCode)
	}

	if status, out := e.postToken(refreshForm(params, tokens.RefreshToken)); status != http.StatusBadRequest || out.Error != "invalid_grant" {
		t.Fatalf("replayed refresh status %d error %q", status, out.Error)
	}
	if status, _ := e.postToken(refreshForm(params, narrowed.RefreshToken)); status == http.StatusOK {
		t.Fatal("refresh replay did not revoke the grant")
	}
	if resp := e.apiGet("/api/v1/me", narrowed.AccessToken); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("access token after refresh replay status %d", resp.StatusCode)
	}
}

// A refresh token is issued only with offline_access, and an OAuth token can
// reach only read routes.
func TestOAuthOfflineAccessAndWriteRoutes(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	params := cliAuthorizeParams(p)
	params.Set("scope", "profile:read logs:read")
	tokens := e.tokens(params, p)
	if tokens.RefreshToken != "" {
		t.Fatal("refresh token issued without offline_access")
	}
	for _, path := range []string{"/api/v1/me/tokens", "/api/v1/admin/users", "/api/v1/dashboards"} {
		if resp := e.apiGet(path, tokens.AccessToken); resp.StatusCode != http.StatusForbidden {
			t.Errorf("GET %s status %d, want 403", path, resp.StatusCode)
		}
	}
	req := httptest.NewRequest(http.MethodPost, testIssuer+"/api/v1/me/tokens", strings.NewReader(`{"name":"x"}`))
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	if resp := e.do(req); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("OAuth token minted a PAT: status %d", resp.StatusCode)
	}
}

// /me reports the auth details of a PAT caller, and PATs still work with
// OAuth enabled.
func TestMeAuthForAPIToken(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	pat, err := core.CreateAPIToken(context.Background(), e.db, slog.New(slog.NewTextHandler(io.Discard, nil)), &e.cfg.Auth, e.user.ID, "pat", nil, []models.TokenScope{models.TokenScopeProfileRead})
	if err != nil {
		t.Fatal(err)
	}
	resp := e.apiGet("/api/v1/me", pat.Token)
	var me struct {
		Data struct {
			Auth map[string]any `json:"auth"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&me); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("/me status %d err %v", resp.StatusCode, err)
	}
	if me.Data.Auth["method"] != "token" || me.Data.Auth["expires_at"] != nil || me.Data.Auth["client_id"] != nil {
		t.Fatalf("/me auth = %v", me.Data.Auth)
	}
	resp = e.consentRequest(http.MethodGet, "/api/v1/me", e.session, "", nil)
	var sess struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatal(err)
	}
	if _, ok := sess.Data["auth"]; ok {
		t.Fatal("session /me has an auth object")
	}
}

// With OAuth disabled no OAuth route or metadata exists and bearer handling
// is unchanged.
func TestOAuthDisabled(t *testing.T) {
	t.Parallel()
	db := newServerTestDB(t)
	cfg := testOAuthConfig()
	cfg.Auth.OAuth.Enabled = false
	srv := newServerForTest(t, cfg, db, nil)
	do := func(method, path string) *testResponse {
		return testRequest(t, srv.app, httptest.NewRequest(method, testIssuer+path, http.NoBody))
	}
	for _, path := range []string{oauth.AuthorizationServerMetadataPath, oauth.ProtectedResourceMetadataPath, "/.well-known/oauth-protected-resource", oauth.AuthorizePath + "?client_id=logchef-cli"} {
		resp := do(http.MethodGet, path)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode == http.StatusFound || strings.Contains(string(body), "issuer") || strings.Contains(string(body), "authorization_servers") {
			t.Errorf("GET %s status %d body %q", path, resp.StatusCode, body)
		}
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, oauth.TokenPath},
		{http.MethodPost, oauth.RevokePath},
		{http.MethodGet, "/api/v1/oauth/requests/x"},
		{http.MethodPost, "/api/v1/oauth/requests/x/decision"},
		{http.MethodGet, "/api/v1/me/connected-apps"},
	} {
		if resp := do(tc.method, tc.path); resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s %s status %d, want 404", tc.method, tc.path, resp.StatusCode)
		}
	}
	resp := do(http.MethodGet, "/api/v1/meta")
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "oauth_issuer") {
		t.Fatalf("meta advertises OAuth while disabled: %s", body)
	}
}

// The consent page is an SPA route served by the index handler.
func TestOAuthConsentPageIsSPA(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	resp := e.do(httptest.NewRequest(http.MethodGet, testIssuer+oauth.ConsentPath+"?request=abc", http.NoBody))
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "spa") {
		t.Fatalf("consent page status %d body %q", resp.StatusCode, body)
	}
}

func TestMetaAdvertisesOAuthIssuer(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	resp := e.do(httptest.NewRequest(http.MethodGet, testIssuer+"/api/v1/meta", http.NoBody))
	var meta struct {
		Data struct {
			OAuthIssuer string `json:"oauth_issuer"`
			Version     string `json:"version"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		t.Fatal(err)
	}
	if meta.Data.OAuthIssuer != testIssuer {
		t.Fatalf("oauth_issuer = %q", meta.Data.OAuthIssuer)
	}
}

// Review 3, R1: an OAuth token never reaches an admin route, even for an admin
// user holding all seven read scopes. Sessions and PATs are unchanged.
func TestOAuthAdminRoutesDeniedForAdminUser(t *testing.T) {
	t.Parallel()
	e := newOAuthEnvWithConfig(t, testOAuthConfig(), models.UserRoleAdmin)
	p := newPKCE()
	tokens := e.tokens(cliAuthorizeParams(p), p)

	param := regexp.MustCompile(`:[A-Za-z]+`)
	checked := 0
	for _, route := range e.srv.app.GetRoutes(true) {
		if !strings.HasPrefix(route.Path, "/api/v1/admin/") || route.Method == http.MethodHead {
			continue
		}
		path := param.ReplaceAllString(route.Path, "1")
		req := httptest.NewRequest(route.Method, testIssuer+path, strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		if resp := e.do(req); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s with an admin's OAuth token: status %d, want 403", route.Method, path, resp.StatusCode)
		}
		checked++
	}
	if checked < 30 {
		t.Fatalf("checked only %d admin routes", checked)
	}

	pat, err := core.CreateAPIToken(context.Background(), e.db, slog.New(slog.NewTextHandler(io.Discard, nil)), &e.cfg.Auth, e.user.ID, "pat", nil, []models.TokenScope{models.TokenScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/admin/teams", "/api/v1/admin/saved-queries", "/api/v1/admin/query-stats"} {
		if resp := e.apiGet(path, tokens.AccessToken); resp.StatusCode != http.StatusForbidden {
			t.Errorf("OAuth GET %s status %d, want 403", path, resp.StatusCode)
		}
		if resp := e.apiGet(path, pat.Token); resp.StatusCode != http.StatusOK {
			t.Errorf("admin PAT GET %s status %d, want 200", path, resp.StatusCode)
		}
		if resp := e.consentRequest(http.MethodGet, path, e.session, "", nil); resp.StatusCode != http.StatusOK {
			t.Errorf("admin session GET %s status %d, want 200", path, resp.StatusCode)
		}
	}
}

// Review 3, R2: a registered callback with its own query, including an iss,
// gets exactly this server's iss on success and on a ZITADEL-generated error.
func TestOAuthIssuerOnQueryBearingCallback(t *testing.T) {
	t.Parallel()
	const callback = "https://chatgpt.example/cb?z=1&iss=https%3A%2F%2Fevil.example"
	cfg := testOAuthConfig()
	cfg.Auth.OAuth.Clients[0].RedirectURIs = []string{callback}
	e := newOAuthEnvWithConfig(t, cfg, models.UserRoleMember)
	p := newPKCE()
	params := webAuthorizeParams(p)
	params.Set("redirect_uri", callback)

	check := func(name string, q url.Values) {
		t.Helper()
		if !slices.Equal(q["iss"], []string{testIssuer}) || q.Get("z") != "1" || q.Get("state") != "state-123" {
			t.Fatalf("%s redirect query %v, want one iss=%s and z=1", name, q, testIssuer)
		}
	}
	redirect := e.decide(e.startRequest(params), e.session, true)
	check("success", redirect.Query())
	if status, _ := e.postToken(codeExchangeForm(params, redirect.Query().Get("code"), p.verifier)); status != http.StatusOK {
		t.Fatalf("exchange with query-bearing callback status %d", status)
	}

	v := url.Values{}
	maps.Copy(v, params)
	v.Set("prompt", "none login")
	resp := e.authorize(v)
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || resp.StatusCode != http.StatusFound {
		t.Fatalf("library error status %d Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if loc.Query().Get("error") != "invalid_request" {
		t.Fatalf("library error redirect %q", loc)
	}
	check("library error", loc.Query())

	v.Del("prompt")
	v.Set("scope", "logs:write")
	resp = e.authorize(v)
	loc, _ = url.Parse(resp.Header.Get("Location"))
	check("boundary error", loc.Query())
}

// malformedAudienceJWT is an unsigned JWT whose aud array holds non-strings,
// the input that panics ZITADEL's claim decoder (dependency audit F1).
func malformedAudienceJWT() string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256","kid":"x"}`)) + "." + enc([]byte(`{"iss":"https://logchef.test","sub":"1","aud":[1,{"a":2}],"exp":4102444800,"iat":1}`)) + "." + enc([]byte("sig"))
}

// Review 3, R3: the authorize boundary refuses OIDC hint parameters, and the
// revocation endpoint never hands a foreign token to ZITADEL's JWT parser.
// Neither endpoint answers 500 to a malformed audience.
func TestOAuthMalformedJWTInputs(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	for _, param := range []string{"id_token_hint", "claims", "registration"} {
		v := webAuthorizeParams(p)
		v.Set(param, malformedAudienceJWT())
		errorRedirect(t, e.authorize(v), "invalid_request")
	}

	revoke := func(form url.Values) *testResponse {
		req := httptest.NewRequest(http.MethodPost, testIssuer+oauth.RevokePath, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return e.do(req)
	}
	for _, hint := range []string{"", "access_token", "refresh_token"} {
		for _, token := range []string{malformedAudienceJWT(), "not-a-token", "a.b.c.d.e", ""} {
			form := url.Values{"client_id": {config.OAuthCLIClientID}, "token": {token}}
			if hint != "" {
				form.Set("token_type_hint", hint)
			}
			if resp := revoke(form); resp.StatusCode != http.StatusOK {
				t.Errorf("revoke %q hint %q: status %d, want 200", token, hint, resp.StatusCode)
			}
		}
	}
	if resp := revoke(url.Values{"client_id": {"evil"}, "token": {malformedAudienceJWT()}}); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("revoke with unknown client status %d, want 401", resp.StatusCode)
	}
	// Real tokens still revoke after the boundary change.
	tokens := e.tokens(cliAuthorizeParams(p), p)
	if resp := revoke(url.Values{"client_id": {config.OAuthCLIClientID}, "token": {tokens.AccessToken}, "token_type_hint": {"access_token"}}); resp.StatusCode != http.StatusOK {
		t.Fatalf("revoke access token status %d", resp.StatusCode)
	}
	if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/me after access-token revoke status %d", resp.StatusCode)
	}
}

// Review 3, R4: a verifier outside RFC 7636 syntax is rejected even when its
// S256 challenge matches.
func TestOAuthCodeVerifierSyntax(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	for name, verifier := range map[string]string{
		"one character":  "a",
		"42 characters":  strings.Repeat("a", 42),
		"129 characters": strings.Repeat("a", 129),
		"bang":           strings.Repeat("a", 42) + "!",
		"space":          strings.Repeat("a", 42) + " ",
	} {
		t.Run(name, func(t *testing.T) {
			sum := sha256.Sum256([]byte(verifier))
			p := pkcePair{verifier: verifier, challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
			params := webAuthorizeParams(p)
			code := e.code(params)
			status, out := e.postToken(codeExchangeForm(params, code, verifier))
			if status != http.StatusBadRequest || out.Error != "invalid_request" {
				t.Fatalf("status %d error %q, want 400 invalid_request", status, out.Error)
			}
		})
	}
	t.Run("128 characters accepted", func(t *testing.T) {
		verifier := strings.Repeat("A-._~9", 21) + "zz"
		sum := sha256.Sum256([]byte(verifier))
		p := pkcePair{verifier: verifier, challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
		e.tokens(webAuthorizeParams(p), p)
	})
}

// Review 3, R6: an empty Authorization header still makes a session-only
// route refuse the request.
func TestOAuthSessionOnlyRejectsEmptyAuthorization(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	id := e.startRequest(webAuthorizeParams(newPKCE()))
	empty := map[string]string{"Origin": testIssuer, "Content-Type": "application/json", "Authorization": ""}
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/oauth/requests/" + id + "/decision", `{"approve":true}`},
		{http.MethodGet, "/api/v1/oauth/requests/" + id, ""},
		{http.MethodGet, "/api/v1/me/connected-apps", ""},
		{http.MethodDelete, "/api/v1/me/connected-apps/1", ""},
	} {
		req := httptest.NewRequest(tc.method, testIssuer+tc.path, strings.NewReader(tc.body))
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: e.session})
		for k, v := range empty {
			req.Header[k] = []string{v}
		}
		if resp := e.do(req); resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s with empty Authorization: status %d, want 401", tc.method, tc.path, resp.StatusCode)
		}
	}
	if resp := e.consentRequest(http.MethodGet, "/api/v1/oauth/requests/"+id, e.session, "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET without Authorization status %d, want 200", resp.StatusCode)
	}
}

func mcpNativeAuthorizeParams(p pkcePair, redirect string) url.Values {
	v := webAuthorizeParams(p)
	v.Set("client_id", config.OAuthMCPClientID)
	v.Set("redirect_uri", redirect)
	return v
}

// Hosts addendum: the built-in logchef-mcp client accepts the loopback
// callbacks local MCP hosts use, gets tokens only for /mcp, and logchef-cli
// gets tokens only for /api.
func TestOAuthMCPNativeClient(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	for _, redirect := range []string{
		"http://localhost:8787/callback",  // Cursor desktop
		"http://localhost:53682/callback", // Claude Code
		"http://127.0.0.1:41000/callback", // Codex CLI with iss support
		"http://[::1]:41000/callback",
	} {
		t.Run("accept "+redirect, func(t *testing.T) {
			e.startRequest(mcpNativeAuthorizeParams(p, redirect))
		})
	}
	for _, redirect := range []string{
		"http://127.0.0.1:41000/callback/9b1d6e0c",
		"http://localhost:8787/callback?x=1",
		testWebRedirect,
	} {
		t.Run("reject "+redirect, func(t *testing.T) {
			noRedirect(t, e.authorize(mcpNativeAuthorizeParams(p, redirect)))
		})
	}

	t.Run("logchef-mcp cannot ask for the API resource", func(t *testing.T) {
		v := mcpNativeAuthorizeParams(p, "http://localhost:8787/callback")
		v.Set("resource", testAPIResource)
		resp := e.authorize(v)
		loc, _ := url.Parse(resp.Header.Get("Location"))
		if loc == nil || loc.Query().Get("error") != "invalid_target" || loc.Query().Get("iss") != testIssuer {
			t.Fatalf("status %d Location %q, want invalid_target with iss", resp.StatusCode, resp.Header.Get("Location"))
		}
	})
	t.Run("logchef-cli cannot ask for the MCP resource", func(t *testing.T) {
		v := cliAuthorizeParams(p)
		v.Set("resource", testMCPResource)
		resp := e.authorize(v)
		loc, _ := url.Parse(resp.Header.Get("Location"))
		if loc == nil || loc.Query().Get("error") != "invalid_target" {
			t.Fatalf("status %d Location %q, want invalid_target", resp.StatusCode, resp.Header.Get("Location"))
		}
	})

	t.Run("full flow yields an MCP-only token", func(t *testing.T) {
		params := mcpNativeAuthorizeParams(p, "http://localhost:8787/callback")
		code := e.code(params)
		cross := codeExchangeForm(params, code, p.verifier)
		cross.Set("resource", testAPIResource)
		if status, out := e.postToken(cross); status == http.StatusOK || out.Error != "invalid_target" {
			t.Fatalf("token for API resource: status %d error %q", status, out.Error)
		}
		status, tokens := e.postToken(codeExchangeForm(params, code, p.verifier))
		if status != http.StatusOK {
			t.Fatalf("exchange status %d error %q", status, tokens.Error)
		}
		if _, err := e.oauth.AuthenticateAccessToken(context.Background(), tokens.AccessToken, oauth.ResourceMCP); err != nil {
			t.Fatalf("logchef-mcp token rejected for /mcp: %v", err)
		}
		if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("logchef-mcp token at /api status %d, want 401", resp.StatusCode)
		}
		refresh := refreshForm(params, tokens.RefreshToken)
		refresh.Set("resource", testAPIResource)
		if status, _ := e.postToken(refresh); status == http.StatusOK {
			t.Fatal("logchef-mcp refreshed into an API token")
		}
	})
}

// Re-review 3, N1: the boundary validates the first value of a parameter and
// ZITADEL decodes the last, so repeated parameters are refused before the
// provider runs. A safe first token followed by a malformed JWT must not
// reach ZITADEL's claim decoder.
func TestOAuthRepeatedParametersRefused(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	post := func(path string, form url.Values) (int, string) {
		req := httptest.NewRequest(http.MethodPost, testIssuer+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp := e.do(req)
		var out tokenResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Error
	}
	p := newPKCE()
	tokens := e.tokens(cliAuthorizeParams(p), p)
	for _, hint := range []string{"", "access_token", "refresh_token"} {
		for name, values := range map[string][]string{
			"safe then malformed": {strings.Repeat("a", 43), malformedAudienceJWT()},
			"real then malformed": {tokens.RefreshToken, malformedAudienceJWT()},
			"malformed then safe": {malformedAudienceJWT(), strings.Repeat("a", 43)},
		} {
			form := url.Values{"client_id": {config.OAuthMCPClientID}, "token": values}
			if hint != "" {
				form.Set("token_type_hint", hint)
			}
			if status, code := post(oauth.RevokePath, form); status != http.StatusBadRequest || code != "invalid_request" {
				t.Errorf("revoke %s hint %q: status %d error %q, want 400 invalid_request", name, hint, status, code)
			}
		}
	}
	for _, key := range []string{"client_id", "token_type_hint"} {
		form := url.Values{"client_id": {config.OAuthCLIClientID}, "token": {tokens.RefreshToken}, "token_type_hint": {"refresh_token"}}
		form.Add(key, form.Get(key))
		if status, code := post(oauth.RevokePath, form); status != http.StatusBadRequest || code != "invalid_request" {
			t.Errorf("revoke repeated %s: status %d error %q", key, status, code)
		}
	}
	if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusOK {
		t.Fatalf("a refused revocation revoked the grant: /me status %d", resp.StatusCode)
	}

	params := cliAuthorizeParams(p)
	refresh := refreshForm(params, tokens.RefreshToken)
	for _, key := range []string{"grant_type", "client_id", "refresh_token", "resource", "scope"} {
		form := url.Values{}
		for k, v := range refresh {
			form[k] = slices.Clone(v)
		}
		if key == "scope" {
			form.Set("scope", "logs:read")
		}
		form.Add(key, form.Get(key))
		if status, code := post(oauth.TokenPath, form); status != http.StatusBadRequest || code != "invalid_request" {
			t.Errorf("token repeated %s: status %d error %q, want 400 invalid_request", key, status, code)
		}
	}
	p2 := newPKCE()
	params2 := cliAuthorizeParams(p2)
	exchange := codeExchangeForm(params2, e.code(params2), p2.verifier)
	for _, key := range []string{"code", "code_verifier", "redirect_uri"} {
		form := url.Values{}
		for k, v := range exchange {
			form[k] = slices.Clone(v)
		}
		form.Add(key, form.Get(key))
		if status, code := post(oauth.TokenPath, form); status != http.StatusBadRequest || code != "invalid_request" {
			t.Errorf("token repeated %s: status %d error %q, want 400 invalid_request", key, status, code)
		}
	}
	if status, _ := post(oauth.TokenPath, exchange); status != http.StatusOK {
		t.Fatalf("refused duplicates burned the code: exchange status %d", status)
	}
}

// The legacy CLI login (ID token for PAT exchange) is retired: its route is
// gone and /meta no longer advertises cli_client_id.
func TestLegacyCLILoginRemoved(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	req := httptest.NewRequest(http.MethodPost, testIssuer+"/api/v1/cli/token", http.NoBody)
	req.Header.Set("Authorization", "Bearer some.id.token")
	if resp := e.do(req); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST /api/v1/cli/token status %d, want 404", resp.StatusCode)
	}
	resp := e.do(httptest.NewRequest(http.MethodGet, testIssuer+"/api/v1/meta", http.NoBody))
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "cli_client_id") {
		t.Fatalf("meta still advertises cli_client_id: %s", body)
	}
}

// Unknown /.well-known/* paths are 404, not the SPA. When OAuth is enabled the
// metadata and PRM still answer; when it is disabled every well-known path is
// 404.
func TestWellKnownPaths(t *testing.T) {
	t.Parallel()
	unknown := []string{"/.well-known/openid-configuration", "/.well-known/foo", "/.well-known/oauth-authorization-server/extra", "/.well-known", "/.well-known/"}
	oauthPaths := []string{oauth.AuthorizationServerMetadataPath, oauth.ProtectedResourceMetadataPath, "/.well-known/oauth-protected-resource"}
	get := func(t *testing.T, app *fiber.App, path string) *testResponse {
		t.Helper()
		return testRequest(t, app, httptest.NewRequest(http.MethodGet, testIssuer+path, http.NoBody))
	}

	enabled := newOAuthEnv(t)
	for _, path := range unknown {
		resp := get(t, enabled.srv.app, path)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "<html") {
			t.Errorf("enabled GET %s: status %d body %.60q, want 404 without HTML", path, resp.StatusCode, body)
		}
	}
	for _, path := range oauthPaths {
		resp := get(t, enabled.srv.app, path)
		var doc map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil || resp.StatusCode != http.StatusOK {
			t.Errorf("enabled GET %s: status %d err %v, want JSON 200", path, resp.StatusCode, err)
		}
	}
	if resp := get(t, enabled.srv.app, "/settings/profile"); resp.StatusCode != http.StatusOK {
		t.Errorf("SPA route status %d, want 200", resp.StatusCode)
	}

	cfg := testOAuthConfig()
	cfg.Auth.OAuth.Enabled = false
	disabled := newServerForTest(t, cfg, newServerTestDB(t), nil)
	for _, path := range append(unknown, oauthPaths...) {
		resp := get(t, disabled.app, path)
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "<html") {
			t.Errorf("disabled GET %s: status %d body %.60q, want 404 without HTML", path, resp.StatusCode, body)
		}
	}

	// Review 6, F6-5: with OAuth disabled /mcp is 404 for every method,
	// never the SPA and never 405. Enabled, the mount keeps its statuses.
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodPut, http.MethodOptions, http.MethodHead} {
		for _, path := range []string{MCPPath, MCPPath + "/"} {
			resp := testRequest(t, disabled.app, httptest.NewRequest(method, testIssuer+path, strings.NewReader(`{}`)))
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != http.StatusNotFound || strings.Contains(string(body), "<html") {
				t.Errorf("disabled %s %s: status %d body %.60q, want 404", method, path, resp.StatusCode, body)
			}
		}
	}
	for method, want := range map[string]int{http.MethodGet: http.StatusMethodNotAllowed, http.MethodDelete: http.StatusMethodNotAllowed, http.MethodPost: http.StatusUnauthorized} {
		if resp := testRequest(t, enabled.srv.app, httptest.NewRequest(method, testIssuer+MCPPath, strings.NewReader(`{}`))); resp.StatusCode != want {
			t.Errorf("enabled %s /mcp: status %d, want %d", method, resp.StatusCode, want)
		}
	}
}

// Review 6, F6-3: every client-authentication field is refused on token and
// revoke, alone (401 invalid_client, public clients only) or repeated (400
// invalid_request), and nothing reaches ZITADEL or revokes the grant.
func TestOAuthClientCredentialFieldsRefused(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	params := cliAuthorizeParams(p)
	tokens := e.tokens(params, p)
	post := func(path string, form url.Values, authorization *string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, testIssuer+path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if authorization != nil {
			req.Header["Authorization"] = []string{*authorization}
		}
		resp := e.do(req)
		var out tokenResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Error
	}
	revokeForm := func() url.Values {
		return url.Values{"client_id": {config.OAuthCLIClientID}, "token": {tokens.RefreshToken}, "token_type_hint": {"refresh_token"}}
	}
	for _, field := range []string{"client_secret", "client_assertion", "client_assertion_type"} {
		for path, base := range map[string]func() url.Values{
			oauth.RevokePath: revokeForm,
			oauth.TokenPath:  func() url.Values { return refreshForm(params, tokens.RefreshToken) },
		} {
			once := base()
			once.Set(field, "")
			if status, code := post(path, once, nil); status != http.StatusUnauthorized || code != "invalid_client" {
				t.Errorf("%s with %s: status %d error %q, want 401 invalid_client", path, field, status, code)
			}
			twice := base()
			twice[field] = []string{"", ""}
			if status, code := post(path, twice, nil); status != http.StatusBadRequest || code != "invalid_request" {
				t.Errorf("%s with repeated %s: status %d error %q, want 400 invalid_request", path, field, status, code)
			}
		}
	}
	empty := ""
	for _, path := range []string{oauth.RevokePath, oauth.TokenPath} {
		form := revokeForm()
		if path == oauth.TokenPath {
			form = refreshForm(params, tokens.RefreshToken)
		}
		if status, code := post(path, form, &empty); status != http.StatusUnauthorized || code != "invalid_client" {
			t.Errorf("%s with an empty Authorization header: status %d error %q, want 401 invalid_client", path, status, code)
		}
	}
	if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusOK {
		t.Fatalf("a refused request revoked the grant: /me status %d", resp.StatusCode)
	}
	if status, _ := post(oauth.RevokePath, revokeForm(), nil); status != http.StatusOK {
		t.Fatalf("plain public-client revocation status %d", status)
	}
	if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revocation did not take effect: /me status %d", resp.StatusCode)
	}
}

// Two-host deployments: server.public_url is the issuer and machine origin
// (metadata, token, revocation, resources, iss); server.browser_url is where
// browsers authorize and consent. Each part answers on its own origin, and
// the same process serves both.
func TestOAuthTwoHosts(t *testing.T) {
	t.Parallel()
	const browser = "https://logchef-ui.test"
	cfg := testOAuthConfig()
	cfg.Server.BrowserURL = browser
	e := newOAuthEnvWithConfig(t, cfg, models.UserRoleMember)

	md := e.oauth.Metadata()
	if md.Issuer != testIssuer || md.AuthorizationEndpoint != browser+oauth.AuthorizePath ||
		md.TokenEndpoint != testIssuer+oauth.TokenPath || md.RevocationEndpoint != testIssuer+oauth.RevokePath {
		t.Fatalf("metadata %+v", md)
	}
	prm := e.oauth.MCPResourceMetadata()
	if prm.Resource != testMCPResource || !slices.Equal(prm.AuthorizationServers, []string{testIssuer}) {
		t.Fatalf("PRM %+v", prm)
	}
	var meta struct {
		Data struct {
			OAuthIssuer string `json:"oauth_issuer"`
			UIURL       string `json:"ui_url"`
		} `json:"data"`
	}
	if resp := e.do(httptest.NewRequest(http.MethodGet, testIssuer+"/api/v1/meta", http.NoBody)); json.NewDecoder(resp.Body).Decode(&meta) != nil ||
		meta.Data.OAuthIssuer != testIssuer || meta.Data.UIURL != browser {
		t.Fatalf("meta oauth_issuer %q ui_url %q, want the issuer and the browser URL", meta.Data.OAuthIssuer, meta.Data.UIURL)
	}

	// /oauth/authorize answers on either host and always sends the browser to
	// consent on the browser origin.
	p := newPKCE()
	params := cliAuthorizeParams(p)
	var id string
	for _, host := range []string{testIssuer, browser} {
		resp := e.do(httptest.NewRequest(http.MethodGet, host+oauth.AuthorizePath+"?"+params.Encode(), http.NoBody))
		loc, err := url.Parse(resp.Header.Get("Location"))
		if err != nil || resp.StatusCode != http.StatusFound || loc.Scheme+"://"+loc.Host+loc.Path != browser+oauth.ConsentPath {
			t.Fatalf("authorize on %s: status %d Location %q, want the browser consent page", host, resp.StatusCode, resp.Header.Get("Location"))
		}
		id = loc.Query().Get("request")
	}

	consentPath := "/api/v1/oauth/requests/" + id
	get := httptest.NewRequest(http.MethodGet, browser+consentPath, http.NoBody)
	get.AddCookie(&http.Cookie{Name: sessionCookieName, Value: e.session})
	var got struct {
		Data struct {
			Instance string `json:"instance"`
		} `json:"data"`
	}
	if resp := e.do(get); resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&got) != nil || got.Data.Instance != browser {
		t.Fatalf("consent GET on the browser host: status %d instance %q", resp.StatusCode, got.Data.Instance)
	}

	decide := func(origin string) *testResponse {
		req := httptest.NewRequest(http.MethodPost, browser+consentPath+"/decision", strings.NewReader(`{"approve":true}`))
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: e.session})
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		return e.do(req)
	}
	if resp := decide(testIssuer); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("decision with the API origin: status %d, want 403", resp.StatusCode)
	}
	resp := decide(browser)
	var decision struct {
		Data struct {
			RedirectURL string `json:"redirect_url"`
		} `json:"data"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&decision) != nil {
		t.Fatalf("decision with the browser origin: status %d", resp.StatusCode)
	}
	redirect, err := url.Parse(decision.Data.RedirectURL)
	if err != nil || redirect.Query().Get("iss") != testIssuer || redirect.Query().Get("code") == "" {
		t.Fatalf("decision redirect %q, want a code with iss = issuer", decision.Data.RedirectURL)
	}

	// Machine endpoints on the API host.
	status, tokens := e.postToken(codeExchangeForm(params, redirect.Query().Get("code"), p.verifier))
	if status != http.StatusOK {
		t.Fatalf("token exchange on the API host: status %d error %q", status, tokens.Error)
	}
	if resp := e.apiGet("/api/v1/me", tokens.AccessToken); resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/v1/me on the API host: status %d", resp.StatusCode)
	}
	if resp := e.do(httptest.NewRequest(http.MethodGet, testIssuer+oauth.AuthorizationServerMetadataPath, http.NoBody)); resp.StatusCode != http.StatusOK {
		t.Fatalf("metadata on the API host: status %d", resp.StatusCode)
	}

	// Connected apps follow the browser origin too.
	revoke := func(origin string) int {
		req := httptest.NewRequest(http.MethodDelete, browser+"/api/v1/me/connected-apps/1", http.NoBody)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: e.session})
		req.Header.Set("Origin", origin)
		return e.do(req).StatusCode
	}
	if status := revoke(testIssuer); status != http.StatusForbidden {
		t.Fatalf("connected-app revoke with the API origin: status %d, want 403", status)
	}
	if status := revoke(browser); status != http.StatusNoContent {
		t.Fatalf("connected-app revoke with the browser origin: status %d, want 204", status)
	}
}

// Without server.browser_url every browser URL is the issuer, as before.
func TestOAuthBrowserURLDefaultsToIssuer(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	if md := e.oauth.Metadata(); md.AuthorizationEndpoint != testIssuer+oauth.AuthorizePath {
		t.Fatalf("authorization_endpoint %q", md.AuthorizationEndpoint)
	}
	if e.oauth.BrowserOrigin() != testIssuer || e.oauth.IssuerOrigin() != testIssuer {
		t.Fatalf("origins %q %q", e.oauth.BrowserOrigin(), e.oauth.IssuerOrigin())
	}
}
