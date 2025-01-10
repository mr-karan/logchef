package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"

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
		source.TableName,
		source.SchemaType,
		source.DSN,
		source.Description,
	)
	if err != nil {
		return fmt.Errorf("error creating source: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting affected rows: %w", err)
	}

	if rows != 1 {
		return fmt.Errorf("expected to affect 1 row, affected %d", rows)
	}

	return nil
}

// GetSource retrieves a source by its ID
func (d *DB) GetSource(ctx context.Context, id string) (*models.Source, error) {
	var source models.Source
	err := d.queries.GetSource.QueryRowxContext(ctx, id).StructScan(&source)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("error getting source: %w", err)
	}

	return &source, nil
}

// GetSourceByName retrieves a source by its table name
func (d *DB) GetSourceByName(ctx context.Context, tableName string) (*models.Source, error) {
	var source models.Source
	err := d.queries.GetSourceByName.QueryRowxContext(ctx, tableName).StructScan(&source)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("error getting source by table name: %w", err)
	}

	return &source, nil
}

// ListSources returns all sources ordered by creation date
func (d *DB) ListSources(ctx context.Context) ([]*models.Source, error) {
	var sources []*models.Source
	rows, err := d.queries.ListSources.QueryxContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("error listing sources: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var source models.Source
		if err := rows.StructScan(&source); err != nil {
			return nil, fmt.Errorf("error scanning source: %w", err)
		}
		sources = append(sources, &source)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sources: %w", err)
	}

	return sources, nil
}

// UpdateSource updates an existing source
func (d *DB) UpdateSource(ctx context.Context, source *models.Source) error {
	result, err := d.queries.UpdateSource.ExecContext(ctx,
		source.TableName,
		source.SchemaType,
		source.DSN,
		source.Description,
		source.ID,
	)
	if err != nil {
		return fmt.Errorf("error updating source: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("error getting affected rows: %w", err)
	}

	if rows != 1 {
		return fmt.Errorf("expected to affect 1 row, affected %d", rows)
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
		return fmt.Errorf("error getting affected rows: %w", err)
	}

	if rows != 1 {
		return fmt.Errorf("expected to affect 1 row, affected %d", rows)
	}

	return nil
}

// Close closes the database connection and all prepared statements
func (d *DB) Close() error {
	return d.conn.Close()
}
