package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	protocolModern = "2026-07-28"
	protocolLegacy = "2025-11-25"
)

// mcpToken runs the logchef-mcp native flow and returns an MCP access token.
func (e *oauthEnv) mcpToken() string {
	e.t.Helper()
	p := newPKCE()
	return e.tokens(mcpNativeAuthorizeParams(p, "http://localhost:8787/callback"), p).AccessToken
}

type mcpEnvelope struct {
	Result map[string]any  `json:"result"`
	Error  json.RawMessage `json:"error"`
}

// mcpPost sends one JSON-RPC request to /mcp. headers override the defaults.
func (e *oauthEnv) mcpPost(bearer string, body []byte, headers map[string]string) *testResponse {
	e.t.Helper()
	req, err := mcpRequest(testIssuer, bearer, body, headers)
	if err != nil {
		e.t.Fatal(err)
	}
	return e.do(req)
}

func mcpRequest(baseURL, bearer string, body []byte, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodPost, baseURL+MCPPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// mcpPostOver sends one JSON-RPC request to /mcp over a real socket.
func mcpPostOver(baseURL, bearer string, body []byte, headers map[string]string) (string, error) {
	req, err := mcpRequest(baseURL, bearer, body, headers)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	text, err := io.ReadAll(resp.Body)
	return string(text), err
}

// mcpCall sends a request in the given protocol version and returns the
// result. The modern protocol carries client metadata in params._meta and
// the method in headers; the 2025-11-25 protocol uses the version header.
func (e *oauthEnv) mcpCall(bearer, protocol, method, name string, params map[string]any) map[string]any {
	e.t.Helper()
	headers := map[string]string{"Mcp-Protocol-Version": protocol}
	if protocol == protocolModern {
		params["_meta"] = map[string]any{
			"io.modelcontextprotocol/protocolVersion":    protocolModern,
			"io.modelcontextprotocol/clientInfo":         map[string]string{"name": "test", "version": "1"},
			"io.modelcontextprotocol/clientCapabilities": map[string]any{},
		}
		headers["Mcp-Method"] = method
		if name != "" {
			headers["Mcp-Name"] = name
		}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		e.t.Fatal(err)
	}
	resp := e.mcpPost(bearer, body, headers)
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		e.t.Fatalf("%s %s: status %d %s", protocol, method, resp.StatusCode, data)
	}
	if resp.Header.Get("Mcp-Session-Id") != "" {
		e.t.Fatalf("%s %s created a session", protocol, method)
	}
	var env mcpEnvelope
	if err := json.Unmarshal(data, &env); err != nil || env.Result == nil {
		e.t.Fatalf("%s %s: err %v body %s", protocol, method, err, data)
	}
	return env.Result
}

func toolNames(t *testing.T, listed map[string]any) []string {
	t.Helper()
	raw, ok := listed["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list result %v", listed)
	}
	names := make([]string, 0, len(raw))
	for _, item := range raw {
		names = append(names, item.(map[string]any)["name"].(string))
	}
	return names
}

func structured(t *testing.T, result map[string]any) any {
	t.Helper()
	if result["isError"] == true {
		t.Fatalf("tool error: %v", result["content"])
	}
	return result["structuredContent"]
}

// toolText returns the text content of a successful tool result.
func toolText(t *testing.T, result map[string]any) string {
	t.Helper()
	if result["isError"] == true {
		t.Fatalf("tool error: %v", result["content"])
	}
	var text strings.Builder
	for _, item := range result["content"].([]any) {
		if s, ok := item.(map[string]any)["text"].(string); ok {
			text.WriteString(s)
		}
	}
	return text.String()
}

// T-MCP-1: no or bad token gets 401 with resource_metadata and scope.
func TestMCPUnauthorizedChallenge(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	wantPRM := `resource_metadata="` + testIssuer + `/.well-known/oauth-protected-resource/mcp"`
	wantScope := `scope="profile:read teams:read sources:read logs:read saved_queries:read collections:read alerts:read"`

	resp := e.mcpPost("", body, nil)
	challenge := resp.Header.Get("WWW-Authenticate")
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(challenge, wantPRM) || !strings.Contains(challenge, wantScope) || strings.Contains(challenge, "error=") {
		t.Fatalf("no token: status %d WWW-Authenticate %q", resp.StatusCode, challenge)
	}

	pat, err := core.CreateAPIToken(context.Background(), e.db, slog.New(slog.NewTextHandler(io.Discard, nil)), &e.cfg.Auth, e.user.ID, "pat", nil, []models.TokenScope{models.TokenScopeAll})
	if err != nil {
		t.Fatal(err)
	}
	cliPKCE := newPKCE()
	apiTokens := e.tokens(cliAuthorizeParams(cliPKCE), cliPKCE)
	for name, bearer := range map[string]string{
		"garbage":              "not-a-token",
		"PAT":                  pat.Token,
		"API-audience token":   apiTokens.AccessToken,
		"ID token":             apiTokens.IDToken,
		"refresh token":        apiTokens.RefreshToken,
		"malformed JWT bearer": malformedAudienceJWT(),
	} {
		resp := e.mcpPost(bearer, body, nil)
		challenge := resp.Header.Get("WWW-Authenticate")
		if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(challenge, `error="invalid_token"`) || !strings.Contains(challenge, wantPRM) {
			t.Errorf("%s at /mcp: status %d WWW-Authenticate %q", name, resp.StatusCode, challenge)
		}
	}
	req := httptest.NewRequest(http.MethodPost, testIssuer+MCPPath, bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: e.session})
	if resp := e.do(req); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("session cookie at /mcp: status %d, want 401", resp.StatusCode)
	}
	if resp := e.apiGet("/api/v1/me", e.mcpToken()); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("MCP token at /api: status %d, want 401", resp.StatusCode)
	}
}

// T-MCP-5: body limit, Origin allow-list, and methods.
func TestMCPRequestLimits(t *testing.T) {
	t.Parallel()
	cfg := testOAuthConfig()
	cfg.Auth.OAuth.MCPAllowedOrigins = []string{"http://localhost:6274"}
	e := newOAuthEnvWithConfig(t, cfg, models.UserRoleMember)
	token := e.mcpToken()
	list := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	legacy := map[string]string{"Mcp-Protocol-Version": protocolLegacy}

	big := append([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"pad":"`), bytes.Repeat([]byte("a"), mcpMaxBodyBytes)...)
	big = append(big, []byte(`"}}`)...)
	if resp := e.mcpPost(token, big, legacy); resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: status %d, want 413", resp.StatusCode)
	}
	for origin, want := range map[string]int{
		"https://attacker.example": http.StatusForbidden,
		"http://localhost:6275":    http.StatusForbidden,
		"null":                     http.StatusForbidden,
		testIssuer:                 http.StatusOK,
		"http://localhost:6274":    http.StatusOK,
	} {
		headers := map[string]string{"Origin": origin, "Mcp-Protocol-Version": protocolLegacy}
		if resp := e.mcpPost(token, list, headers); resp.StatusCode != want {
			t.Errorf("Origin %q: status %d, want %d", origin, resp.StatusCode, want)
		}
	}
	if resp := e.mcpPost("", list, map[string]string{"Origin": "https://attacker.example"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Origin without token: status %d, want 403", resp.StatusCode)
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req := httptest.NewRequest(method, testIssuer+MCPPath, http.NoBody)
		req.Header.Set("Authorization", "Bearer "+token)
		if resp := e.do(req); resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /mcp: status %d, want 405", method, resp.StatusCode)
		}
	}
}

// T-MCP-6: initialize and tools work over 2026-07-28 and 2025-11-25.
func TestMCPProtocolVersions(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	token := e.mcpToken()

	discovery := e.mcpCall(token, protocolModern, "server/discover", "", map[string]any{})
	versions, _ := discovery["supportedVersions"].([]any)
	if len(versions) == 0 || versions[0] != protocolModern || !slices.Contains(versions, any(protocolLegacy)) {
		t.Fatalf("supportedVersions %v", discovery["supportedVersions"])
	}
	modern := toolNames(t, e.mcpCall(token, protocolModern, "tools/list", "", map[string]any{}))
	opened := e.mcpCall(token, protocolModern, "tools/call", "open_investigation", map[string]any{"name": "open_investigation", "arguments": map[string]any{}})
	if opened["isError"] == true {
		t.Fatalf("open_investigation: %v", opened)
	}

	init := e.mcpCall(token, protocolLegacy, "initialize", "", map[string]any{
		"protocolVersion": protocolLegacy,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "test", "version": "1"},
	})
	if init["protocolVersion"] != protocolLegacy {
		t.Fatalf("initialize negotiated %v", init["protocolVersion"])
	}
	legacy := toolNames(t, e.mcpCall(token, protocolLegacy, "tools/list", "", map[string]any{}))
	if !slices.Equal(modern, legacy) || len(modern) < 10 {
		t.Fatalf("tool lists differ or are short: modern %v legacy %v", modern, legacy)
	}
	profile := structured(t, e.mcpCall(token, protocolLegacy, "tools/call", "", map[string]any{"name": "get_profile", "arguments": map[string]any{}}))
	if profile.(map[string]any)["email"] != e.user.Email {
		t.Fatalf("get_profile %v", profile)
	}
}

// Narrow scopes narrow the tool list, and the per-user admission cap applies
// to MCP queries.
func TestMCPScopesAndAdmission(t *testing.T) {
	// Not parallel: the query tracker is process-global and every test DB
	// starts user IDs at 1, so admission tests must not overlap.
	cfg := testOAuthConfig()
	cfg.Query.MaxConcurrentPerUser = 1
	e := newOAuthEnvWithConfig(t, cfg, models.UserRoleMember)
	team, src := mkTestTeam(t, e.db, "admit", e.user)

	p := newPKCE()
	params := mcpNativeAuthorizeParams(p, "http://localhost:8787/callback")
	params.Set("scope", "profile:read")
	narrow := e.tokens(params, p).AccessToken
	names := toolNames(t, e.mcpCall(narrow, protocolLegacy, "tools/list", "", map[string]any{}))
	if !slices.Equal(names, []string{"get_meta", "get_profile"}) {
		t.Fatalf("profile:read tools %v", names)
	}

	token := e.mcpToken()
	cancel := func() {}
	held, err := queryTracker.StartQuery(QueryClassPreview, e.user.ID, src.ID, team.ID, "held", cancel, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	defer queryTracker.RemoveQuery(held)
	result := e.mcpCall(token, protocolLegacy, "tools/call", "", map[string]any{"name": "query_logs", "arguments": map[string]any{
		"team_id": team.ID, "source_id": src.ID, "raw_sql": "SELECT 1", "start_time": "2026-10-06 00:00:00", "end_time": "2026-10-06 01:00:00",
	}})
	text, err := json.Marshal(result["content"])
	if err != nil {
		t.Fatal(err)
	}
	if result["isError"] != true || !strings.Contains(string(text), "Too many active preview queries for this user") {
		t.Fatalf("admission not enforced through /mcp: %v", result)
	}
}

// Integration decision 1 through /mcp: an admin's MCP token sees only the
// teams, sources, saved queries and alerts of its memberships.
func TestMCPAdminIsMembershipOnly(t *testing.T) {
	t.Parallel()
	w := newAdminWorld(t)
	e := w.e
	token := e.mcpToken()
	call := func(name string, args map[string]any) map[string]any {
		return e.mcpCall(token, protocolLegacy, "tools/call", "", map[string]any{"name": name, "arguments": args})
	}
	asJSON := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	teams := asJSON(structured(t, call("get_teams", map[string]any{})))
	if !strings.Contains(teams, `"name":"teama"`) || strings.Contains(teams, "teamb") {
		t.Errorf("get_teams %s, want only team A", teams)
	}
	sources := asJSON(structured(t, call("get_sources", map[string]any{})))
	if !strings.Contains(sources, "teama-src") || strings.Contains(sources, "teamb-src") {
		t.Errorf("get_sources %s, want only source A", sources)
	}
	if denied := call("get_team_sources", map[string]any{"team_id": w.teamB.ID}); denied["isError"] != true {
		t.Errorf("get_team_sources for team B: %v, want an error", denied)
	}
	queries := asJSON(structured(t, call("list_saved_queries", map[string]any{})))
	if !strings.Contains(queries, "q-teama-src") || strings.Contains(queries, "q-teamb-src") {
		t.Errorf("list_saved_queries %s, want only query A", queries)
	}
	if denied := call("get_saved_query", map[string]any{"query_id": w.queryB.ID}); denied["isError"] != true {
		t.Errorf("get_saved_query B: %v, want an error", denied)
	}
	alerts := toolText(t, call("list_alerts", map[string]any{}))
	if !strings.Contains(alerts, "alert-teama-src") || strings.Contains(alerts, "alert-teamb-src") {
		t.Errorf("list_alerts %s, want only alert A", alerts)
	}
	if denied := call("get_alert_history", map[string]any{"alert_id": w.alertB.ID}); denied["isError"] != true {
		t.Errorf("get_alert_history B: %v, want an error", denied)
	}
}

// vlSourceFor links a VictoriaLogs source at baseURL to a new team of the
// env's user.
func (e *oauthEnv) vlSourceFor(baseURL string) (*models.Team, *models.Source) {
	e.t.Helper()
	ctx := context.Background()
	team := &models.Team{Name: "vl-team"}
	if err := e.db.CreateTeam(ctx, team); err != nil {
		e.t.Fatal(err)
	}
	conn, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: baseURL})
	if err != nil {
		e.t.Fatal(err)
	}
	src := &models.Source{Name: "vl", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: conn}
	if err := e.db.CreateSource(ctx, src); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.AddTeamSource(ctx, team.ID, src.ID); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.AddTeamMember(ctx, team.ID, e.user.ID, models.TeamRoleMember); err != nil {
		e.t.Fatal(err)
	}
	return team, src
}

func trackedQueries(userID models.UserID) int {
	queryTracker.mu.RLock()
	defer queryTracker.mu.RUnlock()
	n := 0
	for _, q := range queryTracker.queries {
		if q.UserID == userID {
			n++
		}
	}
	return n
}

// Review 6, F6-1: every standalone log-reading tool is admitted through the
// shared tracker before it reaches the datasource, under both the per-user
// and the global preview cap, and releases its slot afterwards.
func TestMCPDiscoveryToolsAreAdmitted(t *testing.T) {
	// Not parallel: the query tracker is process-global and every test DB
	// starts user IDs at 1, so admission tests must not overlap.
	var backendCalls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer backend.Close()

	cfg := testOAuthConfig()
	cfg.Query.MaxConcurrentPerUser = 1
	cfg.Query.MaxConcurrentGlobal = 1
	e := newOAuthEnvWithConfig(t, cfg, models.UserRoleMember)
	team, src := e.vlSourceFor(backend.URL)
	token := e.mcpToken()
	tools := map[string]map[string]any{
		"get_field_values":         {"team_id": team.ID, "source_id": src.ID, "field_name": "_msg", "field_type": "String", "start_time": "2026-10-01T09:00:00Z", "end_time": "2026-10-01T12:00:00Z"},
		"get_all_field_dimensions": {"team_id": team.ID, "source_id": src.ID, "start_time": "2026-10-01T09:00:00Z", "end_time": "2026-10-01T12:00:00Z"},
		"get_log_context":          {"team_id": team.ID, "source_id": src.ID, "timestamp": 1790850000000},
	}
	call := func(name string) map[string]any {
		return e.mcpCall(token, protocolLegacy, "tools/call", "", map[string]any{"name": name, "arguments": tools[name]})
	}

	for budget, holder := range map[string]models.UserID{"per-user": e.user.ID, "global": e.user.ID + 100000} {
		held, err := queryTracker.StartQuery(QueryClassPreview, holder, src.ID, team.ID, "held", func() {}, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		for name := range tools {
			before := backendCalls.Load()
			result := call(name)
			text, err := json.Marshal(result["content"])
			if err != nil {
				t.Fatal(err)
			}
			if result["isError"] != true || !strings.Contains(string(text), "Too many active preview queries") || backendCalls.Load() != before {
				t.Errorf("%s with the %s budget full: result %s, backend calls %d", name, budget, text, backendCalls.Load()-before)
			}
		}
		queryTracker.RemoveQuery(held)
	}

	for name := range tools {
		before := backendCalls.Load()
		result := call(name)
		text, err := json.Marshal(result["content"])
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.Contains(string(text), "Too many active"):
			t.Errorf("%s refused with free budget: %s", name, text)
		case name == "get_log_context":
			// VictoriaLogs has no log-context support, so an admitted call
			// ends at the provider without a backend request.
			if !strings.Contains(string(text), "not supported") {
				t.Errorf("get_log_context result %s, want not supported after admission", text)
			}
		case backendCalls.Load() == before:
			t.Errorf("%s did not reach the backend with free budget", name)
		}
		if n := trackedQueries(e.user.ID); n != 0 {
			t.Errorf("%s left %d admission slots held", name, n)
		}
	}
}

// F6-2 decision: fasthttp gives no disconnect signal, so an MCP tool call,
// abandoned or not, ends at query.mcp_call_timeout_seconds. With a 2 s MCP
// bound and a 30 s query maximum, a call blocked in the backend is still
// running shortly after it starts, then ends at the MCP bound with its backend
// request cancelled and its admission slot released.
func TestMCPCallEndsAtBound(t *testing.T) {
	// Not parallel: the query tracker is process-global and every test DB
	// starts user IDs at 1, so admission tests must not overlap.
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var cancelled atomic.Bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/select/logsql/query" {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			cancelled.Store(true)
		case <-release:
		}
	}))
	defer backend.Close()
	defer close(release)

	cfg := testOAuthConfig()
	cfg.Query.MCPCallTimeoutSeconds = 2 // the HTTP API keeps max_timeout_seconds (30 s)
	e := newOAuthEnvWithConfig(t, cfg, models.UserRoleMember)
	team, src := e.vlSourceFor(backend.URL)
	token := e.mcpToken()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
		"name": "query_logs", "arguments": map[string]any{"team_id": team.ID, "source_id": src.ID, "raw_sql": "*", "start_time": "2026-10-01T09:00:00Z", "end_time": "2026-10-01T12:00:00Z"},
	}})
	if err != nil {
		t.Fatal(err)
	}

	baseURL, shutdown := e.serve()
	defer shutdown()
	start := time.Now()
	done := make(chan string, 1)
	go func() {
		text, err := mcpPostOver(baseURL, token, body, map[string]string{"Mcp-Protocol-Version": protocolLegacy})
		if err != nil {
			text = err.Error()
		}
		done <- text
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the tool call never reached the backend")
	}
	time.Sleep(300 * time.Millisecond)
	if n := trackedQueries(e.user.ID); n != 1 || cancelled.Load() {
		t.Fatalf("300 ms in: %d slots held, backend cancelled %v; want the call still running", n, cancelled.Load())
	}
	var response string
	select {
	case response = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the call did not end at the MCP bound")
	}
	elapsed := time.Since(start)
	if elapsed < 1800*time.Millisecond || elapsed > 3500*time.Millisecond {
		t.Fatalf("the call ended after %v, want about the 2 s MCP bound", elapsed)
	}
	if !strings.Contains(response, "timed out") {
		t.Fatalf("response %s, want a timed-out tool error", response)
	}
	if n := trackedQueries(e.user.ID); n != 0 {
		t.Fatalf("%d admission slots held after the call ended", n)
	}
	deadline := time.Now().Add(time.Second)
	for !cancelled.Load() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !cancelled.Load() {
		t.Fatal("the backend request was not cancelled at the bound")
	}
}

// Requests run on the server's base context instead of the fasthttp
// RequestCtx. Shutdown cancels that context before stopping fasthttp, so an
// in-flight tool call stops at once rather than running to the MCP bound.
func TestMCPCallCancelledByShutdown(t *testing.T) {
	// Not parallel: the query tracker is process-global and every test DB
	// starts user IDs at 1, so admission tests must not overlap.
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	var cancelled atomic.Bool
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/select/logsql/query" {
			_, _ = io.WriteString(w, `{}`)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case entered <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			cancelled.Store(true)
		case <-release:
		}
	}))
	defer backend.Close()
	defer close(release)

	e := newOAuthEnv(t) // MCP bound 30 s
	team, src := e.vlSourceFor(backend.URL)
	token := e.mcpToken()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{
		"name": "query_logs", "arguments": map[string]any{"team_id": team.ID, "source_id": src.ID, "raw_sql": "*", "start_time": "2026-10-01T09:00:00Z", "end_time": "2026-10-01T12:00:00Z"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	baseURL, shutdown := e.serve()
	done := make(chan map[string]any, 1)
	go func() {
		text, err := mcpPostOver(baseURL, token, body, map[string]string{"Mcp-Protocol-Version": protocolLegacy})
		var env mcpEnvelope
		if err == nil {
			err = json.Unmarshal([]byte(text), &env)
		}
		if err != nil {
			done <- nil
			return
		}
		done <- env.Result
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the tool call never reached the backend")
	}

	start := time.Now()
	shutdown()
	var result map[string]any
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the tool call did not end after Shutdown")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("the call ended %v after Shutdown, want well under the 30 s bound", elapsed)
	}
	// mcp-go writes no body for a call whose context was cancelled, so the
	// client gets no result; a result, if any, must be an error.
	if result != nil && result["isError"] != true {
		t.Fatalf("result %v: the cancelled call reported success", result)
	}
	if !cancelled.Load() {
		t.Fatal("the backend request was not cancelled")
	}
	if n := trackedQueries(e.user.ID); n != 0 {
		t.Fatalf("%d admission slots held after cancellation", n)
	}
}

// A token narrower than a tool needs lists only the tools it can use, and
// calling a hidden tool anyway is refused as not found.
func TestMCPNarrowTokenCannotCallHiddenTool(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	p := newPKCE()
	params := mcpNativeAuthorizeParams(p, "http://localhost:8787/callback")
	params.Set("scope", "profile:read")
	narrow := e.tokens(params, p).AccessToken

	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"query_logs","arguments":{"team_id":1,"source_id":1,"raw_sql":"SELECT 1"}}}`)
	resp := e.mcpPost(narrow, body, map[string]string{"Mcp-Protocol-Version": protocolLegacy})
	data, _ := io.ReadAll(resp.Body)
	var env mcpEnvelope
	if err := json.Unmarshal(data, &env); err != nil || resp.StatusCode != http.StatusOK || env.Result != nil || !strings.Contains(string(env.Error), "tool not found") {
		t.Fatalf("query_logs with profile:read: status %d body %s", resp.StatusCode, data)
	}
}
