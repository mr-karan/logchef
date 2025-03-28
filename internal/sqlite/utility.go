package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// checkRowsAffected checks if exactly one row was affected by a database operation
func checkRowsAffected(result sql.Result, operation string) error {
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("%s: expected 1 row to be affected, got %d", operation, rowsAffected)
	}

	return nil
}

// isUniqueConstraintError checks if an error is a SQLite unique constraint violation
func isUniqueConstraintError(err error, table, column string) bool {
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code == sqlite3.ErrConstraint && sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique
	}
	
	// Fallback to string matching for compatibility
	constraintErr := fmt.Sprintf("UNIQUE constraint failed: %s.%s", table, column)
	return err != nil && strings.Contains(err.Error(), constraintErr)
}

// handleNotFoundError checks if an error is sql.ErrNoRows and returns an appropriate error
// to standardize handling of "not found" cases
func handleNotFoundError(err error, message string) error {
	if errors.Is(err, sql.ErrNoRows) {
		// For session-related queries, return a specific error
		if strings.Contains(message, "session") {
			return fmt.Errorf("session not found")
		}
		// For other entities, return nil to indicate "not found"
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", message, err)
	}
	return nil
}
