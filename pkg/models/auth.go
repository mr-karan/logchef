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

// UserAccountType differentiates interactive users from service principals.
type UserAccountType string

const (
	UserAccountTypeHuman   UserAccountType = "human"
	UserAccountTypeService UserAccountType = "service"
)

// UserRole represents the possible user roles
type UserRole string

const (
	// UserRoleAdmin represents a user with administrative privileges
	UserRoleAdmin UserRole = "admin"

	// UserRoleMember represents a regular user with standard permissions
	UserRoleMember UserRole = "member"
)

// TokenScope is a semantic permission attached to API tokens.
type TokenScope string

const (
	TokenScopeAll               TokenScope = "*"
	TokenScopeProfileRead       TokenScope = "profile:read"
	TokenScopeProfileWrite      TokenScope = "profile:write"
	TokenScopeTokensRead        TokenScope = "tokens:read"
	TokenScopeTokensWrite       TokenScope = "tokens:write"
	TokenScopeUsersRead         TokenScope = "users:read"
	TokenScopeUsersWrite        TokenScope = "users:write"
	TokenScopeTeamsRead         TokenScope = "teams:read"
	TokenScopeTeamsWrite        TokenScope = "teams:write"
	TokenScopeSourcesRead       TokenScope = "sources:read"
	TokenScopeSourcesWrite      TokenScope = "sources:write"
	TokenScopeLogsRead          TokenScope = "logs:read"
	TokenScopeSavedQueriesRead  TokenScope = "saved_queries:read"
	TokenScopeSavedQueriesWrite TokenScope = "saved_queries:write"
	TokenScopeCollectionsRead   TokenScope = "collections:read"
	TokenScopeCollectionsWrite  TokenScope = "collections:write"
	TokenScopeAlertsRead        TokenScope = "alerts:read"
	TokenScopeAlertsWrite       TokenScope = "alerts:write"
	TokenScopeDashboardsRead    TokenScope = "dashboards:read"
	TokenScopeDashboardsWrite   TokenScope = "dashboards:write"
	TokenScopeQuerySharesRead   TokenScope = "query_shares:read"
	TokenScopeQuerySharesWrite  TokenScope = "query_shares:write"
	TokenScopeSettingsRead      TokenScope = "settings:read"
	TokenScopeSettingsWrite     TokenScope = "settings:write"
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
	ID           UserID          `json:"id" db:"id"`
	Email        string          `json:"email" db:"email"`
	FullName     string          `json:"full_name" db:"full_name"`
	Role         UserRole        `json:"role" db:"role"`
	Status       UserStatus      `json:"status" db:"status"`
	AccountType  UserAccountType `json:"account_type" db:"account_type"`
	LastLoginAt  *time.Time      `json:"last_login_at,omitempty" db:"last_login_at"`
	LastActiveAt *time.Time      `json:"last_active_at,omitempty" db:"last_active_at"`
	// PasswordHash holds the bcrypt hash for local (email+password) auth.
	// Empty for OIDC-only users. Never serialized.
	PasswordHash string `json:"-" db:"password_hash"`
	Timestamps
	Managed bool `json:"managed" db:"managed"`
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
	Managed bool `db:"managed" json:"managed"`
}

// TeamMember represents a user's membership in a team
type TeamMember struct {
	TeamID      TeamID          `db:"team_id" json:"team_id"`
	UserID      UserID          `db:"user_id" json:"user_id"`
	Role        TeamRole        `db:"role" json:"role"`
	CreatedAt   time.Time       `db:"created_at" json:"created_at"`
	Email       string          `db:"email" json:"email,omitempty"`
	FullName    string          `db:"full_name" json:"full_name,omitempty"`
	AccountType UserAccountType `db:"account_type" json:"account_type,omitempty"`
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

// APIToken represents an API token for authentication
type APIToken struct {
	ID         int          `json:"id" db:"id"`
	UserID     UserID       `json:"user_id" db:"user_id"`
	Name       string       `json:"name" db:"name"`
	TokenHash  string       `json:"-" db:"token_hash"` // Never expose in JSON
	Prefix     string       `json:"prefix" db:"prefix"`
	Scopes     []TokenScope `json:"scopes" db:"scopes"`
	LastUsedAt *time.Time   `json:"last_used_at,omitempty" db:"last_used_at"`
	ExpiresAt  *time.Time   `json:"expires_at,omitempty" db:"expires_at"`
	// Expired is a computed flag (not persisted) so API consumers don't have to
	// re-derive expiry from ExpiresAt against the current clock.
	Expired bool `json:"expired" db:"-"`
	Timestamps
}

// CreateAPITokenRequest represents a request to create a new API token
type CreateAPITokenRequest struct {
	Name      string       `json:"name"`
	ExpiresAt *time.Time   `json:"expires_at,omitempty"`
	Scopes    []TokenScope `json:"scopes,omitempty"`
}

// CreateAPITokenResponse represents the response when creating an API token
type CreateAPITokenResponse struct {
	Token    string    `json:"token"`     // Full token (only shown once)
	APIToken *APIToken `json:"api_token"` // Token metadata
}
