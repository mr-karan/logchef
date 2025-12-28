package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	"github.com/mr-karan/logchef/pkg/models"
)

func (db *DB) CreateSource(ctx context.Context, source *models.Source) error {
	vlConn, err := serializeVictoriaLogsConnection(source.VictoriaLogsConnection)
	if err != nil {
		db.log.Error("failed to serialize victorialogs connection", "error", err)
		return fmt.Errorf("error serializing victorialogs connection: %w", err)
	}

	backendType := string(source.GetEffectiveBackendType())

	params := sqlc.CreateSourceParams{
		Name:                   source.Name,
		BackendType:            backendType,
		MetaIsAutoCreated:      boolToInt(source.MetaIsAutoCreated),
		MetaTsField:            source.MetaTSField,
		MetaSeverityField:      sql.NullString{String: source.MetaSeverityField, Valid: source.MetaSeverityField != ""},
		Host:                   source.Connection.Host,
		Username:               source.Connection.Username,
		Password:               source.Connection.Password,
		Database:               source.Connection.Database,
		TableName:              source.Connection.TableName,
		Description:            sql.NullString{String: source.Description, Valid: source.Description != ""},
		TtlDays:                int64(source.TTLDays),
		VictorialogsConnection: vlConn,
	}

	id, err := db.queries.CreateSource(ctx, params)
	if err != nil {
		if IsUniqueConstraintError(err) && (strings.Contains(err.Error(), "database") || strings.Contains(err.Error(), "table_name")) {
			return handleUniqueConstraintError(err, "sources", "database_table", fmt.Sprintf("%s.%s", source.Connection.Database, source.Connection.TableName))
		}
		db.log.Error("failed to create source record in db", "error", err)
		return fmt.Errorf("error creating source record: %w", err)
	}

	source.ID = models.SourceID(id)

	sourceRow, err := db.queries.GetSource(ctx, id)
	if err != nil {
		db.log.Error("failed to get newly created source record", "error", err, "assigned_id", id)
		return nil
	}

	newSource := mapSourceRowToModel(&sourceRow)
	if newSource != nil {
		source.CreatedAt = newSource.CreatedAt
		source.UpdatedAt = newSource.UpdatedAt
	}

	return nil
}

// GetSource retrieves a single source by its ID.
// It returns models.ErrNotFound if the source does not exist.
func (db *DB) GetSource(ctx context.Context, id models.SourceID) (*models.Source, error) {

	sourceRow, err := db.queries.GetSource(ctx, int64(id))
	if err != nil {
		// Use handleNotFoundError for consistent not-found error mapping.
		return nil, handleNotFoundError(err, fmt.Sprintf("getting source id %d", id))
	}

	// Map sqlc result to domain model.
	source := mapSourceRowToModel(&sourceRow)
	if source == nil {
		// This case should ideally be covered by handleNotFoundError, but as a safeguard:
		return nil, fmt.Errorf("internal error: source row for id %d mapped to nil", id)
	}
	return source, nil
}

// GetSourceByName retrieves a single source by its database and table name combination.
// It returns models.ErrNotFound if no matching source exists.
func (db *DB) GetSourceByName(ctx context.Context, database, tableName string) (*models.Source, error) {

	sourceRow, err := db.queries.GetSourceByName(ctx, sqlc.GetSourceByNameParams{
		Database:  database,
		TableName: tableName,
	})
	if err != nil {
		// Explicitly map ErrNoRows to models.ErrNotFound for clarity.
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrNotFound // Use the model-defined error
		}
		db.log.Error("failed to get source by name from db", "error", err, "database", database, "table", tableName)
		return nil, fmt.Errorf("error getting source by name: %w", err)
	}

	// Map sqlc result to domain model.
	source := mapSourceRowToModel(&sourceRow)
	if source == nil {
		return nil, fmt.Errorf("internal error: source row for %s.%s mapped to nil", database, tableName)
	}
	return source, nil
}

// ListSources retrieves all source records from the database, ordered by creation date.
func (db *DB) ListSources(ctx context.Context) ([]*models.Source, error) {

	sourceRows, err := db.queries.ListSources(ctx)
	if err != nil {
		db.log.Error("failed to list sources from db", "error", err)
		return nil, fmt.Errorf("error listing sources: %w", err)
	}

	// Map each sqlc row to the domain model.
	sources := make([]*models.Source, 0, len(sourceRows)) // Initialize with 0 length
	for i := range sourceRows {                           // Iterate safely over slice index
		mappedSource := mapSourceRowToModel(&sourceRows[i])
		if mappedSource != nil { // Avoid appending nil if mapping fails
			sources = append(sources, mappedSource)
		}
	}

	return sources, nil
}

func (db *DB) UpdateSource(ctx context.Context, source *models.Source) error {
	vlConn, err := serializeVictoriaLogsConnection(source.VictoriaLogsConnection)
	if err != nil {
		db.log.Error("failed to serialize victorialogs connection", "error", err)
		return fmt.Errorf("error serializing victorialogs connection: %w", err)
	}

	backendType := string(source.GetEffectiveBackendType())

	params := sqlc.UpdateSourceParams{
		Name:                   source.Name,
		BackendType:            backendType,
		MetaIsAutoCreated:      boolToInt(source.MetaIsAutoCreated),
		MetaTsField:            source.MetaTSField,
		MetaSeverityField:      sql.NullString{String: source.MetaSeverityField, Valid: source.MetaSeverityField != ""},
		Host:                   source.Connection.Host,
		Username:               source.Connection.Username,
		Password:               source.Connection.Password,
		Database:               source.Connection.Database,
		TableName:              source.Connection.TableName,
		Description:            sql.NullString{String: source.Description, Valid: source.Description != ""},
		TtlDays:                int64(source.TTLDays),
		VictorialogsConnection: vlConn,
		ID:                     int64(source.ID),
	}

	err = db.queries.UpdateSource(ctx, params)
	if err != nil {
		db.log.Error("failed to update source record in db", "error", err, "source_id", source.ID)
		return fmt.Errorf("error updating source record: %w", err)
	}

	return nil
}

// DeleteSource removes a source record from the database by its ID.
func (db *DB) DeleteSource(ctx context.Context, id models.SourceID) error {

	err := db.queries.DeleteSource(ctx, int64(id))
	if err != nil {
		db.log.Error("failed to delete source record from db", "error", err, "source_id", id)
		// TODO: Check if error indicates "not found"? sqlc exec doesn't usually.
		return fmt.Errorf("error deleting source record: %w", err)
	}

	return nil
}
