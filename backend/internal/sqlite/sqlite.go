package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"backend-v2/internal/auth"
	"backend-v2/pkg/logger"
	"backend-v2/pkg/models"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite3"
	_ "github.com/golang-migrate/migrate/v4/source/file"
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
	CreateTeam           *sqlx.Stmt `query:"CreateTeam"`
	GetTeam              *sqlx.Stmt `query:"GetTeam"`
	UpdateTeam           *sqlx.Stmt `query:"UpdateTeam"`
	DeleteTeam           *sqlx.Stmt `query:"DeleteTeam"`
	ListTeams            *sqlx.Stmt `query:"ListTeams"`
	AddTeamMember        *sqlx.Stmt `query:"AddTeamMember"`
	GetTeamMember        *sqlx.Stmt `query:"GetTeamMember"`
	UpdateTeamMemberRole *sqlx.Stmt `query:"UpdateTeamMemberRole"`
	RemoveTeamMember     *sqlx.Stmt `query:"RemoveTeamMember"`
	ListTeamMembers      *sqlx.Stmt `query:"ListTeamMembers"`
	ListUserTeams        *sqlx.Stmt `query:"ListUserTeams"`

	// Space queries
	CreateSpace           *sqlx.Stmt `query:"CreateSpace"`
	GetSpace              *sqlx.Stmt `query:"GetSpace"`
	UpdateSpace           *sqlx.Stmt `query:"UpdateSpace"`
	DeleteSpace           *sqlx.Stmt `query:"DeleteSpace"`
	ListSpaces            *sqlx.Stmt `query:"ListSpaces"`
	AddSpaceDataSource    *sqlx.Stmt `query:"AddSpaceDataSource"`
	RemoveSpaceDataSource *sqlx.Stmt `query:"RemoveSpaceDataSource"`
	ListSpaceDataSources  *sqlx.Stmt `query:"ListSpaceDataSources"`
	SetSpaceTeamAccess    *sqlx.Stmt `query:"SetSpaceTeamAccess"`
	UpdateSpaceTeamAccess *sqlx.Stmt `query:"UpdateSpaceTeamAccess"`
	RemoveSpaceTeamAccess *sqlx.Stmt `query:"RemoveSpaceTeamAccess"`
	GetSpaceTeamAccess    *sqlx.Stmt `query:"GetSpaceTeamAccess"`
	ListSpaceTeamAccess   *sqlx.Stmt `query:"ListSpaceTeamAccess"`

	// Query queries
	CreateQuery *sqlx.Stmt `query:"CreateQuery"`
	GetQuery    *sqlx.Stmt `query:"GetQuery"`
	UpdateQuery *sqlx.Stmt `query:"UpdateQuery"`
	DeleteQuery *sqlx.Stmt `query:"DeleteQuery"`
	ListQueries *sqlx.Stmt `query:"ListQueries"`
}

type Options struct {
	Path string
}

// New creates a new SQLite database connection and initializes the schema
func New(opts Options) (*DB, error) {
	// Initialize logger with component tag
	log := logger.Default().With("component", "sqlite")

	// Connect to SQLite
	db, err := sqlx.Connect("sqlite", opts.Path)
	if err != nil {
		log.Error("failed to open database",
			"error", err,
			"path", opts.Path,
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

	// Set pragmas for optimal performance
	if err := setPragmas(db); err != nil {
		log.Error("failed to set pragmas",
			"error", err,
		)
		return nil, fmt.Errorf("error setting pragmas: %w", err)
	}

	// Create a separate connection for migrations to avoid interfering with the main connection
	migrationDB, err := sql.Open("sqlite", opts.Path)
	if err != nil {
		log.Error("failed to open migration database",
			"error", err,
			"path", opts.Path,
		)
		return nil, fmt.Errorf("error opening migration database: %w", err)
	}
	defer migrationDB.Close()

	// Run migrations
	if err := runMigrations(migrationDB, opts.Path); err != nil {
		log.Error("failed to run migrations",
			"error", err,
			"path", opts.Path,
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
func setPragmas(db *sqlx.DB) error {
	pragmas := []string{
		"PRAGMA busy_timeout = 5000",
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
func runMigrations(db *sql.DB, dbPath string) error {
	driver, err := sqlite3.WithInstance(db, &sqlite3.Config{
		NoTxWrap:        true, // Disable transaction wrapping
		MigrationsTable: "schema_migrations",
	})
	if err != nil {
		return fmt.Errorf("error creating sqlite driver: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(
		"file://internal/sqlite/migrations",
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
func (d *DB) Close() error {
	return d.conn.Close()
}

// Helper function to check if an error is a SQLite unique constraint violation
func isUniqueConstraintError(err error, table, column string) bool {
	return strings.Contains(err.Error(), fmt.Sprintf("UNIQUE constraint failed: %s.%s", table, column))
}

// Ensure DB implements auth.Store
var _ auth.Store = (*DB)(nil)

// GetTeamSpaceAccess gets a team's access to a space
func (d *DB) GetTeamSpaceAccess(ctx context.Context, teamID, spaceID string) (*models.SpaceTeamAccess, error) {
	var access models.SpaceTeamAccess
	err := d.queries.GetSpaceTeamAccess.GetContext(ctx, &access, spaceID, teamID)
	if err != nil {
		return nil, fmt.Errorf("error getting team space access: %w", err)
	}
	return &access, nil
}

// GetUserTeams gets all teams a user is a member of
func (d *DB) GetUserTeams(ctx context.Context, userID string) ([]*models.Team, error) {
	var teams []*models.Team
	err := d.queries.ListUserTeams.SelectContext(ctx, &teams, userID)
	if err != nil {
		return nil, fmt.Errorf("error getting user teams: %w", err)
	}
	return teams, nil
}

// GrantTeamAccess grants a team access to a space
func (d *DB) GrantTeamAccess(ctx context.Context, access *models.SpaceTeamAccess) error {
	now := time.Now()
	result, err := d.queries.SetSpaceTeamAccess.ExecContext(ctx,
		access.SpaceID,
		access.TeamID,
		access.Permission,
		now,
		now,
	)
	if err != nil {
		return fmt.Errorf("error granting team access: %w", err)
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

// RevokeTeamAccess revokes a team's access to a space
func (d *DB) RevokeTeamAccess(ctx context.Context, spaceID, teamID string) error {
	result, err := d.queries.RemoveSpaceTeamAccess.ExecContext(ctx, spaceID, teamID)
	if err != nil {
		return fmt.Errorf("error revoking team access: %w", err)
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

// ListSpaceAccess lists all team access records for a space
func (d *DB) ListSpaceAccess(ctx context.Context, spaceID string) ([]*models.SpaceTeamAccess, error) {
	var access []*models.SpaceTeamAccess
	err := d.queries.ListSpaceTeamAccess.SelectContext(ctx, &access, spaceID)
	if err != nil {
		return nil, fmt.Errorf("error listing space access: %w", err)
	}
	return access, nil
}
