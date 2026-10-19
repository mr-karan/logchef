package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/document"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
)

// openConverseStream starts one ConverseStream call. The SDK hides the
// stream of a ConverseStreamOutput, so tests replace this function instead.
type openConverseStream func(ctx context.Context, in *bedrockruntime.ConverseStreamInput) (*bedrockruntime.ConverseStreamEventStream, error)

// BedrockChat runs tool-calling conversations through the Bedrock
// ConverseStream API. It authenticates through the default AWS credential
// chain. It never enables reasoning output.
type BedrockChat struct {
	open openConverseStream
}

// NewBedrockChat builds a BedrockChat for region.
func NewBedrockChat(ctx context.Context, region string) (*BedrockChat, error) {
	if region == "" {
		return nil, errors.New("AWS region is required for the bedrock provider (set ai.region)")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config for bedrock: %w", err)
	}
	client := bedrockruntime.NewFromConfig(awsCfg)
	return &BedrockChat{open: func(ctx context.Context, in *bedrockruntime.ConverseStreamInput) (*bedrockruntime.ConverseStreamEventStream, error) {
		out, err := client.ConverseStream(ctx, in)
		if err != nil {
			return nil, err
		}
		return out.GetStream(), nil
	}}, nil
}

// Stream runs one model call. It calls onText with each text delta as it
// arrives and returns the whole assistant message.
func (c *BedrockChat) Stream(ctx context.Context, req ChatRequest, onText func(string)) (ChatResponse, error) {
	in, err := converseStreamInput(req)
	if err != nil {
		return ChatResponse{}, err
	}
	stream, err := c.open(ctx, in)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("bedrock ConverseStream API error: %w", err)
	}
	defer func() { _ = stream.Close() }()

	acc := streamAccumulator{blocks: map[int32]*streamBlock{}}
	for {
		select {
		case <-ctx.Done():
			return ChatResponse{}, ctx.Err()
		case event, ok := <-stream.Events():
			if !ok {
				if err := stream.Err(); err != nil {
					return ChatResponse{}, fmt.Errorf("bedrock ConverseStream stream error: %w", err)
				}
				return acc.response()
			}
			acc.add(event, onText)
		}
	}
}

// streamBlock collects one content block of the streamed message.
type streamBlock struct {
	toolUseID string
	toolName  string
	text      strings.Builder
	input     strings.Builder
}

type streamAccumulator struct {
	blocks     map[int32]*streamBlock
	stopReason ChatStopReason
	usage      ChatUsage
}

// block returns the block at index. Bedrock sends no start event for a text
// block, so the first delta creates it.
func (a *streamAccumulator) block(index *int32) *streamBlock {
	i := aws.ToInt32(index)
	b, ok := a.blocks[i]
	if !ok {
		b = &streamBlock{}
		a.blocks[i] = b
	}
	return b
}

func (a *streamAccumulator) add(event types.ConverseStreamOutput, onText func(string)) {
	switch e := event.(type) {
	case *types.ConverseStreamOutputMemberContentBlockStart:
		if start, ok := e.Value.Start.(*types.ContentBlockStartMemberToolUse); ok {
			b := a.block(e.Value.ContentBlockIndex)
			b.toolUseID = aws.ToString(start.Value.ToolUseId)
			b.toolName = aws.ToString(start.Value.Name)
		}
	case *types.ConverseStreamOutputMemberContentBlockDelta:
		switch delta := e.Value.Delta.(type) {
		case *types.ContentBlockDeltaMemberText:
			a.block(e.Value.ContentBlockIndex).text.WriteString(delta.Value)
			onText(delta.Value)
		case *types.ContentBlockDeltaMemberToolUse:
			a.block(e.Value.ContentBlockIndex).input.WriteString(aws.ToString(delta.Value.Input))
		}
	case *types.ConverseStreamOutputMemberMessageStop:
		a.stopReason = ChatStopReason(e.Value.StopReason)
	case *types.ConverseStreamOutputMemberMetadata:
		if usage := e.Value.Usage; usage != nil {
			a.usage = ChatUsage{InputTokens: int(aws.ToInt32(usage.InputTokens)), OutputTokens: int(aws.ToInt32(usage.OutputTokens))}
		}
	}
}

func (a *streamAccumulator) response() (ChatResponse, error) {
	if a.stopReason == "" {
		return ChatResponse{}, errors.New("bedrock stream ended before the message stopped")
	}
	indexes := make([]int32, 0, len(a.blocks))
	for i := range a.blocks {
		indexes = append(indexes, i)
	}
	slices.Sort(indexes)

	resp := ChatResponse{StopReason: a.stopReason, Usage: a.usage}
	for _, i := range indexes {
		b := a.blocks[i]
		switch {
		case b.toolUseID != "":
			input := json.RawMessage(b.input.String())
			if len(input) == 0 {
				input = json.RawMessage(`{}`)
			}
			// A message cut off by max_tokens can end inside a tool call. Its
			// input is not JSON, and sending it back would fail the next call.
			if !isJSONObject(input) {
				continue
			}
			resp.Blocks = append(resp.Blocks, ToolUseBlock{ID: b.toolUseID, Name: b.toolName, Input: input})
		case b.text.Len() > 0:
			resp.Blocks = append(resp.Blocks, TextBlock{Text: b.text.String()})
		}
	}
	return resp, nil
}

func isJSONObject(raw json.RawMessage) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(raw, &object) == nil && object != nil
}

func converseStreamInput(req ChatRequest) (*bedrockruntime.ConverseStreamInput, error) {
	messages := make([]types.Message, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := types.ConversationRoleUser
		if m.Role == ChatRoleAssistant {
			role = types.ConversationRoleAssistant
		}
		content := make([]types.ContentBlock, 0, len(m.Blocks))
		for _, block := range m.Blocks {
			converted, err := bedrockContentBlock(block)
			if err != nil {
				return nil, err
			}
			content = append(content, converted)
		}
		messages = append(messages, types.Message{Role: role, Content: content})
	}

	maxTokens := min(max(req.MaxTokens, 0), math.MaxInt32)
	in := &bedrockruntime.ConverseStreamInput{
		ModelId:         aws.String(req.Model),
		Messages:        messages,
		InferenceConfig: &types.InferenceConfiguration{MaxTokens: aws.Int32(int32(maxTokens))},
	}
	if req.System != "" {
		in.System = []types.SystemContentBlock{&types.SystemContentBlockMemberText{Value: req.System}}
	}
	if len(req.Tools) > 0 {
		tools := make([]types.Tool, 0, len(req.Tools))
		for _, tool := range req.Tools {
			spec, err := bedrockToolSpec(tool)
			if err != nil {
				return nil, err
			}
			tools = append(tools, spec)
		}
		in.ToolConfig = &types.ToolConfiguration{Tools: tools}
	}
	return in, nil
}

func bedrockContentBlock(block ChatBlock) (types.ContentBlock, error) {
	switch b := block.(type) {
	case TextBlock:
		return &types.ContentBlockMemberText{Value: b.Text}, nil
	case ToolUseBlock:
		var input any
		if err := json.Unmarshal(b.Input, &input); err != nil {
			return nil, fmt.Errorf("decoding input of tool call %s: %w", b.Name, err)
		}
		return &types.ContentBlockMemberToolUse{Value: types.ToolUseBlock{
			ToolUseId: aws.String(b.ID),
			Name:      aws.String(b.Name),
			Input:     document.NewLazyDocument(input),
		}}, nil
	case ToolResultBlock:
		result := types.ToolResultBlock{
			ToolUseId: aws.String(b.ToolUseID),
			Content:   []types.ToolResultContentBlock{&types.ToolResultContentBlockMemberText{Value: b.Text}},
		}
		if b.IsError {
			result.Status = types.ToolResultStatusError
		}
		return &types.ContentBlockMemberToolResult{Value: result}, nil
	}
	return nil, fmt.Errorf("unknown chat block %T", block)
}

// bedrockToolSpec converts a tool's JSON Schema into a Bedrock tool spec.
func bedrockToolSpec(tool ChatTool) (types.Tool, error) {
	var schema map[string]any
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil || schema == nil {
		return nil, fmt.Errorf("input schema of tool %s is not a JSON object", tool.Name)
	}
	return &types.ToolMemberToolSpec{Value: types.ToolSpecification{
		Name:        aws.String(tool.Name),
		Description: aws.String(tool.Description),
		InputSchema: &types.ToolInputSchemaMemberJson{Value: document.NewLazyDocument(schema)},
	}}, nil
}
