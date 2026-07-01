// Package postgres implements store.Store backed by PostgreSQL — the opt-in
// metadata backend for multi-replica / HA deployments. SQLite remains the
// default; this backend is selected via config (database.driver = "postgres").
//
// Every method speaks pkg/models types and translates pgx driver errors into
// the backend-neutral sentinels in pkg/models (ErrNotFound, ErrConflict), so
// callers never see pgx or sqlc types.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres/sqlc"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationLockKey is a fixed application-defined key for the pg advisory lock
// that serializes migrations across replicas (prevents a thundering-herd
// migrate on rollout). The value is arbitrary but must be stable.
const migrationLockKey int64 = 0x10_9C_4E_F0_01

// Options configures the Postgres store.
type Options struct {
	Logger *slog.Logger
	Config config.PostgresConfig
}

// Store is the Postgres-backed implementation of store.Store.
//
// q is bound either to the connection pool (the normal case) or, inside a
// WithTx callback, to that transaction — so every read and write in a tx shares
// it. pool is nil on a tx-scoped Store (it must not Close or start a nested tx).
type Store struct {
	pool *pgxpool.Pool
	q    sqlc.Querier
	log  *slog.Logger
}

// Compile-time guarantee that the Postgres backend satisfies the full contract:
// all 13 data domains (StoreOps), io.Closer, and TxRunner (WithTx).
var _ store.Store = (*Store)(nil)

// New connects to Postgres, tunes the pool, applies migrations under an advisory
// lock, and returns a ready store.
func New(ctx context.Context, opts Options) (*Store, error) {
	log := opts.Logger.With("component", "postgres")

	cfg, err := pgxpool.ParseConfig(opts.Config.DSN)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres dsn: %w", err)
	}
	if opts.Config.MaxOpenConns > 0 {
		cfg.MaxConns = int32(opts.Config.MaxOpenConns) //nolint:gosec // G115: pool size from config, small bounded value
	}
	if opts.Config.MaxIdleConns > 0 {
		cfg.MinConns = int32(opts.Config.MaxIdleConns) //nolint:gosec // G115: pool size from config, small bounded value
	}
	if opts.Config.ConnMaxLifetime > 0 {
		cfg.MaxConnLifetime = opts.Config.ConnMaxLifetime
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	if err := runMigrations(ctx, pool, log); err != nil {
		pool.Close()
		return nil, fmt.Errorf("running postgres migrations: %w", err)
	}

	return &Store{pool: pool, q: sqlc.New(pool), log: log}, nil
}

// Close releases the connection pool.
func (s *Store) Close() error {
	if s.pool != nil {
		s.pool.Close()
	}
	return nil
}

// WithTx runs fn inside a single transaction and satisfies store.TxRunner. The
// store.StoreOps handed to fn routes every read and write through that
// transaction. It commits when fn returns nil and rolls back on any error or
// panic (re-panicking after rollback). Nested transactions are unsupported.
func (s *Store) WithTx(ctx context.Context, fn func(tx store.StoreOps) error) (err error) {
	// A tx-scoped Store carries a nil pool (q is bound to the transaction);
	// starting another transaction from it is unsupported. Reject nesting.
	if s.pool == nil {
		return fmt.Errorf("nested transactions are not supported")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	txStore := &Store{q: sqlc.New(tx), log: s.log} // pool nil: no Close/nested tx

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(txStore); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			s.log.Error("failed to roll back transaction", "error", rbErr, "cause", err)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// --- error translation -------------------------------------------------------

// notFound reports whether err is pgx's no-rows error; callers translate it to
// models.ErrNotFound.
func notFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// isUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505); callers translate it to models.ErrConflict.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// --- migrations --------------------------------------------------------------

// runMigrations applies any not-yet-applied up migrations under a session-level
// advisory lock so only one replica migrates at a time. Versions are tracked in
// schema_migrations. logchef's Postgres schema is a single consolidated baseline
// today, but this scales to future paired migrations.
func runMigrations(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration conn: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	defer func() {
		if _, err := conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationLockKey); err != nil {
			log.Error("failed to release migration advisory lock", "error", err)
		}
	}()

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	var current int64
	if err := conn.QueryRow(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&current); err != nil {
		return fmt.Errorf("read current version: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	applied := 0
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		if _, err := conn.Exec(ctx, m.sql); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", m.version, m.name, err)
		}
		if _, err := conn.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", m.version); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
		log.Info("applied postgres migration", "version", m.version, "name", m.name)
		applied++
	}
	log.Debug("postgres migrations up to date", "version", currentOr(migrations, current), "applied_this_run", applied)
	return nil
}

type migration struct {
	version int64
	name    string
	sql     string
}

// loadMigrations reads the embedded *.up.sql files, parsing the leading numeric
// version from each filename (e.g. 000001_init.up.sql -> 1).
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	var out []migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		verStr, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, fmt.Errorf("malformed migration filename %q", name)
		}
		ver, err := strconv.ParseInt(verStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse version from %q: %w", name, err)
		}
		body, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}
		out = append(out, migration{version: ver, name: name, sql: string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func currentOr(ms []migration, fallback int64) int64 {
	if len(ms) > 0 {
		return ms[len(ms)-1].version
	}
	return fallback
}
