package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/askchef"
	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store/sqlite"
	"github.com/mr-karan/logchef/pkg/models"
)

// echoChat is the model: it answers with fixed text. When gate is set, it
// signals started and waits for gate to close before answering.
type echoChat struct {
	started chan struct{}
	gate    chan struct{}
}

func (c *echoChat) Stream(ctx context.Context, _ ai.ChatRequest, onText func(string)) (ai.ChatResponse, error) {
	if c.gate != nil {
		c.started <- struct{}{}
		select {
		case <-c.gate:
		case <-ctx.Done():
			return ai.ChatResponse{}, ctx.Err()
		}
	}
	onText("Errors rose ")
	onText("[E1].")
	return ai.ChatResponse{
		Blocks: []ai.ChatBlock{ai.TextBlock{Text: "Errors rose [E1]."}},
		Usage:  ai.ChatUsage{InputTokens: 50, OutputTokens: 5},
	}, nil
}

type askChefEnv struct {
	t        *testing.T
	db       *sqlite.DB
	srv      *Server
	session  string
	outsider string
	team     models.TeamID
	source   models.SourceID
	alert    models.AlertID
	other    models.AlertID
}

func askChefConfig(model string) *config.Config {
	return &config.Config{
		Server: config.ServerConfig{HTTPServerTimeout: 30 * time.Second},
		Query: config.QueryConfig{
			DefaultTimeoutSeconds: 30, MaxTimeoutSeconds: 30, DefaultPreviewLimit: 100, MaxPreviewLimit: 1000,
			MaxResponseBytes: 1 << 20, MaxConcurrentPerUser: 3, MaxConcurrentGlobal: 30, MCPCallTimeoutSeconds: 30,
		},
		Alerts: config.AlertsConfig{Enabled: true, HistoryLimit: 50},
		AI:     config.AIConfig{Enabled: true, Provider: "bedrock", Region: "us-east-1", AskChefModel: model},
	}
}

func newAskChefEnv(t *testing.T, cfg *config.Config, chat askchef.ChatModel) *askChefEnv {
	t.Helper()
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := newServerTestDB(t)
	datasources := datasource.NewService(db, log)
	datasources.Register(datasource.NewClickHouseProvider(clickhouse.NewManager(log), log))
	srv := New(ServerOptions{
		Config:      cfg,
		SQLite:      db,
		Datasources: datasources,
		FS:          http.FS(fstest.MapFS{"index.html": {Data: []byte(`<!doctype html><html><head><base href="/" /></head><body>spa</body></html>`)}}),
		Logger:      log,
		AskChefChat: chat,
	})
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	env := &askChefEnv{t: t, db: db, srv: srv}
	user := func(email string) (*models.User, string) {
		u := &models.User{Email: email, FullName: "Customer A", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: models.UserAccountTypeHuman}
		if err := db.CreateUser(ctx, u); err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		session := "session-" + email
		if err := db.CreateSession(ctx, &models.Session{ID: models.SessionID(session), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		return u, session
	}
	member, session := user("member@example.com")
	_, env.outsider = user("outsider@example.com")
	env.session = session

	team := &models.Team{Name: "payments"}
	if err := db.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	env.team = team.ID
	if err := db.AddTeamMember(ctx, team.ID, member.ID, models.TeamRoleMember); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	newSource := func(name string) models.SourceID {
		source := &models.Source{Name: name, SourceType: models.SourceTypeClickHouse, MetaTSField: "timestamp", Connection: models.ConnectionInfo{Host: "127.0.0.1:1", Database: "default", TableName: name}}
		if err := db.CreateSource(ctx, source); err != nil {
			t.Fatalf("CreateSource: %v", err)
		}
		if err := db.AddTeamSource(ctx, team.ID, source.ID); err != nil {
			t.Fatalf("AddTeamSource: %v", err)
		}
		return source.ID
	}
	env.source = newSource("payments-logs")
	otherSource := newSource("billing-logs")
	newAlert := func(source models.SourceID) models.AlertID {
		alert := &models.Alert{
			SourceID: source, Name: "High error rate", QueryLanguage: models.QueryLanguageClickHouseSQL, EditorMode: models.AlertEditorModeNative,
			Query: "SELECT count() FROM logs", LookbackSeconds: 300, ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
			FrequencySeconds: 60, Severity: models.AlertSeverityCritical, IsActive: true,
		}
		if err := db.CreateAlert(ctx, alert); err != nil {
			t.Fatalf("CreateAlert: %v", err)
		}
		return alert.ID
	}
	env.alert = newAlert(env.source)
	env.other = newAlert(otherSource)
	return env
}

func (e *askChefEnv) body(edit func(map[string]any)) []byte {
	e.t.Helper()
	body := map[string]any{
		"team_id": e.team, "source_id": e.source, "timezone": "Asia/Kolkata",
		"context": map[string]any{"kind": "explorer", "mode": "logchefql", "query": `level="error"`, "start": 1789463105000, "end": 1789466705000},
		"history": []map[string]any{}, "evidence": []map[string]any{}, "message": "Why did errors rise?",
	}
	if edit != nil {
		edit(body)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		e.t.Fatal(err)
	}
	return encoded
}

// turnResponse is a whole Ask Chef response.
type turnResponse struct {
	status      int
	contentType string
	body        []byte
}

// post sends one turn request and reads the whole response.
func (e *askChefEnv) post(session, contentType string, body []byte) (turnResponse, error) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ask-chef/turns", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	}
	resp, err := e.srv.app.Test(req, fiber.TestConfig{Timeout: 10 * time.Second})
	if err != nil {
		return turnResponse{}, err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	return turnResponse{status: resp.StatusCode, contentType: resp.Header.Get("Content-Type"), body: payload}, err
}

func (e *askChefEnv) mustPost(body []byte) turnResponse {
	e.t.Helper()
	resp, err := e.post(e.session, "application/json", body)
	if err != nil {
		e.t.Fatalf("post: %v", err)
	}
	return resp
}

var sseFrame = regexp.MustCompile(`^event: ([a-z_]+)\ndata: (\{.*\})$`)

// readFrames parses an SSE body into event names and JSON data.
func readFrames(t *testing.T, body []byte) (names []string, data []json.RawMessage) {
	t.Helper()
	text := string(body)
	if !strings.HasSuffix(text, "\n\n") {
		t.Fatalf("stream does not end with a blank line: %q", text)
	}
	for frame := range strings.SplitSeq(strings.TrimSuffix(text, "\n\n"), "\n\n") {
		m := sseFrame.FindStringSubmatch(frame)
		if m == nil {
			t.Fatalf("frame %q is not `event: <type>\\ndata: <json>`", frame)
		}
		names = append(names, m[1])
		data = append(data, json.RawMessage(m[2]))
	}
	return names, data
}

func TestAskChefTurnStreamsEvents(t *testing.T) {
	env := newAskChefEnv(t, askChefConfig("anthropic.claude-haiku"), &echoChat{})
	resp := env.mustPost(env.body(nil))

	if resp.status != http.StatusOK || resp.contentType != "text/event-stream" {
		t.Fatalf("status = %d, content type = %q", resp.status, resp.contentType)
	}
	names, data := readFrames(t, resp.body)
	if strings.Join(names, ",") != "text,text,done" {
		t.Fatalf("events = %v, want text,text,done", names)
	}
	var text struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal(data[1], &text); err != nil || text.Delta != "[E1]." {
		t.Fatalf("second text = %s", data[1])
	}
	var done map[string]any
	if err := json.Unmarshal(data[2], &done); err != nil {
		t.Fatal(err)
	}
	if done["stop_reason"] != "end" || done["tool_calls"] != 0.0 || done["input_tokens"] != 50.0 || done["output_tokens"] != 5.0 {
		t.Fatalf("done = %s", data[2])
	}
	if _, ok := done["duration_ms"]; !ok {
		t.Fatalf("done has no duration_ms: %s", data[2])
	}
}

func TestAskChefTurnRefusals(t *testing.T) {
	for _, tc := range []struct {
		name    string
		model   string
		session func(e *askChefEnv) string
		body    func(e *askChefEnv) []byte
		ctype   string
		want    int
	}{
		{name: "disabled", model: "", want: http.StatusNotFound},
		{name: "no session", session: func(*askChefEnv) string { return "" }, want: http.StatusUnauthorized},
		{name: "not JSON", body: func(*askChefEnv) []byte { return []byte("{") }, want: http.StatusBadRequest},
		{name: "form content type", ctype: "application/x-www-form-urlencoded", want: http.StatusUnsupportedMediaType},
		{name: "empty message", body: func(e *askChefEnv) []byte { return e.body(func(b map[string]any) { b["message"] = "" }) }, want: http.StatusBadRequest},
		{name: "start after end", body: func(e *askChefEnv) []byte {
			return e.body(func(b map[string]any) { b["context"].(map[string]any)["start"] = 1789466705001 })
		}, want: http.StatusBadRequest},
		{name: "not a team member", session: func(e *askChefEnv) string { return e.outsider }, want: http.StatusForbidden},
		{name: "unknown alert", body: func(e *askChefEnv) []byte {
			return e.body(func(b map[string]any) {
				ctx := b["context"].(map[string]any)
				ctx["kind"], ctx["alert_id"] = "alert", 9999
			})
		}, want: http.StatusNotFound},
		{name: "alert of another source", body: func(e *askChefEnv) []byte {
			return e.body(func(b map[string]any) {
				ctx := b["context"].(map[string]any)
				ctx["kind"], ctx["alert_id"] = "alert", e.other
			})
		}, want: http.StatusBadRequest},
		{name: "unknown alert history entry", body: func(e *askChefEnv) []byte {
			return e.body(func(b map[string]any) {
				ctx := b["context"].(map[string]any)
				ctx["kind"], ctx["alert_id"], ctx["alert_history_id"] = "alert", e.alert, 777
			})
		}, want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := "anthropic.claude-haiku"
			if tc.name == "disabled" {
				model = tc.model
			}
			env := newAskChefEnv(t, askChefConfig(model), &echoChat{})
			session := env.session
			if tc.session != nil {
				session = tc.session(env)
			}
			body := env.body(nil)
			if tc.body != nil {
				body = tc.body(env)
			}
			contentType := "application/json"
			if tc.ctype != "" {
				contentType = tc.ctype
			}
			resp, err := env.post(session, contentType, body)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			if resp.status != tc.want {
				t.Fatalf("status = %d, want %d: %s", resp.status, tc.want, resp.body)
			}
			if !strings.HasPrefix(resp.contentType, "application/json") {
				t.Fatalf("content type = %q, want a JSON error", resp.contentType)
			}
		})
	}
}

func TestAskChefAllowsOneTurnPerUser(t *testing.T) {
	chat := &echoChat{started: make(chan struct{}, 1), gate: make(chan struct{})}
	env := newAskChefEnv(t, askChefConfig("anthropic.claude-haiku"), chat)

	body := env.body(nil)
	first := make(chan turnResponse, 1)
	go func() {
		resp, err := env.post(env.session, "application/json", body)
		if err != nil {
			t.Errorf("first turn: %v", err)
		}
		first <- resp
	}()
	select {
	case <-chat.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the first turn never reached the model")
	}

	if resp := env.mustPost(body); resp.status != http.StatusConflict {
		t.Fatalf("second turn status = %d, want 409", resp.status)
	}

	close(chat.gate)
	resp := <-first
	if resp.status != http.StatusOK {
		t.Fatalf("first turn status = %d: %s", resp.status, resp.body)
	}
	if names, _ := readFrames(t, resp.body); names[len(names)-1] != "done" {
		t.Fatalf("first turn events = %v", names)
	}

	go func() { <-chat.started }()
	if resp := env.mustPost(body); resp.status != http.StatusOK {
		t.Fatalf("turn after the first ended: status = %d, want 200", resp.status)
	}
}

func TestMetaReportsAskChef(t *testing.T) {
	for _, tc := range []struct {
		name  string
		model string
		chat  askchef.ChatModel
		want  bool
	}{
		{"enabled", "anthropic.claude-haiku", &echoChat{}, true},
		{"no model", "", &echoChat{}, false},
		{"no chat", "anthropic.claude-haiku", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newAskChefEnv(t, askChefConfig(tc.model), tc.chat)
			resp, err := env.srv.app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/meta", http.NoBody))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var envelope struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
				t.Fatal(err)
			}
			if got := string(envelope.Data["ask_chef_enabled"]); got != map[bool]string{true: "true", false: "false"}[tc.want] {
				t.Fatalf("ask_chef_enabled = %s, want %v", got, tc.want)
			}
		})
	}
}

func TestAskChefTurnFromAnAlertEntry(t *testing.T) {
	env := newAskChefEnv(t, askChefConfig("anthropic.claude-haiku"), &echoChat{})
	value := 23.0
	entry, err := env.db.InsertAlertHistory(context.Background(), env.alert, models.AlertStatusTriggered, &value, "fired", nil)
	if err != nil {
		t.Fatalf("InsertAlertHistory: %v", err)
	}
	resp := env.mustPost(env.body(func(b map[string]any) {
		ctx := b["context"].(map[string]any)
		ctx["kind"], ctx["alert_id"], ctx["alert_history_id"] = "alert", env.alert, entry.ID
	}))
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d: %s", resp.status, resp.body)
	}
	if names, _ := readFrames(t, resp.body); names[len(names)-1] != "done" {
		t.Fatalf("events = %v", names)
	}
}
