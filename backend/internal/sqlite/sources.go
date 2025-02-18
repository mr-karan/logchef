package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"backend-v2/pkg/models"
)

// Source methods

// CreateSource creates a new source in the database
func (d *DB) CreateSource(ctx context.Context, source *models.Source) error {
	result, err := d.queries.CreateSource.ExecContext(ctx,
		source.ID,
		source.SchemaType,
		source.Connection.Host,
		source.Connection.Username,
		source.Connection.Password,
		source.Connection.Database,
		source.Connection.TableName,
		source.Description,
		source.TTLDays,
	)
	if err != nil {
		if isUniqueConstraintError(err, "sources", "database") {
			return fmt.Errorf("source with database %s and table %s already exists", source.Connection.Database, source.Connection.TableName)
		}
		return fmt.Errorf("error creating source: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// GetSource retrieves a source by its ID
func (d *DB) GetSource(ctx context.Context, id string) (*models.Source, error) {
	var row SourceRow
	err := d.queries.GetSource.GetContext(ctx, &row, id)
	if err != nil {
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	source := &models.Source{
		ID:          row.ID,
		SchemaType:  row.SchemaType,
		Description: row.Description,
		TTLDays:     row.TTLDays,
		Connection: models.ConnectionInfo{
			Host:      row.Host,
			Username:  row.Username,
			Password:  row.Password,
			Database:  row.Database,
			TableName: row.TableName,
		},
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}

	return source, nil
}

// GetSourceByName retrieves a source by its table name and database
func (d *DB) GetSourceByName(ctx context.Context, database, tableName string) (*models.Source, error) {
	var row SourceRow
	err := d.queries.GetSourceByName.GetContext(ctx, &row, database, tableName)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Return nil, nil when no source is found
		}
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	source := &models.Source{
		ID:          row.ID,
		SchemaType:  row.SchemaType,
		Description: row.Description,
		TTLDays:     row.TTLDays,
		Connection: models.ConnectionInfo{
			Host:      row.Host,
			Username:  row.Username,
			Password:  row.Password,
			Database:  row.Database,
			TableName: row.TableName,
		},
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}

	return source, nil
}

// ListSources returns all sources ordered by creation date
func (d *DB) ListSources(ctx context.Context) ([]*models.Source, error) {
	var rows []SourceRow
	err := d.queries.ListSources.SelectContext(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}

	sources := make([]*models.Source, len(rows))
	for i, row := range rows {
		sources[i] = &models.Source{
			ID:          row.ID,
			SchemaType:  row.SchemaType,
			Description: row.Description,
			TTLDays:     row.TTLDays,
			Connection: models.ConnectionInfo{
				Host:      row.Host,
				Username:  row.Username,
				Password:  row.Password,
				Database:  row.Database,
				TableName: row.TableName,
			},
			CreatedAt: row.CreatedAt,
			UpdatedAt: row.UpdatedAt,
		}
	}

	return sources, nil
}

// UpdateSource updates an existing source
func (d *DB) UpdateSource(ctx context.Context, source *models.Source) error {
	result, err := d.queries.UpdateSource.ExecContext(ctx,
		source.SchemaType,
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
		return fmt.Errorf("error updating source: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// DeleteSource deletes a source by its ID
func (d *DB) DeleteSource(ctx context.Context, id string) error {
	result, err := d.queries.DeleteSource.ExecContext(ctx, id)
	if err != nil {
		return fmt.Errorf("error deleting source: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}

	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rowsAffected)
	}

	return nil
}

// SourceRow represents a row in the sources table
type SourceRow struct {
	ID          string    `db:"id"`
	SchemaType  string    `db:"schema_type"`
	Host        string    `db:"host"`
	Username    string    `db:"username"`
	Password    string    `db:"password"`
	Database    string    `db:"database"`
	TableName   string    `db:"table_name"`
	Description string    `db:"description"`
	TTLDays     int       `db:"ttl_days"`
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}
