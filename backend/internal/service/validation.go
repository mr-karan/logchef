package service

import (
	"fmt"
	"net"
	"strings"
	"unicode"

	"backend-v2/pkg/models"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateLogQueryRequest validates a log query request
func ValidateLogQueryRequest(req *models.LogQueryRequest) error {
	if req.StartTimestamp <= 0 {
		return &ValidationError{
			Field:   "StartTimestamp",
			Message: "must be greater than 0",
		}
	}

	if req.EndTimestamp <= 0 {
		return &ValidationError{
			Field:   "EndTimestamp",
			Message: "must be greater than 0",
		}
	}

	if req.EndTimestamp < req.StartTimestamp {
		return &ValidationError{
			Field:   "EndTimestamp",
			Message: "must be greater than StartTimestamp",
		}
	}

	if req.Limit <= 0 || req.Limit > 100000 {
		return &ValidationError{
			Field:   "Limit",
			Message: "must be between 1 and 100000	",
		}
	}

	if req.Sort != nil {
		if req.Sort.Field == "" {
			return &ValidationError{
				Field:   "Sort.Field",
				Message: "field is required when sort is specified",
			}
		}

		if req.Sort.Order != models.SortOrderAsc && req.Sort.Order != models.SortOrderDesc {
			return &ValidationError{
				Field:   "Sort.Order",
				Message: "must be either 'ASC' or 'DESC'",
			}
		}
	}

	// Mode is mandatory
	if req.Mode == "" {
		return &ValidationError{
			Field:   "Mode",
			Message: "query mode is required",
		}
	}

	// Validate based on mode
	switch req.Mode {
	case models.QueryModeFilters:
		// Validate conditions
		for i, condition := range req.Conditions {
			if condition.Field == "" {
				return &ValidationError{
					Field:   fmt.Sprintf("Conditions[%d].Field", i),
					Message: "field is required",
				}
			}

			if condition.Operator == "" {
				return &ValidationError{
					Field:   fmt.Sprintf("Conditions[%d].Operator", i),
					Message: "operator is required",
				}
			}

			validOps := map[models.FilterOperator]bool{
				models.FilterOperatorEquals:      true,
				models.FilterOperatorNotEquals:   true,
				models.FilterOperatorContains:    true,
				models.FilterOperatorNotContains: true,
				models.FilterOperatorIContains:   true,
				models.FilterOperatorStartsWith:  true,
				models.FilterOperatorEndsWith:    true,
				models.FilterOperatorIn:          true,
				models.FilterOperatorNotIn:       true,
				models.FilterOperatorIsNull:      true,
				models.FilterOperatorIsNotNull:   true,
			}

			if !validOps[condition.Operator] {
				return &ValidationError{
					Field:   fmt.Sprintf("Conditions[%d].Operator", i),
					Message: "invalid operator",
				}
			}
		}

	case models.QueryModeRawSQL:
		if req.RawSQL == "" {
			return &ValidationError{
				Field:   "RawSQL",
				Message: "raw SQL query is required in raw_sql mode",
			}
		}
		// Basic SQL injection prevention - only allow SELECT queries
		if !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(req.RawSQL)), "SELECT") {
			return &ValidationError{
				Field:   "RawSQL",
				Message: "only SELECT queries are allowed",
			}
		}

	case models.QueryModeLogChefQL:
		if req.LogChefQL == "" {
			return &ValidationError{
				Field:   "LogChefQL",
				Message: "LogChefQL query is required in logchefql mode",
			}
		}

	default:
		return &ValidationError{
			Field:   "Mode",
			Message: "invalid query mode",
		}
	}

	return nil
}

// ValidateCreateSourceRequest validates a create source request
func ValidateCreateSourceRequest(schemaType string, conn models.ConnectionInfo, description string, ttlDays int) error {
	if conn.TableName == "" {
		return &ValidationError{
			Field:   "TableName",
			Message: "table name is required",
		}
	}

	if !isValidTableName(conn.TableName) {
		return &ValidationError{
			Field:   "TableName",
			Message: "table name must start with a letter and contain only letters, numbers, and underscores",
		}
	}

	if schemaType == "" {
		return &ValidationError{
			Field:   "SchemaType",
			Message: "schema type is required",
		}
	}

	if schemaType != models.SchemaTypeManaged && schemaType != models.SchemaTypeUnmanaged {
		return &ValidationError{
			Field:   "SchemaType",
			Message: fmt.Sprintf("schema type must be either '%s' or '%s'", models.SchemaTypeManaged, models.SchemaTypeUnmanaged),
		}
	}

	// Ensure the host includes a port
	if _, _, err := net.SplitHostPort(conn.Host); err != nil {
		return &ValidationError{
			Field:   "Host",
			Message: "host must include a port (e.g., 'localhost:9000')",
		}
	}

	// Username and Password are optional for Clickhouse, but if username is provided
	// then password must also be provided
	if conn.Username != "" && conn.Password == "" {
		return &ValidationError{
			Field:   "Password",
			Message: "password is required when username is provided",
		}
	}

	if conn.Database == "" {
		return &ValidationError{
			Field:   "Database",
			Message: "database is required",
		}
	}

	if len(description) > 50 {
		return &ValidationError{
			Field:   "Description",
			Message: "description must not exceed 50 characters",
		}
	}

	if ttlDays < -1 {
		return &ValidationError{
			Field:   "TTLDays",
			Message: "TTL days must be -1 (no TTL) or a non-negative number",
		}
	}

	return nil
}

// isValidTableName checks if the name is valid for use as a table name
func isValidTableName(name string) bool {
	// Only allow alphanumeric and underscore, must start with a letter
	if len(name) == 0 || !isLetter(rune(name[0])) {
		return false
	}
	for _, r := range name {
		if !isAlphanumericOrUnderscore(r) {
			return false
		}
	}
	return true
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isAlphanumericOrUnderscore(r rune) bool {
	return isLetter(r) || (r >= '0' && r <= '9') || r == '_'
}

// ValidateCreateUserRequest validates a create user request
func ValidateCreateUserRequest(req *models.CreateUserRequest) error {
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

// ValidateUpdateUserRequest validates an update user request
func ValidateUpdateUserRequest(req *models.UpdateUserRequest) error {
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

// ValidateCreateTeamRequest validates a create team request
func ValidateCreateTeamRequest(req *models.Team) error {
	// Name validation
	if req.Name == "" {
		return &ValidationError{
			Field:   "Name",
			Message: "team name is required",
		}
	}
	if len(req.Name) < 2 || len(req.Name) > 50 {
		return &ValidationError{
			Field:   "Name",
			Message: "team name must be between 2 and 50 characters",
		}
	}
	if !isValidTeamName(req.Name) {
		return &ValidationError{
			Field:   "Name",
			Message: "team name contains invalid characters",
		}
	}

	// Description validation
	if len(req.Description) > 500 {
		return &ValidationError{
			Field:   "Description",
			Message: "description must not exceed 500 characters",
		}
	}

	return nil
}

// ValidateTeamMemberRequest validates a team member request
func ValidateTeamMemberRequest(teamID, userID, role string) error {
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

	if role != "admin" && role != "member" {
		return &ValidationError{
			Field:   "Role",
			Message: "role must be either 'admin' or 'member'",
		}
	}

	return nil
}

// Helper functions

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

func isValidFullName(name string) bool {
	// Allow letters, spaces, hyphens, and apostrophes
	for _, r := range name {
		if !unicode.IsLetter(r) && r != ' ' && r != '-' && r != '\'' {
			return false
		}
	}
	return true
}

func isValidTeamName(name string) bool {
	// Allow letters, numbers, spaces, hyphens, and underscores
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ' && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
