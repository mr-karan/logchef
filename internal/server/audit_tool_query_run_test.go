package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// auditReadProvider is auditQueryProvider with live tail and log context. A
// context read fails the way query does.
type auditReadProvider struct {
	auditQueryProvider
	tail func(ctx context.Context, emit datasource.TailEmitter) error
}

func newAuditReadProvider(query func(context.Context) error, tail func(context.Context, datasource.TailEmitter) error) *auditReadProvider {
	return &auditReadProvider{
		auditQueryProvider: auditQueryProvider{succeedingQueryProvider: succeedingQueryProvider{sourceType: models.SourceTypeClickHouse}, query: query},
		tail:               tail,
	}
}

func (p *auditReadProvider) Describe() datasource.Descriptor {
	d := p.auditQueryProvider.Describe()
	d.Capabilities = append(d.Capabilities, datasource.CapabilityLiveTail, datasource.CapabilityLogContext)
	return d
}

func (p *auditReadProvider) TailLogs(ctx context.Context, _ *models.Source, _ datasource.TailRequest, emit datasource.TailEmitter) error {
	return p.tail(ctx, emit)
}

func (p *auditReadProvider) GetLogContext(ctx context.Context, _ *models.Source, req datasource.LogContextRequest) (*models.LogContextResponse, error) {
	if p.query != nil {
		if err := p.query(ctx); err != nil {
			return nil, err
		}
	}
	row := []map[string]any{previewTestRow}
	return &models.LogContextResponse{TargetTimestamp: req.TargetTimestamp, BeforeLogs: row, TargetLogs: row, AfterLogs: row}, nil
}

// withTail allows each user one tail with no row ceiling.
func withTail(s *auditEnvSetup) {
	s.opts.Config.Tail = config.TailConfig{PollInterval: time.Second, MaxPerUser: 1, MaxGlobal: 10}
}

// onlyEvent returns the one stored event, or fails.
func onlyEvent(t *testing.T, events []models.AuditEvent) models.AuditEvent {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("stored %d events, want one query.run: %+v", len(events), events)
	}
	return events[0]
}

// checkReadEvent checks what every query.run event of a read names: the
// action and outcome, the source, the request, the mode and the rows.
func (e *queryAuditEnv) checkReadEvent(t *testing.T, ev models.AuditEvent, outcome models.AuditOutcome, reason, route, mode string, rows float64) map[string]any {
	t.Helper()
	if ev.Action != string(audit.QueryRun) || ev.Outcome != outcome || ev.Reason != reason {
		t.Errorf("event %s %s %q, want query.run %s %q", ev.Action, ev.Outcome, ev.Reason, outcome, reason)
	}
	if ev.TargetLabel != e.sourceName || ev.SourceID == nil || *ev.SourceID != e.sourceID || ev.TeamID == nil || *ev.TeamID != e.teamID {
		t.Errorf("target label %q source %v team %v", ev.TargetLabel, ev.SourceID, ev.TeamID)
	}
	if ev.Route != route || ev.Status != http.StatusOK && outcome == models.AuditOutcomeSuccess {
		t.Errorf("request %s %s %d, want route %s", ev.HTTPMethod, ev.Route, ev.Status, route)
	}
	d := detailOf(t, ev)
	if d["mode"] != mode || d["rows_returned"] != rows {
		t.Errorf("mode %v rows %v, want %s %v", d["mode"], d["rows_returned"], mode, rows)
	}
	if got := e.historyRows(); got != 0 {
		t.Errorf("history rows %d, want none", got)
	}
	return d
}

// A log context request records one event with its anchor, the rows it asked
// for around it and the rows it returned.
func TestAuditQueryRunRecordsLogContext(t *testing.T) {
	t.Parallel()
	const anchor = 1790850000000
	body := fmt.Sprintf(`{"timestamp":%d,"before_limit":5,"after_limit":7}`, anchor)
	route := teamSourcePrefix + "/logs/context"

	t.Run("success", func(t *testing.T) {
		t.Parallel()
		e := newQueryAuditEnvWith(t, newAuditReadProvider(nil, nil))
		wantStatus(t, e.do(http.MethodPost, e.base+"/logs/context", body, e.cookie), http.StatusOK)
		ev := onlyEvent(t, e.settledEvents())
		checkActorAndTarget(t, ev, string(audit.QueryRun), e.admin, "source", strconv.Itoa(int(e.sourceID)))
		d := e.checkReadEvent(t, ev, models.AuditOutcomeSuccess, "", route, "context", 3)
		if d["anchor"] != time.UnixMilli(anchor).UTC().Format(time.RFC3339Nano) || d["limit_requested"] != float64(12) {
			t.Errorf("anchor %v limit %v", d["anchor"], d["limit_requested"])
		}
	})

	t.Run("failure", func(t *testing.T) {
		t.Parallel()
		e := newQueryAuditEnvWith(t, newAuditReadProvider(failWith(&datasource.UpstreamError{Message: "boom"}), nil))
		wantStatus(t, e.do(http.MethodPost, e.base+"/logs/context", body, e.cookie), http.StatusBadGateway)
		ev := onlyEvent(t, e.settledEvents())
		e.checkReadEvent(t, ev, models.AuditOutcomeFailure, "upstream_error", route, "context", 0)
		if ev.Status != http.StatusBadGateway {
			t.Errorf("status %d, want 502", ev.Status)
		}
	})
}

// tailClient is a live tail read over a real connection, so the test can
// disconnect it.
type tailClient struct {
	conn net.Conn
	r    *bufio.Reader
}

func (e *queryAuditEnv) openTail(t *testing.T, query string) *tailClient {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = e.s.app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	fmt.Fprintf(conn, "GET %s/logs/tail?query=%s HTTP/1.1\r\nHost: logchef\r\nCookie: %s=%s\r\n\r\n", e.base, url.QueryEscape(query), e.cookie.Name, e.cookie.Value)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("tail status %d", resp.StatusCode)
	}
	return &tailClient{conn: conn, r: bufio.NewReader(resp.Body)}
}

// waitFor reads frames until one of event arrives and returns its data.
func (c *tailClient) waitFor(t *testing.T, event string) string {
	t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		line, err := c.r.ReadString('\n')
		if err != nil {
			t.Fatalf("waiting for %s: %v", event, err)
		}
		if strings.TrimSpace(line) != "event: "+event {
			continue
		}
		data, err := c.r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimPrefix(strings.TrimSpace(data), "data: ")
	}
}

// A tail session is one event, recorded when its client disconnects, with
// its filter and the rows it streamed.
func TestAuditQueryRunRecordsTailEndingAtDisconnect(t *testing.T) {
	t.Parallel()
	e := newQueryAuditEnvWith(t, newAuditReadProvider(nil, func(ctx context.Context, emit datasource.TailEmitter) error {
		if err := emit([]map[string]any{previewTestRow, previewTestRow}); err != nil {
			return err
		}
		if err := emit([]map[string]any{previewTestRow}); err != nil {
			return err
		}
		<-ctx.Done()
		return ctx.Err()
	}), withTail)
	client := e.openTail(t, `level="error"`)
	client.waitFor(t, "rows")
	client.waitFor(t, "rows")
	_ = client.conn.Close()

	ev := onlyEvent(t, e.settledEvents())
	checkActorAndTarget(t, ev, string(audit.QueryRun), e.admin, "source", strconv.Itoa(int(e.sourceID)))
	d := e.checkReadEvent(t, ev, models.AuditOutcomeSuccess, "", teamSourcePrefix+"/logs/tail", "tail", 3)
	if d["query"] != `level="error"` || d["language"] != string(models.QueryLanguageLogchefQL) {
		t.Errorf("query %v language %v", d["query"], d["language"])
	}
	if id, _ := d["query_id"].(string); id == "" {
		t.Errorf("query_id missing: %v", d)
	}
}

// A tail that the provider ends with an error is a failed event with the
// rows sent before it.
func TestAuditQueryRunRecordsTailFailure(t *testing.T) {
	t.Parallel()
	e := newQueryAuditEnvWith(t, newAuditReadProvider(nil, func(_ context.Context, emit datasource.TailEmitter) error {
		if err := emit([]map[string]any{previewTestRow}); err != nil {
			return err
		}
		return &datasource.UpstreamError{Message: "boom"}
	}), withTail)
	client := e.openTail(t, "")
	if end := client.waitFor(t, "end"); !strings.Contains(end, `"reason":"error"`) {
		t.Fatalf("end frame %s, want an error", end)
	}
	e.checkReadEvent(t, onlyEvent(t, e.settledEvents()), models.AuditOutcomeFailure, "upstream_error", teamSourcePrefix+"/logs/tail", "tail", 1)
}

// A tail refused before it streams, here by the per-user cap, is a failed
// event with the refusal's status.
func TestAuditQueryRunRecordsRefusedTail(t *testing.T) {
	// Not parallel: the query tracker is process-global and every test DB
	// starts user IDs at 1, so admission tests must not overlap.
	e := newQueryAuditEnvWith(t, newAuditReadProvider(nil, nil), withTail)
	held, err := queryTracker.StartQuery(QueryClassTail, e.admin.ID, e.sourceID, e.teamID, "held", func() {}, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	resp := e.do(http.MethodGet, e.base+"/logs/tail?query=x", "", e.cookie)
	queryTracker.RemoveQuery(held)
	wantStatus(t, resp, http.StatusTooManyRequests)
	ev := onlyEvent(t, e.settledEvents())
	if ev.Outcome != models.AuditOutcomeFailure || ev.Reason != "rejected" || ev.Status != http.StatusTooManyRequests {
		t.Errorf("event %s %q status %d, want failure rejected 429", ev.Outcome, ev.Reason, ev.Status)
	}
	if d := detailOf(t, ev); d["mode"] != "tail" || d["query"] != "x" {
		t.Errorf("detail %v", d)
	}
}

// toolCallingChat is a model that asks for one tool call, then answers.
type toolCallingChat struct {
	tool  string
	input string
}

func (c *toolCallingChat) Stream(_ context.Context, req ai.ChatRequest, onText func(string)) (ai.ChatResponse, error) {
	if len(req.Messages) == 1 {
		return ai.ChatResponse{Blocks: []ai.ChatBlock{ai.ToolUseBlock{ID: "call-1", Name: c.tool, Input: json.RawMessage(c.input)}}}, nil
	}
	onText("Done.")
	return ai.ChatResponse{Blocks: []ai.ChatBlock{ai.TextBlock{Text: "Done."}}}, nil
}

// An Ask Chef tool call that reads logs records one event that names the user
// who asked and the credential of their request.
func TestAuditQueryRunRecordsAskChefToolCalls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		query   func(context.Context) error
		outcome models.AuditOutcome
		reason  string
		rows    float64
	}{
		{"success", nil, models.AuditOutcomeSuccess, "", 1},
		{"failure", failWith(&datasource.UpstreamError{Message: "boom"}), models.AuditOutcomeFailure, "upstream_error", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var e *queryAuditEnv
			chat := &toolCallingChat{tool: "query_logchefql"}
			e = newQueryAuditEnvWith(t, &auditQueryProvider{succeedingQueryProvider: succeedingQueryProvider{sourceType: models.SourceTypeClickHouse}, query: tc.query}, func(s *auditEnvSetup) {
				s.opts.Config.Query.MaxTimeoutSeconds, s.opts.Config.Query.MCPCallTimeoutSeconds = 30, 30
				s.opts.Config.Query.MaxConcurrentPerUser, s.opts.Config.Query.MaxConcurrentGlobal = 3, 30
				s.opts.Config.AI = config.AIConfig{Enabled: true, Provider: "bedrock", Region: "us-east-1", AskChefModel: "anthropic.claude-haiku"}
				s.opts.AskChefChat = chat
			})
			// Ask Chef reads only through team membership, even for an admin.
			if err := e.db.AddTeamMember(context.Background(), e.teamID, e.admin.ID, models.TeamRoleMember); err != nil {
				t.Fatal(err)
			}
			chat.input = fmt.Sprintf(`{"team_id":%d,"source_id":%d,"query":"level=\"error\"","start_time":"2026-09-01T00:00:00Z","end_time":"2026-09-01T01:00:00Z","limit":25}`, e.teamID, e.sourceID)
			body := fmt.Sprintf(`{"team_id":%d,"source_id":%d,"timezone":"UTC","context":{"kind":"explorer","mode":"logchefql","query":"","start":1788220800000,"end":1788224400000},"history":[],"evidence":[],"message":"Why?"}`, e.teamID, e.sourceID)
			resp := e.do(http.MethodPost, "/api/v1"+askChefTurnsPath, body, e.cookie)
			wantStatus(t, resp, http.StatusOK)
			if !strings.Contains(string(resp.body), "event: done") {
				t.Fatalf("turn did not finish: %s", resp.body)
			}

			ev := onlyEvent(t, e.settledEvents())
			if ev.ActorID == nil || *ev.ActorID != e.admin.ID || ev.AuthMethod != models.AuditAuthSession {
				t.Errorf("actor %v %s, want the user's session", ev.ActorID, ev.AuthMethod)
			}
			d := e.checkReadEvent(t, ev, tc.outcome, tc.reason, "/api/v1"+askChefTurnsPath, "ask_chef", tc.rows)
			if d["tool"] != "query_logchefql" || d["query"] != `level="error"` || d["language"] != string(models.QueryLanguageLogchefQL) || d["limit_requested"] != float64(25) {
				t.Errorf("detail %v", d)
			}
			if d["time_from"] != "2026-09-01T00:00:00Z" || d["time_to"] != "2026-09-01T01:00:00Z" {
				t.Errorf("range %v..%v", d["time_from"], d["time_to"])
			}
		})
	}
}

// An MCP tool call that reads logs records one event that names the OAuth
// grant and client. A tool that reads no logs records none, and a call that
// admission refuses records a rejected failure.
func TestAuditQueryRunRecordsMCPToolCalls(t *testing.T) {
	// Not parallel: the query tracker is process-global and every test DB
	// starts user IDs at 1, so admission tests must not overlap.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/select/logsql/field_names":
			_, _ = io.WriteString(w, `{"values":[{"value":"level","hits":1}]}`)
		case "/select/logsql/query":
			_, _ = io.WriteString(w, `{"_time":"2026-10-01T00:00:01Z","_msg":"a","level":"error"}`+"\n")
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	defer backend.Close()
	cfg := testOAuthConfig()
	cfg.Query.MaxConcurrentPerUser = 1
	e := newOAuthEnvWithConfig(t, cfg, models.UserRoleMember)
	team, src := e.vlSourceFor(backend.URL)
	token := e.mcpToken()
	call := func(name string, args map[string]any) map[string]any {
		args["team_id"], args["source_id"] = team.ID, src.ID
		return e.mcpCall(token, protocolLegacy, "tools/call", "", map[string]any{"name": name, "arguments": args})
	}

	if result := call("query_logchefql", map[string]any{"query": "level=error", "start_time": "2026-10-01T00:00:00Z", "end_time": "2026-10-01T01:00:00Z"}); result["isError"] == true {
		t.Fatalf("query_logchefql: %v", result["content"])
	}
	if result := call("get_source_schema", map[string]any{}); result["isError"] == true {
		t.Fatalf("get_source_schema: %v", result["content"])
	}
	held, err := queryTracker.StartQuery(QueryClassPreview, e.user.ID, src.ID, team.ID, "held", func() {}, 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	refused := call("query_logs", map[string]any{"raw_sql": "*"})
	queryTracker.RemoveQuery(held)
	if refused["isError"] != true {
		t.Fatalf("query_logs with the per-user cap held: %v", refused)
	}

	if err := e.srv.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	all, err := e.db.ListAuditEvents(context.Background(), models.AuditFilter{Ascending: true, Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	var runs []models.AuditEvent
	for _, ev := range all {
		if ev.Action == string(audit.QueryRun) {
			runs = append(runs, ev)
		}
	}
	if len(runs) != 2 {
		t.Fatalf("stored %d query.run events, want one per call that reads logs: %+v", len(runs), runs)
	}
	for _, ev := range runs {
		if ev.ActorID == nil || *ev.ActorID != e.user.ID || ev.AuthMethod != models.AuditAuthOAuthMCP || ev.CredentialID == "" || ev.ClientID == "" {
			t.Errorf("actor %v %s credential %q client %q, want the user's MCP grant", ev.ActorID, ev.AuthMethod, ev.CredentialID, ev.ClientID)
		}
		if ev.Route != MCPPath || ev.HTTPMethod != http.MethodPost || ev.Status != http.StatusOK || ev.TargetLabel != src.Name {
			t.Errorf("request %s %s %d target %q", ev.HTTPMethod, ev.Route, ev.Status, ev.TargetLabel)
		}
	}
	read, rejected := runs[0], runs[1]
	if d := detailOf(t, read); read.Outcome != models.AuditOutcomeSuccess || d["mode"] != "mcp" || d["tool"] != "query_logchefql" ||
		d["query"] != "level=error" || d["language"] != string(models.QueryLanguageLogchefQL) || d["rows_returned"] != float64(1) {
		t.Errorf("read %s detail %v", read.Outcome, d)
	}
	if d := detailOf(t, rejected); rejected.Outcome != models.AuditOutcomeFailure || rejected.Reason != "rejected" || d["tool"] != "query_logs" ||
		d["query"] != "*" || d["language"] != string(models.QueryLanguageLogsQL) {
		t.Errorf("refused call %s %q detail %v", rejected.Outcome, rejected.Reason, d)
	}
	rows, err := e.db.ListQueryHistory(context.Background(), e.user.ID, 10)
	if err != nil || len(rows) != 0 {
		t.Errorf("history %v %v, want none", rows, err)
	}
}
