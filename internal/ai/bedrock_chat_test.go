package ai

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

type fakeStreamReader struct {
	events chan types.ConverseStreamOutput
	err    error
}

func (r *fakeStreamReader) Events() <-chan types.ConverseStreamOutput { return r.events }
func (r *fakeStreamReader) Close() error                              { return nil }
func (r *fakeStreamReader) Err() error                                { return r.err }

// scriptedBedrock returns a BedrockChat that replays events and records the
// request it was sent.
func scriptedBedrock(sent **bedrockruntime.ConverseStreamInput, streamErr error, events ...types.ConverseStreamOutput) *BedrockChat {
	return &BedrockChat{open: func(_ context.Context, in *bedrockruntime.ConverseStreamInput) (*bedrockruntime.ConverseStreamEventStream, error) {
		*sent = in
		ch := make(chan types.ConverseStreamOutput, len(events))
		for _, e := range events {
			ch <- e
		}
		close(ch)
		return bedrockruntime.NewConverseStreamEventStream(func(es *bedrockruntime.ConverseStreamEventStream) {
			es.Reader = &fakeStreamReader{events: ch, err: streamErr}
		}), nil
	}}
}

func textDelta(index int32, text string) types.ConverseStreamOutput {
	return &types.ConverseStreamOutputMemberContentBlockDelta{Value: types.ContentBlockDeltaEvent{
		ContentBlockIndex: aws.Int32(index),
		Delta:             &types.ContentBlockDeltaMemberText{Value: text},
	}}
}

func toolStart(index int32, id, name string) types.ConverseStreamOutput {
	return &types.ConverseStreamOutputMemberContentBlockStart{Value: types.ContentBlockStartEvent{
		ContentBlockIndex: aws.Int32(index),
		Start:             &types.ContentBlockStartMemberToolUse{Value: types.ToolUseBlockStart{ToolUseId: aws.String(id), Name: aws.String(name)}},
	}}
}

func toolDelta(index int32, input string) types.ConverseStreamOutput {
	return &types.ConverseStreamOutputMemberContentBlockDelta{Value: types.ContentBlockDeltaEvent{
		ContentBlockIndex: aws.Int32(index),
		Delta:             &types.ContentBlockDeltaMemberToolUse{Value: types.ToolUseBlockDelta{Input: aws.String(input)}},
	}}
}

func messageStop(reason types.StopReason) types.ConverseStreamOutput {
	return &types.ConverseStreamOutputMemberMessageStop{Value: types.MessageStopEvent{StopReason: reason}}
}

func usage(in, out int32) types.ConverseStreamOutput {
	return &types.ConverseStreamOutputMemberMetadata{Value: types.ConverseStreamMetadataEvent{
		Usage: &types.TokenUsage{InputTokens: aws.Int32(in), OutputTokens: aws.Int32(out), TotalTokens: aws.Int32(in + out)},
	}}
}

func TestBedrockChatStreamAssemblesTextAndToolCalls(t *testing.T) {
	var sent *bedrockruntime.ConverseStreamInput
	chat := scriptedBedrock(&sent, nil,
		textDelta(0, "Errors "),
		textDelta(0, "rose."),
		toolStart(1, "tu-1", "query_logchefql"),
		toolDelta(1, `{"query":`),
		toolDelta(1, `"level=\"error\""}`),
		toolStart(2, "tu-2", "get_source_schema"),
		messageStop(types.StopReasonToolUse),
		usage(120, 30),
	)

	var streamed strings.Builder
	resp, err := chat.Stream(context.Background(), ChatRequest{
		Model:     "model-id",
		System:    "be brief",
		MaxTokens: 2048,
		Messages: []ChatMessage{
			{Role: ChatRoleUser, Blocks: []ChatBlock{TextBlock{Text: "why?"}}},
			{Role: ChatRoleAssistant, Blocks: []ChatBlock{ToolUseBlock{ID: "tu-0", Name: "get_source_schema", Input: json.RawMessage(`{"team_id":1}`)}}},
			{Role: ChatRoleUser, Blocks: []ChatBlock{ToolResultBlock{ToolUseID: "tu-0", Text: "[tool did not complete: timed out]", IsError: true}}},
		},
	}, func(delta string) { streamed.WriteString(delta) })
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	if streamed.String() != "Errors rose." {
		t.Fatalf("streamed text = %q, want %q", streamed.String(), "Errors rose.")
	}
	want := []ChatBlock{
		TextBlock{Text: "Errors rose."},
		ToolUseBlock{ID: "tu-1", Name: "query_logchefql", Input: json.RawMessage(`{"query":"level=\"error\""}`)},
		ToolUseBlock{ID: "tu-2", Name: "get_source_schema", Input: json.RawMessage(`{}`)},
	}
	if !reflect.DeepEqual(resp.Blocks, want) {
		t.Fatalf("blocks = %#v, want %#v", resp.Blocks, want)
	}
	if resp.Usage != (ChatUsage{InputTokens: 120, OutputTokens: 30}) || resp.StopReason != "tool_use" {
		t.Fatalf("usage = %+v stop = %q", resp.Usage, resp.StopReason)
	}

	if aws.ToString(sent.ModelId) != "model-id" || aws.ToInt32(sent.InferenceConfig.MaxTokens) != 2048 {
		t.Fatalf("model = %q max tokens = %d", aws.ToString(sent.ModelId), aws.ToInt32(sent.InferenceConfig.MaxTokens))
	}
	if sent.AdditionalModelRequestFields != nil {
		t.Fatal("reasoning or other model fields must not be sent")
	}
	if sent.ToolConfig != nil {
		t.Fatal("a request without tools must not send a tool config")
	}
	if len(sent.Messages) != 3 || sent.Messages[1].Role != types.ConversationRoleAssistant {
		t.Fatalf("messages = %+v", sent.Messages)
	}
	result, ok := sent.Messages[2].Content[0].(*types.ContentBlockMemberToolResult)
	if !ok || aws.ToString(result.Value.ToolUseId) != "tu-0" || result.Value.Status != types.ToolResultStatusError {
		t.Fatalf("tool result block = %#v", sent.Messages[2].Content[0])
	}
}

func TestBedrockChatStreamFailures(t *testing.T) {
	for _, tc := range []struct {
		name      string
		streamErr error
		events    []types.ConverseStreamOutput
	}{
		{"stream error", errors.New("throttled"), []types.ConverseStreamOutput{textDelta(0, "par")}},
		{"stream ends before message stop", nil, []types.ConverseStreamOutput{textDelta(0, "par")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent *bedrockruntime.ConverseStreamInput
			chat := scriptedBedrock(&sent, tc.streamErr, tc.events...)
			if _, err := chat.Stream(context.Background(), ChatRequest{Model: "m"}, func(string) {}); err == nil {
				t.Fatal("Stream succeeded, want an error")
			}
		})
	}
}

func TestBedrockToolSpecFromMCPSchema(t *testing.T) {
	schema := `{"type":"object","properties":{"team_id":{"type":"integer","description":"Team ID"},"query":{"type":"string"}},"required":["team_id","query"],"additionalProperties":false}`
	in, err := converseStreamInput(ChatRequest{
		Model: "m",
		Tools: []ChatTool{{Name: "query_logchefql", Description: "Run LogchefQL", InputSchema: json.RawMessage(schema)}},
	})
	if err != nil {
		t.Fatalf("converseStreamInput: %v", err)
	}
	if in.ToolConfig == nil || len(in.ToolConfig.Tools) != 1 {
		t.Fatalf("tool config = %+v", in.ToolConfig)
	}
	spec, ok := in.ToolConfig.Tools[0].(*types.ToolMemberToolSpec)
	if !ok {
		t.Fatalf("tool = %T, want a tool spec", in.ToolConfig.Tools[0])
	}
	if aws.ToString(spec.Value.Name) != "query_logchefql" || aws.ToString(spec.Value.Description) != "Run LogchefQL" {
		t.Fatalf("spec = %+v", spec.Value)
	}
	doc, ok := spec.Value.InputSchema.(*types.ToolInputSchemaMemberJson)
	if !ok {
		t.Fatalf("input schema = %T, want JSON", spec.Value.InputSchema)
	}
	encoded, err := doc.Value.MarshalSmithyDocument()
	if err != nil {
		t.Fatalf("MarshalSmithyDocument: %v", err)
	}
	var got, want any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(schema), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("schema sent = %s, want %s", encoded, schema)
	}

	for _, bad := range []string{`[]`, `null`, `"object"`, `{`} {
		if _, err := converseStreamInput(ChatRequest{Tools: []ChatTool{{Name: "x", InputSchema: json.RawMessage(bad)}}}); err == nil {
			t.Errorf("schema %s: converted, want an error", bad)
		}
	}
}
