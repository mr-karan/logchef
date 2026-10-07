package mcp

import (
	"context"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/pkg/models"
)

// GetProfileParams represents the parameters for getting user profile.
type GetProfileParams struct{}

// GetTeamsParams represents the parameters for getting user teams.
type GetTeamsParams struct{}

// GetMetaParams represents the parameters for getting server metadata.
type GetMetaParams struct{}

// --- Output schemas ---

// ProfileResult is the profile shape that OpenAI reads from a tool marked
// with _meta["openai/profile"]. Its schema allows no other fields.
type ProfileResult struct {
	ID       string `json:"id" jsonschema:"Opaque profile identifier, unique within this app and unchanged across token refresh, reconnection, and display-metadata changes. Never reassigned to another profile."`
	Name     string `json:"name,omitempty" jsonschema:"Display name for the authenticated profile."`
	Email    string `json:"email,omitempty" jsonschema:"Email address for display; not used as the profile identity."`
	Nickname string `json:"nickname,omitempty" jsonschema:"A useful label that helps users distinguish connected profiles."`
}

type TeamResult struct {
	ID          int    `json:"id" jsonschema:"Team ID"`
	Name        string `json:"name" jsonschema:"Team name"`
	Role        string `json:"role" jsonschema:"Current user role in this team"`
	MemberCount int    `json:"member_count" jsonschema:"Number of members in the team"`
	CreatedAt   string `json:"created_at" jsonschema:"Team creation timestamp"`
	UpdatedAt   string `json:"updated_at" jsonschema:"Team last update timestamp"`
}

type MetaResult struct {
	Version           string `json:"version" jsonschema:"Server version"`
	HTTPServerTimeout string `json:"http_server_timeout" jsonschema:"HTTP server timeout setting"`
}

// --- Handlers ---

// handleGetProfile builds the profile from the authenticated user. The ID is
// the user's primary key. SQLite (AUTOINCREMENT) and Postgres (IDENTITY) never
// reuse a deleted user's key, so the ID is never reassigned.
func (t *tools) handleGetProfile(ctx context.Context, _ mcp.CallToolRequest, _ GetProfileParams) (ProfileResult, error) {
	p, err := principalFrom(ctx)
	if err != nil {
		return ProfileResult{}, t.storeError("get profile", err)
	}
	if err := p.Require(models.TokenScopeProfileRead); err != nil {
		return ProfileResult{}, t.storeError("get profile", err)
	}
	return ProfileResult{
		ID:    strconv.FormatInt(int64(p.User.ID), 10),
		Name:  p.User.FullName,
		Email: p.User.Email,
	}, nil
}

func (t *tools) handleGetTeams(ctx context.Context, _ mcp.CallToolRequest, _ GetTeamsParams) ([]TeamResult, error) {
	p, err := principalFrom(ctx)
	if err != nil {
		return nil, t.storeError("get teams", err)
	}
	if err := p.Require(models.TokenScopeTeamsRead); err != nil {
		return nil, t.storeError("get teams", err)
	}
	teams, err := core.ListTeamsForUser(ctx, t.deps.DB, p.User.ID)
	if err != nil {
		return nil, t.storeError("get teams", err)
	}

	result := make([]TeamResult, len(teams))
	for i, team := range teams {
		result[i] = TeamResult{
			ID:          int(team.ID),
			Name:        team.Name,
			Role:        string(team.Role),
			MemberCount: team.MemberCount,
			CreatedAt:   formatTime(team.CreatedAt),
			UpdatedAt:   formatTime(team.UpdatedAt),
		}
	}
	return result, nil
}

func (t *tools) handleGetMeta(_ context.Context, _ mcp.CallToolRequest, _ GetMetaParams) (MetaResult, error) {
	meta := core.BuildMeta(t.deps.Config, t.deps.Version, "", false)
	return MetaResult{
		Version:           meta.Version,
		HTTPServerTimeout: meta.HTTPServerTimeout,
	}, nil
}

func (t *tools) addProfileTools(s *server.MCPServer) {
	profileTool := mcp.NewTool("get_profile",
		mcp.WithDescription("Return the profile represented by this request's authenticated credentials. The opaque id is unique within this app and remains unchanged across token refresh, reconnection, and display-metadata changes."),
		mcp.WithInputSchema[GetProfileParams](),
		mcp.WithOutputSchema[ProfileResult](),
		mcp.WithTitleAnnotation("Get Profile"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	profileTool.Meta = mcp.NewMetaFromMap(map[string]any{"openai/profile": true})
	s.AddTool(profileTool, mcp.NewStructuredToolHandler(t.handleGetProfile))

	teamsTool := mcp.NewTool("get_teams",
		mcp.WithDescription("Get the teams that the current user belongs to, including their role in each team and member count."),
		mcp.WithInputSchema[GetTeamsParams](),
		mcp.WithOutputSchema[[]TeamResult](),
		mcp.WithTitleAnnotation("Get My Teams"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(teamsTool, mcp.NewStructuredToolHandler(t.handleGetTeams))

	metaTool := mcp.NewTool("get_meta",
		mcp.WithDescription("Get server metadata including version information and configuration details."),
		mcp.WithInputSchema[GetMetaParams](),
		mcp.WithOutputSchema[MetaResult](),
		mcp.WithTitleAnnotation("Get Server Metadata"),
		mcp.WithReadOnlyHintAnnotation(true),
	)
	s.AddTool(metaTool, mcp.NewStructuredToolHandler(t.handleGetMeta))
}
