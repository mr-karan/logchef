package askchef

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/mcp"
	"github.com/mr-karan/logchef/pkg/models"
)

// evidenceRule says where a tool's input holds the query and the time window
// of the logs it read.
type evidenceRule struct {
	mode QueryMode
	// queryField is empty when the tool takes no filter, which reads all logs.
	queryField string
	// windows are start and end input fields. The evidence window spans all
	// of them.
	windows [][2]string
}

// allowedTool is a tool Ask Chef may call.
type allowedTool struct {
	// evidence is nil when the tool reads no logs.
	evidence  *evidenceRule
	summarize func(result string) string
}

var (
	startEndWindow = [][2]string{{"start_time", "end_time"}}
	compareWindows = [][2]string{{"window1_start", "window1_end"}, {"window2_start", "window2_end"}}
)

// allowedTools is every MCP tool Ask Chef may call. The model never sees or
// runs a tool that is missing here, such as the open_investigation UI tool.
// No allowed tool runs model-written SQL: raw_sql runs in the datasource's
// permissive mode, where it can read other tables or call url().
var allowedTools = map[string]allowedTool{
	"get_sources":              {},
	"get_source_schema":        {},
	"translate_logchefql":      {},
	"get_log_context":          {},
	"list_alerts":              {},
	"get_alert_history":        {},
	"query_logchefql":          {evidence: &evidenceRule{mode: ModeLogchefQL, queryField: "query", windows: startEndWindow}, summarize: summarizeRows},
	"compare_windows":          {evidence: &evidenceRule{mode: ModeLogchefQL, queryField: "query", windows: compareWindows}, summarize: summarizeCompare},
	"get_field_values":         {evidence: &evidenceRule{mode: ModeLogchefQL, windows: startEndWindow}},
	"top_values":               {evidence: &evidenceRule{mode: ModeLogchefQL, windows: startEndWindow}},
	"get_all_field_dimensions": {evidence: &evidenceRule{mode: ModeLogchefQL, windows: startEndWindow}},
}

// evidenceFor returns the evidence of a successful call, without ID and
// summary. It returns false when the tool reads no logs or the input has no
// team, source or parseable window.
func (t allowedTool) evidenceFor(tool string, input json.RawMessage) (Evidence, bool) {
	rule := t.evidence
	if rule == nil {
		return Evidence{}, false
	}
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return Evidence{}, false
	}
	teamID, okTeam := positiveInt(args["team_id"])
	sourceID, okSource := positiveInt(args["source_id"])
	if !okTeam || !okSource {
		return Evidence{}, false
	}
	loc := time.UTC
	if tz, _ := args["timezone"].(string); tz != "" {
		var err error
		if loc, err = time.LoadLocation(tz); err != nil {
			return Evidence{}, false
		}
	}
	var start, end time.Time
	for _, fields := range rule.windows {
		s, okStart := parseToolTime(args[fields[0]], loc)
		e, okEnd := parseToolTime(args[fields[1]], loc)
		if !okStart || !okEnd {
			return Evidence{}, false
		}
		if start.IsZero() || s.Before(start) {
			start = s
		}
		if e.After(end) {
			end = e
		}
	}
	query, _ := args[rule.queryField].(string)
	return Evidence{
		Tool:     tool,
		TeamID:   models.TeamID(teamID),
		SourceID: models.SourceID(sourceID),
		Mode:     rule.mode,
		Query:    query,
		StartMS:  start.UnixMilli(),
		EndMS:    end.UnixMilli(),
	}, true
}

func (t allowedTool) summary(result string) string {
	if t.summarize == nil {
		return "done"
	}
	return t.summarize(result)
}

func positiveInt(v any) (int, bool) {
	f, ok := v.(float64)
	if !ok || f < 1 || f > math.MaxInt32 || f != math.Trunc(f) {
		return 0, false
	}
	return int(f), true
}

// parseToolTime reads a time the way the tools do: RFC3339, or
// "YYYY-MM-DD HH:MM:SS" in the tool's timezone.
func parseToolTime(v any, loc *time.Location) (time.Time, bool) {
	raw, _ := v.(string)
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, true
	}
	if t, err := time.ParseInLocation(time.DateTime, raw, loc); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func summarizeRows(result string) string {
	var r mcp.QueryResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		return "done"
	}
	if r.Stats.Truncated {
		return fmt.Sprintf("%d rows (truncated)", r.RowCount)
	}
	return fmt.Sprintf("%d rows", r.RowCount)
}

func summarizeCompare(result string) string {
	var r mcp.CompareWindowsResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		return "done"
	}
	return fmt.Sprintf("%d rows vs %d rows", r.Window1.RowCount, r.Window2.RowCount)
}

// capResult cuts a tool result to maxToolResultBytes on a character
// boundary and says so, so the model knows it saw only part.
func capResult(text string) string {
	if len(text) <= maxToolResultBytes {
		return text
	}
	n := maxToolResultBytes
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	return fmt.Sprintf("%s\n[result cut: showing the first %d of %d bytes]", text[:n], n, len(text))
}

// oneLine collapses whitespace and caps s at limit characters.
func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "..."
}

// toolbox calls the allowed tools on the in-process MCP server. The context
// of every call must carry the turn's principal.
type toolbox struct {
	client *client.Client
	// tools are the allowed tools the principal may call, for the model.
	tools []ai.ChatTool
}

func openToolbox(ctx context.Context, srv *server.MCPServer) (*toolbox, error) {
	c, err := client.NewInProcessClient(srv)
	if err != nil {
		return nil, fmt.Errorf("creating MCP client: %w", err)
	}
	tb := &toolbox{client: c}
	if err := tb.start(ctx); err != nil {
		tb.close()
		return nil, err
	}
	return tb, nil
}

func (tb *toolbox) start(ctx context.Context) error {
	if err := tb.client.Start(ctx); err != nil {
		return fmt.Errorf("starting MCP client: %w", err)
	}
	var init mcpgo.InitializeRequest
	init.Params.ClientInfo = mcpgo.Implementation{Name: "ask-chef", Version: "1"}
	if _, err := tb.client.Initialize(ctx, init); err != nil {
		return fmt.Errorf("initializing MCP client: %w", err)
	}
	listed, err := tb.client.ListTools(ctx, mcpgo.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("listing MCP tools: %w", err)
	}
	for i := range listed.Tools {
		tool := &listed.Tools[i]
		if _, ok := allowedTools[tool.Name]; !ok {
			continue
		}
		schema, err := inputSchema(tool)
		if err != nil {
			return err
		}
		tb.tools = append(tb.tools, ai.ChatTool{Name: tool.Name, Description: tool.Description, InputSchema: schema})
	}
	return nil
}

func (tb *toolbox) close() { _ = tb.client.Close() }

func inputSchema(tool *mcpgo.Tool) (json.RawMessage, error) {
	encoded, err := json.Marshal(tool)
	if err != nil {
		return nil, fmt.Errorf("encoding tool %s: %w", tool.Name, err)
	}
	var fields struct {
		InputSchema json.RawMessage `json:"inputSchema"`
	}
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, fmt.Errorf("decoding tool %s: %w", tool.Name, err)
	}
	return fields.InputSchema, nil
}

// call runs one tool and returns its result text. A tool outside
// allowedTools is refused without a call. The error text of a failed call
// is safe to show.
func (tb *toolbox) call(ctx context.Context, name string, input json.RawMessage) (string, error) {
	if _, ok := allowedTools[name]; !ok {
		return "", fmt.Errorf("tool %q is not available", name)
	}
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil || args == nil {
		return "", errors.New("tool input is not a JSON object")
	}
	var req mcpgo.CallToolRequest
	req.Params.Name = name
	req.Params.Arguments = args
	res, err := tb.client.CallTool(ctx, req)
	if err != nil {
		return "", err
	}
	text := resultText(res)
	if res.IsError {
		return "", errors.New(text)
	}
	return text, nil
}

func resultText(res *mcpgo.CallToolResult) string {
	var parts []string
	for _, content := range res.Content {
		if text, ok := mcpgo.AsTextContent(content); ok {
			parts = append(parts, text.Text)
		}
	}
	if len(parts) == 0 && res.StructuredContent != nil {
		encoded, err := json.Marshal(res.StructuredContent)
		if err == nil {
			return string(encoded)
		}
	}
	return strings.Join(parts, "\n")
}
