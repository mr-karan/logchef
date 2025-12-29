// Package commands provides the CLI command definitions for LogChef.
package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/mr-karan/logchef/internal/cli/config"
	"github.com/urfave/cli/v3"
)

// Styles for CLI output
var (
	logoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7C3AED")).
			Bold(true)

	successStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#10B981"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF4444"))

	mutedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280"))
)

// App holds the shared application state
type App struct {
	Config  *config.Config
	Version string
	Commit  string
	Date    string
}

// New creates the root CLI command with all subcommands
func New(version, commit, date string) *cli.Command {
	app := &App{
		Version: version,
		Commit:  commit,
		Date:    date,
	}

	return &cli.Command{
		Name:    "logchef",
		Usage:   "A powerful CLI for exploring logs with LogChef",
		Version: version,
		Description: `LogChef CLI provides a best-in-class log exploration experience.

   Use 'logchef query' for quick searches, 'logchef tail' for live streaming,
   or just 'logchef' to launch the interactive TUI explorer.

   Documentation: https://logchef.dev/docs/cli`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "path to config file",
				Sources: cli.EnvVars("LOGCHEF_CONFIG"),
			},
			&cli.StringFlag{
				Name:    "server",
				Usage:   "LogChef server URL",
				Sources: cli.EnvVars("LOGCHEF_SERVER_URL"),
			},
			&cli.StringFlag{
				Name:    "token",
				Usage:   "API token for authentication",
				Sources: cli.EnvVars("LOGCHEF_AUTH_TOKEN", "LOGCHEF_API_TOKEN"),
			},
			&cli.StringFlag{
				Name:    "team",
				Aliases: []string{"t"},
				Usage:   "default team name or ID",
				Sources: cli.EnvVars("LOGCHEF_TEAM"),
			},
			&cli.StringFlag{
				Name:    "source",
				Aliases: []string{"S"},
				Usage:   "default source name or ID",
				Sources: cli.EnvVars("LOGCHEF_SOURCE"),
			},
			&cli.StringFlag{
				Name:    "profile",
				Aliases: []string{"p"},
				Usage:   "configuration profile to use",
				Sources: cli.EnvVars("LOGCHEF_PROFILE"),
			},
			&cli.BoolFlag{
				Name:  "debug",
				Usage: "enable debug logging",
			},
			&cli.BoolFlag{
				Name:  "no-color",
				Usage: "disable colored output",
			},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			// Set up logging
			if cmd.Bool("debug") {
				log.SetLevel(log.DebugLevel)
			}

			if cmd.Bool("no-color") {
				log.SetStyles(log.DefaultStyles())
				lipgloss.SetHasDarkBackground(false)
			}

			// Load configuration
			cfg, err := config.Load(config.LoadOptions{
				ConfigPath: cmd.String("config"),
				Profile:    cmd.String("profile"),
			})
			if err != nil {
				log.Debug("config load warning", "error", err)
				// Use defaults if config doesn't exist
				cfg = config.Default()
			}

			// Override with CLI flags
			if server := cmd.String("server"); server != "" {
				cfg.Server.URL = server
			}
			if token := cmd.String("token"); token != "" {
				cfg.Auth.Token = token
			}
			if team := cmd.String("team"); team != "" {
				cfg.Defaults.Team = team
			}
			if source := cmd.String("source"); source != "" {
				cfg.Defaults.Source = source
			}

			app.Config = cfg
			return ctx, nil
		},
		Commands: []*cli.Command{
			app.queryCommand(),
			app.tailCommand(),
			app.exploreCommand(),
			app.sourcesCommand(),
			app.configCommand(),
			app.translateCommand(),
			app.versionCommand(),
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// Default action: launch TUI if TTY, otherwise show help
			if isTerminal() {
				return app.runExplorer(ctx, cmd)
			}
			return cli.ShowAppHelp(cmd)
		},
	}
}

// isTerminal returns true if stdout is a terminal
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// versionCommand shows version information
func (a *App) versionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "show version information",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fmt.Printf("%s version %s\n", logoStyle.Render("logchef"), a.Version)
			fmt.Printf("  commit: %s\n", mutedStyle.Render(a.Commit))
			fmt.Printf("  built:  %s\n", mutedStyle.Render(a.Date))
			return nil
		},
	}
}
