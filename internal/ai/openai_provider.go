package ai

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/sashabaranov/go-openai"
)

// DefaultOpenAIModel is the model used when none is configured.
const DefaultOpenAIModel = openai.GPT4o

// openaiProvider is the OpenAI (and OpenAI-compatible) transport. It wraps
// go-openai and supports a custom BaseURL for compatible endpoints.
type openaiProvider struct {
	client *openai.Client
	logger *slog.Logger
}

// newOpenAIProvider builds an openaiProvider from config. An API key is
// required; BaseURL is optional (used for OpenAI-compatible endpoints).
func newOpenAIProvider(cfg ProviderConfig, logger *slog.Logger) (*openaiProvider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("OpenAI API key not provided")
	}

	config := openai.DefaultConfig(cfg.APIKey)
	// Configure a custom HTTP client with the default timeout. Per-call timeouts
	// are enforced by the Generator via context.
	config.HTTPClient = &http.Client{Timeout: DefaultTimeout}

	if cfg.BaseURL != "" {
		config.BaseURL = cfg.BaseURL
		logger.Debug("using custom OpenAI base URL", "base_url", cfg.BaseURL)
	}

	return &openaiProvider{
		client: openai.NewClientWithConfig(config),
		logger: logger.With("component", "openai_provider"),
	}, nil
}

// Name implements Provider.
func (p *openaiProvider) Name() string { return ProviderOpenAI }

// Complete implements Provider using a single chat completion.
func (p *openaiProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	resp, err := p.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: req.Model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: req.System,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: req.User,
				},
			},
			MaxTokens:   req.MaxTokens,
			Temperature: req.Temperature,
		},
	)
	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response received from OpenAI API")
	}

	return resp.Choices[0].Message.Content, nil
}
