package askchef

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/client"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/ai"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/mcp"
	"github.com/mr-karan/logchef/pkg/models"
)

// allowedTool is a tool Ask Chef may call.
type allowedTool struct {
	// read reports what a successful call read. It is nil for a tool that
	// reads no logs.
	read func(input json.RawMessage, result string) (toolReading, error)
}

// toolReading is what one successful call read: a step summary, and the
// evidence of each window it queried, without IDs.
type toolReading struct {
	summary  string
	evidence []Evidence
}

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
	"query_logchefql":          {read: readQueryLogchefQL},
	"compare_windows":          {read: readCompareWindows},
	"get_field_values":         {read: readFieldValues},
	"top_values":               {read: readTopValues},
	"get_all_field_dimensions": {read: readAllFieldDimensions},
}

// decodeCall decodes a call's input and result into the MCP tool's own
// parameter and result types, which also generate its schemas.
func decodeCall[P, R any](input json.RawMessage, result string) (params P, res R, err error) {
	if err := json.Unmarshal(input, &params); err != nil {
		return params, res, fmt.Errorf("decoding tool input: %w", err)
	}
	if err := json.Unmarshal([]byte(result), &res); err != nil {
		return params, res, fmt.Errorf("decoding tool result: %w", err)
	}
	return params, res, nil
}

// logchefQLWindow parses a window the way the LogchefQL tools run it.
func logchefQLWindow(start, end, timezone string) (startMS, endMS int64, err error) {
	s, e, err := core.ParseLogchefQLTimeRange(start, end, timezone)
	if err != nil {
		return 0, 0, err
	}
	return s.UnixMilli(), e.UnixMilli(), nil
}

// rfc3339Window parses a window the way the field value tools run it.
func rfc3339Window(start, end string) (startMS, endMS int64, err error) {
	s, err := time.Parse(time.RFC3339, start)
	if err != nil {
		return 0, 0, err
	}
	e, err := time.Parse(time.RFC3339, end)
	if err != nil {
		return 0, 0, err
	}
	return s.UnixMilli(), e.UnixMilli(), nil
}

func rowsSummary(rows int, truncated bool) string {
	if truncated {
		return fmt.Sprintf("%d rows (truncated)", rows)
	}
	return fmt.Sprintf("%d rows", rows)
}

// fieldsSummary names the fields a call read values of.
func fieldsSummary(fields []string) string {
	const shown = 5
	if len(fields) <= shown {
		return "values of " + strings.Join(fields, ", ")
	}
	return fmt.Sprintf("values of %s and %d more", strings.Join(fields[:shown], ", "), len(fields)-shown)
}

func readQueryLogchefQL(input json.RawMessage, result string) (toolReading, error) {
	params, res, err := decodeCall[mcp.QueryLogchefQLParams, mcp.QueryResult](input, result)
	if err != nil {
		return toolReading{}, err
	}
	start, end, err := logchefQLWindow(res.StartTime, res.EndTime, params.Timezone)
	if err != nil {
		return toolReading{}, err
	}
	summary := rowsSummary(res.RowCount, res.Stats.Truncated)
	return toolReading{summary: summary, evidence: []Evidence{{
		TeamID: models.TeamID(res.TeamID), SourceID: models.SourceID(res.SourceID), Mode: ModeLogchefQL,
		Query: params.Query, StartMS: start, EndMS: end, Summary: summary,
	}}}, nil
}

func readCompareWindows(input json.RawMessage, result string) (toolReading, error) {
	params, res, err := decodeCall[mcp.CompareWindowsParams, mcp.CompareWindowsResult](input, result)
	if err != nil {
		return toolReading{}, err
	}
	reading := toolReading{summary: fmt.Sprintf("%d rows vs %d rows", res.Window1.RowCount, res.Window2.RowCount)}
	for i, w := range []mcp.WindowResult{res.Window1, res.Window2} {
		start, end, err := logchefQLWindow(w.Start, w.End, params.Timezone)
		if err != nil {
			return toolReading{}, err
		}
		reading.evidence = append(reading.evidence, Evidence{
			TeamID: models.TeamID(params.TeamID), SourceID: models.SourceID(params.SourceID), Mode: ModeLogchefQL,
			Query: params.Query, StartMS: start, EndMS: end,
			Summary: fmt.Sprintf("window %d: %s", i+1, rowsSummary(w.RowCount, w.Truncated)),
		})
	}
	return reading, nil
}

// fieldValuesReading is the reading of a field value tool. These tools take
// no filter, so the evidence query is empty: all logs in the window.
func fieldValuesReading(teamID, sourceID int, start, end string, fields []string) (toolReading, error) {
	startMS, endMS, err := rfc3339Window(start, end)
	if err != nil {
		return toolReading{}, err
	}
	summary := fieldsSummary(fields)
	return toolReading{summary: summary, evidence: []Evidence{{
		TeamID: models.TeamID(teamID), SourceID: models.SourceID(sourceID), Mode: ModeLogchefQL,
		StartMS: startMS, EndMS: endMS, Summary: summary,
	}}}, nil
}

func readFieldValues(input json.RawMessage, result string) (toolReading, error) {
	params, res, err := decodeCall[mcp.GetFieldValuesParams, core.FieldValuesResult](input, result)
	if err != nil {
		return toolReading{}, err
	}
	return fieldValuesReading(params.TeamID, params.SourceID, params.StartTime, params.EndTime, []string{res.FieldName})
}

func readTopValues(input json.RawMessage, result string) (toolReading, error) {
	params, res, err := decodeCall[mcp.TopValuesParams, mcp.TopValuesResult](input, result)
	if err != nil {
		return toolReading{}, err
	}
	fields := make([]string, len(res.Fields))
	for i, f := range res.Fields {
		fields[i] = f.FieldName
	}
	return fieldValuesReading(params.TeamID, params.SourceID, params.StartTime, params.EndTime, fields)
}

func readAllFieldDimensions(input json.RawMessage, result string) (toolReading, error) {
	params, res, err := decodeCall[mcp.GetAllFieldDimensionsParams, map[string]*core.FieldValuesResult](input, result)
	if err != nil {
		return toolReading{}, err
	}
	return fieldValuesReading(params.TeamID, params.SourceID, params.StartTime, params.EndTime, slices.Sorted(maps.Keys(res)))
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
