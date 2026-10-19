package ai

import (
	"context"
	"fmt"
	"log/slog"
)

// DefaultOpenAIModel is the model used when none is configured.
const DefaultOpenAIModel = "gpt-4o"

// openaiProvider is the OpenAI (and OpenAI-compatible) transport. It supports
// a custom BaseURL for compatible endpoints.
type openaiProvider struct {
	client *openAIClient
	logger *slog.Logger
}

// newOpenAIProvider builds an openaiProvider from config. An API key is
// required; BaseURL is optional (used for OpenAI-compatible endpoints).
func newOpenAIProvider(cfg ProviderConfig, logger *slog.Logger) (*openaiProvider, error) {
	client, err := newOpenAIClient(cfg.BaseURL, cfg.APIKey)
	if err != nil {
		return nil, err
	}
	if cfg.BaseURL != "" {
		logger.Debug("using custom OpenAI base URL", "base_url", cfg.BaseURL)
	}
	return &openaiProvider{
		client: client,
		logger: logger.With("component", "openai_provider"),
	}, nil
}

// Name implements Provider.
func (p *openaiProvider) Name() string { return ProviderOpenAI }

// Complete implements Provider using a single chat completion.
func (p *openaiProvider) Complete(ctx context.Context, req CompletionRequest) (string, error) {
	text, err := p.client.complete(ctx, openAIChatRequest{
		Model: req.Model,
		Messages: []openAIMessage{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.User},
		},
		MaxCompletionTokens: req.MaxTokens,
		Temperature:         req.Temperature,
	})
	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}
	return text, nil
}
