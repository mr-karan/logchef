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

	// Space operations
	CreateSpace(ctx context.Context, space *models.Space) error
	GetSpace(ctx context.Context, spaceID string) (*models.Space, error)
	UpdateSpace(ctx context.Context, space *models.Space) error
	DeleteSpace(ctx context.Context, spaceID string) error
	ListSpaces(ctx context.Context) ([]*models.Space, error)

	// Space access operations
	GrantTeamAccess(ctx context.Context, access *models.SpaceTeamAccess) error
	RevokeTeamAccess(ctx context.Context, spaceID, teamID string) error
	ListSpaceAccess(ctx context.Context, spaceID string) ([]*models.SpaceTeamAccess, error)
	GetTeamSpaceAccess(ctx context.Context, teamID, spaceID string) (*models.SpaceTeamAccess, error)

	// Space data source operations
	AddSpaceDataSource(ctx context.Context, spaceDS *models.SpaceDataSource) error
	RemoveSpaceDataSource(ctx context.Context, spaceID, dataSourceID string) error
	ListSpaceDataSources(ctx context.Context, spaceID string) ([]*models.SpaceDataSource, error)
}
