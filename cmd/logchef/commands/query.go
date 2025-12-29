// Package commands provides the CLI command definitions for LogChef.
package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/mr-karan/logchef/internal/cli/client"
	"github.com/mr-karan/logchef/internal/cli/query"
	"github.com/mr-karan/logchef/internal/cli/render"
	"github.com/urfave/cli/v3"
)

// queryCommand returns the query subcommand
func (a *App) queryCommand() *cli.Command {
	return &cli.Command{
		Name:      "query",
		Usage:     "execute a LogChefQL query",
		ArgsUsage: "[query]",
		Description: `Execute a LogChefQL query against the configured source.

If no query is provided, returns all logs (equivalent to no filter).

Examples:
   logchef query --since 1h                          # all logs from last hour
   logchef query 'level:error'                       # filter by level
   logchef query 'level:error AND service:api'      # multiple filters
   logchef query 'status>=500' --output jsonl        # output as JSONL
   logchef query --sql 'SELECT * FROM logs LIMIT 10' # raw SQL mode`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "since",
				Aliases: []string{"s"},
				Usage:   "relative time range (e.g., 15m, 1h, 24h, 7d)",
				Value:   "15m",
			},
			&cli.StringFlag{
				Name:  "from",
				Usage: "absolute start time (ISO8601 format)",
			},
			&cli.StringFlag{
				Name:  "to",
				Usage: "absolute end time (ISO8601 format)",
			},
			&cli.StringFlag{
				Name:  "tz",
				Usage: "timezone for time display",
				Value: "Local",
			},
			&cli.IntFlag{
				Name:    "limit",
				Aliases: []string{"l"},
				Usage:   "maximum number of results",
				Value:   100,
			},
			&cli.IntFlag{
				Name:  "timeout",
				Usage: "query timeout in seconds",
				Value: 60,
			},
			&cli.StringFlag{
				Name:    "output",
				Aliases: []string{"o"},
				Usage:   "output format: text, json, jsonl, csv, table",
				Value:   "text",
			},
			&cli.StringFlag{
				Name:    "format",
				Aliases: []string{"f"},
				Usage:   "custom Go template format",
			},
			&cli.StringFlag{
				Name:  "fields",
				Usage: "comma-separated list of fields to display",
			},
			&cli.BoolFlag{
				Name:  "sql",
				Usage: "treat query as raw SQL instead of LogChefQL",
			},
			&cli.BoolFlag{
				Name:  "show-sql",
				Usage: "display the generated SQL query",
			},
			&cli.BoolFlag{
				Name:  "no-pager",
				Usage: "disable pager for output",
			},
			&cli.BoolFlag{
				Name:    "count",
				Aliases: []string{"c"},
				Usage:   "only show count of matching logs",
			},
			&cli.BoolFlag{
				Name:  "stats",
				Usage: "only show query statistics",
			},
			&cli.StringFlag{
				Name:  "time-format",
				Usage: "timestamp format: rfc3339, short, time, relative",
				Value: "rfc3339",
			},
		},
		Action: a.runQuery,
	}
}

func (a *App) runQuery(ctx context.Context, cmd *cli.Command) error {
	// Get query from args - empty string is valid (returns all logs, like frontend default)
	queryStr := strings.Join(cmd.Args().Slice(), " ")

	// Validate configuration
	if a.Config.Server.URL == "" {
		return fmt.Errorf("server URL not configured. Run 'logchef config init' or set LOGCHEF_SERVER_URL")
	}
	if a.Config.Auth.Token == "" {
		return fmt.Errorf("API token not configured. Run 'logchef config set-token' or set LOGCHEF_API_TOKEN")
	}

	// Create API client
	apiClient, err := client.New(a.Config)
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	// Resolve team and source
	teamID, sourceID, err := a.resolveTeamAndSource(ctx, apiClient)
	if err != nil {
		return err
	}

	// Parse time range
	startTime, endTime, err := query.ParseTimeRange(query.TimeRangeOptions{
		Since: cmd.String("since"),
		From:  cmd.String("from"),
		To:    cmd.String("to"),
	})
	if err != nil {
		return fmt.Errorf("invalid time range: %w", err)
	}

	tz := cmd.String("tz")
	if tz == "" || tz == "Local" {
		tz = a.Config.ResolveTimezone()
	}

	log.Debug("executing query",
		"query", queryStr,
		"team_id", teamID,
		"source_id", sourceID,
		"start", startTime.Format("2006-01-02 15:04:05"),
		"end", endTime.Format("2006-01-02 15:04:05"),
		"timezone", tz,
		"limit", cmd.Int("limit"),
	)

	var result *render.QueryResult

	// Server expects time format: "YYYY-MM-DD HH:MM:SS"
	const timeFormat = "2006-01-02 15:04:05"

	if cmd.Bool("sql") {
		// Raw SQL query
		resp, err := apiClient.QuerySQL(ctx, teamID, sourceID, client.SQLQueryRequest{
			RawSQL:       queryStr,
			Limit:        int(cmd.Int("limit")),
			Timezone:     tz,
			StartTime:    startTime.Format(timeFormat),
			EndTime:      endTime.Format(timeFormat),
			QueryTimeout: int(cmd.Int("timeout")),
		})
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		result = &render.QueryResult{
			Logs:    resp.Data,
			Columns: toRenderColumns(resp.Columns),
			Stats:   toRenderStats(resp.Stats),
		}
	} else {
		// LogChefQL query
		resp, err := apiClient.Query(ctx, teamID, sourceID, client.QueryRequest{
			Query:        queryStr,
			StartTime:    startTime.Format(timeFormat),
			EndTime:      endTime.Format(timeFormat),
			Timezone:     tz,
			Limit:        int(cmd.Int("limit")),
			QueryTimeout: int(cmd.Int("timeout")),
		})
		if err != nil {
			return fmt.Errorf("query failed: %w", err)
		}
		result = &render.QueryResult{
			Logs:         resp.Logs,
			Columns:      toRenderColumns(resp.Columns),
			Stats:        toRenderStats(resp.Stats),
			GeneratedSQL: resp.GeneratedSQL,
		}
	}

	log.Debug("query result",
		"rows", len(result.Logs),
		"execution_time_ms", result.Stats.ExecutionTimeMs,
		"rows_read", result.Stats.RowsRead,
		"bytes_read", result.Stats.BytesRead,
	)

	// Show SQL if requested
	if cmd.Bool("show-sql") && result.GeneratedSQL != "" {
		fmt.Printf("%s\n\n", mutedStyle.Render("Generated SQL: "+result.GeneratedSQL))
	}

	renderer, err := render.New(render.Options{
		Format:     cmd.String("output"),
		Template:   cmd.String("format"),
		Fields:     parseFields(cmd.String("fields")),
		Color:      a.Config.Output.Color != "never" && isTerminal(),
		UsePager:   !cmd.Bool("no-pager") && isTerminal() && !cmd.Bool("count") && !cmd.Bool("stats"),
		Pager:      a.Config.Output.Pager,
		TimeFormat: cmd.String("time-format"),
		CountOnly:  cmd.Bool("count"),
		StatsOnly:  cmd.Bool("stats"),
	})
	if err != nil {
		return fmt.Errorf("failed to create renderer: %w", err)
	}

	return renderer.Render(result)
}

// resolveTeamAndSource resolves team and source IDs from names or config
func (a *App) resolveTeamAndSource(ctx context.Context, c *client.Client) (teamID, sourceID int, err error) {
	teamName := a.Config.Defaults.Team
	sourceName := a.Config.Defaults.Source

	if teamName == "" {
		return 0, 0, fmt.Errorf("team not specified. Use --team flag or set defaults.team in config")
	}
	if sourceName == "" {
		return 0, 0, fmt.Errorf("source not specified. Use --source flag or set defaults.source in config")
	}

	// Get teams
	teams, err := c.ListTeams(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list teams: %w", err)
	}

	// Find team
	for _, t := range teams {
		if t.Name == teamName || fmt.Sprintf("%d", t.ID) == teamName {
			teamID = t.ID
			break
		}
	}
	if teamID == 0 {
		return 0, 0, fmt.Errorf("team not found: %s", teamName)
	}

	// Get sources
	sources, err := c.ListSources(ctx, teamID)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list sources: %w", err)
	}

	// Find source
	for _, s := range sources {
		if s.Name == sourceName || fmt.Sprintf("%d", s.ID) == sourceName {
			sourceID = s.ID
			break
		}
	}
	if sourceID == 0 {
		return 0, 0, fmt.Errorf("source not found: %s", sourceName)
	}

	return teamID, sourceID, nil
}

func parseFields(s string) []string {
	if s == "" {
		return nil
	}
	fields := strings.Split(s, ",")
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	return fields
}

func toRenderColumns(cols []client.Column) []render.Column {
	result := make([]render.Column, len(cols))
	for i, c := range cols {
		result[i] = render.Column{Name: c.Name, Type: c.Type}
	}
	return result
}

func toRenderStats(stats client.QueryStats) render.QueryStats {
	return render.QueryStats{
		ExecutionTimeMs: stats.ExecutionTimeMs,
		RowsRead:        stats.RowsRead,
		BytesRead:       stats.BytesRead,
	}
}
