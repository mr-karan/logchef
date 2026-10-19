package askchef_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/askchef"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/mcp"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

// scriptedChat is the model: it replays one response per call and records
// every request.
type scriptedChat struct {
	mu       sync.Mutex
	replies  []ai.ChatResponse
	err      error
	requests []ai.ChatRequest
}

func (c *scriptedChat) Stream(_ context.Context, req ai.ChatRequest, onText func(string)) (ai.ChatResponse, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	if c.err != nil {
		c.mu.Unlock()
		return ai.ChatResponse{}, c.err
	}
	reply := ai.ChatResponse{Blocks: []ai.ChatBlock{ai.TextBlock{Text: "No more script."}}}
	if len(c.replies) > 0 {
		reply, c.replies = c.replies[0], c.replies[1:]
	}
	c.mu.Unlock()
	for _, block := range reply.Blocks {
		if text, ok := block.(ai.TextBlock); ok {
			onText(text.Text)
		}
	}
	return reply, nil
}

func (c *scriptedChat) request(i int) ai.ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests[i]
}

func reply(blocks ...ai.ChatBlock) ai.ChatResponse {
	return ai.ChatResponse{Blocks: blocks, Usage: ai.ChatUsage{InputTokens: 100, OutputTokens: 10}}
}

func toolUse(id, name string, input map[string]any) ai.ToolUseBlock {
	encoded, err := json.Marshal(input)
	if err != nil {
		panic(err)
	}
	return ai.ToolUseBlock{ID: id, Name: name, Input: encoded}
}

// logsBackend is a VictoriaLogs stand-in. Field values succeed and log
// queries fail.
type logsBackend struct {
	fieldQueries atomic.Int32
	url          string
}

func newLogsBackend(t *testing.T) *logsBackend {
	t.Helper()
	b := &logsBackend{}
	stub := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			rw.WriteHeader(http.StatusOK)
		case "/select/logsql/field_values":
			b.fieldQueries.Add(1)
			_ = r.ParseForm()
			if r.Form.Get("field") == "message" {
				values := make([]string, 100)
				for i := range values {
					values[i] = `{"value":"` + strings.Repeat("long log line ", 20) + `","hits":1}`
				}
				_, _ = rw.Write([]byte(`{"values":[` + strings.Join(values, ",") + `]}`))
				return
			}
			_, _ = rw.Write([]byte(`{"values":[{"value":"api","hits":7}]}`))
		default:
			http.Error(rw, "backend unavailable", http.StatusBadGateway)
		}
	}))
	t.Cleanup(stub.Close)
	b.url = stub.URL
	return b
}

type world struct {
	db      *sqlite.DB
	backend *logsBackend
	tools   *mcp.Deps
	member  *models.User
	admin   *models.User
	team    models.TeamID
	source  models.SourceID
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	log := discardLogger()
	db, err := sqlite.New(ctx, sqlite.Options{Logger: log, Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "askchef.db")}})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	user := func(email string, role models.UserRole) *models.User {
		u := &models.User{Email: email, FullName: email, Role: role, Status: models.UserStatusActive}
		if err := db.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		return u
	}
	w := &world{db: db, backend: newLogsBackend(t), member: user("member@example.com", models.UserRoleMember), admin: user("admin@example.com", models.UserRoleAdmin)}

	team := &models.Team{Name: "payments"}
	if err := db.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	w.team = team.ID
	if err := db.AddTeamMember(ctx, w.team, w.member.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}

	ds := datasource.NewService(db, log)
	manager := clickhouse.NewManager(log)
	t.Cleanup(func() { _ = manager.Close() })
	ds.Register(datasource.NewClickHouseProvider(manager, log))
	ds.Register(victorialogs.NewProvider(log))
	conn, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: w.backend.url})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "payments-logs", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: conn}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if err := db.AddTeamSource(ctx, w.team, source.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	if err := ds.InitializeSource(ctx, source); err != nil {
		t.Fatalf("InitializeSource: %v", err)
	}
	w.source = source.ID

	w.tools = &mcp.Deps{
		DB:          db,
		Datasources: ds,
		Config: &config.Config{
			Query:  config.QueryConfig{DefaultPreviewLimit: 100, MaxPreviewLimit: 500, DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 60, MCPCallTimeoutSeconds: 60},
			Alerts: config.AlertsConfig{Enabled: true},
		},
		Version: "test",
		Log:     log,
		Admit: func(_ mcp.QueryClass, _ access.AuthorizedSource, _ string, _ context.CancelFunc) (string, func(), error) {
			return "q-test", func() {}, nil
		},
	}
	return w
}

func (w *world) fieldValuesInput() map[string]any {
	return map[string]any{
		"team_id": w.team, "source_id": w.source, "field_name": "service", "field_type": "String",
		"start_time": "2026-10-19T04:00:00Z", "end_time": "2026-10-19T05:00:00Z",
	}
}

func (w *world) request(prior ...askchef.Evidence) askchef.Request {
	return askchef.Request{
		TeamID:   w.team,
		SourceID: w.source,
		Location: time.UTC,
		View:     askchef.View{Mode: askchef.ModeLogchefQL, Query: `level="error"`, Start: time.UnixMilli(1789460000000), End: time.UnixMilli(1789463600000)},
		Evidence: prior,
		Message:  "Why are there errors?",
	}
}

// run runs one turn and returns its events.
func (w *world) run(t *testing.T, chat askchef.ChatModel, p access.Principal, req askchef.Request) []askchef.Event {
	t.Helper()
	agent := askchef.New(askchef.Opts{Tools: mcp.NewToolServer(*w.tools), Chat: chat, Model: "test-model", Log: discardLogger()})
	var events []askchef.Event
	agent.Run(t.Context(), askchef.Turn{Principal: p, Request: req}, func(ev askchef.Event) bool {
		events = append(events, ev)
		return true
	})
	return events
}

func eventNames(events []askchef.Event) []string {
	names := make([]string, len(events))
	for i, ev := range events {
		names[i] = ev.Name()
	}
	return names
}

func toolEnds(events []askchef.Event) []askchef.ToolEnd {
	var ends []askchef.ToolEnd
	for _, ev := range events {
		if end, ok := ev.(askchef.ToolEnd); ok {
			ends = append(ends, end)
		}
	}
	return ends
}

// toolResults returns the tool results the model received in request i.
func toolResults(req ai.ChatRequest) []ai.ToolResultBlock {
	last := req.Messages[len(req.Messages)-1]
	var results []ai.ToolResultBlock
	for _, block := range last.Blocks {
		if result, ok := block.(ai.ToolResultBlock); ok {
			results = append(results, result)
		}
	}
	return results
}

func TestTurnRunsToolsAndStreamsEvidence(t *testing.T) {
	w := newWorld(t)
	chat := &scriptedChat{replies: []ai.ChatResponse{
		reply(ai.TextBlock{Text: "Checking services."},
			toolUse("tu-1", "get_field_values", w.fieldValuesInput()),
			toolUse("tu-2", "get_field_values", w.fieldValuesInput())),
		reply(ai.TextBlock{Text: "The errors come from api [E4]."}),
	}}
	prior := []askchef.Evidence{
		{ID: 1, Tool: "query_logchefql", TeamID: w.team, SourceID: w.source, Mode: askchef.ModeLogchefQL, StartMS: 1, EndMS: 2, Summary: "3 rows"},
		{ID: 3, Tool: "compare_windows", TeamID: w.team, SourceID: w.source, Mode: askchef.ModeLogchefQL, StartMS: 1, EndMS: 2, Summary: "3 rows vs 23 rows"},
	}

	events := w.run(t, chat, access.AssistantPrincipal(w.member), w.request(prior...))

	wantNames := []string{"text", "tool_start", "tool_end", "tool_start", "tool_end", "text", "done"}
	if got := eventNames(events); !slices.Equal(got, wantNames) {
		t.Fatalf("events = %v, want %v", got, wantNames)
	}
	if start := events[1].(askchef.ToolStart); start != (askchef.ToolStart{CallID: "c1", Tool: "get_field_values"}) {
		t.Fatalf("first tool_start = %+v", start)
	}
	window := askchef.Evidence{
		Tool: "get_field_values", TeamID: w.team, SourceID: w.source, Mode: askchef.ModeLogchefQL,
		StartMS: time.Date(2026, 10, 19, 4, 0, 0, 0, time.UTC).UnixMilli(),
		EndMS:   time.Date(2026, 10, 19, 5, 0, 0, 0, time.UTC).UnixMilli(),
		Summary: "done",
	}
	for i, end := range toolEnds(events) {
		want := window
		want.ID = askchef.EvidenceID(4 + i)
		if !end.OK || end.Evidence == nil || *end.Evidence != want {
			t.Fatalf("tool_end %d = %+v evidence %+v, want ok with %+v", i, end, end.Evidence, want)
		}
	}
	done := events[len(events)-1].(askchef.Done)
	if done.StopReason != askchef.StopEnd || done.ToolCalls != 2 || done.InputTokens != 200 || done.OutputTokens != 20 {
		t.Fatalf("done = %+v", done)
	}

	results := toolResults(chat.request(1))
	if len(results) != 2 || !strings.HasPrefix(results[0].Text, "[evidence E4]\n") || !strings.HasPrefix(results[1].Text, "[evidence E5]\n") {
		t.Fatalf("tool results sent to the model = %+v", results)
	}
	if !strings.Contains(results[0].Text, `"api"`) {
		t.Fatalf("tool result does not carry the backend values: %q", results[0].Text)
	}
	if !strings.Contains(chat.request(0).System, "[E3] compare_windows") {
		t.Fatal("system prompt does not list the prior evidence")
	}
}

func TestTurnCapsLargeToolResults(t *testing.T) {
	w := newWorld(t)
	input := w.fieldValuesInput()
	input["field_name"] = "message"
	input["limit"] = 100
	chat := &scriptedChat{replies: []ai.ChatResponse{reply(toolUse("tu-1", "get_field_values", input))}}
	w.run(t, chat, access.AssistantPrincipal(w.member), w.request())

	results := toolResults(chat.request(1))
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	text := results[0].Text
	if len(text) > 12*1024+100 || !strings.Contains(text, "[result cut: showing the first ") || !strings.HasPrefix(text, "[evidence E1]") {
		t.Fatalf("result sent to the model is %d bytes and ends %q", len(text), text[max(0, len(text)-80):])
	}
}

func TestTurnOffersOnlyAllowedTools(t *testing.T) {
	w := newWorld(t)
	chat := &scriptedChat{}
	w.run(t, chat, access.AssistantPrincipal(w.member), w.request())

	var names []string
	for _, tool := range chat.request(0).Tools {
		names = append(names, tool.Name)
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema["type"] != "object" {
			t.Errorf("tool %s schema = %s", tool.Name, tool.InputSchema)
		}
	}
	for _, want := range []string{"query_logchefql", "compare_windows", "get_field_values", "get_alert_history"} {
		if !slices.Contains(names, want) {
			t.Errorf("tools %v do not include %s", names, want)
		}
	}
	for _, hidden := range []string{"open_investigation", "get_profile", "list_saved_queries"} {
		if slices.Contains(names, hidden) {
			t.Errorf("tools %v include %s", names, hidden)
		}
	}
}

func TestTurnStopsAtToolLimit(t *testing.T) {
	w := newWorld(t)
	batch := func(prefix string) ai.ChatResponse {
		var uses []ai.ChatBlock
		for i := range 5 {
			uses = append(uses, toolUse(prefix+string(rune('a'+i)), "get_sources", map[string]any{}))
		}
		return reply(uses...)
	}
	chat := &scriptedChat{replies: []ai.ChatResponse{
		batch("one-"), batch("two-"), batch("three-"),
		reply(ai.TextBlock{Text: "Here is what I found."}, toolUse("late", "get_sources", map[string]any{})),
	}}

	events := w.run(t, chat, access.AssistantPrincipal(w.member), w.request())

	starts := 0
	for _, ev := range events {
		if _, ok := ev.(askchef.ToolStart); ok {
			starts++
		}
	}
	if starts != 12 {
		t.Fatalf("tool_start events = %d, want 12", starts)
	}
	done, ok := events[len(events)-1].(askchef.Done)
	if !ok || done.StopReason != askchef.StopToolLimit || done.ToolCalls != 12 {
		t.Fatalf("last event = %+v, want done with tool_limit after 12 calls", events[len(events)-1])
	}
	if len(chat.requests) != 4 {
		t.Fatalf("model calls = %d, want 4", len(chat.requests))
	}
	final := chat.request(3)
	results := toolResults(final)
	refused := 0
	for _, r := range results {
		if strings.HasPrefix(r.Text, "[tool not run:") {
			refused++
		}
	}
	last := final.Messages[len(final.Messages)-1].Blocks
	note, ok := last[len(last)-1].(ai.TextBlock)
	if refused != 3 || !ok || !strings.Contains(note.Text, "Do not call tools") {
		t.Fatalf("final request: %d refused results and closing block %#v", refused, last[len(last)-1])
	}
}

func TestTurnReportsToolFailuresToTheModel(t *testing.T) {
	for _, tc := range []struct {
		name  string
		tool  string
		input func(w *world) map[string]any
		want  string
	}{
		{
			name: "backend error",
			tool: "query_logchefql",
			input: func(w *world) map[string]any {
				return map[string]any{"team_id": w.team, "source_id": w.source, "query": `level="error"`, "start_time": "2026-10-19T04:00:00Z", "end_time": "2026-10-19T05:00:00Z"}
			},
			want: "[tool did not complete: ",
		},
		{
			name:  "tool outside the registry",
			tool:  "open_investigation",
			input: func(*world) map[string]any { return map[string]any{} },
			want:  `[tool did not complete: tool "open_investigation" is not available]`,
		},
		{
			name:  "unknown tool",
			tool:  "drop_table",
			input: func(*world) map[string]any { return map[string]any{} },
			want:  `[tool did not complete: tool "drop_table" is not available]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			chat := &scriptedChat{replies: []ai.ChatResponse{
				reply(toolUse("tu-1", tc.tool, tc.input(w))),
				reply(ai.TextBlock{Text: "The query failed, so this is **Not established**."}),
			}}
			events := w.run(t, chat, access.AssistantPrincipal(w.member), w.request())

			ends := toolEnds(events)
			if len(ends) != 1 || ends[0].OK || ends[0].Evidence != nil || ends[0].Summary == "" {
				t.Fatalf("tool_end = %+v, want one failed call with a summary and no evidence", ends)
			}
			results := toolResults(chat.request(1))
			if len(results) != 1 || !results[0].IsError || !strings.HasPrefix(results[0].Text, tc.want) {
				t.Fatalf("result sent to the model = %+v, want prefix %q", results, tc.want)
			}
			if _, ok := events[len(events)-1].(askchef.Done); !ok {
				t.Fatalf("last event = %+v, want done", events[len(events)-1])
			}
		})
	}
}

// A global admin has no bypass through Ask Chef: tools reach only the teams
// the user belongs to.
func TestTurnToolsFollowTeamMembership(t *testing.T) {
	for _, tc := range []struct {
		name   string
		user   func(w *world) *models.User
		wantOK bool
	}{
		{"member", func(w *world) *models.User { return w.member }, true},
		{"global admin outside the team", func(w *world) *models.User { return w.admin }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			chat := &scriptedChat{replies: []ai.ChatResponse{reply(toolUse("tu-1", "get_field_values", w.fieldValuesInput()))}}
			events := w.run(t, chat, access.AssistantPrincipal(tc.user(w)), w.request())

			ends := toolEnds(events)
			if len(ends) != 1 || ends[0].OK != tc.wantOK {
				t.Fatalf("tool_end = %+v, want ok=%v", ends, tc.wantOK)
			}
			if queried := w.backend.fieldQueries.Load() > 0; queried != tc.wantOK {
				t.Fatalf("backend queried = %v, want %v", queried, tc.wantOK)
			}
			if !tc.wantOK && !strings.Contains(ends[0].Summary, "team membership required") {
				t.Fatalf("summary = %q, want the membership error", ends[0].Summary)
			}
		})
	}
}

func TestTurnModelFailureEndsWithError(t *testing.T) {
	w := newWorld(t)
	chat := &scriptedChat{err: errors.New("ThrottlingException: secret upstream detail")}
	events := w.run(t, chat, access.AssistantPrincipal(w.member), w.request())

	if len(events) != 1 {
		t.Fatalf("events = %v, want one error", eventNames(events))
	}
	failure, ok := events[0].(askchef.Failure)
	if !ok || strings.Contains(failure.Message, "secret") || failure.Message == "" {
		t.Fatalf("event = %+v, want a sanitized error", events[0])
	}
}

func TestTurnStopsWhenTheClientIsGone(t *testing.T) {
	w := newWorld(t)
	chat := &scriptedChat{replies: []ai.ChatResponse{
		reply(ai.TextBlock{Text: "Checking."}, toolUse("tu-1", "get_field_values", w.fieldValuesInput())),
		reply(ai.TextBlock{Text: "Never sent."}),
	}}
	agent := askchef.New(askchef.Opts{Tools: mcp.NewToolServer(*w.tools), Chat: chat, Model: "m", Log: discardLogger()})
	var events []askchef.Event
	agent.Run(t.Context(), askchef.Turn{Principal: access.AssistantPrincipal(w.member), Request: w.request()}, func(ev askchef.Event) bool {
		events = append(events, ev)
		return false
	})
	if len(chat.requests) != 1 {
		t.Fatalf("model calls = %d, want the turn to stop after the client left", len(chat.requests))
	}
	if slices.Contains(eventNames(events), "done") {
		t.Fatalf("events = %v, want no done after the client left", eventNames(events))
	}
}
