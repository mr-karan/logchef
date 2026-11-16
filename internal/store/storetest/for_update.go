package storetest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

// testForUpdateReads checks that the locking reads return the stored row,
// see the writes of their own transaction, and report a missing row.
func testForUpdateReads(t *testing.T, ctx context.Context, s store.Store) {
	if _, err := s.GetSourceForUpdate(ctx, 987654); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetSourceForUpdate of a missing source: %v, want ErrNotFound", err)
	}
	if _, err := s.GetSettingForUpdate(ctx, "test.for_update.missing"); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("GetSettingForUpdate of a missing key: %v, want ErrNotFound", err)
	}

	src := mkSource(t, ctx, s, "for_update_read")
	const key = "test.for_update"
	t.Cleanup(func() { _ = s.DeleteSetting(ctx, key) })
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		got, err := tx.GetSourceForUpdate(ctx, src.ID)
		if err != nil {
			return err
		}
		if got.Name != src.Name || got.Connection.TableName != src.Connection.TableName {
			t.Errorf("GetSourceForUpdate = %q %q, want %q %q", got.Name, got.Connection.TableName, src.Name, src.Connection.TableName)
		}
		if err := tx.UpsertSetting(ctx, key, "v1", "string", "server", "a test setting", true); err != nil {
			return err
		}
		setting, err := tx.GetSettingForUpdate(ctx, key)
		if err != nil {
			return err
		}
		want := models.SystemSetting{Key: key, Value: "v1", ValueType: "string", Category: "server", Description: "a test setting", IsSensitive: true}
		setting.CreatedAt, setting.UpdatedAt = time.Time{}, time.Time{}
		if *setting != want {
			t.Errorf("GetSettingForUpdate inside the writing transaction = %+v, want %+v", *setting, want)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
}

// RunForUpdateSerialized checks that GetSourceForUpdate and
// GetSettingForUpdate hold a second reader until the first transaction ends,
// and that the second reader then sees the first one's write. The setting key
// has no row when both start, so the lock must cover a missing key too.
func RunForUpdateSerialized(t *testing.T, s store.Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("Source", func(t *testing.T) {
		src := mkSource(t, ctx, s, "for_update_serialized")
		var seen string
		wantSecondWaits(ctx, t, s, func(ctx context.Context, tx store.StoreOps) error {
			locked, err := tx.GetSourceForUpdate(ctx, src.ID)
			if err != nil {
				return err
			}
			locked.Name = "renamed-under-lock"
			return tx.UpdateSource(ctx, locked)
		}, func(ctx context.Context, tx store.StoreOps) error {
			got, err := tx.GetSourceForUpdate(ctx, src.ID)
			if err == nil {
				seen = got.Name
			}
			return err
		})
		if seen != "renamed-under-lock" {
			t.Errorf("second reader saw %q, want the first transaction's write", seen)
		}
	})

	t.Run("NewSetting", func(t *testing.T) {
		const key = "test.for_update.serialized"
		t.Cleanup(func() { _ = s.DeleteSetting(ctx, key) })
		var seen string
		wantSecondWaits(ctx, t, s, func(ctx context.Context, tx store.StoreOps) error {
			if _, err := tx.GetSettingForUpdate(ctx, key); !errors.Is(err, models.ErrNotFound) {
				return fmt.Errorf("first read of a new key: %w", err)
			}
			return tx.UpsertSetting(ctx, key, "written-under-lock", "string", "server", "", false)
		}, func(ctx context.Context, tx store.StoreOps) error {
			got, err := tx.GetSettingForUpdate(ctx, key)
			if err == nil {
				seen = got.Value
			}
			return err
		})
		if seen != "written-under-lock" {
			t.Errorf("second reader saw %q, want the first transaction's write", seen)
		}
	})
}

// wantSecondWaits runs first in a transaction, starts second in another one,
// and fails unless second is still running 100ms later. It returns after both
// transactions ended.
func wantSecondWaits(ctx context.Context, t *testing.T, s store.Store, first, second func(context.Context, store.StoreOps) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	started := make(chan struct{})
	result := make(chan error, 1)
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		if err := first(ctx, tx); err != nil {
			return err
		}
		go func() {
			result <- s.WithTx(ctx, func(tx store.StoreOps) error {
				close(started)
				return second(ctx, tx)
			})
		}()
		select {
		case <-started:
		case err := <-result:
			return fmt.Errorf("second transaction failed to start: %w", err)
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case err := <-result:
			return fmt.Errorf("second read did not wait for the first transaction: %w", err)
		case <-time.After(100 * time.Millisecond):
			return nil
		}
	})
	if err != nil {
		t.Fatalf("first transaction: %v", err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("second transaction: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("second transaction did not finish after the first ended")
	}
}
