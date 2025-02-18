package models

import "time"

// User represents a user in the system
type User struct {
	ID           string     `json:"id" db:"id"`
	Email        string     `json:"email" db:"email"`
	FullName     string     `json:"full_name" db:"full_name"`
	Role         string     `json:"role" db:"role"`
	Status       string     `json:"status" db:"status"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
	LastActiveAt *time.Time `json:"last_active_at,omitempty" db:"last_active_at"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at"`
}

// UserStatus represents the possible user statuses
const (
	UserStatusActive   = "active"
	UserStatusInactive = "inactive"
)

// UserRole represents the possible user roles
const (
	UserRoleAdmin  = "admin"
	UserRoleMember = "member"
)

// CreateUserRequest represents a request to create a new user
type CreateUserRequest struct {
	Email    string `json:"email" validate:"required,email"`
	FullName string `json:"full_name" validate:"required"`
	Role     string `json:"role" validate:"required,oneof=admin member"`
}

// UpdateUserRequest represents a request to update a user
type UpdateUserRequest struct {
	FullName string `json:"full_name,omitempty" validate:"omitempty"`
	Role     string `json:"role,omitempty" validate:"omitempty,oneof=admin member"`
	Status   string `json:"status,omitempty" validate:"omitempty,oneof=active inactive"`
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

// TeamQuery represents a saved query in a team
type TeamQuery struct {
	ID           string    `json:"id" db:"id"`
	TeamID       string    `json:"team_id" db:"team_id"`
	Name         string    `json:"name" db:"name"`
	Description  string    `json:"description" db:"description"`
	QueryContent string    `json:"query_content" db:"query_content"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" db:"updated_at"`
}
