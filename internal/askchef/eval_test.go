package askchef_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/askchef"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

// evalScenario is one investigation in testdata/evals: the user's question
// and view, the field values a VictoriaLogs backend serves for each LogsQL
// filter, and a scripted run of the model. The deterministic test replays
// the script. A real-model eval can give the same scenario to a live model.
type evalScenario struct {
	Description string `json:"description"`
	Question    string `json:"question"`
	View        struct {
		Mode  askchef.QueryMode `json:"mode"`
		Query string            `json:"query"`
		Start time.Time         `json:"start"`
		End   time.Time         `json:"end"`
	} `json:"view"`
	// FieldValues maps a LogsQL filter to each field's values in the logs
	// it matches. "*" is every log.
	FieldValues map[string]map[string][]struct {
		Value string `json:"value"`
		Hits  int64  `json:"hits"`
	} `json:"field_values"`
	Script []struct {
		Tool  string         `json:"tool"`
		Input map[string]any `json:"input"`
	} `json:"script"`
	Expect struct {
		SystemPromptContains []string `json:"system_prompt_contains"`
		// ToolResultsContain lists, for each scripted call, text its result
		// must show the model.
		ToolResultsContain [][]string `json:"tool_results_contain"`
	} `json:"expect"`
}

func loadEvalScenario(t *testing.T, path string) evalScenario {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s evalScenario
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	if len(s.Expect.ToolResultsContain) != len(s.Script) {
		t.Fatalf("%s: %d scripted calls but %d tool result expectations", path, len(s.Script), len(s.Expect.ToolResultsContain))
	}
	return s
}

// serveFieldValues is a VictoriaLogs stand-in that answers field value
// queries from the scenario. A filter the scenario does not list fails.
func (s evalScenario) serveFieldValues(t *testing.T) string {
	t.Helper()
	fields := map[string]bool{}
	for _, byField := range s.FieldValues {
		for name := range byField {
			fields[name] = true
		}
	}
	stub := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		switch r.URL.Path {
		case "/health":
			rw.WriteHeader(http.StatusOK)
		case "/select/logsql/field_names":
			names := make([]map[string]any, 0, len(fields))
			for _, name := range slices.Sorted(maps.Keys(fields)) {
				names = append(names, map[string]any{"value": name, "hits": 1})
			}
			_ = json.NewEncoder(rw).Encode(map[string]any{"values": names})
		case "/select/logsql/field_values":
			byField, ok := s.FieldValues[r.Form.Get("query")]
			if !ok {
				http.Error(rw, fmt.Sprintf("no fixture for filter %q", r.Form.Get("query")), http.StatusBadGateway)
				return
			}
			_ = json.NewEncoder(rw).Encode(map[string]any{"values": byField[r.Form.Get("field")]})
		default:
			http.NotFound(rw, r)
		}
	}))
	t.Cleanup(stub.Close)
	return stub.URL
}

// addSource adds a VictoriaLogs source at url to the world's team.
func (w *world) addSource(t *testing.T, url string) models.SourceID {
	t.Helper()
	ctx := context.Background()
	conn, err := json.Marshal(models.VictoriaLogsConnectionInfo{BaseURL: url})
	if err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "eval-logs", SourceType: models.SourceTypeVictoriaLogs, MetaTSField: "_time", ConnectionConfig: conn}
	if err := w.db.CreateSource(ctx, source); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	if err := w.db.AddTeamSource(ctx, w.team, source.ID); err != nil {
		t.Fatalf("AddTeamSource: %v", err)
	}
	if err := w.tools.Datasources.InitializeSource(ctx, source); err != nil {
		t.Fatalf("InitializeSource: %v", err)
	}
	return source.ID
}

// TestEvalScenarios replays each scenario's scripted model run against its
// stubbed backend. It checks what the model is told, not what a model
// answers.
func TestEvalScenarios(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "evals", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no eval scenarios: %v", err)
	}
	for _, path := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".json"), func(t *testing.T) {
			s := loadEvalScenario(t, path)
			w := newWorld(t)
			source := w.addSource(t, s.serveFieldValues(t))

			var calls []ai.ChatBlock
			for i, call := range s.Script {
				input := map[string]any{"team_id": w.team, "source_id": source}
				maps.Copy(input, call.Input)
				calls = append(calls, toolUse(fmt.Sprintf("tu-%d", i+1), call.Tool, input))
			}
			chat := &scriptedChat{replies: []ai.ChatResponse{reply(calls...)}}
			req := askchef.Request{
				TeamID: w.team, SourceID: source, Location: time.UTC, Message: s.Question,
				View: askchef.View{Mode: s.View.Mode, Query: s.View.Query, Start: s.View.Start, End: s.View.End},
			}
			w.run(t, chat, access.AssistantPrincipal(w.member), req)

			system := chat.request(0).System
			for _, want := range s.Expect.SystemPromptContains {
				if !strings.Contains(system, want) {
					t.Errorf("system prompt lacks %q", want)
				}
			}
			results := toolResults(chat.request(1))
			if len(results) != len(s.Script) {
				t.Fatalf("model got %d tool results, want %d", len(results), len(s.Script))
			}
			for i, wants := range s.Expect.ToolResultsContain {
				for _, want := range wants {
					if !strings.Contains(results[i].Text, want) {
						t.Errorf("tool result %d lacks %q:\n%s", i+1, want, results[i].Text)
					}
				}
			}
		})
	}
}
