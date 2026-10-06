package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

// admissions is the test's AdmitQuery. It records admissions and can refuse.
type admissions struct {
	mu       sync.Mutex
	active   int
	admitted int
	refuse   bool
}

func (a *admissions) admit(_ QueryClass, _ access.AuthorizedSource, _ string, _ context.CancelFunc) (queryID string, release func(), err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.refuse {
		return "", nil, errors.New("too many active preview queries for this user")
	}
	a.active++
	a.admitted++
	return "q-test", func() {
		a.mu.Lock()
		a.active--
		a.mu.Unlock()
	}, nil
}

func (a *admissions) counts() (active, admitted int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.active, a.admitted
}

// world is a real SQLite store with users, teams and sources:
//   - member belongs to team, which has source.
//   - team also has nothing else; otherTeam has otherSource and no member.
//   - bareTeam has member but no source link.
type world struct {
	db          *sqlite.DB
	ds          *datasource.Service
	cfg         *config.Config
	admits      *admissions
	member      *models.User
	admin       *models.User
	outsider    *models.User
	team        models.TeamID
	bareTeam    models.TeamID
	otherTeam   models.TeamID
	source      *models.Source
	otherSource *models.Source
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	log := discardLogger()
	db, err := sqlite.New(ctx, sqlite.Options{
		Logger: log,
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "mcp.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	w := &world{db: db, admits: &admissions{}}
	w.cfg = &config.Config{
		Query:  config.QueryConfig{DefaultPreviewLimit: 100, MaxPreviewLimit: 500, DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 60},
		Alerts: config.AlertsConfig{Enabled: true},
	}
	w.member = w.user(t, "member@example.com", models.UserRoleMember)
	w.admin = w.user(t, "admin@example.com", models.UserRoleAdmin)
	w.outsider = w.user(t, "outsider@example.com", models.UserRoleMember)
	w.team = w.newTeam(t, "payments")
	w.bareTeam = w.newTeam(t, "bare")
	w.otherTeam = w.newTeam(t, "other")
	w.source = w.newSource(t, "payments-logs")
	w.otherSource = w.newSource(t, "other-logs")
	w.link(t, w.team, w.source.ID)
	w.link(t, w.otherTeam, w.otherSource.ID)
	w.join(t, w.team, w.member.ID)
	w.join(t, w.bareTeam, w.member.ID)

	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })
	w.ds = datasource.NewService(db, log)
	w.ds.Register(datasource.NewClickHouseProvider(manager, log))
	w.ds.Register(victorialogs.NewProvider(log))
	return w
}

func (w *world) user(t *testing.T, email string, role models.UserRole) *models.User {
	t.Helper()
	u := &models.User{Email: email, FullName: strings.Split(email, "@")[0], Role: role, Status: models.UserStatusActive}
	if err := w.db.CreateUser(context.Background(), u); err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	return u
}

func (w *world) newTeam(t *testing.T, name string) models.TeamID {
	t.Helper()
	team := &models.Team{Name: name}
	if err := w.db.CreateTeam(context.Background(), team); err != nil {
		t.Fatalf("CreateTeam(%s): %v", name, err)
	}
	return team.ID
}

func (w *world) newSource(t *testing.T, name string) *models.Source {
	t.Helper()
	source := &models.Source{
		Name:        name,
		SourceType:  models.SourceTypeClickHouse,
		MetaTSField: "timestamp",
		Connection:  models.ConnectionInfo{Host: "127.0.0.1:1", Database: "default", TableName: name},
	}
	if err := w.db.CreateSource(context.Background(), source); err != nil {
		t.Fatalf("CreateSource(%s): %v", name, err)
	}
	return source
}

func (w *world) link(t *testing.T, team models.TeamID, source models.SourceID) {
	t.Helper()
	if err := w.db.AddTeamSource(context.Background(), team, source); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
}

func (w *world) join(t *testing.T, team models.TeamID, user models.UserID) {
	t.Helper()
	if err := w.db.AddTeamMember(context.Background(), team, user, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
}

func (w *world) deps() Deps {
	return Deps{DB: w.db, Datasources: w.ds, Config: w.cfg, Version: "test", Log: discardLogger(), Admit: w.admits.admit}
}

func (w *world) server() *server.MCPServer { return newMCPServer(w.deps()) }

func session(u *models.User) *access.Principal {
	p := access.SessionPrincipal(u)
	return &p
}

func token(u *models.User, scopes ...models.TokenScope) *access.Principal {
	p := access.APITokenPrincipal(u, &models.APIToken{Scopes: scopes})
	return &p
}

// rpc sends one JSON-RPC message to s as p, or as an unauthenticated caller
// when p is nil. It returns the result, or fails the test on a JSON-RPC error.
func rpc(t *testing.T, s *server.MCPServer, p *access.Principal, method string, params any) json.RawMessage {
	t.Helper()
	result, rpcErr := rpcRaw(t, s, p, method, params)
	if len(result) == 0 {
		t.Fatalf("%s: JSON-RPC error %s", method, rpcErr)
	}
	return result
}

func rpcRaw(t *testing.T, s *server.MCPServer, p *access.Principal, method string, params any) (result, rpcErr json.RawMessage) {
	t.Helper()
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	if p != nil {
		ctx = WithPrincipal(ctx, *p)
	}
	encoded, err := json.Marshal(s.HandleMessage(ctx, request))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Result, envelope.Error
}

type toolResult struct {
	IsError           bool            `json:"isError"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	Content           []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (r toolResult) text() string {
	parts := make([]string, 0, len(r.Content))
	for _, c := range r.Content {
		parts = append(parts, c.Text)
	}
	return strings.Join(parts, "\n")
}

func callTool(t *testing.T, s *server.MCPServer, p *access.Principal, name string, args map[string]any) toolResult {
	t.Helper()
	var result toolResult
	if err := json.Unmarshal(rpc(t, s, p, "tools/call", map[string]any{"name": name, "arguments": args}), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func listToolNames(t *testing.T, s *server.MCPServer, p *access.Principal) []string {
	t.Helper()
	var listed struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rpc(t, s, p, "tools/list", map[string]any{}), &listed); err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(listed.Tools))
	for i, tool := range listed.Tools {
		names[i] = tool.Name
	}
	sort.Strings(names)
	return names
}

// T-MCP-2: tools/list shows only the tools whose scopes the principal holds,
// and tools/call refuses the others.
func TestToolsListFiltersByScope(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := w.server()

	all := make([]string, 0, len(toolScopes))
	for name := range toolScopes {
		all = append(all, name)
	}
	sort.Strings(all)

	for _, tc := range []struct {
		name string
		p    *access.Principal
		want []string
	}{
		{"session sees every tool", session(w.member), all},
		{"wildcard token sees every tool", token(w.member, models.TokenScopeAll), all},
		{"no principal sees nothing", nil, []string{}},
		{"token without scopes sees only public tools", token(w.member), []string{"get_meta"}},
		{"profile only", token(w.member, models.TokenScopeProfileRead), []string{"get_meta", "get_profile"}},
		{"sources without teams", token(w.member, models.TokenScopeSourcesRead), []string{"get_meta", "get_source_schema", "get_team_sources"}},
		{"teams and sources", token(w.member, models.TokenScopeTeamsRead, models.TokenScopeSourcesRead), []string{"get_meta", "get_source_schema", "get_sources", "get_team_sources", "get_teams"}},
		{"logs only", token(w.member, models.TokenScopeLogsRead), []string{
			"compare_windows", "get_all_field_dimensions", "get_field_values", "get_log_context", "get_log_histogram",
			"get_meta", "open_investigation", "query_logchefql", "query_logs", "translate_logchefql", "validate_logchefql",
		}},
		{"saved queries and alerts", token(w.member, models.TokenScopeSavedQueriesRead, models.TokenScopeAlertsRead), []string{
			"get_alert_history", "get_meta", "get_saved_query", "list_alerts", "list_saved_queries",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := listToolNames(t, s, tc.p)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("tools/list = %v\nwant %v", got, tc.want)
			}
		})
	}

	// Every registered tool has a scope entry, so the session list is complete.
	if got := listToolNames(t, s, session(w.member)); len(got) != len(toolScopes) {
		t.Fatalf("session sees %d tools, toolScopes has %d", len(got), len(toolScopes))
	}

	// A hidden tool cannot be called directly.
	result, rpcErr := rpcRaw(t, s, token(w.member, models.TokenScopeLogsRead), "tools/call",
		map[string]any{"name": "get_teams", "arguments": map[string]any{}})
	if len(result) != 0 && !strings.Contains(string(result), `"isError":true`) {
		t.Fatalf("hidden tool ran: %s", result)
	}
	if len(result) == 0 && len(rpcErr) == 0 {
		t.Fatal("hidden tool call returned neither result nor error")
	}
}

// T-MCP-3: tools return only the sources the user can access, with the same
// verdicts as the HTTP middleware (phase 1 T-AZ-3 and T-AZ-4).
func TestToolsReturnOnlyAccessibleSources(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := w.server()

	var sources SourcesAggregateResult
	result := callTool(t, s, session(w.member), "get_sources", map[string]any{})
	if result.IsError {
		t.Fatalf("get_sources: %s", result.text())
	}
	if err := json.Unmarshal(result.StructuredContent, &sources); err != nil {
		t.Fatal(err)
	}
	if len(sources.Sources) != 1 || sources.Sources[0].ID != int(w.source.ID) {
		t.Fatalf("member sources = %+v, want only %d", sources.Sources, w.source.ID)
	}
	if teams := sources.Sources[0].Teams; len(teams) != 1 || teams[0].ID != int(w.team) {
		t.Fatalf("source teams = %+v", teams)
	}

	result = callTool(t, s, session(w.outsider), "get_sources", map[string]any{})
	if err := json.Unmarshal(result.StructuredContent, &sources); err != nil || len(sources.Sources) != 0 {
		t.Fatalf("outsider sources = %+v, %v", sources.Sources, err)
	}

	type verdict struct {
		user    *models.User
		team    models.TeamID
		source  models.SourceID
		allowed bool
		reason  string
	}
	for _, tc := range []verdict{
		{w.member, w.team, w.source.ID, true, ""},
		{w.outsider, w.team, w.source.ID, false, access.ErrNotTeamMember.Error()},
		{w.admin, w.team, w.source.ID, true, ""},
		{w.member, w.bareTeam, w.source.ID, false, access.ErrSourceNotInTeam.Error()},
		{w.admin, w.otherTeam, w.source.ID, false, access.ErrSourceNotInTeam.Error()},
		{w.member, w.team, w.otherSource.ID, false, access.ErrSourceNotInTeam.Error()},
	} {
		result := callTool(t, s, session(tc.user), "validate_logchefql", map[string]any{
			"team_id": tc.team, "source_id": tc.source, "query": "level=error",
		})
		if result.IsError == tc.allowed || (!tc.allowed && !strings.Contains(result.text(), tc.reason)) {
			t.Errorf("%s team %d source %d: isError=%v text=%q, want allowed=%v (%s)",
				tc.user.Email, tc.team, tc.source, result.IsError, result.text(), tc.allowed, tc.reason)
		}
	}

	if result := callTool(t, s, session(w.member), "get_team_sources", map[string]any{"team_id": w.otherTeam}); !result.IsError {
		t.Fatalf("member listed sources of a team they are not in: %s", result.text())
	}
	if result := callTool(t, s, token(w.member, models.TokenScopeAll), "query_logchefql", map[string]any{
		"team_id": w.team, "source_id": w.otherSource.ID, "query": "", "start_time": "2026-10-01T00:00:00Z", "end_time": "2026-10-01T01:00:00Z",
	}); !result.IsError || !strings.Contains(result.text(), access.ErrSourceNotInTeam.Error()) {
		t.Fatalf("query on unlinked source: %s", result.text())
	}
	if active, admitted := w.admits.counts(); active != 0 || admitted != 0 {
		t.Fatalf("an unauthorized query was admitted: active=%d admitted=%d", active, admitted)
	}

	// The next call after a membership removal is denied (T-AZ-4).
	if err := w.db.RemoveTeamMember(context.Background(), w.team, w.member.ID); err != nil {
		t.Fatal(err)
	}
	if result := callTool(t, s, session(w.member), "validate_logchefql", map[string]any{
		"team_id": w.team, "source_id": w.source.ID, "query": "level=error",
	}); !result.IsError {
		t.Fatal("removed member still authorized")
	}
}

func TestSavedQueriesAndAlertsFollowSourceAccess(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := w.server()
	ctx := context.Background()
	creator := w.admin.ID
	visible, err := w.db.CreateSavedQuery(ctx, w.source.ID, nil, "errors", "", models.QueryLanguageLogchefQL, models.SavedQueryEditorModeBuilder, `{}`, &creator)
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := w.db.CreateSavedQuery(ctx, w.otherSource.ID, nil, "hidden", "", models.QueryLanguageClickHouseSQL, models.SavedQueryEditorModeNative, `{}`, &creator)
	if err != nil {
		t.Fatal(err)
	}

	var listed []SavedQueryResult
	result := callTool(t, s, session(w.member), "list_saved_queries", map[string]any{})
	if err := json.Unmarshal(result.StructuredContent, &listed); err != nil {
		t.Fatalf("%v: %s", err, result.text())
	}
	if len(listed) != 1 || listed[0].ID != visible.ID || listed[0].QueryType != "logchefql" {
		t.Fatalf("listed = %+v", listed)
	}
	result = callTool(t, s, session(w.member), "list_saved_queries", map[string]any{"source_id": w.otherSource.ID})
	if err := json.Unmarshal(result.StructuredContent, &listed); err != nil || len(listed) != 0 {
		t.Fatalf("filtered list on hidden source = %+v, %v", listed, err)
	}
	if result := callTool(t, s, session(w.member), "get_saved_query", map[string]any{"query_id": hidden.ID}); !result.IsError || !strings.Contains(result.text(), "not found") {
		t.Fatalf("hidden saved query: %s", result.text())
	}
	if result := callTool(t, s, session(w.member), "get_saved_query", map[string]any{"query_id": visible.ID}); result.IsError {
		t.Fatalf("visible saved query: %s", result.text())
	}

	var contents struct {
		Contents []struct {
			Text string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(rpc(t, s, session(w.member), "resources/read", map[string]any{"uri": "logchef://source/" + strconv.Itoa(int(w.otherSource.ID)) + "/saved-queries"}), &contents); err != nil {
		t.Fatal(err)
	}
	if len(contents.Contents) != 1 || strings.TrimSpace(contents.Contents[0].Text) != "[]" {
		t.Fatalf("saved-queries resource on hidden source = %+v", contents)
	}
	if _, rpcErr := rpcRaw(t, s, session(w.member), "resources/read", map[string]any{"uri": "logchef://saved-query/" + strconv.Itoa(hidden.ID)}); len(rpcErr) == 0 {
		t.Fatal("saved-query resource returned a hidden query")
	}
	if _, rpcErr := rpcRaw(t, s, token(w.member, models.TokenScopeLogsRead), "resources/read", map[string]any{"uri": "logchef://saved-query/" + strconv.Itoa(visible.ID)}); len(rpcErr) == 0 {
		t.Fatal("saved-query resource ignored the saved_queries:read scope")
	}

	if result := callTool(t, s, session(w.member), "list_alerts", map[string]any{"source_id": w.otherSource.ID}); !result.IsError || !strings.Contains(result.text(), "no team you belong to") {
		t.Fatalf("alerts of a hidden source: %s", result.text())
	}
	w.cfg.Alerts.Enabled = false
	if result := callTool(t, s, session(w.member), "list_alerts", map[string]any{}); !result.IsError || !strings.Contains(result.text(), "disabled") {
		t.Fatalf("alerts while disabled: %s", result.text())
	}
}

// T-MCP-7: get_profile returns the same opaque id for the same user across
// credentials, and the tool carries the OpenAI profile marker.
func TestGetProfileStableID(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := w.server()

	profile := func(p *access.Principal) ProfileResult {
		t.Helper()
		result := callTool(t, s, p, "get_profile", map[string]any{})
		var out ProfileResult
		if err := json.Unmarshal(result.StructuredContent, &out); err != nil || result.IsError {
			t.Fatalf("get_profile: %v %s", err, result.text())
		}
		return out
	}
	first := profile(session(w.member))
	again := profile(token(w.member, models.TokenScopeProfileRead))
	if first.ID == "" || strings.TrimSpace(first.ID) != first.ID || first.ID != again.ID {
		t.Fatalf("profile ids differ or are blank: %q %q", first.ID, again.ID)
	}
	if first.Email != w.member.Email || first.Name != w.member.FullName {
		t.Fatalf("profile = %+v", first)
	}
	if other := profile(session(w.outsider)); other.ID == first.ID {
		t.Fatal("two users share a profile id")
	}

	// A deleted user's ID is never given to a new user.
	ctx := context.Background()
	newest := w.user(t, "newest@example.com", models.UserRoleMember)
	if err := w.db.DeleteUser(ctx, newest.ID); err != nil {
		t.Fatal(err)
	}
	replacement := w.user(t, "replacement@example.com", models.UserRoleMember)
	if replacement.ID == newest.ID {
		t.Fatalf("user id %d was reused", newest.ID)
	}

	var listed struct {
		Tools []struct {
			Name         string         `json:"name"`
			Meta         map[string]any `json:"_meta"`
			OutputSchema struct {
				Required             []string `json:"required"`
				AdditionalProperties *bool    `json:"additionalProperties"`
			} `json:"outputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(rpc(t, s, session(w.member), "tools/list", map[string]any{}), &listed); err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name != "get_profile" {
			continue
		}
		if tool.Meta["openai/profile"] != true {
			t.Fatalf("get_profile _meta = %v", tool.Meta)
		}
		if !slices.Equal(tool.OutputSchema.Required, []string{"id"}) {
			t.Fatalf("get_profile output schema required = %v", tool.OutputSchema.Required)
		}
		if tool.OutputSchema.AdditionalProperties == nil || *tool.OutputSchema.AdditionalProperties {
			t.Fatal("get_profile output schema allows additional properties")
		}
		return
	}
	t.Fatal("get_profile not listed")
}

// T-MCP-4: a tool call ends at the configured maximum query timeout, its
// datasource request is cancelled, and its admission slot is released. The
// VictoriaLogs stand-in answers health checks and never answers a query.
func TestToolCallEndsAtQueryTimeout(t *testing.T) {
	t.Parallel()
	queryCancelled := make(chan struct{})
	var once sync.Once
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			rw.WriteHeader(http.StatusOK)
			return
		}
		// The server sees a client disconnect only after the body is read.
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
		once.Do(func() { close(queryCancelled) })
	}))
	t.Cleanup(backend.Close)

	w := newWorld(t)
	w.cfg.Query.MaxTimeoutSeconds = 1
	w.cfg.Query.DefaultTimeoutSeconds = 1
	source := w.victoriaLogsSource(t, backend.URL)
	s := w.server()

	started := time.Now()
	result := callTool(t, s, session(w.member), "query_logs", map[string]any{
		"team_id": w.team, "source_id": source.ID, "raw_sql": "*",
		"start_time": "2026-10-01T00:00:00Z", "end_time": "2026-10-01T01:00:00Z",
	})
	elapsed := time.Since(started)
	if !result.IsError || !strings.Contains(result.text(), "timed out") {
		t.Fatalf("query_logs = %s, want a timeout", result.text())
	}
	if elapsed > 3*time.Second {
		t.Fatalf("query_logs took %s with a 1 s timeout", elapsed)
	}
	select {
	case <-queryCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("the backend request was not cancelled")
	}
	if active, admitted := w.admits.counts(); active != 0 || admitted != 1 {
		t.Fatalf("admission active=%d admitted=%d, want 0 and 1", active, admitted)
	}

	// core.GetAllFieldValues sets no deadline of its own, so only the
	// whole-call deadline can end this call.
	started = time.Now()
	result = callTool(t, s, session(w.member), "get_all_field_dimensions", map[string]any{
		"team_id": w.team, "source_id": source.ID, "start_time": "2026-10-01T00:00:00Z", "end_time": "2026-10-01T01:00:00Z",
	})
	if elapsed := time.Since(started); !result.IsError || elapsed > 3*time.Second {
		t.Fatalf("get_all_field_dimensions = %q after %s, want a timeout within 3 s", result.text(), elapsed)
	}
}

// A ClickHouse that accepts connections and never answers held tool calls
// for the driver's 10 s dial timeout, whatever the query deadline, because
// the clickhouse-go handshake ignored the context.
func TestToolCallEndsAtQueryTimeoutOnSilentClickHouse(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
		}
	}()

	w := newWorld(t)
	w.cfg.Query.MaxTimeoutSeconds = 1
	w.cfg.Query.DefaultTimeoutSeconds = 1
	log := discardLogger()
	source := &models.Source{
		Name: "silent", SourceType: models.SourceTypeClickHouse, MetaTSField: "timestamp",
		Connection: models.ConnectionInfo{Host: ln.Addr().String(), Username: "default", Database: "default", TableName: "http"},
	}
	if err := w.db.CreateSource(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	w.link(t, w.team, source.ID)
	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })
	if err := manager.AddSource(context.Background(), source); err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	w.ds = datasource.NewService(w.db, log)
	w.ds.Register(datasource.NewClickHouseProvider(manager, log))
	s := w.server()

	for name, args := range map[string]map[string]any{
		"query_logs":        {"raw_sql": "SELECT 1"},
		"query_logchefql":   {"query": "", "start_time": "2026-10-01T00:00:00Z", "end_time": "2026-10-01T01:00:00Z"},
		"get_source_schema": {},
	} {
		args["team_id"] = w.team
		args["source_id"] = source.ID
		started := time.Now()
		result := callTool(t, s, session(w.member), name, args)
		if elapsed := time.Since(started); !result.IsError || elapsed > 3*time.Second {
			t.Errorf("%s = %q after %s, want an error within 3 s", name, result.text(), elapsed)
		}
	}
}

// A metadata-store failure after authorization (here the store closes while
// the query is admitted) must not reach the caller; its details are logged.
func TestStoreFailureAfterAuthorizationIsInternalError(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) { rw.WriteHeader(http.StatusOK) }))
	t.Cleanup(backend.Close)
	w := newWorld(t)
	source := w.victoriaLogsSource(t, backend.URL)
	var logs bytes.Buffer
	deps := w.deps()
	deps.Log = slog.New(slog.NewTextHandler(&logs, nil))
	deps.Admit = func(_ QueryClass, _ access.AuthorizedSource, _ string, _ context.CancelFunc) (string, func(), error) {
		if err := w.db.Close(); err != nil {
			t.Errorf("closing the store: %v", err)
		}
		return "q-closed", func() {}, nil
	}
	result := callTool(t, newMCPServer(deps), session(w.member), "query_logs", map[string]any{
		"team_id": w.team, "source_id": source.ID, "raw_sql": "*",
	})
	if !result.IsError || result.text() != "query logs: internal error" {
		t.Fatalf("query_logs = %q, want only %q", result.text(), "query logs: internal error")
	}
	if !strings.Contains(logs.String(), "sql:") {
		t.Fatalf("the store error was not logged: %s", logs.String())
	}
}

func TestRefusedAdmissionDoesNotRunQuery(t *testing.T) {
	t.Parallel()
	queried := make(chan struct{}, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			queried <- struct{}{}
		}
		rw.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(backend.Close)

	w := newWorld(t)
	w.admits.refuse = true
	source := w.victoriaLogsSource(t, backend.URL)
	result := callTool(t, w.server(), session(w.member), "query_logchefql", map[string]any{
		"team_id": w.team, "source_id": source.ID, "query": "", "start_time": "2026-10-01T00:00:00Z", "end_time": "2026-10-01T01:00:00Z",
	})
	if !result.IsError || !strings.Contains(result.text(), "too many active preview queries") {
		t.Fatalf("query_logchefql = %s, want the admission refusal", result.text())
	}
	select {
	case <-queried:
		t.Fatal("a refused query reached the backend")
	default:
	}
}

// victoriaLogsSource adds a VictoriaLogs source at baseURL to the member's team.
func (w *world) victoriaLogsSource(t *testing.T, baseURL string) *models.Source {
	t.Helper()
	ctx := context.Background()
	conn, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: baseURL})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "vl", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: conn}
	if err := w.db.CreateSource(ctx, source); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	w.link(t, w.team, source.ID)
	if err := w.ds.InitializeSource(ctx, source); err != nil {
		t.Fatalf("InitializeSource: %v", err)
	}
	return source
}

func TestOpenInvestigationRange(t *testing.T) {
	ctx := context.Background()
	for _, params := range []OpenInvestigationParams{
		{TeamID: 1}, {SourceID: -1}, {StartTime: "invalid"},
		{StartTime: "2026-10-01T02:00:00Z", EndTime: "2026-10-01T01:00:00Z"},
		{StartTime: "2026-09-01T00:00:00Z", EndTime: "2026-10-01T00:00:00Z"},
	} {
		if _, err := handleOpenInvestigation(ctx, mcp.CallToolRequest{}, params); err == nil {
			t.Fatalf("accepted invalid state: %+v", params)
		}
	}
	state, err := handleOpenInvestigation(ctx, mcp.CallToolRequest{}, OpenInvestigationParams{TeamID: 1, SourceID: 4, StartTime: "2026-10-01T10:30:00+05:30", EndTime: "2026-10-01T06:00:00Z"})
	if err != nil || state.StartTime != "2026-10-01T05:00:00Z" || state.Timezone != "UTC" {
		t.Fatalf("state = %+v, %v", state, err)
	}
}

func TestInvestigationExtensionContract(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	s := w.server()
	type appTool struct {
		Name string `json:"name"`
		Meta struct {
			UI struct {
				ResourceURI string `json:"resourceUri"`
			} `json:"ui"`
			OpenAI struct {
				Entrypoints []struct {
					Type string `json:"type"`
				} `json:"entrypoints"`
			} `json:"openai/ui"`
		} `json:"_meta"`
	}
	var listed struct {
		Tools []appTool `json:"tools"`
	}
	if err := json.Unmarshal(rpc(t, s, token(w.member, models.TokenScopeLogsRead), "tools/list", map[string]any{}), &listed); err != nil {
		t.Fatal(err)
	}
	index := slices.IndexFunc(listed.Tools, func(tool appTool) bool {
		return tool.Name == "open_investigation"
	})
	if index < 0 {
		t.Fatal("open_investigation not listed")
	}
	tool := listed.Tools[index]
	if tool.Meta.UI.ResourceURI != InvestigationURI {
		t.Fatal("invalid UI tool association")
	}
	entrypoints := tool.Meta.OpenAI.Entrypoints
	if len(entrypoints) != 2 || entrypoints[0].Type != "global" || entrypoints[1].Type != "thread" {
		t.Fatalf("invalid entrypoints: %+v", entrypoints)
	}
	var resource struct {
		Contents []struct {
			URI      string `json:"uri"`
			MIMEType string `json:"mimeType"`
			Text     string `json:"text"`
			Meta     struct {
				OpenAI struct {
					Modes []string `json:"availableDisplayModes"`
				} `json:"openai/ui"`
				UI struct {
					CSP struct {
						ConnectDomains  []string `json:"connectDomains"`
						ResourceDomains []string `json:"resourceDomains"`
					} `json:"csp"`
				} `json:"ui"`
			} `json:"_meta"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(rpc(t, s, token(w.member, models.TokenScopeLogsRead), "resources/read", map[string]any{"uri": InvestigationURI}), &resource); err != nil {
		t.Fatal(err)
	}
	if len(resource.Contents) != 1 {
		t.Fatal("missing UI resource")
	}
	content := resource.Contents[0]
	if content.URI != InvestigationURI || content.MIMEType != appMIMEType || !strings.Contains(content.Text, "Investigate logs") {
		t.Fatal("invalid UI resource")
	}
	if content.Meta.UI.CSP.ConnectDomains == nil || len(content.Meta.UI.CSP.ConnectDomains) != 0 || content.Meta.UI.CSP.ResourceDomains == nil || len(content.Meta.UI.CSP.ResourceDomains) != 0 {
		t.Fatalf("UI CSP is not deny-all: %+v", content.Meta.UI.CSP)
	}
	if strings.Contains(content.Text, w.member.Email) {
		t.Fatal("UI resource contains user identity")
	}
	modes := content.Meta.OpenAI.Modes
	if len(modes) != 2 || modes[0] != "inline" || modes[1] != "fullscreen" {
		t.Fatalf("invalid display modes: %v", modes)
	}
}

// fieldBackend is a VictoriaLogs stand-in that knows fields f0..f29 and
// records the field value queries it receives.
type fieldBackend struct {
	mu       sync.Mutex
	inFlight int
	peak     int
	queried  map[string]int
}

func newFieldBackend(t *testing.T) (backend *fieldBackend, url string) {
	t.Helper()
	b := &fieldBackend{queried: map[string]int{}}
	stub := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/health":
			rw.WriteHeader(http.StatusOK)
		case "/select/logsql/field_names":
			names := make([]string, 30)
			for i := range names {
				names[i] = `{"value":"f` + strconv.Itoa(i) + `","hits":1}`
			}
			_, _ = rw.Write([]byte(`{"values":[` + strings.Join(names, ",") + `]}`))
		case "/select/logsql/field_values":
			b.mu.Lock()
			b.inFlight++
			b.peak = max(b.peak, b.inFlight)
			b.queried[r.Form.Get("field")]++
			b.mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			b.mu.Lock()
			b.inFlight--
			b.mu.Unlock()
			_, _ = rw.Write([]byte(`{"values":[{"value":"v","hits":3}]}`))
		default:
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(stub.Close)
	return b, stub.URL
}

func (b *fieldBackend) stats() (peak int, queried map[string]int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.peak, maps.Clone(b.queried)
}

func TestTopValuesBoundsItsFanOut(t *testing.T) {
	t.Parallel()
	topValues := func(t *testing.T, w *world, source *models.Source, fields []string) toolResult {
		t.Helper()
		return callTool(t, w.server(), session(w.member), "top_values", map[string]any{
			"team_id": w.team, "source_id": source.ID, "fields": fields,
			"start_time": "2026-10-01T00:00:00Z", "end_time": "2026-10-01T01:00:00Z",
		})
	}

	t.Run("duplicates are queried once", func(t *testing.T) {
		t.Parallel()
		backend, url := newFieldBackend(t)
		w := newWorld(t)
		source := w.victoriaLogsSource(t, url)
		result := topValues(t, w, source, []string{"f1", "f1", "f2", "f1", "f2"})
		var out TopValuesResult
		if err := json.Unmarshal(result.StructuredContent, &out); err != nil || result.IsError {
			t.Fatalf("top_values: %v %s", err, result.text())
		}
		if len(out.Fields) != 2 || out.Fields[0].FieldName != "f1" || out.Fields[1].FieldName != "f2" || len(out.Fields[0].Values) != 1 {
			t.Fatalf("fields = %+v", out.Fields)
		}
		if _, queried := backend.stats(); queried["f1"] != 1 || queried["f2"] != 1 {
			t.Fatalf("field queries = %v, want one each", queried)
		}
		if active, admitted := w.admits.counts(); active != 0 || admitted != 1 {
			t.Fatalf("admission active=%d admitted=%d, want one admission for the call", active, admitted)
		}
	})

	t.Run("concurrency is bounded", func(t *testing.T) {
		t.Parallel()
		backend, url := newFieldBackend(t)
		w := newWorld(t)
		source := w.victoriaLogsSource(t, url)
		fields := make([]string, 12)
		for i := range fields {
			fields[i] = "f" + strconv.Itoa(i)
		}
		if result := topValues(t, w, source, fields); result.IsError {
			t.Fatalf("top_values: %s", result.text())
		}
		peak, queried := backend.stats()
		if len(queried) != 12 || peak > topValuesWorkers || peak < 2 {
			t.Fatalf("queried %d fields with peak concurrency %d, want 12 with 2..%d", len(queried), peak, topValuesWorkers)
		}
	})

	t.Run("too many fields are rejected before backend work", func(t *testing.T) {
		t.Parallel()
		backend, url := newFieldBackend(t)
		w := newWorld(t)
		source := w.victoriaLogsSource(t, url)
		fields := make([]string, maxTopValuesFields+1)
		for i := range fields {
			fields[i] = "f" + strconv.Itoa(i)
		}
		result := topValues(t, w, source, fields)
		if !result.IsError || !strings.Contains(result.text(), "at most") {
			t.Fatalf("top_values = %s, want the field cap", result.text())
		}
		if _, queried := backend.stats(); len(queried) != 0 {
			t.Fatalf("field queries ran: %v", queried)
		}
		if _, admitted := w.admits.counts(); admitted != 0 {
			t.Fatal("a rejected call was admitted")
		}
	})

	t.Run("a refused admission runs no backend work", func(t *testing.T) {
		t.Parallel()
		backend, url := newFieldBackend(t)
		w := newWorld(t)
		w.admits.refuse = true
		source := w.victoriaLogsSource(t, url)
		result := topValues(t, w, source, []string{"f1", "f2"})
		if !result.IsError || !strings.Contains(result.text(), "too many active preview queries") {
			t.Fatalf("top_values = %s, want the admission refusal", result.text())
		}
		if _, queried := backend.stats(); len(queried) != 0 {
			t.Fatalf("field queries ran after a refused admission: %v", queried)
		}
	})
}
