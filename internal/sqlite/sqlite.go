package sqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/mr-karan/logchef/pkg/logger"

	"github.com/mr-karan/logchef/internal/auth"
	"github.com/mr-karan/logchef/internal/config"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
	"github.com/knadh/goyesql/v2"
	goyesqlx "github.com/knadh/goyesql/v2/sqlx"
	_ "modernc.org/sqlite"
)

//go:embed queries.sql
var queriesSQL string

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB represents our SQLite database manager
type DB struct {
	conn    *sqlx.DB
	queries *Queries
	log     *slog.Logger
}

// Queries contains all prepared SQL statements
type Queries struct {
	// Source queries
	CreateSource    *sqlx.Stmt `query:"CreateSource"`
	GetSource       *sqlx.Stmt `query:"GetSource"`
	GetSourceByName *sqlx.Stmt `query:"GetSourceByName"`
	ListSources     *sqlx.Stmt `query:"ListSources"`
	UpdateSource    *sqlx.Stmt `query:"UpdateSource"`
	DeleteSource    *sqlx.Stmt `query:"DeleteSource"`

	// User queries
	CreateUser      *sqlx.Stmt `query:"CreateUser"`
	GetUser         *sqlx.Stmt `query:"GetUser"`
	GetUserByEmail  *sqlx.Stmt `query:"GetUserByEmail"`
	UpdateUser      *sqlx.Stmt `query:"UpdateUser"`
	ListUsers       *sqlx.Stmt `query:"ListUsers"`
	CountAdminUsers *sqlx.Stmt `query:"CountAdminUsers"`
	DeleteUser      *sqlx.Stmt `query:"DeleteUser"`

	// Session queries
	CreateSession      *sqlx.Stmt `query:"CreateSession"`
	GetSession         *sqlx.Stmt `query:"GetSession"`
	DeleteSession      *sqlx.Stmt `query:"DeleteSession"`
	DeleteUserSessions *sqlx.Stmt `query:"DeleteUserSessions"`
	CountUserSessions  *sqlx.Stmt `query:"CountUserSessions"`

	// Team queries
	CreateTeam                *sqlx.Stmt `query:"CreateTeam"`
	GetTeam                   *sqlx.Stmt `query:"GetTeam"`
	UpdateTeam                *sqlx.Stmt `query:"UpdateTeam"`
	DeleteTeam                *sqlx.Stmt `query:"DeleteTeam"`
	ListTeams                 *sqlx.Stmt `query:"ListTeams"`
	AddTeamMember             *sqlx.Stmt `query:"AddTeamMember"`
	GetTeamMember             *sqlx.Stmt `query:"GetTeamMember"`
	UpdateTeamMemberRole      *sqlx.Stmt `query:"UpdateTeamMemberRole"`
	RemoveTeamMember          *sqlx.Stmt `query:"RemoveTeamMember"`
	ListTeamMembers           *sqlx.Stmt `query:"ListTeamMembers"`
	ListTeamMembersWithDetails *sqlx.Stmt `query:"ListTeamMembersWithDetails"`
	ListUserTeams             *sqlx.Stmt `query:"ListUserTeams"`

	// Team source queries
	AddTeamSource      *sqlx.Stmt `query:"AddTeamSource"`
	RemoveTeamSource   *sqlx.Stmt `query:"RemoveTeamSource"`
	ListTeamSources    *sqlx.Stmt `query:"ListTeamSources"`
	ListSourceTeams    *sqlx.Stmt `query:"ListSourceTeams"`
	TeamHasSource      *sqlx.Stmt `query:"TeamHasSource"`
	UserHasSourceAccess *sqlx.Stmt `query:"UserHasSourceAccess"`
	GetTeamByName      *sqlx.Stmt `query:"GetTeamByName"`

	// Team query queries
	CreateTeamQuery            *sqlx.Stmt `query:"CreateTeamQuery"`
	GetTeamQuery               *sqlx.Stmt `query:"GetTeamQuery"`
	GetTeamQueryWithAccess     *sqlx.Stmt `query:"GetTeamQueryWithAccess"`
	UpdateTeamQuery            *sqlx.Stmt `query:"UpdateTeamQuery"`
	DeleteTeamQuery            *sqlx.Stmt `query:"DeleteTeamQuery"`
	ListTeamQueries            *sqlx.Stmt `query:"ListTeamQueries"`
	ListQueriesForUserAndTeam  *sqlx.Stmt `query:"ListQueriesForUserAndTeam"`
	ListQueriesForUser         *sqlx.Stmt `query:"ListQueriesForUser"`
	ListQueriesBySource        *sqlx.Stmt `query:"ListQueriesBySource"`
	ListQueriesByTeamAndSource *sqlx.Stmt `query:"ListQueriesByTeamAndSource"`
	ListQueriesForUserBySource *sqlx.Stmt `query:"ListQueriesForUserBySource"`
}

type Options struct {
	Config config.SQLiteConfig
}

// New creates a new SQLite database connection and initializes the schema
func New(opts Options) (*DB, error) {
	// Initialize logger with component tag
	log := logger.NewLogger("sqlite")

	// Connect to SQLite with multi-statement support
	db, err := sqlx.Connect("sqlite", opts.Config.Path+"?_multi_stmt=1")
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

	// Configure connection pool
	db.SetMaxOpenConns(opts.Config.MaxOpenConns)
	db.SetMaxIdleConns(opts.Config.MaxIdleConns)
	db.SetConnMaxLifetime(opts.Config.ConnMaxLifetime)
	db.SetConnMaxIdleTime(opts.Config.ConnMaxIdleTime)

	// Set pragmas for optimal performance
	if err := setPragmas(db, opts.Config.BusyTimeout); err != nil {
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

	// Parse our queries
	qMap, err := goyesql.ParseBytes([]byte(queriesSQL))
	if err != nil {
		log.Error("failed to parse SQL queries",
			"error", err,
		)
		return nil, fmt.Errorf("error parsing SQL queries: %w", err)
	}

	// Prepare statements
	var q Queries
	if err := goyesqlx.ScanToStruct(&q, qMap, db); err != nil {
		log.Error("failed to prepare SQL queries",
			"error", err,
		)
		return nil, fmt.Errorf("error preparing SQL queries: %w", err)
	}

	sqlite := &DB{
		conn:    db,
		queries: &q,
		log:     log,
	}

	// Mark initialization as successful
	success = true
	return sqlite, nil
}

// setPragmas sets SQLite pragmas for optimal performance and reliability
func setPragmas(db *sqlx.DB, busyTimeout int) error {
	pragmas := []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d", busyTimeout),
		"PRAGMA journal_mode = WAL",
		"PRAGMA journal_size_limit = 5000000",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA temp_store = MEMORY",
		"PRAGMA cache_size = -16000",
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

// Close closes the database connection and all prepared statements
func (db *DB) Close() error {
	return db.conn.Close()
}

// Ensure DB implements auth.Store
var _ auth.Store = (*DB)(nil)
