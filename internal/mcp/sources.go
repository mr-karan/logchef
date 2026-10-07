package mcp

import (
	"context"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"golang.org/x/sync/errgroup"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/pkg/models"
)

// GetTeamSourcesParams represents the parameters for getting sources for a specific team.
type GetTeamSourcesParams struct {
	TeamID int `json:"team_id" jsonschema:"The ID of the team to get sources for"`
}

// GetSourcesParams represents the parameters for getting all sources accessible to the user.
type GetSourcesParams struct{}

// --- Output schemas ---

type SourceResult struct {
	ID             int              `json:"id" jsonschema:"Source ID"`
	SourceType     string           `json:"source_type" jsonschema:"Backend type: clickhouse or victorialogs"`
	QueryLanguages []string         `json:"query_languages" jsonschema:"Supported query languages"`
	Capabilities   []string         `json:"capabilities" jsonschema:"Supported backend operations"`
	Name           string           `json:"name" jsonschema:"Source name"`
	Description    string           `json:"description" jsonschema:"Source description"`
	Connection     ConnectionResult `json:"connection" jsonschema:"ClickHouse connection details"`
	TsField        string           `json:"ts_field" jsonschema:"Timestamp field name"`
	IsConnected    bool             `json:"is_connected" jsonschema:"Whether the source is currently connected"`
	TTLDays        int              `json:"ttl_days" jsonschema:"Data retention in days"`
	CreatedAt      string           `json:"created_at" jsonschema:"Creation timestamp"`
}

type ConnectionResult struct {
	Host      string `json:"host" jsonschema:"ClickHouse host"`
	Database  string `json:"database" jsonschema:"ClickHouse database name"`
	TableName string `json:"table_name" jsonschema:"ClickHouse table name"`
}

type SourceWithTeamsResult struct {
	SourceResult
	Teams []TeamInfo `json:"teams" jsonschema:"Teams this source belongs to"`
}

type TeamInfo struct {
	ID   int    `json:"id" jsonschema:"Team ID"`
	Name string `json:"name" jsonschema:"Team name"`
	Role string `json:"role" jsonschema:"User role in this team"`
}

type SourcesAggregateResult struct {
	Sources []SourceWithTeamsResult `json:"sources" jsonschema:"All accessible sources with team associations"`
}

// --- Handlers ---

// listTeamSources returns the sources of a team the principal belongs to.
func (t *tools) listTeamSources(ctx context.Context, p access.Principal, teamID models.TeamID) ([]*models.Source, error) {
	if err := access.AuthorizeTeam(ctx, t.deps.DB, p, teamID, models.TokenScopeSourcesRead); err != nil {
		return nil, err
	}
	return core.ListTeamSources(ctx, t.deps.DB, t.deps.Datasources, t.deps.Log, teamID)
}

func (t *tools) handleGetTeamSources(ctx context.Context, _ mcp.CallToolRequest, args GetTeamSourcesParams) ([]SourceResult, error) {
	p, err := principalFrom(ctx)
	if err != nil {
		return nil, t.storeError("get team sources", err)
	}
	sources, err := t.listTeamSources(ctx, p, models.TeamID(args.TeamID))
	if err != nil {
		return nil, t.storeError("get team sources", err)
	}

	result := make([]SourceResult, len(sources))
	for i, s := range sources {
		result[i] = sourceToResult(s)
	}
	return result, nil
}

func (t *tools) handleGetSources(ctx context.Context, _ mcp.CallToolRequest, _ GetSourcesParams) (SourcesAggregateResult, error) {
	p, err := principalFrom(ctx)
	if err != nil {
		return SourcesAggregateResult{}, t.storeError("get sources", err)
	}
	if err := p.Require(models.TokenScopeTeamsRead); err != nil {
		return SourcesAggregateResult{}, t.storeError("get sources", err)
	}
	teams, err := core.ListTeamsForUser(ctx, t.deps.DB, p.User.ID)
	if err != nil {
		return SourcesAggregateResult{}, t.storeError("get user teams", err)
	}

	// Fetch sources for all teams in parallel to avoid N+1.
	type teamSourceResult struct {
		team    TeamInfo
		sources []*models.Source
	}

	results := make([]teamSourceResult, len(teams))
	g, gctx := errgroup.WithContext(ctx)

	for i, team := range teams {
		g.Go(func() error {
			teamSources, err := t.listTeamSources(gctx, p, team.ID)
			if err != nil {
				return err
			}
			results[i] = teamSourceResult{
				team:    TeamInfo{ID: int(team.ID), Name: team.Name, Role: string(team.Role)},
				sources: teamSources,
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return SourcesAggregateResult{}, t.storeError("get sources", err)
	}

	// Merge results into deduplicated map.
	sourceMap := make(map[models.SourceID]*SourceWithTeamsResult)
	for _, r := range results {
		for _, s := range r.sources {
			if existing, exists := sourceMap[s.ID]; exists {
				existing.Teams = append(existing.Teams, r.team)
			} else {
				sourceMap[s.ID] = &SourceWithTeamsResult{
					SourceResult: sourceToResult(s),
					Teams:        []TeamInfo{r.team},
				}
			}
		}
	}

	sources := make([]SourceWithTeamsResult, 0, len(sourceMap))
	for _, entry := range sourceMap {
		sources = append(sources, *entry)
	}

	sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
	return SourcesAggregateResult{Sources: sources}, nil
}

func sourceToResult(s *models.Source) SourceResult {
	languages := make([]string, len(s.QueryLanguages))
	for i, language := range s.QueryLanguages {
		languages[i] = string(language)
	}
	return SourceResult{
		ID:             int(s.ID),
		SourceType:     string(models.NormalizeSourceType(s.SourceType)),
		QueryLanguages: languages,
		Capabilities:   s.Capabilities,
		Name:           s.Name,
		Description:    s.Description,
		Connection: ConnectionResult{
			Host:      s.Connection.Host,
			Database:  s.Connection.Database,
			TableName: s.Connection.TableName,
		},
		TsField:     s.MetaTSField,
		IsConnected: s.IsConnected,
		TTLDays:     s.TTLDays,
		CreatedAt:   formatTime(s.CreatedAt),
	}
}

func (t *tools) addSourcesTools(s *server.MCPServer) {
	teamSourcesTool := mcp.NewTool("get_team_sources",
		mcp.WithDescription("Get the sources that belong to a specific team. Requires the team ID. If the user gives a team name instead of an ID, call get_teams first to find the numeric ID."),
		mcp.WithInputSchema[GetTeamSourcesParams](),
		mcp.WithOutputSchema[[]SourceResult](),
		mcp.WithTitleAnnotation("Get Team Sources"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(teamSourcesTool, mcp.NewStructuredToolHandler(t.handleGetTeamSources))

	sourcesTool := mcp.NewTool("get_sources",
		mcp.WithDescription("Get all sources the current user can access across all teams. Returns sources with team associations. Use this when the user mentions a source by name — find the matching source and use its team_id and source_id for subsequent queries."),
		mcp.WithInputSchema[GetSourcesParams](),
		mcp.WithOutputSchema[SourcesAggregateResult](),
		mcp.WithTitleAnnotation("Get All Accessible Sources"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(sourcesTool, mcp.NewStructuredToolHandler(t.handleGetSources))
}
