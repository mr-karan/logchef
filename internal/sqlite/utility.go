package sqlite

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	_ "github.com/mattn/go-sqlite3"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// Define standard error types
var (
	// ErrNotFound is returned when a resource is not found
	ErrNotFound = errors.New("resource not found")

	// ErrUserNotFound is returned when a user is not found
	ErrUserNotFound = fmt.Errorf("%w: user", ErrNotFound)

	// ErrTeamNotFound is returned when a team is not found
	ErrTeamNotFound = fmt.Errorf("%w: team", ErrNotFound)

	// ErrSourceNotFound is returned when a source is not found
	ErrSourceNotFound = fmt.Errorf("%w: source", ErrNotFound)

	// ErrSessionNotFound is returned when a session is not found
	ErrSessionNotFound = fmt.Errorf("%w: session", ErrNotFound)

	// ErrQueryNotFound is returned when a query is not found
	ErrQueryNotFound = fmt.Errorf("%w: query", ErrNotFound)

	// ErrUniqueConstraint is returned when a unique constraint is violated
	ErrUniqueConstraint = errors.New("unique constraint violation")

	// ErrUserExists is returned when a user with the same email already exists
	ErrUserExists = fmt.Errorf("%w: user with this email already exists", ErrUniqueConstraint)

	// ErrTeamExists is returned when a team with the same name already exists
	ErrTeamExists = fmt.Errorf("%w: team with this name already exists", ErrUniqueConstraint)

	// ErrSourceExists is returned when a source with the same database/table already exists
	ErrSourceExists = fmt.Errorf("%w: source with this database/table already exists", ErrUniqueConstraint)
)

// IsNotFoundError checks if an error is any type of not found error
func IsNotFoundError(err error) bool {
	return errors.Is(err, ErrNotFound) ||
		errors.Is(err, sql.ErrNoRows) ||
		errors.Is(err, models.ErrNotFound) ||
		errors.Is(err, models.ErrUserNotFound) ||
		errors.Is(err, models.ErrTeamNotFound)
}

// IsUserNotFoundError checks if an error is specifically a user not found error
func IsUserNotFoundError(err error) bool {
	return errors.Is(err, ErrUserNotFound) ||
		(errors.Is(err, ErrNotFound) && strings.Contains(err.Error(), "user"))
}

// IsTeamNotFoundError checks if an error is specifically a team not found error
func IsTeamNotFoundError(err error) bool {
	return errors.Is(err, ErrTeamNotFound) ||
		(errors.Is(err, ErrNotFound) && strings.Contains(err.Error(), "team"))
}

// IsSourceNotFoundError checks if an error is specifically a source not found error
func IsSourceNotFoundError(err error) bool {
	return errors.Is(err, ErrSourceNotFound) ||
		(errors.Is(err, ErrNotFound) && strings.Contains(err.Error(), "source"))
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

func mapSourceRowToModel(row *sqlc.Source) *models.Source {
	if row == nil {
		return nil
	}

	source := &models.Source{
		ID:                models.SourceID(row.ID),
		Name:              row.Name,
		BackendType:       models.BackendType(row.BackendType),
		MetaIsAutoCreated: row.MetaIsAutoCreated == 1,
		MetaTSField:       row.MetaTsField,
		MetaSeverityField: row.MetaSeverityField.String,
		Description:       row.Description.String,
		TTLDays:           int(row.TtlDays),
		Connection: models.ConnectionInfo{
			Host:      row.Host,
			Username:  row.Username,
			Password:  row.Password,
			Database:  row.Database,
			TableName: row.TableName,
		},
		Timestamps: models.Timestamps{
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		},
	}

	if row.VictorialogsConnection.Valid && row.VictorialogsConnection.String != "" {
		vlConn, err := parseVictoriaLogsConnection(row.VictorialogsConnection.String)
		if err == nil {
			source.VictoriaLogsConnection = vlConn
		}
	}

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

// wrapError wraps an error with additional context
func wrapError(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf(format+": %w", append(args, err)...)
}

// handleNotFoundError checks if the error is sql.ErrNoRows and maps it
// to appropriate application-level not found errors based on the provided resource type.
func handleNotFoundError(err error, prefix string) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		// Map to specific resource error types
		if strings.Contains(prefix, "user") {
			if strings.Contains(prefix, "email") {
				return wrapError(ErrUserNotFound, "getting user email %s", strings.TrimPrefix(prefix, "getting user email "))
			}
			return wrapError(ErrUserNotFound, prefix)
		}
		if strings.Contains(prefix, "team") {
			return wrapError(ErrTeamNotFound, prefix)
		}
		if strings.Contains(prefix, "source") {
			return wrapError(ErrSourceNotFound, prefix)
		}
		if strings.Contains(prefix, "session") {
			return wrapError(ErrSessionNotFound, prefix)
		}
		if strings.Contains(prefix, "query") {
			return wrapError(ErrQueryNotFound, prefix)
		}
		// Generic not found error
		return wrapError(ErrNotFound, prefix)
	}

	// For other errors, just wrap them with the prefix
	return wrapError(err, prefix)
}

// handleUniqueConstraintError maps SQLite unique constraint errors to specific domain errors
func handleUniqueConstraintError(err error, table, column, value string) error {
	if err == nil {
		return nil
	}

	if isUniqueConstraintSQLiteError(err, table, column) {
		switch {
		case table == "users" && column == "email":
			return wrapError(ErrUserExists, "email %s", value)
		case table == "teams" && column == "name":
			return wrapError(ErrTeamExists, "name %s", value)
		case table == "sources" && (column == "name" || column == "database_table"):
			return wrapError(ErrSourceExists, value)
		default:
			return wrapError(ErrUniqueConstraint, "%s.%s: %s", table, column, value)
		}
	}

	return err
}

func parseVictoriaLogsConnection(jsonStr string) (*models.VictoriaLogsConnectionInfo, error) {
	var conn models.VictoriaLogsConnectionInfo
	if err := json.Unmarshal([]byte(jsonStr), &conn); err != nil {
		return nil, err
	}
	return &conn, nil
}

func serializeVictoriaLogsConnection(conn *models.VictoriaLogsConnectionInfo) (sql.NullString, error) {
	if conn == nil {
		return sql.NullString{Valid: false}, nil
	}
	data, err := json.Marshal(conn)
	if err != nil {
		return sql.NullString{}, err
	}
	return sql.NullString{String: string(data), Valid: true}, nil
}
