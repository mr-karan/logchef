package logs

import (
	"context"
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/pkg/models"
)

type QueryMode string

const (
	QueryModeBasic     QueryMode = "basic"
	QueryModeLogchefQL QueryMode = "logchefql"
	QueryModeSQL       QueryMode = "sql"
)

// QueryRequest represents the unified request structure for querying logs
type QueryRequest struct {
	Mode    QueryMode             `json:"mode"`
	Query   string                `json:"query,omitempty"`
	Preview bool                  `json:"preview,omitempty"`
	Params  models.LogQueryParams `json:"params,omitempty"`
}

// QueryError represents an error that occurred during query execution
type QueryError struct {
	Code     string    `json:"code"`
	Message  string    `json:"message"`
	Mode     QueryMode `json:"mode"`
	Details  string    `json:"details,omitempty"`
	Position int       `json:"position,omitempty"`
}

func (e *QueryError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

var (
	ErrInvalidMode  = &QueryError{Code: "INVALID_MODE", Message: "Invalid query mode specified"}
	ErrSyntaxError  = &QueryError{Code: "SYNTAX_ERROR", Message: "Invalid query syntax"}
	ErrForbiddenSQL = &QueryError{Code: "FORBIDDEN_SQL", Message: "Forbidden SQL operation"}
)

// QueryValidator validates queries for a specific mode
type QueryValidator interface {
	Validate(ctx context.Context, req *QueryRequest) error
}

// SQLValidator validates SQL queries
type SQLValidator struct {
	forbiddenPatterns []string
	allowedTables     map[string]bool
}

func NewSQLValidator() *SQLValidator {
	return &SQLValidator{
		forbiddenPatterns: []string{
			"INSERT", "UPDATE", "DELETE", "DROP", "CREATE", "ALTER", "TRUNCATE",
			"RENAME", "REPLACE", "MERGE", "COPY", "GRANT", "REVOKE",
		},
		allowedTables: make(map[string]bool),
	}
}

func (v *SQLValidator) Validate(ctx context.Context, req *QueryRequest) error {
	query := strings.ToUpper(req.Query)
	for _, pattern := range v.forbiddenPatterns {
		if strings.Contains(query, pattern) {
			return ErrForbiddenSQL
		}
	}
	return nil
}
