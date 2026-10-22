package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

var (
	// ErrUniqueConstraint is returned when a unique constraint is violated
	ErrUniqueConstraint = models.ErrConflict

	// ErrUserExists is returned when a user with the same email already exists
	ErrUserExists = fmt.Errorf("%w: user with this email already exists", ErrUniqueConstraint)

	// ErrTeamExists is returned when a team with the same name already exists
	ErrTeamExists = fmt.Errorf("%w: team with this name already exists", ErrUniqueConstraint)

	// ErrSourceExists is returned when a source with the same database/table already exists
	ErrSourceExists = fmt.Errorf("%w: source with this identity already exists", ErrUniqueConstraint)
)

// translateNotFound gives callers a backend-neutral missing-resource error.
func translateNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return models.ErrNotFound
	}
	return err
}

// IsUniqueConstraintError checks if an error is a unique constraint violation
func IsUniqueConstraintError(err error) bool {
	return errors.Is(err, ErrUniqueConstraint) || isUniqueConstraintSQLiteError(err, "", "")
}

// boolToInt converts a bool to int64 for SQLite storage
func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// nullString wraps a string into sql.NullString, treating empty as NULL.
func nullString(value string) sql.NullString {
	return sql.NullString{String: value, Valid: value != ""}
}

// mapSourceRowToModel maps a sqlc.Source to a models.Source
func mapSourceRowToModel(row *sqlc.Source) *models.Source {
	if row == nil {
		return nil
	}
	source := &models.Source{
		ID:                models.SourceID(row.ID),
		Name:              row.Name,
		MetaIsAutoCreated: row.MetaIsAutoCreated == 1,
		SourceType:        models.SourceType(row.SourceType),
		MetaTSField:       row.MetaTsField,
		MetaSeverityField: row.MetaSeverityField.String,
		Description:       row.Description.String,
		TTLDays:           int(row.TtlDays),
		ConnectionConfig:  []byte(row.ConnectionConfig),
		IdentityKey:       row.IdentityKey,
		Timestamps: models.Timestamps{
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
		Managed:   row.Managed == 1,
		SecretRef: row.SecretRef.String,
	}

	_ = source.HydrateConnection()

	return source
}

// Note: IsConnected and Schema/Columns are populated dynamically, not from DB row.

// isUniqueConstraintSQLiteError checks if an error is likely a SQLite UNIQUE constraint violation.
// It performs a simple string check on the error message.
func isUniqueConstraintSQLiteError(err error, table, column string) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// Simple string checks for SQLite constraint errors
	if strings.Contains(errMsg, "UNIQUE constraint failed") {
		if table != "" && column != "" {
			constraint := fmt.Sprintf("%s.%s", table, column)
			return strings.Contains(errMsg, constraint)
		}
		return true
	}

	return false
}

func handleNotFoundError(err error, prefix string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", prefix, translateNotFound(err))
}

// handleUniqueConstraintError maps SQLite unique constraint errors to specific domain errors
func handleUniqueConstraintError(err error, table, column, value string) error {
	if err == nil {
		return nil
	}

	if isUniqueConstraintSQLiteError(err, table, column) {
		switch {
		case table == "users" && column == "email":
			return fmt.Errorf("email %s: %w", value, ErrUserExists)
		case table == "teams" && column == "name":
			return fmt.Errorf("name %s: %w", value, ErrTeamExists)
		case table == "sources" && (column == "name" || column == "identity_key"):
			return fmt.Errorf("%s: %w", value, ErrSourceExists)
		default:
			return fmt.Errorf("%s.%s: %s: %w", table, column, value, ErrUniqueConstraint)
		}
	}

	return err
}
