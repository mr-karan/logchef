package models

import "time"

// UserStatus represents the possible user statuses
type UserStatus string

const (
	// UserStatusActive represents an active user account
	UserStatusActive UserStatus = "active"

	// UserStatusInactive represents an inactive/suspended user account
	UserStatusInactive UserStatus = "inactive"
)

// UserRole represents the possible user roles
type UserRole string

const (
	// UserRoleAdmin represents a user with administrative privileges
	UserRoleAdmin UserRole = "admin"

	// UserRoleMember represents a regular user with standard permissions
	UserRoleMember UserRole = "member"
)

// TeamRole represents the possible team member roles
type TeamRole string

const (
	// TeamRoleOwner represents the owner of a team
	TeamRoleOwner TeamRole = "owner"

	// TeamRoleAdmin represents a team admin
	TeamRoleAdmin TeamRole = "admin"

	// TeamRoleEditor represents a team editor with collection management permissions
	TeamRoleEditor TeamRole = "editor"

	// TeamRoleMember represents a regular team member
	TeamRoleMember TeamRole = "member"
)

// User represents a user in the system
type User struct {
	ID           UserID     `json:"id" db:"id"`
	Email        string     `json:"email" db:"email"`
	FullName     string     `json:"full_name" db:"full_name"`
	Role         UserRole   `json:"role" db:"role"`
	Status       UserStatus `json:"status" db:"status"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty" db:"last_active_at"`
	Timestamps
}

// CreateUserRequest represents a request to create a new user
type CreateUserRequest struct {
	Email    string   `json:"email"`
	FullName string   `json:"full_name"`
	Role     UserRole `json:"role"`
}

// UpdateUserRequest represents a request to update a user
type UpdateUserRequest struct {
	Email    string     `json:"email,omitempty"`
	FullName string     `json:"full_name,omitempty"`
	Role     UserRole   `json:"role,omitempty"`
	Status   UserStatus `json:"status,omitempty"`
}

// Session represents a user session
type Session struct {
	ID        SessionID `db:"id" json:"id"`
	UserID    UserID    `db:"user_id" json:"user_id"`
	ExpiresAt time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// Team represents a team in the system
type Team struct {
	ID          TeamID `db:"id" json:"id"`
	Name        string `db:"name" json:"name"`
	Description string `db:"description" json:"description"`
	MemberCount int    `db:"-" json:"member_count"`
	Timestamps
}

// TeamMember represents a user's membership in a team
type TeamMember struct {
	TeamID    TeamID    `db:"team_id" json:"team_id"`
	UserID    UserID    `db:"user_id" json:"user_id"`
	Role      TeamRole  `db:"role" json:"role"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	Email     string    `db:"email" json:"email,omitempty"`
	FullName  string    `db:"full_name" json:"full_name,omitempty"`
}

// UserTeamDetails represents the details of a team a user is part of, including their role.
type UserTeamDetails struct {
	ID          TeamID    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	MemberCount int       `json:"member_count"`
	Role        TeamRole  `json:"role"`
}

// TeamQuery represents a saved query in a team
type TeamQuery struct {
	ID           int            `json:"id" db:"id"`
	TeamID       TeamID         `json:"team_id" db:"team_id"`
	SourceID     SourceID       `json:"source_id" db:"source_id"`
	Name         string         `json:"name" db:"name"`
	Description  string         `json:"description" db:"description"`
	QueryType    SavedQueryType `json:"query_type" db:"query_type"`
	QueryContent string         `json:"query_content" db:"query_content"`
	Timestamps
}
