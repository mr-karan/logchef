package models

import "time"

// User represents a user in the system
type User struct {
	ID           string     `db:"id" json:"id"`
	Email        string     `db:"email" json:"email"`
	FullName     string     `db:"full_name" json:"full_name"`
	Role         string     `db:"role" json:"role"`
	LastLoginAt  *time.Time `db:"last_login_at" json:"last_login_at,omitempty"`
	LastActiveAt *time.Time `db:"last_active_at" json:"last_active_at,omitempty"`
	CreatedAt    time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at" json:"updated_at"`
}

// Session represents a user session
type Session struct {
	ID        string    `db:"id" json:"id"`
	UserID    string    `db:"user_id" json:"user_id"`
	ExpiresAt time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// Team represents a team in the system
type Team struct {
	ID          string    `db:"id" json:"id"`
	Name        string    `db:"name" json:"name"`
	Description string    `db:"description" json:"description"`
	CreatedBy   string    `db:"created_by" json:"created_by"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

// TeamMember represents a user's membership in a team
type TeamMember struct {
	TeamID    string    `db:"team_id" json:"team_id"`
	UserID    string    `db:"user_id" json:"user_id"`
	Role      string    `db:"role" json:"role"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// Space represents a workspace in the system
type Space struct {
	ID          string    `db:"id" json:"id"`
	Name        string    `db:"name" json:"name"`
	Description string    `db:"description" json:"description"`
	CreatedBy   string    `db:"created_by" json:"created_by"`
	CreatedAt   time.Time `db:"created_at" json:"created_at"`
	UpdatedAt   time.Time `db:"updated_at" json:"updated_at"`
}

// SpaceDataSource represents a data source linked to a space
type SpaceDataSource struct {
	SpaceID      string    `db:"space_id" json:"space_id"`
	DataSourceID string    `db:"data_source_id" json:"data_source_id"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

// SpaceTeamAccess represents a team's access level to a space
type SpaceTeamAccess struct {
	SpaceID    string    `db:"space_id" json:"space_id"`
	TeamID     string    `db:"team_id" json:"team_id"`
	Permission string    `db:"permission" json:"permission"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time `db:"updated_at" json:"updated_at"`
}

// Query represents a saved query in a space
type Query struct {
	ID           string    `db:"id" json:"id"`
	SpaceID      string    `db:"space_id" json:"space_id"`
	Name         string    `db:"name" json:"name"`
	Description  string    `db:"description" json:"description"`
	QueryContent string    `db:"query_content" json:"query_content"`
	CreatedBy    string    `db:"created_by" json:"created_by"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time `db:"updated_at" json:"updated_at"`
}
