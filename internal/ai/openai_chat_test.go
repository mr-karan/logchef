package ai

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// openRouterStub serves one canned response to every chat completions call
// and records the request bodies it was sent.
type openRouterStub struct {
	mu     sync.Mutex
	bodies []map[string]any
	sizes  []int
	auth   []string
}

func (s *openRouterStub) serve(t *testing.T, status int, contentType string, responses ...string) *OpenAIChat {
	t.Helper()
	chat, err := NewOpenAIChat(s.start(t, status, contentType, responses...), "test-key")
	if err != nil {
		t.Fatalf("NewOpenAIChat: %v", err)
	}
	return chat
}

// start starts the stub and returns its base URL.
func (s *openRouterStub) start(t *testing.T, status int, contentType string, responses ...string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/chat/completions" {
			http.NotFound(rw, r)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		s.sizes = append(s.sizes, len(raw))
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		call := len(s.bodies) - 1
		s.mu.Unlock()
		rw.Header().Set("Content-Type", contentType)
		rw.WriteHeader(status)
		_, _ = io.WriteString(rw, responses[min(call, len(responses)-1)])
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/api/v1"
}

func (s *openRouterStub) body(i int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[i]
}

// streamOf serves an OpenRouter event stream.
func (s *openRouterStub) streamOf(t *testing.T, events ...string) *OpenAIChat {
	t.Helper()
	return s.serve(t, http.StatusOK, "text/event-stream", strings.Join(events, ""))
}

const keepalive = ": OPENROUTER PROCESSING\n\n"

// chunk is one OpenRouter chat.completion.chunk event with one choice.
func chunk(delta, finishReason string) string {
	finish := "null"
	if finishReason != "" {
		finish = `"` + finishReason + `"`
	}
	return fmt.Sprintf(`data: {"id":"gen-1789460000-abc","provider":"Anthropic","model":"anthropic/claude-haiku-5.5","object":"chat.completion.chunk","created":1789460000,"choices":[{"index":0,"delta":%s,"finish_reason":%s,"native_finish_reason":%s,"logprobs":null}]}`+"\n\n", delta, finish, finish)
}

func textChunk(text string) string {
	return chunk(`{"role":"assistant","content":`+mustJSON(text)+`}`, "")
}

// usageChunk is OpenRouter's last chunk: no choices, the call's usage.
func usageChunk(in, out int) string {
	return fmt.Sprintf(`data: {"id":"gen-1789460000-abc","provider":"Anthropic","model":"anthropic/claude-haiku-5.5","object":"chat.completion.chunk","created":1789460000,"choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d,"cost":0.0001}}`+"\n\n", in, out, in+out)
}

const done = "data: [DONE]\n\n"

func toolCallChunk(index int, id, name, arguments string) string {
	call := map[string]any{"index": index, "type": "function", "function": map[string]any{"arguments": arguments}}
	if id != "" {
		call["id"] = id
		call["function"].(map[string]any)["name"] = name
	}
	return chunk(mustJSON(map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{call}}), "")
}

func mustJSON(v any) string {
	encoded, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// split cuts s into pieces of at most n bytes.
func split(s string, n int) []string {
	var parts []string
	for len(s) > n {
		parts = append(parts, s[:n])
		s = s[n:]
	}
	return append(parts, s)
}

func testRequest() ChatRequest {
	return ChatRequest{
		Model:     "anthropic/claude-haiku-5.5",
		System:    "You are Ask Chef.",
		Messages:  []ChatMessage{{Role: ChatRoleUser, Blocks: []ChatBlock{TextBlock{Text: "Why are there errors?"}}}},
		Tools:     []ChatTool{{Name: "query_logchefql", Description: "Run a query.", InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)}},
		MaxTokens: 2048,
	}
}

func streamText(t *testing.T, chat *OpenAIChat) (ChatResponse, string, error) {
	t.Helper()
	var streamed strings.Builder
	resp, err := chat.Stream(t.Context(), testRequest(), func(delta string) { streamed.WriteString(delta) })
	return resp, streamed.String(), err
}

func TestOpenAIChatStreamsTextAndUsage(t *testing.T) {
	stub := &openRouterStub{}
	chat := stub.streamOf(t,
		chunk(`{"role":"assistant","content":"","reasoning":"Let me think.","reasoning_details":[{"type":"reasoning.text","text":"Let me think."}]}`, ""),
		textChunk("Errors rose "),
		textChunk("after the deploy [E1]."),
		chunk(`{"role":"assistant","content":""}`, "stop"),
		usageChunk(1200, 34),
		done,
	)

	resp, streamed, err := streamText(t, chat)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	want := ChatResponse{
		Blocks:     []ChatBlock{TextBlock{Text: "Errors rose after the deploy [E1]."}},
		StopReason: "end_turn",
		Usage:      ChatUsage{InputTokens: 1200, OutputTokens: 34},
	}
	if !reflect.DeepEqual(resp, want) {
		t.Fatalf("response = %+v, want %+v", resp, want)
	}
	if streamed != "Errors rose after the deploy [E1]." {
		t.Fatalf("streamed text = %q; reasoning must not stream", streamed)
	}

	body := stub.body(0)
	if stub.auth[0] != "Bearer test-key" {
		t.Fatalf("Authorization = %q", stub.auth[0])
	}
	for key, want := range map[string]any{
		"model":                 "anthropic/claude-haiku-5.5",
		"stream":                true,
		"max_completion_tokens": float64(2048),
		"reasoning_effort":      "none",
		"stream_options":        map[string]any{"include_usage": true},
	} {
		if !reflect.DeepEqual(body[key], want) {
			t.Errorf("request %s = %#v, want %#v", key, body[key], want)
		}
	}
	wantMessages := []any{
		map[string]any{"role": "system", "content": "You are Ask Chef."},
		map[string]any{"role": "user", "content": "Why are there errors?"},
	}
	if !reflect.DeepEqual(body["messages"], wantMessages) {
		t.Errorf("request messages = %#v", body["messages"])
	}
	wantTools := []any{map[string]any{"type": "function", "function": map[string]any{
		"name": "query_logchefql", "description": "Run a query.",
		"parameters": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
	}}}
	if !reflect.DeepEqual(body["tools"], wantTools) {
		t.Errorf("request tools = %#v", body["tools"])
	}
}

func TestOpenAIChatAssemblesParallelToolCalls(t *testing.T) {
	first := `{"query":"level=\"error\" and service=\"api\"","start":"2026-10-19T04:00:00Z"}`
	second := `{"field_name":"service","field_type":"String"}`
	events := []string{
		textChunk("Checking two things."),
		toolCallChunk(0, "toolu_01", "query_logchefql", ""),
		toolCallChunk(1, "toolu_02", "get_field_values", ""),
	}
	firstParts, secondParts := split(first, 5), split(second, 3)
	for i := range max(len(firstParts), len(secondParts)) {
		if i < len(firstParts) {
			events = append(events, toolCallChunk(0, "", "", firstParts[i]))
		}
		if i < len(secondParts) {
			events = append(events, toolCallChunk(1, "", "", secondParts[i]))
		}
	}
	events = append(events, chunk(`{"role":"assistant","content":""}`, "tool_calls"), usageChunk(900, 120), done)
	chat := (&openRouterStub{}).streamOf(t, events...)

	resp, _, err := streamText(t, chat)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	want := ChatResponse{
		Blocks: []ChatBlock{
			TextBlock{Text: "Checking two things."},
			ToolUseBlock{ID: "toolu_01", Name: "query_logchefql", Input: json.RawMessage(first)},
			ToolUseBlock{ID: "toolu_02", Name: "get_field_values", Input: json.RawMessage(second)},
		},
		StopReason: "tool_use",
		Usage:      ChatUsage{InputTokens: 900, OutputTokens: 120},
	}
	if !reflect.DeepEqual(resp, want) {
		t.Fatalf("response = %+v, want %+v", resp, want)
	}
}

// Compatible endpoints do not all stream tool calls the way OpenAI does.
func TestOpenAIChatAssemblesToolCallsFromLooseDeltas(t *testing.T) {
	// call is one tool call delta. A nil index or an empty id or name is
	// left out.
	call := func(index *int, id, name, arguments string) string {
		fn := map[string]any{"arguments": arguments}
		if name != "" {
			fn["name"] = name
		}
		delta := map[string]any{"type": "function", "function": fn}
		if index != nil {
			delta["index"] = *index
		}
		if id != "" {
			delta["id"] = id
		}
		return chunk(mustJSON(map[string]any{"role": "assistant", "tool_calls": []any{delta}}), "")
	}
	zero := new(int)
	sources, schema := `{}`, `{"team_id":1}`
	for _, tc := range []struct {
		name   string
		deltas []string
		want   []ChatBlock
	}{
		{
			name:   "no index starts a new call",
			deltas: []string{call(nil, "toolu_01", "get_sources", sources), call(nil, "toolu_02", "get_source_schema", schema)},
			want: []ChatBlock{
				ToolUseBlock{ID: "toolu_01", Name: "get_sources", Input: json.RawMessage(sources)},
				ToolUseBlock{ID: "toolu_02", Name: "get_source_schema", Input: json.RawMessage(schema)},
			},
		},
		{
			name:   "a new id at the same index starts a new call",
			deltas: []string{call(zero, "toolu_01", "get_sources", sources), call(zero, "toolu_02", "get_source_schema", `{"team_id"`), call(zero, "", "", `:1}`)},
			want: []ChatBlock{
				ToolUseBlock{ID: "toolu_01", Name: "get_sources", Input: json.RawMessage(sources)},
				ToolUseBlock{ID: "toolu_02", Name: "get_source_schema", Input: json.RawMessage(schema)},
			},
		},
		{
			name:   "a call with no id gets one",
			deltas: []string{call(zero, "", "get_source_schema", `{"team_id"`), call(zero, "", "", `:1}`), call(nil, "", "get_sources", sources)},
			want: []ChatBlock{
				ToolUseBlock{ID: "call_1", Name: "get_source_schema", Input: json.RawMessage(schema)},
				ToolUseBlock{ID: "call_2", Name: "get_sources", Input: json.RawMessage(sources)},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := slices.Concat(tc.deltas, []string{chunk(`{"role":"assistant","content":""}`, "tool_calls"), usageChunk(10, 2), done})
			resp, _, err := streamText(t, (&openRouterStub{}).streamOf(t, events...))
			if err != nil {
				t.Fatalf("Stream: %v", err)
			}
			if !reflect.DeepEqual(resp.Blocks, tc.want) {
				t.Fatalf("blocks = %+v, want %+v", resp.Blocks, tc.want)
			}
		})
	}
}

// An endpoint that sends no usage chunk still gets an input token count, so
// the caller's token budget applies.
func TestOpenAIChatEstimatesUsageWhenTheStreamHasNone(t *testing.T) {
	stub := &openRouterStub{}
	resp, _, err := streamText(t, stub.streamOf(t, textChunk("Errors rose."), chunk(`{"role":"assistant","content":""}`, "stop"), done))
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	stub.mu.Lock()
	size := stub.sizes[0]
	stub.mu.Unlock()
	if want := (ChatUsage{InputTokens: size / 4, Estimated: true}); resp.Usage != want || want.InputTokens == 0 {
		t.Fatalf("usage = %+v, want %+v", resp.Usage, want)
	}
}

func TestOpenAIChatSkipsKeepaliveComments(t *testing.T) {
	chat := (&openRouterStub{}).streamOf(t,
		strings.Repeat(keepalive, 450), textChunk("Errors "),
		strings.Repeat(keepalive, 3), textChunk("rose."), keepalive,
		chunk(`{"role":"assistant","content":""}`, "stop"), keepalive, usageChunk(10, 2), done,
	)

	resp, streamed, err := streamText(t, chat)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if streamed != "Errors rose." || !reflect.DeepEqual(resp.Blocks, []ChatBlock{TextBlock{Text: "Errors rose."}}) || resp.Usage.InputTokens != 10 {
		t.Fatalf("response = %+v, streamed %q", resp, streamed)
	}
}

func TestOpenAIChatMidStreamErrorFailsTheCall(t *testing.T) {
	cases := map[string][]string{
		"bare error event": {
			textChunk("Errors "),
			`data: {"error":{"code":502,"message":"Provider returned error"}}` + "\n\n",
		},
		"error event after keepalives": {
			strings.Repeat(keepalive, 450),
			textChunk("Errors "),
			keepalive,
			`data: {"error":{"code":502,"message":"Provider returned error"}}` + "\n\n",
		},
		"error on a chunk": {
			textChunk("Errors "),
			`data: {"id":"gen-1789460000-abc","object":"chat.completion.chunk","created":1789460000,"model":"anthropic/claude-haiku-5.5","provider":"Anthropic","error":{"code":502,"message":"Provider returned error"},"choices":[{"index":0,"delta":{"content":""},"finish_reason":"error","native_finish_reason":"error"}]}` + "\n\n",
			done,
		},
	}
	for name, events := range cases {
		t.Run(name, func(t *testing.T) {
			chat := (&openRouterStub{}).streamOf(t, events...)
			_, _, err := streamText(t, chat)
			if err == nil || !strings.Contains(err.Error(), "Provider returned error") {
				t.Fatalf("Stream error = %v, want the provider error", err)
			}
		})
	}
}

func TestOpenAIChatStreamCutShortFailsTheCall(t *testing.T) {
	chat := (&openRouterStub{}).streamOf(t, textChunk("Errors "))
	if _, _, err := streamText(t, chat); err == nil {
		t.Fatal("Stream returned no error for a stream that ended before the message stopped")
	}
}

func TestOpenAIChatLengthStopsAtMaxTokens(t *testing.T) {
	chat := (&openRouterStub{}).streamOf(t,
		textChunk("The errors come from"),
		toolCallChunk(0, "toolu_01", "query_logchefql", `{"query":"level=`),
		chunk(`{"role":"assistant","content":""}`, "length"),
		usageChunk(500, 2048),
		done,
	)

	resp, _, err := streamText(t, chat)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	want := ChatResponse{
		Blocks:     []ChatBlock{TextBlock{Text: "The errors come from"}},
		StopReason: ChatStopMaxTokens,
		Usage:      ChatUsage{InputTokens: 500, OutputTokens: 2048},
	}
	if !reflect.DeepEqual(resp, want) {
		t.Fatalf("response = %+v, want %+v; a cut-off tool call must be dropped", resp, want)
	}
}

// A tool call with arguments that are not JSON, in a message that was not
// cut off, is kept with an empty input and marked, so the model hears why.
func TestOpenAIChatMarksInvalidToolArguments(t *testing.T) {
	chat := (&openRouterStub{}).streamOf(t,
		toolCallChunk(0, "toolu_01", "query_logchefql", `{"query":"level=`),
		chunk(`{"role":"assistant","content":""}`, "tool_calls"),
		usageChunk(500, 20),
		done,
	)

	resp, _, err := streamText(t, chat)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	want := []ChatBlock{ToolUseBlock{ID: "toolu_01", Name: "query_logchefql", Input: json.RawMessage(`{}`), InvalidInput: true}}
	if !reflect.DeepEqual(resp.Blocks, want) {
		t.Fatalf("blocks = %+v, want %+v", resp.Blocks, want)
	}
}

func TestOpenAIChatContentFilterStop(t *testing.T) {
	chat := (&openRouterStub{}).streamOf(t, textChunk("I"), chunk(`{"role":"assistant","content":""}`, "content_filter"), done)
	resp, _, err := streamText(t, chat)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if resp.StopReason != ChatStopContentFiltered {
		t.Fatalf("stop reason = %q, want %q", resp.StopReason, ChatStopContentFiltered)
	}
}

func TestOpenAIChatErrorResponse(t *testing.T) {
	stub := &openRouterStub{}
	chat := stub.serve(t, http.StatusNotFound, "application/json",
		`{"error":{"message":"No endpoints found that support tool use. To learn more about provider routing, visit: https://openrouter.ai/docs/provider-routing","code":404}}`)

	_, _, err := streamText(t, chat)
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "No endpoints found that support tool use") {
		t.Fatalf("Stream error = %v, want the status and the provider message", err)
	}
}

func TestOpenAIChatSendsToolTurns(t *testing.T) {
	stub := &openRouterStub{}
	chat := stub.streamOf(t, textChunk("Done."), chunk(`{"role":"assistant","content":""}`, "stop"), done)
	req := testRequest()
	req.Messages = append(req.Messages,
		ChatMessage{Role: ChatRoleAssistant, Blocks: []ChatBlock{
			TextBlock{Text: "Checking."},
			ToolUseBlock{ID: "toolu_01", Name: "query_logchefql", Input: json.RawMessage(`{"query":"level=\"error\""}`)},
			ToolUseBlock{ID: "toolu_02", Name: "query_logchefql", Input: json.RawMessage(`{"query":"x"}`)},
		}},
		ChatMessage{Role: ChatRoleUser, Blocks: []ChatBlock{
			ToolResultBlock{ToolUseID: "toolu_01", Text: "[evidence E1]\n3 rows"},
			ToolResultBlock{ToolUseID: "toolu_02", Text: "[tool did not complete: bad query]", IsError: true},
			TextBlock{Text: "[Answer now.]"},
		}},
	)
	if _, err := chat.Stream(t.Context(), req, func(string) {}); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	got := stub.body(0)["messages"].([]any)[2:]
	want := []any{
		map[string]any{"role": "assistant", "content": "Checking.", "tool_calls": []any{
			map[string]any{"id": "toolu_01", "type": "function", "function": map[string]any{"name": "query_logchefql", "arguments": `{"query":"level=\"error\""}`}},
			map[string]any{"id": "toolu_02", "type": "function", "function": map[string]any{"name": "query_logchefql", "arguments": `{"query":"x"}`}},
		}},
		map[string]any{"role": "tool", "tool_call_id": "toolu_01", "content": "[evidence E1]\n3 rows"},
		map[string]any{"role": "tool", "tool_call_id": "toolu_02", "content": "[tool did not complete: bad query]"},
		map[string]any{"role": "user", "content": "[Answer now.]"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages = %#v\nwant %#v", got, want)
	}
}

func TestNewOpenAIChatNeedsAnAPIKey(t *testing.T) {
	if _, err := NewOpenAIChat("https://openrouter.ai/api/v1", ""); err == nil {
		t.Fatal("NewOpenAIChat accepted an empty API key")
	}
}

func TestOpenAIProviderComplete(t *testing.T) {
	stub := &openRouterStub{}
	baseURL := stub.start(t, http.StatusOK, "application/json",
		`{"id":"gen-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"SELECT 1"},"finish_reason":"stop"}]}`)
	provider, err := NewProvider(t.Context(), ProviderConfig{Provider: ProviderOpenAI, APIKey: "test-key", BaseURL: baseURL}, testLogger())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	text, err := provider.Complete(t.Context(), CompletionRequest{System: "Write SQL.", User: "count rows", Model: "gpt-4o", MaxTokens: 1024, Temperature: 0.5})
	if err != nil || text != "SELECT 1" {
		t.Fatalf("Complete = %q, %v", text, err)
	}
	want := map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{"role": "system", "content": "Write SQL."},
			map[string]any{"role": "user", "content": "count rows"},
		},
		"max_completion_tokens": float64(1024),
		"temperature":           0.5,
	}
	if got := stub.body(0); !reflect.DeepEqual(got, want) {
		t.Fatalf("request = %#v, want %#v", got, want)
	}
}

func TestOpenAIProviderErrorResponse(t *testing.T) {
	baseURL := (&openRouterStub{}).start(t, http.StatusUnauthorized, "application/json", `{"error":{"message":"No auth credentials found","code":401}}`)
	provider, err := NewProvider(t.Context(), ProviderConfig{Provider: ProviderOpenAI, APIKey: "test-key", BaseURL: baseURL}, testLogger())
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if _, err := provider.Complete(t.Context(), CompletionRequest{User: "count rows", Model: "gpt-4o"}); err == nil || !strings.Contains(err.Error(), "401: No auth credentials found") {
		t.Fatalf("Complete error = %v", err)
	}
}
