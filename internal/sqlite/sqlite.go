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

	// --- Main Database Connection Setup ---
	// Use default DEFERRED locking and rely on busy_timeout
	dsn := opts.Config.Path
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Error("failed to open main database",
			"error", err,
			"path", opts.Config.Path)
		return nil, fmt.Errorf("error opening main database: %w", err)
	}

	// Set up cleanup for main DB in case of initialization error
	var success bool
	defer func() {
		if !success {
			log.Debug("closing main database due to initialization error")
			db.Close() // Attempt to close, ignore error as we're already failing
		}
	}()

	// Configure connection pool with settings based on expected workload
	db.SetMaxOpenConns(25) // Allow more concurrent connections based on workload
	db.SetMaxIdleConns(10) // Keep more idle connections for better performance
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Set pragmas for optimal performance
	if err := setPragmas(db); err != nil {
		log.Error("failed to set pragmas on main database",
			"error", err)
		return nil, fmt.Errorf("error setting pragmas on main database: %w", err)
	}

	// --- Migration Setup ---
	// Open a separate, temporary connection just for migrations
	migrationDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Error("failed to open migration database",
			"error", err,
			"path", opts.Config.Path)
		return nil, fmt.Errorf("error opening migration database: %w", err)
	}

	// Ensure migration DB is closed after migrations run
	defer func() {
		log.Debug("closing migration database connection")
		migrationDB.Close() // Attempt to close, ignore error
	}()

	// Set busy_timeout on migration connection
	if _, err := migrationDB.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		log.Error("failed to set busy_timeout on migration database",
			"error", err)
		return nil, fmt.Errorf("error setting busy_timeout on migration database: %w", err)
	}

	// Run migrations using the dedicated migration database connection
	log.Info("running database migrations")
	if err := runMigrations(migrationDB, log); err != nil {
		log.Error("failed to run migrations",
			"error", err,
			"path", opts.Config.Path)
		return nil, fmt.Errorf("error running migrations: %w", err)
	}
	log.Info("database migrations completed successfully")

	// Initialize sqlc queries
	queries := sqlc.New(db)

	sqlite := &DB{
		db:      db,
		queries: queries,
		log:     log,
	}

	// Mark initialization as successful
	success = true
	log.Info("sqlite database initialized successfully")
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
		"PRAGMA cache_size = -16000",       // ~16MB cache
		"PRAGMA mmap_size = 0",             // Disable memory-mapped I/O which can cause issues with modernc.org/sqlite
		"PRAGMA page_size = 4096",          // Set consistent page size
		"PRAGMA wal_autocheckpoint = 1000", // Checkpoint WAL after 1000 pages
		"PRAGMA secure_delete = OFF",       // Improve performance
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			// Log the specific pragma that failed
			return fmt.Errorf("error setting pragma %q: %w", pragma, err)
		}
	}

	return nil
}

// runMigrations runs all pending migrations
func runMigrations(db *sql.DB, log *slog.Logger) error {
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
		// Enable transaction wrapping for atomic migrations
		MigrationsTable: "schema_migrations",
	})
	if err != nil {
		return fmt.Errorf("error creating sqlite migration driver: %w", err)
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
				log.Warn("error closing migration source driver", "error", sourceErr)
			}
			if dbErr != nil {
				log.Warn("error closing migration database driver", "error", dbErr)
			}
		}
	}()

	// Run migrations
	log.Info("applying migrations...")
	if err := m.Up(); err != nil {
		if err == migrate.ErrNoChange {
			// This is fine - it just means we're up to date
			log.Info("migrations are already up to date")
			return nil
		}
		log.Error("migration failed", "error", err)
		return fmt.Errorf("error applying migrations: %w", err)
	}
	log.Info("migrations applied successfully")

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	// Log that we're closing the database
	db.log.Debug("closing main database connection pool")

	// Close the connection - db.Close() waits for idle connections
	if err := db.db.Close(); err != nil {
		db.log.Error("error closing main database connection pool",
			"error", err)
		return fmt.Errorf("error closing database connection: %w", err)
	}

	return nil
}

// Ensure DB implements auth.Store
var _ auth.Store = (*DB)(nil)
