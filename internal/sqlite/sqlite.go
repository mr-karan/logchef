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
// It uses separate connections for reads and writes to optimize for SQLite's
// WAL mode which allows concurrent reads but only one writer at a time.
type DB struct {
	readDB       *sql.DB      // Connection pool for read operations (multiple concurrent readers)
	writeDB      *sql.DB      // Single connection for write operations (serialized writes)
	readQueries  sqlc.Querier // Querier bound to read connection pool
	writeQueries sqlc.Querier // Querier bound to write connection
	log          *slog.Logger
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

	// Run migrations first using a temporary connection.
	if err := setupAndRunMigrations(opts.Config.Path, log); err != nil {
		return nil, err
	}

	// Open read connection pool (concurrent readers allowed in WAL mode).
	readDB, err := sql.Open("sqlite", opts.Config.Path)
	if err != nil {
		log.Error("failed to open read database", "error", err, "path", opts.Config.Path)
		return nil, fmt.Errorf("error opening read database: %w", err)
	}

	readDB.SetMaxOpenConns(25)
	readDB.SetMaxIdleConns(10)
	readDB.SetConnMaxLifetime(30 * time.Minute)
	readDB.SetConnMaxIdleTime(5 * time.Minute)

	if err := setPragmas(readDB); err != nil {
		readDB.Close()
		log.Error("failed to set pragmas on read database", "error", err)
		return nil, fmt.Errorf("error setting pragmas on read database: %w", err)
	}

	// Open write connection with _txlock=immediate to acquire write lock early.
	// This prevents deadlocks when multiple goroutines compete for writes.
	writeDSN := opts.Config.Path + "?_txlock=immediate"
	writeDB, err := sql.Open("sqlite", writeDSN)
	if err != nil {
		readDB.Close()
		log.Error("failed to open write database", "error", err, "path", opts.Config.Path)
		return nil, fmt.Errorf("error opening write database: %w", err)
	}

	// Single connection enforces serialized writes (SQLite limitation).
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	writeDB.SetConnMaxLifetime(0)

	if err := setPragmas(writeDB); err != nil {
		readDB.Close()
		writeDB.Close()
		log.Error("failed to set pragmas on write database", "error", err)
		return nil, fmt.Errorf("error setting pragmas on write database: %w", err)
	}

	log.Debug("sqlite initialized with read/write separation", "path", opts.Config.Path)

	return &DB{
		readDB:       readDB,
		writeDB:      writeDB,
		readQueries:  sqlc.New(readDB),
		writeQueries: sqlc.New(writeDB),
		log:          log,
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
	log.Debug("running database migrations")
	if err := runMigrations(migrationDB, log); err != nil {
		log.Error("migration failed", "error", err, "path", dsn)
		return fmt.Errorf("error running migrations: %w", err)
	}
	log.Debug("database migrations completed")
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
	log.Debug("initializing database migrations")
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
	switch {
	case err != nil && !errors.Is(err, migrate.ErrNilVersion):
		log.Error("failed to get current migration version", "error", err)
		// Do not return here, as we still want to attempt migrations
	case errors.Is(err, migrate.ErrNilVersion):
		log.Debug("no previous migrations found")
	default:
		log.Debug("current migration version", "version", currentVersion, "dirty", dirty)
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

	log.Debug("applying database migrations")
	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			log.Debug("migrations up to date")
			return nil
		}
		log.Error("migration failed", "error", err)
		return fmt.Errorf("error applying migrations: %w", err)
	}

	finalVersion, dirty, err := m.Version()
	if err != nil {
		log.Error("failed to get migration version", "error", err)
	} else {
		log.Debug("migrations applied", "new_version", finalVersion, "dirty", dirty)
	}

	return nil
}

// Close gracefully shuts down both database connections.
func (db *DB) Close() error {
	db.log.Debug("closing database connections")
	var errs []error
	if err := db.writeDB.Close(); err != nil {
		db.log.Error("error closing write database", "error", err)
		errs = append(errs, err)
	}
	if err := db.readDB.Close(); err != nil {
		db.log.Error("error closing read database", "error", err)
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("error closing database connections: %v", errs)
	}
	return nil
}
