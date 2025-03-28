package sqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/sqlite/sqlc"
	// Ensure sqlc generated code is imported correctly

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB represents our SQLite database manager
type DB struct {
	db      *sql.DB
	queries sqlc.Querier
	log     *slog.Logger
}

type Options struct {
	Logger *slog.Logger
	Config config.SQLiteConfig
}

// New creates a new SQLite database connection and initializes the schema
func New(opts Options) (*DB, error) {
	// Initialize logger with component tag
	log := opts.Logger.With("component", "sqlite")

	// Connect to SQLite with more conservative parameters
	// Add _txlock=immediate to reduce chance of "database is locked" errors
	// Keep _multi_stmt=1 to maintain compatibility with existing code
	dsn := opts.Config.Path + "?_multi_stmt=1&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Error("failed to open database",
			"error", err,
			"path", opts.Config.Path,
		)
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	// Set up cleanup in case of initialization error
	var success bool
	defer func() {
		if !success {
			db.Close()
		}
	}()

	// Configure connection pool with conservative settings to prevent memory issues
	db.SetMaxOpenConns(5) // Limit connections to reduce memory/resource issues
	db.SetMaxIdleConns(2) // Keep fewer idle connections

	// Set shorter connection lifetimes to prevent stale connections
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Set pragmas for optimal performance
	if err := setPragmas(db); err != nil {
		log.Error("failed to set pragmas",
			"error", err,
		)
		return nil, fmt.Errorf("error setting pragmas: %w", err)
	}

	// Create a separate connection for migrations to avoid interfering with the main connection
	migrationDB, err := sql.Open("sqlite", opts.Config.Path)
	if err != nil {
		log.Error("failed to open migration database",
			"error", err,
			"path", opts.Config.Path,
		)
		return nil, fmt.Errorf("error opening migration database: %w", err)
	}
	defer migrationDB.Close()

	// Run migrations
	if err := runMigrations(migrationDB); err != nil {
		log.Error("failed to run migrations",
			"error", err,
			"path", opts.Config.Path,
		)
		return nil, fmt.Errorf("error running migrations: %w", err)
	}

	// Initialize sqlc queries
	queries := sqlc.New(db)

	sqlite := &DB{
		db:      db,
		queries: queries,
		log:     log,
	}

	// Mark initialization as successful
	success = true
	return sqlite, nil
}

// setPragmas sets SQLite pragmas for optimal performance and reliability
func setPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA journal_size_limit = 5000000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA cache_size = -16000",
		"PRAGMA mmap_size = 0",             // Disable memory-mapped I/O which can cause issues with modernc.org/sqlite
		"PRAGMA page_size = 4096",          // Set consistent page size
		"PRAGMA wal_autocheckpoint = 1000", // Checkpoint WAL after 1000 pages
		"PRAGMA secure_delete = OFF",       // Improve performance
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return fmt.Errorf("error setting pragma %q: %w", pragma, err)
		}
	}

	return nil
}

// runMigrations runs all pending migrations
func runMigrations(db *sql.DB) error {
	// Create a filesystem driver for migrations
	migrationFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("error creating migrations filesystem: %w", err)
	}

	sourceDriver, err := iofs.New(migrationFS, ".")
	if err != nil {
		return fmt.Errorf("error creating migration source driver: %w", err)
	}

	driver, err := sqlite3.WithInstance(db, &sqlite3.Config{
		NoTxWrap:        true, // Disable transaction wrapping
		MigrationsTable: "schema_migrations",
	})
	if err != nil {
		return fmt.Errorf("error creating sqlite driver: %w", err)
	}

	m, err := migrate.NewWithInstance(
		"iofs",
		sourceDriver,
		"sqlite3",
		driver,
	)
	if err != nil {
		return fmt.Errorf("error creating migrate instance: %w", err)
	}

	// Important: Close the migration instance after we're done
	defer func() {
		if m != nil {
			sourceErr, dbErr := m.Close()
			if sourceErr != nil {
				fmt.Printf("Warning: error closing migration source: %v\n", sourceErr)
			}
			if dbErr != nil {
				fmt.Printf("Warning: error closing migration db: %v\n", dbErr)
			}
		}
	}()

	// Run migrations
	if err := m.Up(); err != nil {
		if err == migrate.ErrNoChange {
			// This is fine - it just means we're up to date
			return nil
		}
		return fmt.Errorf("error running migrations: %w", err)
	}

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	// Log that we're closing the database
	db.log.Debug("closing database connection")

	// Add a small delay before closing to allow any in-flight transactions to complete
	// This can help prevent memory issues during shutdown
	time.Sleep(100 * time.Millisecond)

	// Close the connection
	if err := db.db.Close(); err != nil {
		db.log.Error("error closing database connection",
			"error", err)
		return fmt.Errorf("error closing database connection: %w", err)
	}

	return nil
}

// Ensure DB implements auth.Store
var _ auth.Store = (*DB)(nil)
