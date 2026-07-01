package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/sqlite/sqlc"
)

// WithTx runs fn inside a single write transaction and satisfies
// store.TxRunner. The store.StoreOps handed to fn routes every read and write
// through that transaction, so read-after-write within fn is consistent. It
// commits when fn returns nil and rolls back on any error or panic (re-panicking
// after rollback).
//
// Nested transactions are unsupported: the handle given to fn shares this DB's
// type but is tx-scoped and must not be used to start another transaction.
func (db *DB) WithTx(ctx context.Context, fn func(tx store.StoreOps) error) (err error) {
	// A tx-scoped handle already holds the single write connection; starting
	// another transaction on it would deadlock. Reject nesting explicitly.
	if db.inTx {
		return fmt.Errorf("nested transactions are not supported")
	}

	tx, err := db.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Point both queriers at the transaction connection. SQLite uses a separate
	// read pool that cannot see this tx's uncommitted writes, so a tx-scoped
	// store must never touch it — routing reads through the tx fixes that.
	q := sqlc.New(tx)
	txDB := *db
	txDB.readQueries = q
	txDB.writeQueries = q
	txDB.inTx = true

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	if err := fn(&txDB); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
			db.log.Error("failed to roll back transaction", "error", rbErr, "cause", err)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
