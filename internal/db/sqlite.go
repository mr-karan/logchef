package db

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

type SQLite struct {
	db *sql.DB
}

func (s *SQLite) DB() *sql.DB {
	return s.db
}

func NewSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	// Initialize schema
	if err := initSchema(db); err != nil {
		return nil, err
	}

	return &SQLite{db: db}, nil
}

func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS sources (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		table_name TEXT NOT NULL,
		schema_type TEXT NOT NULL,
		dsn TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);`

	_, err := db.Exec(schema)
	return err
}

func (s *SQLite) Close() error {
	return s.db.Close()
}
