package auth

import (
	"context"

	"backend-v2/pkg/models"
)

// Store defines the interface for auth-related storage operations
type Store interface {
	// User operations
	CreateUser(ctx context.Context, user *models.User) error
	GetUser(ctx context.Context, id string) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	UpdateUser(ctx context.Context, user *models.User) error
	ListUsers(ctx context.Context) ([]*models.User, error)
	CountAdminUsers(ctx context.Context) (int, error)

	// Session operations
	CreateSession(ctx context.Context, session *models.Session) error
	GetSession(ctx context.Context, id string) (*models.Session, error)
	DeleteSession(ctx context.Context, id string) error
	DeleteUserSessions(ctx context.Context, userID string) error
	CountUserSessions(ctx context.Context, userID string) (int, error)

	// Team operations
	CreateTeam(ctx context.Context, team *models.Team) error
	GetTeam(ctx context.Context, id string) (*models.Team, error)
	UpdateTeam(ctx context.Context, team *models.Team) error
	DeleteTeam(ctx context.Context, id string) error
	ListTeams(ctx context.Context) ([]*models.Team, error)

	// Team member operations
	AddTeamMember(ctx context.Context, teamID, userID, role string) error
	RemoveTeamMember(ctx context.Context, teamID, userID string) error
	ListTeamMembers(ctx context.Context, teamID string) ([]*models.TeamMember, error)
	GetUserTeams(ctx context.Context, userID string) ([]*models.Team, error)

	// Team source operations
	AddTeamSource(ctx context.Context, teamID, sourceID string) error
	RemoveTeamSource(ctx context.Context, teamID, sourceID string) error
	ListTeamSources(ctx context.Context, teamID string) ([]*models.Source, error)
	ListSourceTeams(ctx context.Context, sourceID string) ([]*models.Team, error)

	// Team query operations
	CreateTeamQuery(ctx context.Context, query *models.TeamQuery) error
	GetTeamQuery(ctx context.Context, queryID string) (*models.TeamQuery, error)
	UpdateTeamQuery(ctx context.Context, query *models.TeamQuery) error
	DeleteTeamQuery(ctx context.Context, queryID string) error
	ListTeamQueries(ctx context.Context, teamID string) ([]*models.TeamQuery, error)
}
