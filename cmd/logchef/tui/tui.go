package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mr-karan/logchef/internal/cli/client"
	"github.com/mr-karan/logchef/internal/cli/config"
	"github.com/mr-karan/logchef/internal/cli/query"
)

type focus int

const (
	focusQuery focus = iota
	focusTable
	focusDetail
)

type Model struct {
	cfg       *config.Config
	client    *client.Client
	teamID    int
	sourceID  int
	width     int
	height    int
	focus     focus
	err       error
	input     textinput.Model
	viewport  viewport.Model
	spinner   spinner.Model
	logs      []map[string]any
	columns   []client.Column
	stats     client.QueryStats
	selected  int
	offset    int
	loading   bool
	startTime time.Time
	endTime   time.Time
	timezone  string
	showSQL   bool
	sql       string
}

type queryResultMsg struct {
	resp *client.QueryResponse
	err  error
}

type tickMsg time.Time

func New(cfg *config.Config, teamID, sourceID int, since string) (*Model, error) {
	apiClient, err := client.New(cfg)
	if err != nil {
		return nil, err
	}

	ti := textinput.New()
	ti.Placeholder = "Enter LogChefQL query (e.g., level=\"error\", msg~\"timeout\")"
	ti.Focus()
	ti.CharLimit = 500
	ti.Width = 80

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(highlight)

	vp := viewport.New(80, 20)

	duration, err := query.ParseDuration(since)
	if err != nil {
		duration = 15 * time.Minute
	}

	endTime := time.Now()
	startTime := endTime.Add(-duration)

	tz := cfg.ResolveTimezone()

	return &Model{
		cfg:       cfg,
		client:    apiClient,
		teamID:    teamID,
		sourceID:  sourceID,
		input:     ti,
		spinner:   sp,
		viewport:  vp,
		focus:     focusQuery,
		startTime: startTime,
		endTime:   endTime,
		timezone:  tz,
	}, nil
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.spinner.Tick,
	)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyPress(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 4
		m.viewport.Width = msg.Width - 2
		m.viewport.Height = msg.Height - 10
		return m, nil

	case queryResultMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.logs = msg.resp.Logs
		m.columns = msg.resp.Columns
		m.stats = msg.resp.Stats
		m.sql = msg.resp.GeneratedSQL
		m.selected = 0
		m.offset = 0
		m.updateViewport()
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	if m.focus == focusQuery {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)
	}

	if m.focus == focusDetail {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m Model) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		if m.focus == focusDetail {
			m.focus = focusTable
			return m, nil
		}
		if m.focus == focusTable && m.input.Value() == "" {
			return m, tea.Quit
		}
		if m.focus == focusQuery {
			return m, tea.Quit
		}
		m.focus = focusQuery
		m.input.Focus()
		return m, nil

	case "esc":
		if m.focus == focusDetail {
			m.focus = focusTable
			return m, nil
		}
		if m.focus == focusTable {
			m.focus = focusQuery
			m.input.Focus()
			return m, nil
		}
		return m, tea.Quit

	case "enter":
		if m.focus == focusQuery && !m.loading {
			q := m.input.Value()
			m.loading = true
			m.focus = focusTable
			m.input.Blur()
			return m, m.executeQuery(q)
		}
		if m.focus == focusTable && len(m.logs) > 0 {
			m.focus = focusDetail
			m.updateDetailView()
			return m, nil
		}

	case "tab":
		if m.focus == focusQuery {
			m.focus = focusTable
			m.input.Blur()
		} else if m.focus == focusTable {
			m.focus = focusQuery
			m.input.Focus()
		}
		return m, nil

	case "up", "k":
		if m.focus == focusTable && m.selected > 0 {
			m.selected--
			if m.selected < m.offset {
				m.offset = m.selected
			}
		}
		if m.focus == focusDetail {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil

	case "down", "j":
		if m.focus == focusTable && m.selected < len(m.logs)-1 {
			m.selected++
			visibleRows := m.tableHeight()
			if m.selected >= m.offset+visibleRows {
				m.offset = m.selected - visibleRows + 1
			}
		}
		if m.focus == focusDetail {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil

	case "g":
		if m.focus == focusTable {
			m.selected = 0
			m.offset = 0
		}
		return m, nil

	case "G":
		if m.focus == focusTable && len(m.logs) > 0 {
			m.selected = len(m.logs) - 1
			visibleRows := m.tableHeight()
			if m.selected >= visibleRows {
				m.offset = m.selected - visibleRows + 1
			}
		}
		return m, nil

	case "s":
		if m.focus == focusTable {
			m.showSQL = !m.showSQL
		}
		return m, nil

	case "r":
		if !m.loading {
			m.loading = true
			return m, m.executeQuery(m.input.Value())
		}
		return m, nil

	case "/":
		m.focus = focusQuery
		m.input.Focus()
		return m, nil
	}

	if m.focus == focusQuery {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m *Model) executeQuery(q string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		resp, err := m.client.Query(ctx, m.teamID, m.sourceID, client.QueryRequest{
			Query:     q,
			StartTime: m.startTime.Format("2006-01-02 15:04:05"),
			EndTime:   m.endTime.Format("2006-01-02 15:04:05"),
			Timezone:  m.timezone,
			Limit:     500,
		})

		return queryResultMsg{resp: resp, err: err}
	}
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.renderQueryInput())
	b.WriteString("\n")

	if m.focus == focusDetail {
		b.WriteString(m.renderDetailPane())
	} else {
		b.WriteString(m.renderTable())
	}

	b.WriteString("\n")
	b.WriteString(m.renderStatusBar())

	return b.String()
}

func (m Model) renderHeader() string {
	title := titleStyle.Render("LogChef Explorer")
	timeRange := fmt.Sprintf("%s to %s (%s)",
		m.startTime.Format("15:04:05"),
		m.endTime.Format("15:04:05"),
		m.timezone,
	)
	right := lipgloss.NewStyle().Foreground(muted).Render(timeRange)

	gap := m.width - lipgloss.Width(title) - lipgloss.Width(right) - 2
	if gap < 0 {
		gap = 0
	}

	return headerStyle.Width(m.width).Render(
		title + strings.Repeat(" ", gap) + right,
	)
}

func (m Model) renderQueryInput() string {
	style := inputStyle
	if m.focus == focusQuery {
		style = inputFocusedStyle
	}

	prefix := "Query: "
	if m.loading {
		prefix = m.spinner.View() + " "
	}

	return style.Width(m.width - 2).Render(prefix + m.input.View())
}

func (m Model) renderTable() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v", m.err))
	}

	if len(m.logs) == 0 {
		if m.loading {
			return lipgloss.NewStyle().Padding(1).Render(m.spinner.View() + " Querying...")
		}
		return lipgloss.NewStyle().Padding(1).Foreground(muted).Render("No results. Press Enter to execute query.")
	}

	var b strings.Builder

	headers := m.getDisplayColumns()
	headerRow := m.renderTableRow(headers, true, false)
	b.WriteString(headerRow)
	b.WriteString("\n")

	visibleRows := m.tableHeight()
	for i := m.offset; i < len(m.logs) && i < m.offset+visibleRows; i++ {
		isSelected := i == m.selected
		row := m.renderLogRow(m.logs[i], headers, isSelected)
		b.WriteString(row)
		b.WriteString("\n")
	}

	return b.String()
}

func (m Model) getDisplayColumns() []string {
	priority := []string{"_timestamp", "level", "severity", "msg", "message"}
	result := make([]string, 0)

	for _, col := range priority {
		for _, c := range m.columns {
			if c.Name == col {
				result = append(result, col)
				break
			}
		}
	}

	for _, c := range m.columns {
		found := false
		for _, r := range result {
			if r == c.Name {
				found = true
				break
			}
		}
		if !found && !strings.HasPrefix(c.Name, "_") {
			result = append(result, c.Name)
		}
	}

	maxCols := 6
	if len(result) > maxCols {
		result = result[:maxCols]
	}

	return result
}

func (m Model) renderTableRow(cols []string, isHeader, isSelected bool) string {
	widths := m.calculateColumnWidths(cols)
	cells := make([]string, len(cols))

	for i, col := range cols {
		cell := truncate(col, widths[i])
		cells[i] = cell
	}

	row := strings.Join(cells, " | ")

	if isHeader {
		return tableHeaderStyle.Width(m.width).Render(row)
	}
	if isSelected {
		return tableSelectedStyle.Width(m.width).Render(row)
	}
	return tableRowStyle.Width(m.width).Render(row)
}

func (m Model) renderLogRow(log map[string]any, cols []string, isSelected bool) string {
	widths := m.calculateColumnWidths(cols)
	cells := make([]string, len(cols))

	for i, col := range cols {
		val := ""
		if v, ok := log[col]; ok {
			val = fmt.Sprintf("%v", v)
		}

		if col == "level" || col == "severity" {
			style := getLevelStyle(strings.ToLower(val))
			val = style.Render(val)
		}

		cells[i] = truncate(val, widths[i])
	}

	row := strings.Join(cells, " | ")

	if isSelected {
		return tableSelectedStyle.Width(m.width).Render(row)
	}
	return tableRowStyle.Width(m.width).Render(row)
}

func (m Model) calculateColumnWidths(cols []string) []int {
	if len(cols) == 0 {
		return nil
	}

	available := m.width - (len(cols) * 3) - 4
	baseWidth := available / len(cols)

	widths := make([]int, len(cols))
	for i := range widths {
		widths[i] = baseWidth
		if widths[i] < 8 {
			widths[i] = 8
		}
		if widths[i] > 50 {
			widths[i] = 50
		}
	}

	if len(cols) > 0 {
		for i, col := range cols {
			if col == "msg" || col == "message" {
				widths[i] = min(available/2, 80)
			}
			if col == "_timestamp" {
				widths[i] = 26
			}
		}
	}

	return widths
}

func (m Model) renderDetailPane() string {
	return detailPaneStyle.Width(m.width - 2).Height(m.height - 8).Render(m.viewport.View())
}

func (m *Model) updateDetailView() {
	if m.selected >= len(m.logs) {
		return
	}

	log := m.logs[m.selected]
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Render("Log Details"))
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", 40))
	b.WriteString("\n\n")

	priority := []string{"_timestamp", "level", "severity", "msg", "message"}
	rendered := make(map[string]bool)

	for _, key := range priority {
		if val, ok := log[key]; ok {
			b.WriteString(renderField(key, val))
			rendered[key] = true
		}
	}

	if len(rendered) > 0 {
		b.WriteString("\n")
	}

	for key, val := range log {
		if !rendered[key] {
			b.WriteString(renderField(key, val))
		}
	}

	m.viewport.SetContent(b.String())
}

func renderField(key string, val any) string {
	keyStyle := lipgloss.NewStyle().Foreground(highlight).Bold(true)
	valStr := fmt.Sprintf("%v", val)

	if key == "level" || key == "severity" {
		valStr = getLevelStyle(strings.ToLower(valStr)).Render(valStr)
	}

	return fmt.Sprintf("%s: %s\n", keyStyle.Render(key), valStr)
}

func (m *Model) updateViewport() {
	if m.focus == focusDetail {
		m.updateDetailView()
	}
}

func (m Model) renderStatusBar() string {
	var left, right string

	if m.showSQL && m.sql != "" {
		left = fmt.Sprintf("SQL: %s", truncate(m.sql, m.width/2))
	} else if len(m.logs) > 0 {
		left = fmt.Sprintf("%d logs | %dms | %d rows read",
			len(m.logs),
			m.stats.ExecutionTimeMs,
			m.stats.RowsRead,
		)
	} else {
		left = "Ready"
	}

	help := "q:quit  /:search  Enter:select  s:sql  r:refresh  Tab:switch"
	if m.focus == focusDetail {
		help = "Esc:back  j/k:scroll  q:quit"
	}

	right = helpStyle.Render(help)
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 0 {
		gap = 0
	}

	return statusBarStyle.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) tableHeight() int {
	h := m.height - 8
	if h < 5 {
		h = 5
	}
	return h
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) <= maxLen {
		return s + strings.Repeat(" ", maxLen-len(s))
	}
	return s[:maxLen-1] + "…"
}

func Run(cfg *config.Config, teamID, sourceID int, since string) error {
	m, err := New(cfg, teamID, sourceID, since)
	if err != nil {
		return err
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
