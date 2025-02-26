package auth

import (
	"context"

	"github.com/mr-karan/logchef/pkg/models"
)

// Store defines the interface for auth-related storage operations
type Store interface {
	// User operations
	CreateUser(ctx context.Context, user *models.User) error
	GetUser(ctx context.Context, id models.UserID) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	UpdateUser(ctx context.Context, user *models.User) error
	ListUsers(ctx context.Context) ([]*models.User, error)
	CountAdminUsers(ctx context.Context) (int, error)

	// Session operations
	CreateSession(ctx context.Context, session *models.Session) error
	GetSession(ctx context.Context, id models.SessionID) (*models.Session, error)
	DeleteSession(ctx context.Context, id models.SessionID) error
	DeleteUserSessions(ctx context.Context, userID models.UserID) error
	CountUserSessions(ctx context.Context, userID models.UserID) (int, error)

	// Team operations
	CreateTeam(ctx context.Context, team *models.Team) error
	GetTeam(ctx context.Context, id models.TeamID) (*models.Team, error)
	UpdateTeam(ctx context.Context, team *models.Team) error
	DeleteTeam(ctx context.Context, id models.TeamID) error
	ListTeams(ctx context.Context) ([]*models.Team, error)

	// Team member operations
	AddTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID, role models.TeamRole) error
	RemoveTeamMember(ctx context.Context, teamID models.TeamID, userID models.UserID) error
	ListTeamMembers(ctx context.Context, teamID models.TeamID) ([]*models.TeamMember, error)
	ListUserTeams(ctx context.Context, userID models.UserID) ([]*models.Team, error)

	// Team source operations
	AddTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error
	RemoveTeamSource(ctx context.Context, teamID models.TeamID, sourceID models.SourceID) error
	ListTeamSources(ctx context.Context, teamID models.TeamID) ([]*models.Source, error)
	ListSourceTeams(ctx context.Context, sourceID models.SourceID) ([]*models.Team, error)

	// Team query operations
	CreateTeamQuery(ctx context.Context, query *models.TeamQuery) error
	GetTeamQuery(ctx context.Context, queryID string) (*models.TeamQuery, error)
	UpdateTeamQuery(ctx context.Context, query *models.TeamQuery) error
	DeleteTeamQuery(ctx context.Context, queryID string) error
	ListTeamQueries(ctx context.Context, teamID models.TeamID) ([]*models.TeamQuery, error)
}
