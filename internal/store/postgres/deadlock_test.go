package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// Two transactions that lock two collections in opposite orders deadlock.
// Postgres aborts one with 40P01, which WithTx reports as ErrDatabaseBusy so
// the request can answer 503. The other commits.
func TestDeadlockMapsToDatabaseBusy(t *testing.T) {
	dsn := resetSchema(t)
	a, b := opener(dsn)(t), opener(dsn)(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	owner := &models.User{Email: "deadlock@test.dev", FullName: "Owner", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := a.CreateUser(ctx, owner); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	first, err := a.CreateCollection(ctx, "first", "", false, owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}
	second, err := a.CreateCollection(ctx, "second", "", false, owner.ID)
	if err != nil {
		t.Fatalf("CreateCollection: %v", err)
	}

	lockBoth := func(s store.Store, x, y int, holdFirst chan struct{}, otherHeld chan struct{}) error {
		return s.WithTx(ctx, func(tx store.StoreOps) error {
			if err := tx.LockCollection(ctx, x); err != nil {
				return err
			}
			close(holdFirst)
			select {
			case <-otherHeld:
			case <-ctx.Done():
				return ctx.Err()
			}
			return tx.LockCollection(ctx, y)
		})
	}
	aHeld, bHeld := make(chan struct{}), make(chan struct{})
	errs := make(chan error, 2)
	go func() { errs <- lockBoth(a, first.ID, second.ID, aHeld, bHeld) }()
	go func() { errs <- lockBoth(b, second.ID, first.ID, bHeld, aHeld) }()

	busy, ok := 0, 0
	for range 2 {
		select {
		case err := <-errs:
			switch {
			case err == nil:
				ok++
			case models.IsDatabaseBusy(err):
				busy++
			default:
				t.Fatalf("unexpected error: %v", err)
			}
		case <-ctx.Done():
			t.Fatalf("deadlock was not detected")
		}
	}
	if busy != 1 || ok != 1 {
		t.Fatalf("outcomes = %d busy, %d committed; want one of each", busy, ok)
	}
}
