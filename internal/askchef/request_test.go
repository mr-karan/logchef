package askchef_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/askchef"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/mcp"
	"github.com/mr-karan/logchef/pkg/models"
)

// validBody returns a request body that meets the contract. edit changes it.
func validBody(t *testing.T, edit func(map[string]any)) []byte {
	t.Helper()
	body := map[string]any{
		"team_id":   1,
		"source_id": 3,
		"timezone":  "Asia/Kolkata",
		"context": map[string]any{
			"kind": "explorer", "mode": "logchefql", "query": `level="error"`,
			"start": 1789463105000, "end": 1789466705000,
		},
		"history": []map[string]any{{"role": "user", "text": "hi"}, {"role": "assistant", "text": "hello"}},
		"evidence": []map[string]any{{
			"id": "E7", "tool": "compare_windows", "team_id": 1, "source_id": 3, "mode": "logchefql",
			"query": "", "start": 1, "end": 2, "summary": "3 rows vs 23 rows",
		}},
		"message": "Why did this fire?",
	}
	if edit != nil {
		edit(body)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func contextField(key string, value any) func(map[string]any) {
	return func(body map[string]any) {
		ctx := body["context"].(map[string]any)
		if value == nil {
			delete(ctx, key)
			return
		}
		ctx[key] = value
	}
}

func TestParseRequestAcceptsTheContract(t *testing.T) {
	req, err := askchef.ParseRequest(validBody(t, nil))
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if req.TeamID != 1 || req.SourceID != 3 || req.Location.String() != "Asia/Kolkata" || req.Message != "Why did this fire?" {
		t.Fatalf("request = %+v", req)
	}
	if req.View.Mode != askchef.ModeLogchefQL || req.View.Query != `level="error"` || req.View.Alert != nil ||
		!req.View.Start.Equal(time.UnixMilli(1789463105000)) || !req.View.End.Equal(time.UnixMilli(1789466705000)) {
		t.Fatalf("view = %+v", req.View)
	}
	if len(req.History) != 2 || req.History[1].Role != ai.ChatRoleAssistant {
		t.Fatalf("history = %+v", req.History)
	}
	if len(req.Evidence) != 1 || req.Evidence[0].ID != 7 {
		t.Fatalf("evidence = %+v", req.Evidence)
	}

	alert, err := askchef.ParseRequest(validBody(t, func(b map[string]any) {
		contextField("kind", "alert")(b)
		contextField("alert_id", 9)(b)
		contextField("alert_history_id", 4)(b)
		contextField("mode", nil)(b)
		contextField("query", nil)(b)
	}))
	if err != nil {
		t.Fatalf("alert request: %v", err)
	}
	if alert.View.Alert == nil || *alert.View.Alert != (askchef.AlertRef{AlertID: 9, HistoryID: 4}) || alert.View.Mode != "" {
		t.Fatalf("alert view = %+v", alert.View)
	}
}

func TestParseRequestRejectsInvalidInput(t *testing.T) {
	history := func(n int, text string) []map[string]any {
		items := make([]map[string]any, n)
		for i := range items {
			items[i] = map[string]any{"role": "user", "text": text}
		}
		return items
	}
	evidence := func(n int) []map[string]any {
		items := make([]map[string]any, n)
		for i := range items {
			items[i] = map[string]any{"id": "E1", "tool": "t", "mode": "native"}
		}
		return items
	}
	set := func(key string, value any) func(map[string]any) {
		return func(b map[string]any) { b[key] = value }
	}

	for _, tc := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"missing team", set("team_id", 0)},
		{"missing source", set("source_id", nil)},
		{"unknown timezone", set("timezone", "Mars/Olympus")},
		{"empty message", set("message", "   ")},
		{"message too long", set("message", strings.Repeat("é", 4001))},
		{"too much history", set("history", history(21, "x"))},
		{"history text too long", set("history", history(1, strings.Repeat("x", 8001)))},
		{"history role", set("history", []map[string]any{{"role": "system", "text": "x"}})},
		{"too much evidence", set("evidence", evidence(51))},
		{"evidence id without E", set("evidence", []map[string]any{{"id": "7", "mode": "native"}})},
		{"evidence id zero", set("evidence", []map[string]any{{"id": "E0", "mode": "native"}})},
		{"evidence id signed", set("evidence", []map[string]any{{"id": "E-2", "mode": "native"}})},
		{"evidence without id", set("evidence", []map[string]any{{"mode": "native"}})},
		{"evidence mode", set("evidence", []map[string]any{{"id": "E1", "tool": "compare_windows", "mode": "sql"}})},
		{"evidence tool reads no logs", set("evidence", []map[string]any{{"id": "E1", "tool": "get_sources", "mode": "logchefql"}})},
		{"evidence tool unknown", set("evidence", []map[string]any{{"id": "E1", "tool": strings.Repeat("t", 65), "mode": "logchefql"}})},
		{"evidence query too long", set("evidence", []map[string]any{{"id": "E1", "tool": "compare_windows", "mode": "logchefql", "query": strings.Repeat("é", 4001)}})},
		{"evidence summary too long", set("evidence", []map[string]any{{"id": "E1", "tool": "compare_windows", "mode": "logchefql", "summary": strings.Repeat("é", 301)}})},
		{"unknown kind", contextField("kind", "dashboard")},
		{"unknown mode", contextField("mode", "sql")},
		{"missing start", contextField("start", nil)},
		{"start after end", contextField("start", 1789466705001)},
		{"start equals end", contextField("start", 1789466705000)},
		{"alert fields on explorer", contextField("alert_id", 3)},
		{"alert without alert_id", contextField("kind", "alert")},
		{"not JSON", func(b map[string]any) { b["context"] = "x" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := askchef.ParseRequest(validBody(t, tc.edit))
			if _, ok := errors.AsType[*askchef.RequestError](err); !ok {
				t.Fatalf("ParseRequest error = %v, want a RequestError", err)
			}
		})
	}
	if _, err := askchef.ParseRequest(validBody(t, set("message", strings.Repeat("é", 4000)))); err != nil {
		t.Fatalf("a 4000-character message was rejected: %v", err)
	}
	atLimit := set("evidence", []map[string]any{{
		"id": "E1", "tool": "top_values", "mode": "logchefql", "query": strings.Repeat("é", 4000), "summary": strings.Repeat("é", 300),
	}})
	if _, err := askchef.ParseRequest(validBody(t, atLimit)); err != nil {
		t.Fatalf("evidence at its limits was rejected: %v", err)
	}
}

func TestEvidenceIDWireForm(t *testing.T) {
	encoded, err := json.Marshal(askchef.Evidence{ID: 12, Mode: askchef.ModeNative})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"id":"E12"`) || !strings.Contains(string(encoded), `"start":0`) {
		t.Fatalf("evidence JSON = %s", encoded)
	}
}

// The alert the user asks about reaches the model in the user's timezone.
func TestTurnPromptCarriesTheAlert(t *testing.T) {
	w := newWorld(t)
	chat := &scriptedChat{}
	value := 23.0
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	req := w.request()
	req.Location = kolkata
	alert := &askchef.AlertContext{
		Alert: &models.Alert{
			Name: "High error rate", Severity: models.AlertSeverityCritical, QueryLanguage: models.QueryLanguageLogchefQL,
			Query: `level="error"`, ThresholdOperator: models.AlertThresholdGreaterThan, ThresholdValue: 10,
			LookbackSeconds: 300, FrequencySeconds: 60,
		},
		Entry: &models.AlertHistoryEntry{ID: 4, Status: models.AlertStatusTriggered, Value: &value, TriggeredAt: time.Date(2026, 10, 19, 4, 30, 0, 0, time.UTC)},
	}
	agent := askchef.New(askchef.Opts{Tools: mcp.NewToolServer(*w.tools), Chat: chat, Model: "m", Log: discardLogger()})
	agent.Run(t.Context(), askchef.Turn{Principal: access.AssistantPrincipal(w.member), Request: req, Alert: alert}, func(askchef.Event) bool { return true })

	system := chat.request(0).System
	for _, want := range []string{`"High error rate"`, "value > 10", "value 23", "2026-10-19T10:00:00+05:30", "Asia/Kolkata", "**Not established**", "<untrusted>"} {
		if !strings.Contains(system, want) {
			t.Errorf("system prompt lacks %q:\n%s", want, system)
		}
	}
}
