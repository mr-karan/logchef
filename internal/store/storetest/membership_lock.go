package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
)

func testLockMembershipWrites(t *testing.T, ctx context.Context, s store.Store) {
	t.Helper()
	if err := s.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.LockMembershipWrites(ctx); err != nil {
			return err
		}
		return tx.LockMembershipWrites(ctx)
	}); err != nil {
		t.Fatalf("lock membership writes inside transaction: %v", err)
	}
}

// RunMembershipWritesSerialized checks transaction-scoped membership write exclusion.
func RunMembershipWritesSerialized(t *testing.T, s store.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan struct{})
	result := make(chan error, 1)
	waiting := false
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		if err := tx.LockMembershipWrites(ctx); err != nil {
			return err
		}
		waiting = true
		go func() {
			result <- s.WithTx(ctx, func(second store.StoreOps) error {
				close(started)
				return second.LockMembershipWrites(ctx)
			})
		}()
		select {
		case <-started:
		case err := <-result:
			t.Errorf("second transaction failed to start: %v", err)
			waiting = false
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()
		select {
		case err := <-result:
			waiting = false
			t.Errorf("second lock did not wait for commit: %v", err)
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	if waiting {
		select {
		case err := <-result:
			if err != nil {
				t.Errorf("second lock after first transaction ended: %v", err)
			}
		case <-ctx.Done():
			t.Error("second lock did not complete after first transaction ended")
		}
	}
	if err != nil {
		t.Fatalf("first transaction: %v", err)
	}
}
