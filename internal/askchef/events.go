package askchef

// Event is one server-sent event of a turn. Name is the SSE event type and
// the value encodes as its JSON data.
type Event interface{ Name() string }

// StopReason says why a turn ended.
type StopReason string

const (
	StopEnd       StopReason = "end"
	StopToolLimit StopReason = "tool_limit"
	StopDeadline  StopReason = "deadline"
)

// ToolStart is sent before a tool runs.
type ToolStart struct {
	CallID string `json:"call_id"`
	Tool   string `json:"tool"`
}

// ToolEnd is sent after a tool ran. Evidence is nil when the call read no
// logs or failed.
type ToolEnd struct {
	CallID   string    `json:"call_id"`
	OK       bool      `json:"ok"`
	Summary  string    `json:"summary"`
	Evidence *Evidence `json:"evidence"`
}

// Text is a chunk of the answer.
type Text struct {
	Delta string `json:"delta"`
}

// Done ends a turn that produced an answer.
type Done struct {
	StopReason   StopReason `json:"stop_reason"`
	ToolCalls    int        `json:"tool_calls"`
	InputTokens  int        `json:"input_tokens"`
	OutputTokens int        `json:"output_tokens"`
	DurationMS   int64      `json:"duration_ms"`
}

// Failure ends a turn that could not finish. No Done follows it.
type Failure struct {
	Message string `json:"message"`
}

func (ToolStart) Name() string { return "tool_start" }
func (ToolEnd) Name() string   { return "tool_end" }
func (Text) Name() string      { return "text" }
func (Done) Name() string      { return "done" }
func (Failure) Name() string   { return "error" }
