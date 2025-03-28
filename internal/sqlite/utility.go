package sqlite

import (
	"database/sql"
	"fmt"
	"strings"

	// Import the SQLite driver
	_ "github.com/mattn/go-sqlite3"
	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

// boolToInt converts a bool to int64 for SQLite storage
func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// mapSourceRowToModel maps a sqlc.Source to a models.Source
func mapSourceRowToModel(row *sqlc.Source) *models.Source {
	return &models.Source{
		ID:                models.SourceID(row.ID),
		Name:              row.Name,
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
}

// isUniqueConstraintError checks if an error is a SQLite unique constraint violation
func isUniqueConstraintError(err error, table, column string) bool {
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

// handleNotFoundError returns a standardized error when a record is not found
func handleNotFoundError(err error, prefix string) error {
	if err == sql.ErrNoRows {
		return fmt.Errorf("%s: not found", prefix)
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
