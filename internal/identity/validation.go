package identity

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/mr-karan/logchef/pkg/models"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Validator provides validation logic for users and teams
type Validator struct{}

// NewValidator creates a new validator
func NewValidator() *Validator {
	return &Validator{}
}

// ValidateCreateUser validates a create user request
func (v *Validator) ValidateCreateUser(req *models.CreateUserRequest) error {
	// Email validation
	if req.Email == "" {
		return &ValidationError{
			Field:   "Email",
			Message: "email is required",
		}
	}
	if !isValidEmail(req.Email) {
		return &ValidationError{
			Field:   "Email",
			Message: "invalid email format",
		}
	}

	// Full Name validation
	if req.FullName == "" {
		return &ValidationError{
			Field:   "FullName",
			Message: "full name is required",
		}
	}
	if len(req.FullName) < 2 || len(req.FullName) > 100 {
		return &ValidationError{
			Field:   "FullName",
			Message: "full name must be between 2 and 100 characters",
		}
	}
	if !isValidFullName(req.FullName) {
		return &ValidationError{
			Field:   "FullName",
			Message: "full name contains invalid characters",
		}
	}

	// Role validation
	if req.Role == "" {
		return &ValidationError{
			Field:   "Role",
			Message: "role is required",
		}
	}
	if req.Role != models.UserRoleAdmin && req.Role != models.UserRoleMember {
		return &ValidationError{
			Field:   "Role",
			Message: "role must be either 'admin' or 'member'",
		}
	}

	return nil
}

// ValidateUpdateUser validates an update user request
func (v *Validator) ValidateUpdateUser(req *models.UpdateUserRequest) error {
	// Full Name validation (if provided)
	if req.FullName != "" {
		if len(req.FullName) < 2 || len(req.FullName) > 100 {
			return &ValidationError{
				Field:   "FullName",
				Message: "full name must be between 2 and 100 characters",
			}
		}
		if !isValidFullName(req.FullName) {
			return &ValidationError{
				Field:   "FullName",
				Message: "full name contains invalid characters",
			}
		}
	}

	// Role validation (if provided)
	if req.Role != "" {
		if req.Role != models.UserRoleAdmin && req.Role != models.UserRoleMember {
			return &ValidationError{
				Field:   "Role",
				Message: "role must be either 'admin' or 'member'",
			}
		}
	}

	// Status validation (if provided)
	if req.Status != "" {
		if req.Status != models.UserStatusActive && req.Status != models.UserStatusInactive {
			return &ValidationError{
				Field:   "Status",
				Message: "status must be either 'active' or 'inactive'",
			}
		}
	}

	return nil
}

// ValidateCreateTeam validates a create team request
func (v *Validator) ValidateCreateTeam(team *models.Team) error {
	// Name validation
	if team.Name == "" {
		return &ValidationError{
			Field:   "Name",
			Message: "team name is required",
		}
	}
	if len(team.Name) < 2 || len(team.Name) > 50 {
		return &ValidationError{
			Field:   "Name",
			Message: "team name must be between 2 and 50 characters",
		}
	}
	if !isValidTeamName(team.Name) {
		return &ValidationError{
			Field:   "Name",
			Message: "team name contains invalid characters",
		}
	}

	// Description validation
	if len(team.Description) > 500 {
		return &ValidationError{
			Field:   "Description",
			Message: "description must not exceed 500 characters",
		}
	}

	return nil
}

// ValidateTeamMember validates a team member request
func (v *Validator) ValidateTeamMember(teamID models.TeamID, userID models.UserID, role models.TeamRole) error {
	if teamID == "" {
		return &ValidationError{
			Field:   "TeamID",
			Message: "team ID is required",
		}
	}

	if userID == "" {
		return &ValidationError{
			Field:   "UserID",
			Message: "user ID is required",
		}
	}

	if role != models.TeamRoleAdmin && role != models.TeamRoleMember {
		return &ValidationError{
			Field:   "Role",
			Message: "role must be either 'admin' or 'member'",
		}
	}

	return nil
}

// Helper functions

// isValidEmail checks if the email format is valid
func isValidEmail(email string) bool {
	// Basic email validation
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false
	}
	if len(parts[0]) == 0 || len(parts[1]) == 0 {
		return false
	}
	if !strings.Contains(parts[1], ".") {
		return false
	}
	return true
}

// isValidFullName checks if the name contains only valid characters
func isValidFullName(name string) bool {
	// Allow letters, spaces, hyphens, and apostrophes
	for _, r := range name {
		if !unicode.IsLetter(r) && r != ' ' && r != '-' && r != '\'' {
			return false
		}
	}
	return true
}

// isValidTeamName checks if the team name contains only valid characters
func isValidTeamName(name string) bool {
	// Allow letters, numbers, spaces, hyphens, and underscores
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ' && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
