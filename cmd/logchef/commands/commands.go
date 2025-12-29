package commands

import (
	"context"
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/mr-karan/logchef/cmd/logchef/tui"
	"github.com/mr-karan/logchef/internal/cli/client"
	"github.com/urfave/cli/v3"
)

// tailCommand returns the tail subcommand
func (a *App) tailCommand() *cli.Command {
	return &cli.Command{
		Name:      "tail",
		Usage:     "stream logs in real-time",
		ArgsUsage: "[query]",
		Description: `Stream logs in real-time with live tailing.

Examples:
   logchef tail 'service="api"'
   logchef tail 'level="error"' --rate 100`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "since",
				Aliases: []string{"s"},
				Usage:   "start from relative time",
				Value:   "5m",
			},
			&cli.IntFlag{
				Name:  "rate",
				Usage: "max logs per second (0 = unlimited)",
				Value: 0,
			},
			&cli.IntFlag{
				Name:    "count",
				Aliases: []string{"c"},
				Usage:   "stop after N logs (0 = unlimited)",
				Value:   0,
			},
			&cli.BoolFlag{
				Name:  "no-color",
				Usage: "disable colored output",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return fmt.Errorf("tail command not yet implemented")
		},
	}
}

// exploreCommand returns the explore subcommand
func (a *App) exploreCommand() *cli.Command {
	return &cli.Command{
		Name:      "explore",
		Usage:     "launch interactive TUI explorer",
		ArgsUsage: "[query]",
		Description: `Launch the interactive TUI explorer for log exploration.

The explorer provides a full-screen interface with:
  - Query input with autocomplete
  - Results table with scrolling
  - Log details pane
  - Time histogram
  - Field sidebar`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "since",
				Aliases: []string{"s"},
				Usage:   "initial time range",
				Value:   "15m",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return a.runExplorer(ctx, cmd)
		},
	}
}

func (a *App) runExplorer(ctx context.Context, cmd *cli.Command) error {
	if a.Config.Auth.Token == "" {
		return fmt.Errorf("API token not configured. Run 'logchef config init' or set LOGCHEF_AUTH_TOKEN")
	}

	apiClient, err := client.New(a.Config)
	if err != nil {
		return err
	}

	teamID, sourceID, err := a.resolveTeamAndSource(ctx, apiClient)
	if err != nil {
		teamID, sourceID, err = a.selectTeamAndSource(ctx, apiClient)
		if err != nil {
			return err
		}
	}

	since := cmd.String("since")
	if since == "" {
		since = "15m"
	}

	return tui.Run(a.Config, teamID, sourceID, since)
}

func (a *App) selectTeamAndSource(ctx context.Context, apiClient *client.Client) (int, int, error) {
	teams, err := apiClient.ListTeams(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list teams: %w", err)
	}

	if len(teams) == 0 {
		return 0, 0, fmt.Errorf("no teams available")
	}

	teamOptions := make([]huh.Option[int], len(teams))
	for i, t := range teams {
		teamOptions[i] = huh.NewOption(t.Name, t.ID)
	}

	var selectedTeamID int
	err = huh.NewSelect[int]().
		Title("Select Team").
		Options(teamOptions...).
		Value(&selectedTeamID).
		Run()
	if err != nil {
		return 0, 0, err
	}

	sources, err := apiClient.ListSources(ctx, selectedTeamID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list sources: %w", err)
	}

	if len(sources) == 0 {
		return 0, 0, fmt.Errorf("no sources available for this team")
	}

	sourceOptions := make([]huh.Option[int], len(sources))
	for i, s := range sources {
		label := s.Name
		if s.Description != "" {
			label = fmt.Sprintf("%s - %s", s.Name, s.Description)
		}
		sourceOptions[i] = huh.NewOption(label, s.ID)
	}

	var selectedSourceID int
	err = huh.NewSelect[int]().
		Title("Select Source").
		Options(sourceOptions...).
		Value(&selectedSourceID).
		Run()
	if err != nil {
		return 0, 0, err
	}

	return selectedTeamID, selectedSourceID, nil
}

// sourcesCommand returns the sources subcommand
func (a *App) sourcesCommand() *cli.Command {
	return &cli.Command{
		Name:  "sources",
		Usage: "manage and view sources",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list available sources",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return a.runSourcesList(ctx, cmd)
				},
			},
			{
				Name:      "show",
				Usage:     "show source details",
				ArgsUsage: "<source-name>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return fmt.Errorf("sources show not yet implemented")
				},
			},
			{
				Name:      "schema",
				Usage:     "show source schema",
				ArgsUsage: "<source-name>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return a.runSourcesSchema(ctx, cmd)
				},
			},
			{
				Name:  "select",
				Usage: "interactively select a source",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return a.runSourcesSelect(ctx, cmd)
				},
			},
		},
	}
}

func (a *App) runSourcesList(ctx context.Context, cmd *cli.Command) error {
	if a.Config.Auth.Token == "" {
		return fmt.Errorf("API token not configured")
	}

	apiClient, err := client.New(a.Config)
	if err != nil {
		return err
	}

	// Get teams first
	teams, err := apiClient.ListTeams(ctx)
	if err != nil {
		return fmt.Errorf("failed to list teams: %w", err)
	}

	fmt.Println("Available Sources:")
	fmt.Println()

	for _, team := range teams {
		sources, err := apiClient.ListSources(ctx, team.ID)
		if err != nil {
			continue
		}

		if len(sources) > 0 {
			fmt.Printf("%s %s\n", successStyle.Render("Team:"), team.Name)
			for _, source := range sources {
				status := successStyle.Render("●")
				if !source.IsConnected {
					status = errorStyle.Render("●")
				}
				fmt.Printf("  %s %s\n", status, source.Name)
				if source.Description != "" {
					fmt.Printf("    %s\n", mutedStyle.Render(source.Description))
				}
			}
			fmt.Println()
		}
	}

	return nil
}

func (a *App) runSourcesSchema(ctx context.Context, cmd *cli.Command) error {
	if a.Config.Auth.Token == "" {
		return fmt.Errorf("API token not configured")
	}

	apiClient, err := client.New(a.Config)
	if err != nil {
		return err
	}

	teamID, sourceID, err := a.resolveTeamAndSource(ctx, apiClient)
	if err != nil {
		return err
	}

	columns, err := apiClient.GetSchema(ctx, teamID, sourceID)
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	fmt.Println("Schema:")
	fmt.Println()
	for _, col := range columns {
		fmt.Printf("  %s %s\n", col.Name, mutedStyle.Render(col.Type))
	}

	return nil
}

func (a *App) runSourcesSelect(ctx context.Context, cmd *cli.Command) error {
	if a.Config.Auth.Token == "" {
		return fmt.Errorf("API token not configured")
	}

	apiClient, err := client.New(a.Config)
	if err != nil {
		return err
	}

	// Get teams
	teams, err := apiClient.ListTeams(ctx)
	if err != nil {
		return fmt.Errorf("failed to list teams: %w", err)
	}

	if len(teams) == 0 {
		return fmt.Errorf("no teams available")
	}

	// Select team
	teamOptions := make([]huh.Option[int], len(teams))
	for i, t := range teams {
		teamOptions[i] = huh.NewOption(t.Name, t.ID)
	}

	var selectedTeamID int
	err = huh.NewSelect[int]().
		Title("Select Team").
		Options(teamOptions...).
		Value(&selectedTeamID).
		Run()
	if err != nil {
		return err
	}

	// Get sources for selected team
	sources, err := apiClient.ListSources(ctx, selectedTeamID)
	if err != nil {
		return fmt.Errorf("failed to list sources: %w", err)
	}

	if len(sources) == 0 {
		return fmt.Errorf("no sources available for this team")
	}

	// Select source
	sourceOptions := make([]huh.Option[string], len(sources))
	for i, s := range sources {
		label := s.Name
		if s.Description != "" {
			label = fmt.Sprintf("%s - %s", s.Name, s.Description)
		}
		sourceOptions[i] = huh.NewOption(label, s.Name)
	}

	var selectedSource string
	err = huh.NewSelect[string]().
		Title("Select Source").
		Options(sourceOptions...).
		Value(&selectedSource).
		Run()
	if err != nil {
		return err
	}

	// Find team name
	var teamName string
	for _, t := range teams {
		if t.ID == selectedTeamID {
			teamName = t.Name
			break
		}
	}

	fmt.Printf("\n%s\n", successStyle.Render("Selected:"))
	fmt.Printf("  Team:   %s\n", teamName)
	fmt.Printf("  Source: %s\n", selectedSource)
	fmt.Printf("\n%s\n", mutedStyle.Render("To save as defaults, run:"))
	fmt.Printf("  logchef config set defaults.team %s\n", teamName)
	fmt.Printf("  logchef config set defaults.source %s\n", selectedSource)

	return nil
}

// configCommand returns the config subcommand
func (a *App) configCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "manage CLI configuration",
		Commands: []*cli.Command{
			{
				Name:  "show",
				Usage: "show current configuration",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return a.runConfigShow(ctx, cmd)
				},
			},
			{
				Name:      "set",
				Usage:     "set a configuration value",
				ArgsUsage: "<key> <value>",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return a.runConfigSet(ctx, cmd)
				},
			},
			{
				Name:  "init",
				Usage: "initialize configuration interactively",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return a.runConfigInit(ctx, cmd)
				},
			},
			{
				Name:  "set-token",
				Usage: "set API token interactively",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return a.runConfigSetToken(ctx, cmd)
				},
			},
		},
	}
}

func (a *App) runConfigShow(ctx context.Context, cmd *cli.Command) error {
	fmt.Printf("Server URL:     %s\n", a.Config.Server.URL)
	fmt.Printf("Default Team:   %s\n", a.Config.Defaults.Team)
	fmt.Printf("Default Source: %s\n", a.Config.Defaults.Source)
	fmt.Printf("Output Format:  %s\n", a.Config.Output.Format)
	fmt.Printf("Timezone:       %s\n", a.Config.Defaults.Timezone)

	if a.Config.Auth.Token != "" {
		// Only show prefix of token
		token := a.Config.Auth.Token
		if len(token) > 20 {
			token = token[:20] + "..."
		}
		fmt.Printf("API Token:      %s\n", mutedStyle.Render(token))
	} else {
		fmt.Printf("API Token:      %s\n", errorStyle.Render("not set"))
	}

	return nil
}

func (a *App) runConfigSet(ctx context.Context, cmd *cli.Command) error {
	args := cmd.Args().Slice()
	if len(args) < 2 {
		return fmt.Errorf("usage: logchef config set <key> <value>")
	}

	key := args[0]
	value := args[1]

	switch key {
	case "server.url":
		a.Config.Server.URL = value
	case "defaults.team":
		a.Config.Defaults.Team = value
	case "defaults.source":
		a.Config.Defaults.Source = value
	case "defaults.timezone":
		a.Config.Defaults.Timezone = value
	case "defaults.limit":
		var limit int
		fmt.Sscanf(value, "%d", &limit)
		a.Config.Defaults.Limit = limit
	case "output.format":
		a.Config.Output.Format = value
	case "auth.token":
		a.Config.Auth.Token = value
	default:
		return fmt.Errorf("unknown config key: %s\nValid keys: server.url, defaults.team, defaults.source, defaults.timezone, defaults.limit, output.format, auth.token", key)
	}

	if err := a.Config.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("%s %s = %s\n", successStyle.Render("Set"), key, value)
	return nil
}

func (a *App) runConfigInit(ctx context.Context, cmd *cli.Command) error {
	var serverURL, token, team, source string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("LogChef Server URL").
				Description("The URL of your LogChef server").
				Placeholder("https://logchef.example.com").
				Value(&serverURL),
			huh.NewInput().
				Title("API Token").
				Description("Your LogChef API token").
				Placeholder("logchef_...").
				EchoMode(huh.EchoModePassword).
				Value(&token),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Default Team").
				Description("Default team name (optional)").
				Value(&team),
			huh.NewInput().
				Title("Default Source").
				Description("Default source name (optional)").
				Value(&source),
		),
	)

	err := form.Run()
	if err != nil {
		return err
	}

	// Update config
	if serverURL != "" {
		a.Config.Server.URL = serverURL
	}
	if token != "" {
		a.Config.Auth.Token = token
	}
	if team != "" {
		a.Config.Defaults.Team = team
	}
	if source != "" {
		a.Config.Defaults.Source = source
	}

	if err := a.Config.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("\n%s Configuration saved!\n", successStyle.Render("✓"))
	return nil
}

func (a *App) runConfigSetToken(ctx context.Context, cmd *cli.Command) error {
	var token string

	err := huh.NewInput().
		Title("API Token").
		Description("Enter your LogChef API token").
		Placeholder("logchef_...").
		EchoMode(huh.EchoModePassword).
		Value(&token).
		Run()
	if err != nil {
		return err
	}

	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	a.Config.Auth.Token = token
	if err := a.Config.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("%s API token saved!\n", successStyle.Render("✓"))
	return nil
}

// translateCommand returns the translate subcommand
func (a *App) translateCommand() *cli.Command {
	return &cli.Command{
		Name:      "translate",
		Usage:     "translate LogChefQL to SQL",
		ArgsUsage: "<query>",
		Description: `Translate a LogChefQL query to SQL without executing it.

This is useful for understanding how your query will be executed.

Examples:
   logchef translate 'level="error"'
   logchef translate 'status>=500 AND service~"api.*"' --since 1h`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "since",
				Aliases: []string{"s"},
				Usage:   "time range for full SQL generation",
				Value:   "15m",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return a.runTranslate(ctx, cmd)
		},
	}
}

func (a *App) runTranslate(ctx context.Context, cmd *cli.Command) error {
	query := cmd.Args().First()
	if query == "" {
		return fmt.Errorf("query is required")
	}

	if a.Config.Auth.Token == "" {
		return fmt.Errorf("API token not configured")
	}

	apiClient, err := client.New(a.Config)
	if err != nil {
		return err
	}

	teamID, sourceID, err := a.resolveTeamAndSource(ctx, apiClient)
	if err != nil {
		return err
	}

	resp, err := apiClient.Translate(ctx, teamID, sourceID, client.TranslateRequest{
		Query: query,
	})
	if err != nil {
		return fmt.Errorf("translation failed: %w", err)
	}

	if !resp.Valid {
		if resp.Error != nil {
			return fmt.Errorf("invalid query: %s", resp.Error.Message)
		}
		return fmt.Errorf("invalid query")
	}

	fmt.Printf("%s %s\n\n", mutedStyle.Render("LogChefQL:"), query)
	fmt.Printf("%s\n%s\n", mutedStyle.Render("Generated SQL (WHERE clause):"), resp.SQL)

	if len(resp.FieldsUsed) > 0 {
		fmt.Printf("\n%s %v\n", mutedStyle.Render("Fields used:"), resp.FieldsUsed)
	}

	return nil
}
