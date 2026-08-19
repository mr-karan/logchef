package ai

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type fakeConverse struct {
	in  *bedrockruntime.ConverseInput
	out *bedrockruntime.ConverseOutput
	err error
}

func (f *fakeConverse) Converse(_ context.Context, in *bedrockruntime.ConverseInput, _ ...func(*bedrockruntime.Options)) (*bedrockruntime.ConverseOutput, error) {
	f.in = in
	return f.out, f.err
}

func TestBedrockProviderComplete(t *testing.T) {
	p := &bedrockProvider{logger: testLogger()}
	client := &fakeConverse{
		out: &bedrockruntime.ConverseOutput{
			Output: &types.ConverseOutputMemberMessage{
				Value: types.Message{
					Role: types.ConversationRoleAssistant,
					Content: []types.ContentBlock{
						&types.ContentBlockMemberText{Value: "SELECT 1"},
						&types.ContentBlockMemberText{Value: " FROM t"},
					},
				},
			},
		},
	}
	p.client = client

	got, err := p.Complete(context.Background(), CompletionRequest{User: "q", System: "s", Model: "m", MaxTokens: 10, Temperature: 0.1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "SELECT 1 FROM t" {
		t.Fatalf("unexpected text: %q", got)
	}
	if client.in.InferenceConfig.Temperature != nil {
		t.Fatal("temperature must be omitted for Bedrock model compatibility")
	}
}

func TestBedrockProviderCompleteEmpty(t *testing.T) {
	p := &bedrockProvider{logger: testLogger()}
	p.client = &fakeConverse{
		out: &bedrockruntime.ConverseOutput{
			Output: &types.ConverseOutputMemberMessage{
				Value: types.Message{Role: types.ConversationRoleAssistant},
			},
		},
	}
	if _, err := p.Complete(context.Background(), CompletionRequest{}); err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestBedrockProviderReasoningEffort(t *testing.T) {
	newClient := func() *fakeConverse {
		return &fakeConverse{
			out: &bedrockruntime.ConverseOutput{
				Output: &types.ConverseOutputMemberMessage{
					Value: types.Message{
						Role:    types.ConversationRoleAssistant,
						Content: []types.ContentBlock{&types.ContentBlockMemberText{Value: "SELECT 1"}},
					},
				},
			},
		}
	}

	// Unset effort must not add the model-specific field: models that do not
	// support reasoning reject an unknown parameter outright.
	p := &bedrockProvider{logger: testLogger()}
	client := newClient()
	p.client = client
	if _, err := p.Complete(context.Background(), CompletionRequest{User: "q", Model: "m", MaxTokens: 10}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.in.AdditionalModelRequestFields != nil {
		t.Fatal("AdditionalModelRequestFields must be nil when reasoning effort is unset")
	}

	client = newClient()
	p.client = client
	if _, err := p.Complete(context.Background(), CompletionRequest{User: "q", Model: "m", MaxTokens: 10, ReasoningEffort: "high"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if client.in.AdditionalModelRequestFields == nil {
		t.Fatal("expected AdditionalModelRequestFields to carry the reasoning effort")
	}
	raw, err := client.in.AdditionalModelRequestFields.MarshalSmithyDocument()
	if err != nil {
		t.Fatalf("marshal additional fields: %v", err)
	}
	if want := `{"reasoning":{"effort":"high"}}`; string(raw) != want {
		t.Fatalf("unexpected additional fields: got %s, want %s", raw, want)
	}
}
