package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultOpenAIBaseURL is the endpoint used when no base URL is configured.
const DefaultOpenAIBaseURL = "https://api.openai.com/v1"

// openAIResponseHeaderTimeout bounds the wait for a response's headers. A
// streamed body has no total timeout: the caller's context bounds it.
const openAIResponseHeaderTimeout = 30 * time.Second

// Body size bounds of the chat completions client.
const (
	maxOpenAIErrorBody    = 8 << 10
	maxOpenAIResponseBody = 4 << 20
	maxOpenAIStreamLine   = 1 << 20
)

// openAIClient calls the chat completions API of OpenAI or of a compatible
// endpoint such as OpenRouter.
type openAIClient struct {
	url    string
	apiKey string
	http   *http.Client
}

func newOpenAIClient(baseURL, apiKey string) (*openAIClient, error) {
	if apiKey == "" {
		return nil, errors.New("OpenAI API key not provided (set ai.api_key)")
	}
	if baseURL == "" {
		baseURL = DefaultOpenAIBaseURL
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = openAIResponseHeaderTimeout
	return &openAIClient{
		url:    strings.TrimSuffix(baseURL, "/") + "/chat/completions",
		apiKey: apiKey,
		http:   &http.Client{Transport: transport},
	}, nil
}

type openAIChatRequest struct {
	Model               string               `json:"model"`
	Messages            []openAIMessage      `json:"messages"`
	Tools               []openAITool         `json:"tools,omitempty"`
	MaxCompletionTokens int                  `json:"max_completion_tokens,omitempty"`
	Temperature         float32              `json:"temperature,omitempty"`
	ReasoningEffort     string               `json:"reasoning_effort,omitempty"`
	Stream              bool                 `json:"stream,omitempty"`
	StreamOptions       *openAIStreamOptions `json:"stream_options,omitempty"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openAIFunctionCall `json:"function"`
}

type openAIFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// openAIError is the error object of an error response or of a streamed
// chunk.
type openAIError struct {
	Message string `json:"message"`
}

func encodeRequest(body openAIChatRequest) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding chat completions request: %w", err)
	}
	return encoded, nil
}

// post sends one encoded chat completions request. It returns the response
// of a 2xx status; the caller closes its body.
func (c *openAIClient) post(ctx context.Context, encoded []byte, streaming bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if streaming {
		req.Header.Set("Accept", "text/event-stream")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		defer func() { _ = resp.Body.Close() }()
		return nil, statusError(resp)
	}
	return resp, nil
}

// statusError reads the message of an error response. Endpoints that are not
// OpenAI may send any body, so an unknown one is quoted as is.
func statusError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxOpenAIErrorBody))
	var body struct {
		Error *openAIError `json:"error"`
	}
	message := strings.TrimSpace(string(raw))
	if json.Unmarshal(raw, &body) == nil && body.Error != nil && body.Error.Message != "" {
		message = body.Error.Message
	}
	return fmt.Errorf("status %d: %s", resp.StatusCode, message)
}

// complete runs one chat completion without streaming and returns the text.
// It takes at most DefaultTimeout.
func (c *openAIClient) complete(ctx context.Context, body openAIChatRequest) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	encoded, err := encodeRequest(body)
	if err != nil {
		return "", err
	}
	resp, err := c.post(ctx, encoded, false)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxOpenAIResponseBody)).Decode(&out); err != nil {
		return "", fmt.Errorf("decoding chat completions response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("chat completions response has no choices")
	}
	return out.Choices[0].Message.Content, nil
}

// openAIChunk is one streamed chat.completion.chunk.
type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    *int               `json:"index"`
				ID       string             `json:"id"`
				Function openAIFunctionCall `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *openAIError `json:"error"`
}

// stream runs one streamed chat completion and passes each chunk to onChunk.
// It skips SSE comments, such as OpenRouter's keepalives, and ends at
// [DONE] or at the end of the body. A chunk that carries an error fails the
// call. It returns the size of the request body in bytes.
func (c *openAIClient) stream(ctx context.Context, body openAIChatRequest, onChunk func(openAIChunk)) (int, error) {
	body.Stream = true
	body.StreamOptions = &openAIStreamOptions{IncludeUsage: true}
	encoded, err := encodeRequest(body)
	if err != nil {
		return 0, err
	}
	resp, err := c.post(ctx, encoded, true)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return len(encoded), readStream(resp.Body, onChunk)
}

func readStream(body io.Reader, onChunk func(openAIChunk)) error {

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64<<10), maxOpenAIStreamLine)
	for scanner.Scan() {
		data, ok := strings.CutPrefix(scanner.Text(), "data:")
		if !ok {
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			return nil
		}
		var chunk openAIChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("decoding stream chunk: %w", err)
		}
		if chunk.Error != nil {
			return fmt.Errorf("stream error: %s", chunk.Error.Message)
		}
		onChunk(chunk)
	}
	return scanner.Err()
}
