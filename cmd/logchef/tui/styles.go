package tui

import "github.com/charmbracelet/lipgloss"

var (
	subtle    = lipgloss.AdaptiveColor{Light: "#D9DCCF", Dark: "#383838"}
	highlight = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7C3AED"}
	special   = lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#10B981"}
	warning   = lipgloss.AdaptiveColor{Light: "#F59E0B", Dark: "#FBBF24"}
	danger    = lipgloss.AdaptiveColor{Light: "#EF4444", Dark: "#F87171"}
	muted     = lipgloss.AdaptiveColor{Light: "#9CA3AF", Dark: "#6B7280"}

	titleStyle = lipgloss.NewStyle().
			Foreground(highlight).
			Bold(true).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1F2937")).
			Foreground(lipgloss.Color("#F9FAFB")).
			Padding(0, 1)

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#1F2937")).
			Foreground(lipgloss.Color("#9CA3AF")).
			Padding(0, 1)

	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(highlight).
			Padding(0, 1)

	inputFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(special).
				Padding(0, 1)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#F9FAFB")).
				Background(lipgloss.Color("#374151")).
				Padding(0, 1)

	tableRowStyle = lipgloss.NewStyle().
			Padding(0, 1)

	tableSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#374151")).
				Foreground(lipgloss.Color("#F9FAFB")).
				Padding(0, 1)

	detailPaneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(muted).
			Padding(1)

	helpStyle = lipgloss.NewStyle().
			Foreground(muted)

	errorStyle = lipgloss.NewStyle().
			Foreground(danger).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(special)

	levelStyles = map[string]lipgloss.Style{
		"error":   lipgloss.NewStyle().Foreground(danger).Bold(true),
		"err":     lipgloss.NewStyle().Foreground(danger).Bold(true),
		"fatal":   lipgloss.NewStyle().Foreground(danger).Bold(true),
		"warn":    lipgloss.NewStyle().Foreground(warning),
		"warning": lipgloss.NewStyle().Foreground(warning),
		"info":    lipgloss.NewStyle().Foreground(special),
		"debug":   lipgloss.NewStyle().Foreground(muted),
		"trace":   lipgloss.NewStyle().Foreground(muted),
	}
)

func getLevelStyle(level string) lipgloss.Style {
	if style, ok := levelStyles[level]; ok {
		return style
	}
	return lipgloss.NewStyle()
}
