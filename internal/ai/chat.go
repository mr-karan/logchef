package ai

import "encoding/json"

// ChatRole is the author of a ChatMessage.
type ChatRole string

const (
	ChatRoleUser      ChatRole = "user"
	ChatRoleAssistant ChatRole = "assistant"
)

// ChatMessage is one message of a tool-calling conversation.
type ChatMessage struct {
	Role   ChatRole
	Blocks []ChatBlock
}

// ChatBlock is one piece of message content: TextBlock, ToolUseBlock or
// ToolResultBlock.
type ChatBlock interface{ chatBlock() }

// TextBlock is plain text.
type TextBlock struct {
	Text string
}

// ToolUseBlock is a tool call that the model requested. Input is a JSON
// object.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResultBlock answers the ToolUseBlock with the same ID.
type ToolResultBlock struct {
	ToolUseID string
	Text      string
	IsError   bool
}

func (TextBlock) chatBlock()       {}
func (ToolUseBlock) chatBlock()    {}
func (ToolResultBlock) chatBlock() {}

// ChatTool is a tool the model may call. InputSchema is a JSON Schema object.
type ChatTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// ChatRequest is one model call.
type ChatRequest struct {
	Model     string
	System    string
	Messages  []ChatMessage
	Tools     []ChatTool
	MaxTokens int
}

// ChatUsage counts the tokens of one model call.
type ChatUsage struct {
	InputTokens  int
	OutputTokens int
}

// ChatStopReason says why the model stopped. The values are Bedrock's; the
// constants are the reasons callers act on.
type ChatStopReason string

const (
	ChatStopMaxTokens           ChatStopReason = "max_tokens"
	ChatStopContentFiltered     ChatStopReason = "content_filtered"
	ChatStopGuardrailIntervened ChatStopReason = "guardrail_intervened"
)

// ChatResponse is the assistant message of one model call.
type ChatResponse struct {
	Blocks     []ChatBlock
	StopReason ChatStopReason
	Usage      ChatUsage
}

// ToolUses returns the tool calls in the response, in order.
func (r ChatResponse) ToolUses() []ToolUseBlock {
	var uses []ToolUseBlock
	for _, block := range r.Blocks {
		if use, ok := block.(ToolUseBlock); ok {
			uses = append(uses, use)
		}
	}
	return uses
}
