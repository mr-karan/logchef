// Package render provides output rendering for the LogChef CLI.
package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// Column represents a result column
type Column struct {
	Name string
	Type string
}

// QueryStats represents query execution statistics
type QueryStats struct {
	ExecutionTimeMs int64
	RowsRead        int64
	BytesRead       int64
}

// QueryResult represents the result of a query
type QueryResult struct {
	Logs         []map[string]any
	Columns      []Column
	Stats        QueryStats
	GeneratedSQL string
}

// Options configures the renderer
type Options struct {
	Format     string   // text, table, json, jsonl, csv
	Template   string   // Custom Go template
	Fields     []string // Fields to display (nil = all)
	Color      bool     // Enable colored output
	UsePager   bool     // Use pager for output
	Pager      string   // Pager command
	TimeFormat string   // Timestamp format: rfc3339, short, relative
	StatsOnly  bool     // Only show stats, not logs
	CountOnly  bool     // Only show count
}

// Renderer renders query results
type Renderer struct {
	opts Options
}

// New creates a new renderer
func New(opts Options) (*Renderer, error) {
	if opts.Format == "" {
		opts.Format = "text"
	}
	return &Renderer{opts: opts}, nil
}

// Render renders the query result
func (r *Renderer) Render(result *QueryResult) error {
	if r.opts.CountOnly {
		fmt.Printf("%d\n", len(result.Logs))
		return nil
	}

	if r.opts.StatsOnly {
		fmt.Printf("Count: %d\n", len(result.Logs))
		fmt.Printf("Execution: %.2fs\n", float64(result.Stats.ExecutionTimeMs)/1000)
		fmt.Printf("Rows scanned: %s\n", formatNumber(result.Stats.RowsRead))
		fmt.Printf("Bytes read: %s\n", formatBytes(result.Stats.BytesRead))
		return nil
	}

	var buf bytes.Buffer
	var err error

	switch r.opts.Format {
	case "text":
		err = r.renderText(&buf, result)
	case "json":
		err = r.renderJSON(&buf, result, true)
	case "jsonl":
		err = r.renderJSONL(&buf, result)
	case "csv":
		err = r.renderCSV(&buf, result)
	case "table":
		err = r.renderTable(&buf, result)
	default:
		if r.opts.Template != "" {
			err = r.renderTemplate(&buf, result)
		} else {
			return fmt.Errorf("unknown output format: %s (valid: text, json, jsonl, csv, table)", r.opts.Format)
		}
	}

	if err != nil {
		return err
	}

	if r.opts.UsePager && buf.Len() > 0 {
		return r.outputWithPager(buf.Bytes())
	}

	_, err = os.Stdout.Write(buf.Bytes())
	return err
}

// renderJSON renders as pretty JSON
func (r *Renderer) renderJSON(w io.Writer, result *QueryResult, pretty bool) error {
	output := map[string]any{
		"logs": r.filterFields(result.Logs),
		"stats": map[string]any{
			"execution_time_ms": result.Stats.ExecutionTimeMs,
			"rows_read":         result.Stats.RowsRead,
			"bytes_read":        result.Stats.BytesRead,
		},
		"count": len(result.Logs),
	}

	encoder := json.NewEncoder(w)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(output)
}

// renderJSONL renders as JSON Lines (one JSON object per line)
func (r *Renderer) renderJSONL(w io.Writer, result *QueryResult) error {
	logs := r.filterFields(result.Logs)
	for _, log := range logs {
		data, err := json.Marshal(log)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n")); err != nil {
			return err
		}
	}
	return nil
}

func (r *Renderer) renderText(w io.Writer, result *QueryResult) error {
	if len(result.Logs) == 0 {
		fmt.Fprintln(w, "No results found.")
		return nil
	}

	tsKeys := []string{"_timestamp", "timestamp", "time", "ts", "@timestamp"}
	msgKeys := []string{"msg", "message", "log", "body"}
	levelKeys := []string{"level", "severity", "log_level", "loglevel"}

	logs := r.filterFields(result.Logs)
	for _, log := range logs {
		ts := extractField(log, tsKeys)
		msg := extractField(log, msgKeys)
		level := strings.ToUpper(extractField(log, levelKeys))

		if msg == "" {
			data, _ := json.Marshal(log)
			msg = string(data)
		}

		ts = r.formatTimestamp(ts)

		var line string
		if level != "" && r.opts.Color {
			levelStyled := r.styleLevel(level)
			if ts != "" {
				line = fmt.Sprintf("%s %s %s", dimStyle.Render(ts), levelStyled, msg)
			} else {
				line = fmt.Sprintf("%s %s", levelStyled, msg)
			}
		} else if level != "" {
			if ts != "" {
				line = fmt.Sprintf("%s %s %s", ts, level, msg)
			} else {
				line = fmt.Sprintf("%s %s", level, msg)
			}
		} else {
			if ts != "" {
				line = fmt.Sprintf("%s  %s", ts, msg)
			} else {
				line = msg
			}
		}
		fmt.Fprintln(w, line)
	}

	return nil
}

var (
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	infoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	debugStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
)

func (r *Renderer) styleLevel(level string) string {
	switch level {
	case "ERROR", "ERR", "FATAL", "PANIC", "CRITICAL":
		return errorStyle.Render(level)
	case "WARN", "WARNING":
		return warnStyle.Render(level)
	case "INFO":
		return infoStyle.Render(level)
	case "DEBUG", "TRACE":
		return debugStyle.Render(level)
	default:
		return level
	}
}

func (r *Renderer) formatTimestamp(ts string) string {
	if ts == "" || r.opts.TimeFormat == "" || r.opts.TimeFormat == "rfc3339" {
		return ts
	}

	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return ts
		}
	}

	switch r.opts.TimeFormat {
	case "short":
		return t.Format("01-02 15:04:05")
	case "time":
		return t.Format("15:04:05")
	case "relative":
		return formatRelativeTime(t)
	default:
		return ts
	}
}

func formatRelativeTime(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func extractField(log map[string]any, keys []string) string {
	for _, key := range keys {
		if v, ok := log[key]; ok {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// renderCSV renders as CSV
func (r *Renderer) renderCSV(w io.Writer, result *QueryResult) error {
	if len(result.Logs) == 0 {
		return nil
	}

	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Get headers
	headers := r.getHeaders(result)
	if err := writer.Write(headers); err != nil {
		return err
	}

	// Write rows
	logs := r.filterFields(result.Logs)
	for _, log := range logs {
		row := make([]string, len(headers))
		for i, h := range headers {
			if v, ok := log[h]; ok {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

// renderTable renders as a formatted table
func (r *Renderer) renderTable(w io.Writer, result *QueryResult) error {
	if len(result.Logs) == 0 {
		fmt.Fprintln(w, "No results found.")
		r.renderStats(w, result.Stats)
		return nil
	}

	headers := r.getHeaders(result)
	logs := r.filterFields(result.Logs)

	// Build table data
	rows := make([][]string, len(logs))
	for i, log := range logs {
		row := make([]string, len(headers))
		for j, h := range headers {
			if v, ok := log[h]; ok {
				row[j] = formatValue(v)
			}
		}
		rows[i] = row
	}

	// Create styled table
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("238"))).
		Headers(headers...).
		Rows(rows...)

	// Style headers
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("252"))
	t.StyleFunc(func(row, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return headerStyle
		}
		// Alternate row colors
		if row%2 == 0 {
			return lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	})

	fmt.Fprintln(w, t.Render())
	r.renderStats(w, result.Stats)

	return nil
}

// renderTemplate renders using a custom Go template
func (r *Renderer) renderTemplate(w io.Writer, result *QueryResult) error {
	tmpl, err := template.New("output").Funcs(templateFuncs()).Parse(r.opts.Template)
	if err != nil {
		return fmt.Errorf("invalid template: %w", err)
	}

	logs := r.filterFields(result.Logs)
	for _, log := range logs {
		if err := tmpl.Execute(w, log); err != nil {
			return err
		}
		fmt.Fprintln(w)
	}

	return nil
}

// renderStats renders query statistics
func (r *Renderer) renderStats(w io.Writer, stats QueryStats) {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	fmt.Fprintf(w, "%s\n", style.Render(fmt.Sprintf(
		"Query: %.2fs | Scanned: %s rows, %s",
		float64(stats.ExecutionTimeMs)/1000,
		formatNumber(stats.RowsRead),
		formatBytes(stats.BytesRead),
	)))
}

// filterFields filters log fields based on the Fields option
func (r *Renderer) filterFields(logs []map[string]any) []map[string]any {
	if len(r.opts.Fields) == 0 {
		return logs
	}

	result := make([]map[string]any, len(logs))
	for i, log := range logs {
		filtered := make(map[string]any)
		for _, field := range r.opts.Fields {
			if v, ok := log[field]; ok {
				filtered[field] = v
			}
		}
		result[i] = filtered
	}
	return result
}

// getHeaders returns column headers in order
func (r *Renderer) getHeaders(result *QueryResult) []string {
	if len(r.opts.Fields) > 0 {
		return r.opts.Fields
	}

	// Use columns from response if available
	if len(result.Columns) > 0 {
		headers := make([]string, len(result.Columns))
		for i, c := range result.Columns {
			headers[i] = c.Name
		}
		return headers
	}

	// Fall back to keys from first log
	if len(result.Logs) > 0 {
		headers := make([]string, 0, len(result.Logs[0]))
		for k := range result.Logs[0] {
			headers = append(headers, k)
		}
		return headers
	}

	return nil
}

// outputWithPager pipes output through a pager
func (r *Renderer) outputWithPager(data []byte) error {
	pagerCmd := r.opts.Pager
	if pagerCmd == "" {
		pagerCmd = "less -R"
	}

	parts := strings.Fields(pagerCmd)
	if len(parts) == 0 {
		_, err := os.Stdout.Write(data)
		return err
	}

	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Stdin = bytes.NewReader(data)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// formatValue formats a value for table display
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		// Truncate long strings
		if len(val) > 80 {
			return val[:77] + "..."
		}
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%.2f", val)
	case nil:
		return ""
	default:
		s := fmt.Sprintf("%v", val)
		if len(s) > 80 {
			return s[:77] + "..."
		}
		return s
	}
}

// formatNumber formats a number with commas
func formatNumber(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var result strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result.WriteRune(',')
		}
		result.WriteRune(c)
	}
	return result.String()
}

// formatBytes formats bytes in human-readable form
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// templateFuncs returns custom template functions
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"upper": strings.ToUpper,
		"lower": strings.ToLower,
		"title": strings.Title,
		"trunc": func(n int, s string) string {
			if len(s) <= n {
				return s
			}
			return s[:n]
		},
		"json": func(v any) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
	}
}
