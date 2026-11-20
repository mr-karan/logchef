package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/audit"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

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
