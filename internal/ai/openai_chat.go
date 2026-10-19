package ai

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// OpenAIChat runs tool-calling conversations through a streamed chat
// completions API: OpenAI, OpenRouter or any compatible endpoint. It asks
// for no reasoning and drops any reasoning output.
type OpenAIChat struct {
	client *openAIClient
}

// NewOpenAIChat builds an OpenAIChat for the endpoint at baseURL. An empty
// baseURL means OpenAI.
func NewOpenAIChat(baseURL, apiKey string) (*OpenAIChat, error) {
	client, err := newOpenAIClient(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	return &OpenAIChat{client: client}, nil
}

// openAIStopReasons maps finish_reason to the Bedrock stop reasons.
var openAIStopReasons = map[string]ChatStopReason{
	"stop":           "end_turn",
	"tool_calls":     "tool_use",
	"length":         ChatStopMaxTokens,
	"content_filter": ChatStopContentFiltered,
}

// Stream runs one model call. It calls onText with each text delta as it
// arrives and returns the whole assistant message.
func (c *OpenAIChat) Stream(ctx context.Context, req ChatRequest, onText func(string)) (ChatResponse, error) {
	body, err := openAIChatBody(req)
	if err != nil {
		return ChatResponse{}, err
	}
	var acc openAIAccumulator
	err = c.client.stream(ctx, body, func(chunk openAIChunk) {
		if chunk.Usage != nil {
			acc.usage = ChatUsage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				acc.text.WriteString(choice.Delta.Content)
				onText(choice.Delta.Content)
			}
			for _, call := range choice.Delta.ToolCalls {
				acc.addToolCall(call.Index, call.ID, call.Function)
			}
			if choice.FinishReason != "" {
				acc.finishReason = choice.FinishReason
			}
		}
	})
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai chat completions API error: %w", err)
	}
	return acc.response()
}

// streamedToolCall collects one streamed tool call. index is nil when the
// endpoint sent none.
type streamedToolCall struct {
	index     *int
	id        string
	name      string
	arguments strings.Builder
}

type openAIAccumulator struct {
	text         strings.Builder
	calls        []*streamedToolCall
	finishReason string
	usage        ChatUsage
}

// addToolCall adds a delta to the call at index. The first delta of a call
// carries its id and name; later ones carry argument fragments. A delta with
// no index, or with an id other than the one at its index, starts a call.
func (a *openAIAccumulator) addToolCall(index *int, id string, fn openAIFunctionCall) {
	call := a.lastCallAt(index)
	if call == nil || (id != "" && call.id != "" && id != call.id) {
		call = &streamedToolCall{index: index}
		a.calls = append(a.calls, call)
	}
	if id != "" {
		call.id = id
	}
	if fn.Name != "" {
		call.name = fn.Name
	}
	call.arguments.WriteString(fn.Arguments)
}

// lastCallAt returns the latest call at index, or nil.
func (a *openAIAccumulator) lastCallAt(index *int) *streamedToolCall {
	if index == nil {
		return nil
	}
	for _, call := range slices.Backward(a.calls) {
		if call.index != nil && *call.index == *index {
			return call
		}
	}
	return nil
}

func (a *openAIAccumulator) response() (ChatResponse, error) {
	if a.finishReason == "" {
		return ChatResponse{}, errors.New("openai stream ended before the message stopped")
	}
	stop, ok := openAIStopReasons[a.finishReason]
	if !ok {
		return ChatResponse{}, fmt.Errorf("openai stream stopped with finish_reason %q", a.finishReason)
	}
	resp := ChatResponse{StopReason: stop, Usage: a.usage}
	if a.text.Len() > 0 {
		resp.Blocks = append(resp.Blocks, TextBlock{Text: a.text.String()})
	}
	for i, call := range a.calls {
		id := call.id
		if id == "" {
			id = "call_" + strconv.Itoa(i+1)
		}
		if use, ok := streamedToolUse(id, call.name, call.arguments.String(), stop); ok {
			resp.Blocks = append(resp.Blocks, use)
		}
	}
	return resp, nil
}

func openAIChatBody(req ChatRequest) (openAIChatRequest, error) {
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, openAIMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		converted, err := openAIMessages(m)
		if err != nil {
			return openAIChatRequest{}, err
		}
		messages = append(messages, converted...)
	}
	tools := make([]openAITool, 0, len(req.Tools))
	for _, tool := range req.Tools {
		if !isJSONObject(tool.InputSchema) {
			return openAIChatRequest{}, fmt.Errorf("input schema of tool %s is not a JSON object", tool.Name)
		}
		tools = append(tools, openAITool{Type: "function", Function: openAIFunction{
			Name: tool.Name, Description: tool.Description, Parameters: tool.InputSchema,
		}})
	}
	return openAIChatRequest{
		Model:               req.Model,
		Messages:            messages,
		Tools:               tools,
		MaxCompletionTokens: min(max(req.MaxTokens, 0), math.MaxInt32),
		ReasoningEffort:     "none",
	}, nil
}

// openAIMessages converts one message. Tool results become tool messages,
// which must directly follow the assistant message that called the tools,
// so any text of the same message follows them as a user message.
func openAIMessages(m ChatMessage) ([]openAIMessage, error) {
	var (
		out   []openAIMessage
		text  []string
		calls []openAIToolCall
	)
	for _, block := range m.Blocks {
		switch b := block.(type) {
		case TextBlock:
			text = append(text, b.Text)
		case ToolUseBlock:
			calls = append(calls, openAIToolCall{ID: b.ID, Type: "function", Function: openAIFunctionCall{Name: b.Name, Arguments: string(b.Input)}})
		case ToolResultBlock:
			out = append(out, openAIMessage{Role: "tool", ToolCallID: b.ToolUseID, Content: b.Text})
		default:
			return nil, fmt.Errorf("unknown chat block %T", block)
		}
	}
	if m.Role == ChatRoleAssistant {
		return append(out, openAIMessage{Role: "assistant", Content: strings.Join(text, "\n\n"), ToolCalls: calls}), nil
	}
	if len(text) > 0 {
		out = append(out, openAIMessage{Role: "user", Content: strings.Join(text, "\n\n")})
	}
	return out, nil
}
