// Package sqlite implements store.Store backed by SQLite — logchef's default,
// single-binary, zero-config metadata backend.
//
// Every method speaks pkg/models types and translates driver errors into the
// backend-neutral sentinels in pkg/models (ErrNotFound, ErrConflict), so callers
// never see database/sql or sqlc types. It uses split read/write connection
// pools (WAL mode: concurrent readers, a single serialized writer) with pragmas
// carried in the DSN so they apply on every pooled connection.
package sqlite

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"
)

// Compile-time guarantee that *DB satisfies the full store.Store contract: all
// data domains (StoreOps), io.Closer, and TxRunner (WithTx). If a method
// regresses to leaking a sqlc/driver type, this assertion fails to build.
var _ store.Store = (*DB)(nil)

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
	inTx         bool // true on a tx-scoped handle; guards against nested WithTx
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
	// PRAGMAs are carried in the DSN so modernc applies them on EVERY connection
	// the pool opens — not just the one that happens to run a PRAGMA statement,
	// which is the database/sql pitfall that would leave most pooled read
	// connections without busy_timeout/foreign_keys/etc.
	readDB, err := sql.Open("sqlite", buildDSN(opts.Config.Path))
	if err != nil {
		log.Error("failed to open read database", "error", err, "path", opts.Config.Path)
		return nil, fmt.Errorf("error opening read database: %w", err)
	}

	readDB.SetMaxOpenConns(25)
	readDB.SetMaxIdleConns(10)
	readDB.SetConnMaxLifetime(30 * time.Minute)
	readDB.SetConnMaxIdleTime(5 * time.Minute)

	// Open write connection with _txlock=immediate to acquire the write lock
	// early. This prevents deadlocks when multiple goroutines compete for writes.
	writeDB, err := sql.Open("sqlite", buildDSN(opts.Config.Path, "_txlock=immediate"))
	if err != nil {
		readDB.Close()
		log.Error("failed to open write database", "error", err, "path", opts.Config.Path)
		return nil, fmt.Errorf("error opening write database: %w", err)
	}

	// Single connection enforces serialized writes (SQLite limitation).
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)
	writeDB.SetConnMaxLifetime(0)

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

// connectionPragmas are applied to every SQLite connection via the DSN (modernc
// _pragma), for reliability and performance. Several of these (busy_timeout,
// foreign_keys, synchronous, cache_size, temp_store, mmap_size) are
// per-connection state and MUST be set on each connection, not once on the pool.
// The DB-level ones (journal_mode=WAL, checkpoint/size limits) are persistent
// but harmless to re-assert per connection.
var connectionPragmas = []string{
	"busy_timeout(5000)",
	"journal_mode(WAL)",
	"journal_size_limit(5000000)", // cap the WAL at ~5MB
	"synchronous(NORMAL)",         // safe with WAL; not the corruption-prone OFF
	"foreign_keys(ON)",
	"temp_store(MEMORY)",
	"cache_size(-16000)", // ~16MB (negative = KiB)
	"mmap_size(0)",       // disable mmap (avoids the mmap corruption class)
	"wal_autocheckpoint(1000)",
	"secure_delete(OFF)",
}

// buildDSN returns a modernc.org/sqlite DSN that applies connectionPragmas on
// every connection open, plus any extra raw query params (e.g. _txlock=immediate).
func buildDSN(path string, extra ...string) string {
	params := make([]string, 0, len(connectionPragmas)+len(extra))
	for _, p := range connectionPragmas {
		params = append(params, "_pragma="+url.QueryEscape(p))
	}
	params = append(params, extra...)
	return "file:" + path + "?" + strings.Join(params, "&")
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

	driver, err := migratesqlite.WithInstance(db, &migratesqlite.Config{
		MigrationsTable: "schema_migrations",
	})
	if err != nil {
		log.Error("failed to create sqlite migration database driver", "error", err)
		return fmt.Errorf("error creating sqlite migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", driver)
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
