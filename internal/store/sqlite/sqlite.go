// Package sqlite implements store.Store backed by SQLite — logchef's default,
// single-binary, zero-config backend.
//
// It wraps the lower-level *isqlite.DB (sqlc-generated queries + separate
// read/write connection pools, WAL, busy_timeout, foreign_keys), whose methods
// all speak pkg/models, so the embedded DB satisfies store.StoreOps directly.
// The compile-time assertion below guards that: any method that regresses to a
// sqlc or driver type will break the build here.
package sqlite

import (
	isqlite "github.com/mr-karan/logchef/internal/sqlite"
	"github.com/mr-karan/logchef/internal/store"
)

// Options configures the SQLite store. Re-exported from the underlying package
// so callers depend on store/sqlite, not internal/sqlite.
type Options = isqlite.Options

// Store is the SQLite-backed implementation of store.Store. Embedding *isqlite.DB
// promotes its model-based methods (CreateSession, GetUserPreferencesJSON, …) so
// they satisfy the store interfaces directly.
type Store struct {
	*isqlite.DB
}

// Compile-time guarantee that the adapter satisfies the complete store.Store
// contract: all 13 data domains (StoreOps), io.Closer, and TxRunner (WithTx),
// all inherited from the embedded *isqlite.DB. If a method regresses to leaking
// a sqlc/driver type, this assertion fails.
var _ store.Store = (*Store)(nil)

// New opens the SQLite store, running migrations as the underlying DB does.
func New(opts Options) (*Store, error) {
	db, err := isqlite.New(opts)
	if err != nil {
		return nil, err
	}
	return &Store{DB: db}, nil
}

// WithTx (store.TxRunner), Close (io.Closer), and every domain method are all
// inherited from the embedded *isqlite.DB, so this thin wrapper satisfies the
// full store.Store contract without adding methods of its own.
