package sqlite

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func newTxTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New(Options{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Config: config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "tx.db")},
	})
	if err != nil {
		t.Fatalf("sqlite.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func makeUser(email string) *models.User {
	return &models.User{Email: email, FullName: email, Role: models.UserRoleMember, Status: "active"}
}

// A committed transaction's writes are visible afterwards.
func TestWithTx_Commit(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()

	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		return tx.CreateUser(ctx, makeUser("commit@example.com"))
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	if _, err := db.GetUserByEmail(ctx, "commit@example.com"); err != nil {
		t.Fatalf("user should exist after commit, got: %v", err)
	}
}

// When fn returns an error the whole transaction is rolled back.
func TestWithTx_RollbackOnError(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()

	boom := errors.New("boom")
	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.CreateUser(ctx, makeUser("rollback@example.com")); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithTx should surface fn's error, got: %v", err)
	}

	if _, err := db.GetUserByEmail(ctx, "rollback@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("user should not exist after rollback, got err: %v", err)
	}
}

// Reads inside the transaction see that transaction's uncommitted writes — i.e.
// reads route through the tx connection, not SQLite's separate read pool.
func TestWithTx_ReadAfterWriteInTx(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()

	err := db.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.CreateUser(ctx, makeUser("raw@example.com")); err != nil {
			return err
		}
		// Not yet committed; a read on the separate pool would miss this.
		u, err := tx.GetUserByEmail(ctx, "raw@example.com")
		if err != nil {
			return err
		}
		if u.Email != "raw@example.com" {
			t.Errorf("read-after-write mismatch: %q", u.Email)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
}

// A panic inside fn rolls back and re-panics rather than committing.
func TestWithTx_RollbackOnPanic(t *testing.T) {
	db := newTxTestDB(t)
	ctx := context.Background()

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected panic to propagate")
			}
		}()
		_ = db.WithTx(ctx, func(tx store.StoreOps) error {
			if err := tx.CreateUser(ctx, makeUser("panic@example.com")); err != nil {
				return err
			}
			panic("kaboom")
		})
	}()

	if _, err := db.GetUserByEmail(ctx, "panic@example.com"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("user should not exist after panic rollback, got err: %v", err)
	}
}
