// Package askchef runs Ask Chef, Logchef's read-only investigation assistant.
// A turn sends the conversation to the model, runs the tools it asks for
// through the in-process MCP server as the user's assistant principal, and
// streams text, tool progress and evidence back as events.
package askchef

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/mcp"
)

// TurnTimeout bounds one turn.
const TurnTimeout = 120 * time.Second

// ErrPreempted is the cancel cause of a turn that a newer turn from the same
// user replaced.
var ErrPreempted = errors.New("a newer turn replaced this turn")

// Turn limits.
const (
	maxToolCalls       = 12
	maxOutputTokens    = 2048
	maxToolResultBytes = 12 * 1024
	maxErrorChars      = 300
	// maxInputTokens is the input token budget of a turn, summed over its
	// model calls. Every call resends the whole conversation.
	maxInputTokens = 300_000
)

// limitNote follows the tool results once a turn limit stops tool calls.
// Bedrock needs the tool config while the history holds tool blocks, so the
// model still sees the tools; the loop runs none after this.
func limitNote(limit StopReason) string {
	used := fmt.Sprintf("all %d tool calls", maxToolCalls)
	if limit == StopBudget {
		used = "its token budget"
	}
	return "[This turn has used " + used + ". Do not call tools. Answer now from the results above, and list what you could not check under **Not established**.]"
}

// ChatModel streams one model call. ai.BedrockChat and ai.OpenAIChat
// implement it.
type ChatModel interface {
	Stream(ctx context.Context, req ai.ChatRequest, onText func(string)) (ai.ChatResponse, error)
}

// Agent runs Ask Chef turns.
type Agent struct {
	tools *server.MCPServer
	chat  ChatModel
	model string
	log   *slog.Logger
}

// Opts configures an Agent.
type Opts struct {
	// Tools is the MCP server from mcp.NewToolServer.
	Tools *server.MCPServer
	Chat  ChatModel
	Model string
	Log   *slog.Logger
}

// New returns an Agent.
func New(opts Opts) *Agent {
	return &Agent{tools: opts.Tools, chat: opts.Chat, model: opts.Model, log: opts.Log}
}

// Turn is one authorized turn.
type Turn struct {
	// Principal is the assistant principal of the user who asks.
	Principal access.Principal
	Request   Request
	Alert     *AlertContext
}

// failure is a turn error with a message that is safe to show.
type failure struct {
	message string
	err     error
}

func (f *failure) Error() string { return f.err.Error() }
func (f *failure) Unwrap() error { return f.err }

// turnRun is the state of one running turn.
type turnRun struct {
	agent     *Agent
	log       *slog.Logger
	send      func(Event)
	tools     *toolbox
	messages  []ai.ChatMessage
	toolCalls int
	nextID    EvidenceID
	usage     ai.ChatUsage
}

// Run runs one turn and passes its events to emit, in order. The last event
// is Done or Failure, unless the turn is cancelled. A turn cancelled with
// ErrPreempted ends with Done. emit returns false when the client is gone,
// which cancels the turn.
func (a *Agent) Run(ctx context.Context, turn Turn, emit func(Event) bool) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(mcp.WithPrincipal(ctx, turn.Principal), TurnTimeout)
	defer cancel()

	run := &turnRun{
		agent: a,
		log: a.log.With("user_id", turn.Principal.User.ID, "team_id", turn.Request.TeamID,
			"source_id", turn.Request.SourceID),
		send: func(ev Event) {
			if !emit(ev) {
				cancel()
			}
		},
		messages: conversation(turn.Request),
		nextID:   nextEvidenceID(turn.Request.Evidence),
	}
	stop, err := run.converse(ctx, systemPrompt(turn.Request, turn.Alert))
	durationMS := time.Since(started).Milliseconds()
	logAttrs := []any{"tool_calls", run.toolCalls, "duration_ms", durationMS,
		"input_tokens", run.usage.InputTokens, "output_tokens", run.usage.OutputTokens}

	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		stop = StopDeadline
	case errors.Is(context.Cause(ctx), ErrPreempted):
		stop = StopPreempted
	case ctx.Err() != nil:
		run.log.Info("ask chef turn cancelled", append(logAttrs, "cause", context.Cause(ctx))...)
		return
	case err != nil:
		run.log.Error("ask chef turn failed", append(logAttrs, "error", err)...)
		message := "Ask Chef could not finish this answer."
		if f, ok := errors.AsType[*failure](err); ok {
			message = f.message
		}
		run.send(Failure{Message: message})
		return
	}
	run.log.Info("ask chef turn", append(logAttrs, "stop_reason", stop)...)
	run.send(Done{
		StopReason:   stop,
		ToolCalls:    run.toolCalls,
		InputTokens:  run.usage.InputTokens,
		OutputTokens: run.usage.OutputTokens,
		DurationMS:   durationMS,
	})
}

func (r *turnRun) converse(ctx context.Context, system string) (StopReason, error) {
	tools, err := openToolbox(ctx, r.agent.tools)
	if err != nil {
		return "", &failure{message: "Ask Chef could not load its tools.", err: err}
	}
	defer tools.close()
	r.tools = tools

	// limit is set once a turn limit stops tool calls. The model call after
	// that is the last one. refused is set when the tool limit refused a
	// call the model asked for.
	var limit StopReason
	refused := false
	for {
		resp, err := r.agent.chat.Stream(ctx, ai.ChatRequest{
			Model:     r.agent.model,
			System:    system,
			Messages:  r.messages,
			Tools:     tools.tools,
			MaxTokens: maxOutputTokens,
		}, func(delta string) { r.send(Text{Delta: delta}) })
		if err != nil {
			return "", &failure{message: "Ask Chef could not get an answer from the model.", err: err}
		}
		r.usage.InputTokens += resp.Usage.InputTokens
		r.usage.OutputTokens += resp.Usage.OutputTokens
		switch resp.StopReason {
		case ai.ChatStopContentFiltered, ai.ChatStopGuardrailIntervened:
			return "", &failure{message: "The model's content filter stopped this answer.", err: fmt.Errorf("model stopped: %s", resp.StopReason)}
		case ai.ChatStopMaxTokens:
			// A cut-off message can hold a cut-off plan, so none of its tool
			// calls run.
			return StopMaxTokens, nil
		}
		uses := resp.ToolUses()
		if limit != "" {
			if !hasText(resp) {
				return "", &failure{message: "Ask Chef reached a turn limit before it wrote an answer. Try a narrower question.", err: fmt.Errorf("no answer after the %s limit", limit)}
			}
			// A turn that used exactly its tool calls and then answered
			// ended normally.
			if limit == StopToolLimit && !refused && len(uses) == 0 {
				return StopEnd, nil
			}
			return limit, nil
		}
		if len(uses) == 0 {
			if !hasText(resp) {
				return "", &failure{message: "Ask Chef got an empty answer from the model. Try again.", err: errors.New("model ended the turn with no text and no tool calls")}
			}
			return StopEnd, nil
		}

		r.messages = append(r.messages, ai.ChatMessage{Role: ai.ChatRoleAssistant, Blocks: resp.Blocks})
		if r.usage.InputTokens >= maxInputTokens {
			limit = StopBudget
		}
		results := make([]ai.ChatBlock, 0, len(uses)+1)
		for _, use := range uses {
			if limit == StopBudget {
				results = append(results, ai.ToolResultBlock{ToolUseID: use.ID, Text: "[tool not run: this turn has used its token budget]", IsError: true})
				continue
			}
			result, refusedCall := r.runTool(ctx, use)
			if err := ctx.Err(); err != nil {
				return "", err
			}
			refused = refused || refusedCall
			results = append(results, result)
		}
		if limit == "" && r.toolCalls >= maxToolCalls {
			limit = StopToolLimit
		}
		if limit != "" {
			results = append(results, ai.TextBlock{Text: limitNote(limit)})
		}
		r.messages = append(r.messages, ai.ChatMessage{Role: ai.ChatRoleUser, Blocks: results})
	}
}

func hasText(resp ai.ChatResponse) bool {
	for _, block := range resp.Blocks {
		if text, ok := block.(ai.TextBlock); ok && strings.TrimSpace(text.Text) != "" {
			return true
		}
	}
	return false
}

// runTool runs one tool call and returns the result for the model, and
// whether the tool limit refused the call. Every call gets a result, because
// the model API needs one for each tool use.
func (r *turnRun) runTool(ctx context.Context, use ai.ToolUseBlock) (result ai.ToolResultBlock, refused bool) {
	if r.toolCalls >= maxToolCalls {
		return ai.ToolResultBlock{ToolUseID: use.ID, Text: "[tool not run: this turn has used all its tool calls]", IsError: true}, true
	}
	r.toolCalls++
	callID := "c" + strconv.Itoa(r.toolCalls)
	r.send(ToolStart{CallID: callID, Tool: use.Name})

	var (
		text string
		err  error
	)
	if use.InvalidInput {
		err = errors.New("arguments were not valid JSON")
	} else {
		text, err = r.tools.call(ctx, use.Name, use.Input)
	}
	if ctx.Err() != nil {
		// A cancelled turn did not fail this call. converse ends the turn.
		return ai.ToolResultBlock{ToolUseID: use.ID, Text: "[tool not run: the turn was cancelled]", IsError: true}, false
	}
	if err != nil {
		message := oneLine(err.Error(), maxErrorChars)
		r.log.Warn("ask chef tool failed", "tool", use.Name, "error", message)
		r.send(ToolEnd{CallID: callID, OK: false, Summary: message, Evidence: []Evidence{}})
		return ai.ToolResultBlock{ToolUseID: use.ID, Text: "[tool did not complete: " + message + "]", IsError: true}, false
	}

	reading := r.read(use, text)
	if len(reading.evidence) > 0 {
		text = evidenceMarker(reading.evidence) + "\n" + text
	}
	r.send(ToolEnd{CallID: callID, OK: true, Summary: reading.summary, Evidence: reading.evidence})
	return ai.ToolResultBlock{ToolUseID: use.ID, Text: capResult(text)}, false
}

// read reports what a successful call read and numbers its evidence. A result
// it cannot read gives no evidence, so the answer cannot cite the call.
func (r *turnRun) read(use ai.ToolUseBlock, result string) toolReading {
	reading := toolReading{summary: "done"}
	if read := allowedTools[use.Name].read; read != nil {
		got, err := read(use.Input, result)
		if err != nil {
			r.log.Warn("ask chef could not read tool evidence", "tool", use.Name, "error", err)
		} else {
			reading = got
		}
	}
	evidence := make([]Evidence, 0, len(reading.evidence))
	for _, e := range reading.evidence {
		e.Tool = use.Name
		e.Summary = clip(e.Summary, maxEvidenceSummaryChars)
		if err := e.check(); err != nil {
			r.log.Warn("ask chef dropped tool evidence", "tool", use.Name, "error", err)
			continue
		}
		e.ID = r.nextID
		r.nextID++
		evidence = append(evidence, e)
	}
	reading.evidence = evidence
	return reading
}

// clip cuts s to at most limit characters.
func clip(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}

// evidenceMarker heads a tool result with the IDs the answer may cite and
// what each ID covers, such as which logs a field count is over.
func evidenceMarker(evidence []Evidence) string {
	parts := make([]string, len(evidence))
	for i, e := range evidence {
		parts[i] = e.ID.String() + " = " + e.Summary
	}
	return "[evidence " + strings.Join(parts, "; ") + "]"
}

// conversation turns the history and the new message into model messages.
// The model API needs a conversation that starts with the user and
// alternates, so an empty message is dropped, a leading assistant message is
// dropped, and messages in a row from one role are joined.
func conversation(req Request) []ai.ChatMessage {
	var messages []ai.ChatMessage
	add := func(role ai.ChatRole, text string) {
		if strings.TrimSpace(text) == "" || (len(messages) == 0 && role == ai.ChatRoleAssistant) {
			return
		}
		if n := len(messages); n > 0 && messages[n-1].Role == role {
			messages[n-1].Blocks = append(messages[n-1].Blocks, ai.TextBlock{Text: text})
			return
		}
		messages = append(messages, ai.ChatMessage{Role: role, Blocks: []ai.ChatBlock{ai.TextBlock{Text: text}}})
	}
	for _, h := range req.History {
		add(h.Role, h.Text)
	}
	add(ai.ChatRoleUser, req.Message)
	return messages
}

// nextEvidenceID continues the numbering of the prior evidence.
func nextEvidenceID(prior []Evidence) EvidenceID {
	next := EvidenceID(1)
	for _, e := range prior {
		next = max(next, e.ID+1)
	}
	return next
}
