package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"time"

	"backend-v2/pkg/models"

	"github.com/jmoiron/sqlx"
	"github.com/knadh/goyesql/v2"
	goyesqlx "github.com/knadh/goyesql/v2/sqlx"
	_ "modernc.org/sqlite"
)

//go:embed queries.sql
var queriesSQL string

// DB represents our SQLite database manager
type DB struct {
	conn    *sqlx.DB
	queries *Queries
}

// Queries contains all prepared SQL statements
type Queries struct {
	SetPragmas         *sqlx.Stmt `query:"SetPragmas"`
	CreateSourcesTable *sqlx.Stmt `query:"CreateSourcesTable"`
	CreateSource       *sqlx.Stmt `query:"CreateSource"`
	GetSource          *sqlx.Stmt `query:"GetSource"`
	GetSourceByName    *sqlx.Stmt `query:"GetSourceByName"`
	ListSources        *sqlx.Stmt `query:"ListSources"`
	UpdateSource       *sqlx.Stmt `query:"UpdateSource"`
	DeleteSource       *sqlx.Stmt `query:"DeleteSource"`
}

type Options struct {
	Path string
}

// New creates a new SQLite database connection and initializes the schema
func New(opts Options) (*DB, error) {
	// Connect to SQLite
	db, err := sqlx.Connect("sqlite", opts.Path)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	// Parse our queries
	qMap, err := goyesql.ParseBytes([]byte(queriesSQL))
	if err != nil {
		return nil, fmt.Errorf("error parsing SQL queries: %w", err)
	}

	// Prepare statements
	var q Queries
	if err := goyesqlx.ScanToStruct(&q, qMap, db.Unsafe()); err != nil {
		return nil, fmt.Errorf("error preparing SQL queries: %w", err)
	}

	sqlite := &DB{
		conn:    db,
		queries: &q,
	}

	// Initialize the database
	if err := sqlite.initialize(); err != nil {
		return nil, err
	}

	return sqlite, nil
}

// initialize sets up the database with proper pragmas and schema
func (d *DB) initialize() error {
	// Set pragmas
	if _, err := d.queries.SetPragmas.Exec(); err != nil {
		return fmt.Errorf("error setting pragmas: %w", err)
	}

	// Create tables
	if _, err := d.queries.CreateSourcesTable.Exec(); err != nil {
		return fmt.Errorf("error creating tables: %w", err)
	}

	return nil
}

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
		return fmt.Errorf("error creating source: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rows)
	}

	return nil
}

// GetSource retrieves a source by its ID
func (d *DB) GetSource(ctx context.Context, id string) (*models.Source, error) {
	var row SourceRow
	err := d.queries.GetSource.QueryRowxContext(ctx, id).StructScan(&row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	return &models.Source{
		ID:         row.ID,
		SchemaType: row.SchemaType,
		Connection: models.ConnectionInfo{
			Host:      row.Host,
			Database:  row.Database,
			TableName: row.TableName,
		},
		Description: row.Description,
		TTLDays:     row.TTLDays,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
}

// GetSourceByName retrieves a source by its table name and database
func (d *DB) GetSourceByName(ctx context.Context, database, tableName string) (*models.Source, error) {
	var row SourceRow
	err := d.queries.GetSourceByName.QueryRowxContext(ctx, database, tableName).StructScan(&row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	return &models.Source{
		ID:         row.ID,
		SchemaType: row.SchemaType,
		Connection: models.ConnectionInfo{
			Host:      row.Host,
			Database:  row.Database,
			TableName: row.TableName,
		},
		Description: row.Description,
		TTLDays:     row.TTLDays,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}, nil
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

// ListSources returns all sources ordered by creation date
func (d *DB) ListSources(ctx context.Context) ([]*models.Source, error) {
	var sourceRows []*SourceRow
	rows, err := d.queries.ListSources.QueryxContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var row SourceRow
		if err := rows.StructScan(&row); err != nil {
			return nil, fmt.Errorf("error scanning source: %w", err)
		}
		sourceRows = append(sourceRows, &row)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sources: %w", err)
	}

	// Convert to models.Source
	sources := make([]*models.Source, len(sourceRows))
	for i, row := range sourceRows {
		sources[i] = &models.Source{
			ID:         row.ID,
			SchemaType: row.SchemaType,
			Connection: models.ConnectionInfo{
				Host:      row.Host,
				Database:  row.Database,
				TableName: row.TableName,
			},
			Description: row.Description,
			TTLDays:     row.TTLDays,
			CreatedAt:   row.CreatedAt,
			UpdatedAt:   row.UpdatedAt,
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

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rows)
	}

	return nil
}

// DeleteSource deletes a source by its ID
func (d *DB) DeleteSource(ctx context.Context, id string) error {
	result, err := d.queries.DeleteSource.ExecContext(ctx, id)
	if err != nil {
		return fmt.Errorf("error deleting source: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting rows affected: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("expected 1 row to be affected, got %d", rows)
	}

	return nil
}

// Close closes the database connection and all prepared statements
func (d *DB) Close() error {
	return d.conn.Close()
}
