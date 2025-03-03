package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// Source methods

// CreateSource creates a new source in the database
func (db *DB) CreateSource(ctx context.Context, source *models.Source) error {
	db.log.Debug("creating source", "database", source.Connection.Database, "table", source.Connection.TableName)

	var id int64
	err := db.queries.CreateSource.QueryRowContext(ctx,
		source.MetaIsAutoCreated,
		source.MetaTSField,
		source.MetaSeverityField,
		source.Connection.Host,
		source.Connection.Username,
		source.Connection.Password,
		source.Connection.Database,
		source.Connection.TableName,
		source.Description,
		source.TTLDays,
	).Scan(&id)

	if err != nil {
		if isUniqueConstraintError(err, "sources", "database") {
			return fmt.Errorf("source with database %s and table %s already exists", source.Connection.Database, source.Connection.TableName)
		}
		db.log.Error("failed to create source", "error", err)
		return fmt.Errorf("error creating source: %w", err)
	}

	// Set the auto-generated ID
	source.ID = models.SourceID(id)

	db.log.Debug("source created", "source_id", source.ID)
	return nil
}

// GetSource retrieves a source by ID
func (db *DB) GetSource(ctx context.Context, id models.SourceID) (*models.Source, error) {
	db.log.Debug("getting source", "source_id", id)

	var row SourceRow
	err := db.queries.GetSource.QueryRowxContext(ctx, id).StructScan(&row)
	if err != nil {
		return nil, handleNotFoundError(err, "error getting source")
	}

	source := mapSourceRowToModel(&row)
	return source, nil
}

// GetSourceByName retrieves a source by its table name and database
func (db *DB) GetSourceByName(ctx context.Context, database, tableName string) (*models.Source, error) {
	db.log.Debug("getting source by name", "database", database, "table", tableName)

	var row SourceRow
	err := db.queries.GetSourceByName.QueryRowxContext(ctx, database, tableName).StructScan(&row)
	if err != nil {
		return nil, handleNotFoundError(err, "error getting source by name")
	}

	source := mapSourceRowToModel(&row)
	return source, nil
}

// ListSources returns all sources ordered by creation date
func (db *DB) ListSources(ctx context.Context) ([]*models.Source, error) {
	db.log.Debug("listing sources")

	rows, err := db.queries.ListSources.QueryxContext(ctx)
	if err != nil {
		db.log.Error("failed to list sources", "error", err)
		return nil, fmt.Errorf("error listing sources: %w", err)
	}
	defer rows.Close()

	var sources []*models.Source
	for rows.Next() {
		var row SourceRow
		if err := rows.StructScan(&row); err != nil {
			db.log.Error("failed to scan source row", "error", err)
			return nil, fmt.Errorf("error scanning row: %w", err)
		}

		sources = append(sources, mapSourceRowToModel(&row))
	}

	if err := rows.Err(); err != nil {
		db.log.Error("error iterating source rows", "error", err)
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	db.log.Debug("sources listed", "count", len(sources))
	return sources, nil
}

// UpdateSource updates an existing source
func (db *DB) UpdateSource(ctx context.Context, source *models.Source) error {
	db.log.Debug("updating source", "source_id", source.ID, "database", source.Connection.Database, "table", source.Connection.TableName)

	result, err := db.queries.UpdateSource.ExecContext(ctx,
		source.MetaIsAutoCreated,
		source.MetaTSField,
		source.MetaSeverityField,
		source.Connection.Host,
		source.Connection.Username,
		source.Connection.Password,
		source.Connection.Database,
		source.Connection.TableName,
		source.Description,
		source.TTLDays,
		source.ID,
	)
	if err != nil {
		db.log.Error("failed to update source", "error", err, "source_id", source.ID)
		return fmt.Errorf("error updating source: %w", err)
	}

	if err := checkRowsAffected(result, "update source"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "source_id", source.ID)
		return err
	}

	return nil
}

// DeleteSource deletes a source by ID
func (db *DB) DeleteSource(ctx context.Context, id models.SourceID) error {
	db.log.Debug("deleting source", "source_id", id)

	result, err := db.queries.DeleteSource.ExecContext(ctx, id)
	if err != nil {
		db.log.Error("failed to delete source", "error", err, "source_id", id)
		return fmt.Errorf("error deleting source: %w", err)
	}

	if err := checkRowsAffected(result, "delete source"); err != nil {
		db.log.Error("unexpected rows affected", "error", err, "source_id", id)
		return err
	}

	return nil
}

// SourceRow represents a row in the sources table
type SourceRow struct {
	ID                int       `db:"id"`
	MetaIsAutoCreated int       `db:"_meta_is_auto_created"`
	MetaTSField       string    `db:"_meta_ts_field"`
	MetaSeverityField string    `db:"_meta_severity_field"`
	Host              string    `db:"host"`
	Username          string    `db:"username"`
	Password          string    `db:"password"`
	Database          string    `db:"database"`
	TableName         string    `db:"table_name"`
	Description       string    `db:"description"`
	TTLDays           int       `db:"ttl_days"`
	CreatedAt         time.Time `db:"created_at"`
	UpdatedAt         time.Time `db:"updated_at"`
}

// mapSourceRowToModel maps a SourceRow to a models.Source
func mapSourceRowToModel(row *SourceRow) *models.Source {
	return &models.Source{
		ID:                models.SourceID(row.ID),
		MetaIsAutoCreated: row.MetaIsAutoCreated == 1,
		MetaTSField:       row.MetaTSField,
		MetaSeverityField: row.MetaSeverityField,
		Description:       row.Description,
		TTLDays:           row.TTLDays,
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
