package ai

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewProvider(t *testing.T) {
	log := testLogger()

	t.Run("empty defaults to openai", func(t *testing.T) {
		p, err := NewProvider(context.Background(), ProviderConfig{Provider: "", APIKey: "sk-test"}, log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name() != ProviderOpenAI {
			t.Fatalf("expected provider %q, got %q", ProviderOpenAI, p.Name())
		}
	})

	t.Run("explicit openai", func(t *testing.T) {
		p, err := NewProvider(context.Background(), ProviderConfig{Provider: ProviderOpenAI, APIKey: "sk-test", BaseURL: "https://example.test/v1"}, log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if p.Name() != ProviderOpenAI {
			t.Fatalf("expected provider %q, got %q", ProviderOpenAI, p.Name())
		}
	})

	t.Run("openai requires api key", func(t *testing.T) {
		if _, err := NewProvider(context.Background(), ProviderConfig{Provider: ProviderOpenAI}, log); err == nil {
			t.Fatal("expected error for missing API key")
		}
	})

	t.Run("bedrock requires region", func(t *testing.T) {
		if _, err := NewProvider(context.Background(), ProviderConfig{Provider: ProviderBedrock}, log); err == nil {
			t.Fatal("expected error for missing region")
		}
	})

	t.Run("unknown provider errors", func(t *testing.T) {
		if _, err := NewProvider(context.Background(), ProviderConfig{Provider: "gemini"}, log); err == nil {
			t.Fatal("expected error for unknown provider")
		}
	})
}

func TestNewProviderBedrock(t *testing.T) {
	// LoadDefaultConfig may reach out for credentials/metadata depending on the
	// environment; only run when a region is explicitly configured.
	if os.Getenv("AWS_REGION") == "" && os.Getenv("AWS_DEFAULT_REGION") == "" {
		t.Skip("skipping bedrock construction: no AWS_REGION set")
	}
	p, err := NewProvider(context.Background(), ProviderConfig{Provider: ProviderBedrock, Region: "us-east-1"}, testLogger())
	if err != nil {
		t.Fatalf("unexpected error building bedrock provider: %v", err)
	}
	if p.Name() != ProviderBedrock {
		t.Fatalf("expected provider %q, got %q", ProviderBedrock, p.Name())
	}
}
