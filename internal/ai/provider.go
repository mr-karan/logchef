package ai

import (
	"context"
	"fmt"
	"log/slog"
)

// Provider is a text-completion transport. It knows nothing about SQL, schemas,
// or validation — it just turns a system+user prompt into text.
type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (string, error)
	// Name returns the provider id for logging ("openai", "bedrock").
	Name() string
}

// CompletionRequest is a single text-completion request for a Provider.
type CompletionRequest struct {
	System      string
	User        string
	Model       string
	MaxTokens   int
	Temperature float32
}

// ProviderConfig carries the fields needed to build any Provider. Which fields
// are used depends on Provider (openai uses APIKey/BaseURL; bedrock uses Region).
type ProviderConfig struct {
	// Provider selects the transport: "openai" (or "" → default) | "bedrock".
	Provider string
	// APIKey / BaseURL are used by the openai provider.
	APIKey  string
	BaseURL string
	// Region is the AWS region used by the bedrock provider.
	Region string
}

// Provider identifiers.
const (
	ProviderOpenAI  = "openai"
	ProviderBedrock = "bedrock"
)

// NewProvider builds a Provider from config. An empty Provider defaults to
// "openai" for backward compatibility. Unknown providers return an error.
// The context is used for provider initialization that performs I/O (e.g. the
// bedrock provider resolving AWS credentials).
func NewProvider(ctx context.Context, cfg ProviderConfig, logger *slog.Logger) (Provider, error) {
	switch cfg.Provider {
	case "", ProviderOpenAI:
		return newOpenAIProvider(cfg, logger)
	case ProviderBedrock:
		return newBedrockProvider(ctx, cfg, logger)
	default:
		return nil, fmt.Errorf("unknown AI provider %q (supported: %q, %q)", cfg.Provider, ProviderOpenAI, ProviderBedrock)
	}
}
