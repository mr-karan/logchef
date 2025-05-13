package sqlite

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/sqlite/sqlc"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB provides access to the SQLite database and generated queries.
type DB struct {
	db      *sql.DB
	queries sqlc.Querier
	log     *slog.Logger
}

// Options holds configuration for creating a new DB instance.
type Options struct {
	Logger *slog.Logger
	Config config.SQLiteConfig
}

// New establishes a connection to the SQLite database, configures it,
// runs migrations, and returns a DB instance ready for use.
func New(opts Options) (*DB, error) {
	log := opts.Logger.With("component", "sqlite")

	// --- Main Database Connection ---
	db, err := sql.Open("sqlite", opts.Config.Path)
	if err != nil {
		log.Error("failed to open main database", "error", err, "path", opts.Config.Path)
		return nil, fmt.Errorf("error opening main database: %w", err)
	}

	// Ensure DB is closed if initialization fails partway through.
	var success bool
	defer func() {
		if !success {
			log.Debug("closing main database due to initialization error")
			_ = db.Close() // Attempt close, ignore error as we're already failing.
		}
	}()

	// Configure connection pool settings.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)

	// Apply performance and reliability PRAGMAs.
	if err := setPragmas(db); err != nil {
		log.Error("failed to set pragmas on main database", "error", err)
		return nil, fmt.Errorf("error setting pragmas on main database: %w", err)
	}

	// --- Run Migrations ---
	// Migrations are run using a separate, temporary connection to avoid
	// interfering with the main pool during potentially long operations.
	if err := setupAndRunMigrations(opts.Config.Path, log); err != nil {
		return nil, err // Error already logged within setupAndRunMigrations
	}

	// Initialization successful.
	success = true
	log.Info("sqlite database initialized successfully", "path", opts.Config.Path)

	return &DB{
		db:      db,
		queries: sqlc.New(db), // Initialize sqlc querier.
		log:     log,
	}, nil
}

// setupAndRunMigrations handles the setup and execution of database migrations.
func setupAndRunMigrations(dsn string, log *slog.Logger) error {
	// Open a separate connection specifically for migrations.
	migrationDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Error("failed to open migration database", "error", err, "path", dsn)
		return fmt.Errorf("error opening migration database: %w", err)
	}
	defer func() {
		log.Debug("closing migration database connection")
		_ = migrationDB.Close() // Ensure migration DB is closed.
	}()

	// Set a busy timeout for the migration connection.
	if _, err := migrationDB.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		log.Error("failed to set busy_timeout on migration database", "error", err)
		return fmt.Errorf("error setting busy_timeout on migration database: %w", err)
	}

	// Run the migrations using the dedicated connection.
	log.Info("running database migrations")
	if err := runMigrations(migrationDB, log); err != nil {
		log.Error("failed to run migrations", "error", err, "path", dsn)
		return fmt.Errorf("error running migrations: %w", err)
	}
	log.Info("database migrations completed successfully")
	return nil
}

// setPragmas applies a set of recommended PRAGMA settings to the SQLite connection
// for performance and reliability (e.g., enabling WAL mode).
func setPragmas(db *sql.DB) error {
	pragmas := []string{
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
		"PRAGMA journal_size_limit = 5000000", // Limit WAL size to ~5MB
		"PRAGMA synchronous = NORMAL",         // Less strict than FULL, good balance with WAL.
		"PRAGMA foreign_keys = ON",
		"PRAGMA temp_store = MEMORY", // Use memory for temporary tables.
		"PRAGMA cache_size = -16000", // Set cache size (e.g., ~16MB). Negative value is KiB.
		"PRAGMA mmap_size = 0",       // Disable memory-mapped I/O (can cause issues with modernc.org/sqlite).
		// "PRAGMA page_size = 4096", // Setting page_size after DB creation requires VACUUM. Usually set at creation.
		"PRAGMA wal_autocheckpoint = 1000", // Checkpoint WAL after 1000 pages (adjust based on workload).
		"PRAGMA secure_delete = OFF",       // Faster deletes, assumes filesystem is secure.
	}

	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			// Log the specific pragma that failed for easier debugging.
			return fmt.Errorf("error setting pragma %q: %w", pragma, err)
		}
	}
	return nil
}

// runMigrations uses the golang-migrate library to apply migrations
// embedded in the migrationsFS filesystem.
func runMigrations(db *sql.DB, log *slog.Logger) error {
	log.Info("initializing database migrations")
	migrationFS, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		log.Error("failed to create migrations filesystem subsection", "error", err)
		return fmt.Errorf("error creating migrations filesystem: %w", err)
	}

	sourceDriver, err := iofs.New(migrationFS, ".")
	if err != nil {
		log.Error("failed to create migration source driver", "error", err)
		return fmt.Errorf("error creating migration source driver: %w", err)
	}

	driver, err := sqlite3.WithInstance(db, &sqlite3.Config{
		MigrationsTable: "schema_migrations",
		// NoTransaction: true, // Set if migrations need to run outside a transaction (e.g., for certain PRAGMA statements)
	})
	if err != nil {
		log.Error("failed to create sqlite migration database driver", "error", err)
		return fmt.Errorf("error creating sqlite migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite3", driver)
	if err != nil {
		log.Error("failed to create migrate instance", "error", err)
		return fmt.Errorf("error creating migrate instance: %w", err)
	}

	currentVersion, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		log.Error("failed to get current migration version", "error", err)
		// Do not return here, as we still want to attempt migrations
	} else if errors.Is(err, migrate.ErrNilVersion) {
		log.Info("no previous migrations found, starting fresh")
	} else {
		log.Info("current database migration version", "version", currentVersion, "dirty", dirty)
		if dirty {
			log.Warn("database is in a dirty migration state. Manual intervention may be required if migrations fail.")
		}
	}

	// Ensure migration resources are closed.
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

	log.Info("applying database migrations...")
	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Info("migrations are already up to date.")
			return nil
		}
		log.Error("failed during migration application", "error", err)
		return fmt.Errorf("error applying migrations: %w", err)
	}

	finalVersion, dirty, err := m.Version()
	if err != nil {
		log.Error("failed to get final migration version after apply", "error", err)
	} else {
		log.Info("migrations applied successfully.", "new_version", finalVersion, "dirty", dirty)
	}

	return nil
}

// Close gracefully shuts down the database connection pool.
func (db *DB) Close() error {
	db.log.Debug("closing main database connection pool")
	if err := db.db.Close(); err != nil {
		db.log.Error("error closing main database connection pool", "error", err)
		return fmt.Errorf("error closing database connection: %w", err)
	}
	return nil
}
