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

func testConcurrentAdminRemovals(t *testing.T, s store.Store) {
	for _, operation := range []string{"demote", "deactivate", "delete"} {
		t.Run(operation, func(t *testing.T) {
			testConcurrentAdminRemoval(t, s, operation)
		})
	}
}

func testConcurrentAdminRemoval(t *testing.T, s store.Store, operation string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admins := make([]*models.User, 2)
	for i := range admins {
		admins[i] = mkUser(t, ctx, s, fmt.Sprintf("%s-%d@test.dev", operation, i))
		admins[i].Role = models.UserRoleAdmin
		if err := s.UpdateUser(ctx, admins[i]); err != nil {
			t.Fatal(err)
		}
		id := admins[i].ID
		t.Cleanup(func() { _ = s.DeleteUser(context.Background(), id) })
	}

	lastAdmin := errors.New("last active admin")
	remove := func(tx store.StoreOps, user *models.User) error {
		switch operation {
		case "demote":
			user.Role = models.UserRoleMember
		case "deactivate":
			user.Status = models.UserStatusInactive
		case "delete":
			return tx.DeleteUser(ctx, user.ID)
		}
		return tx.UpdateUser(ctx, user)
	}
	counted := make(chan struct{})
	release := make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- s.WithTx(ctx, func(tx store.StoreOps) error {
			count, err := tx.CountAdminUsers(ctx)
			close(counted)
			if err != nil {
				return err
			}
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			if count <= 1 {
				return lastAdmin
			}
			return remove(tx, admins[0])
		})
	}()
	select {
	case <-counted:
	case err := <-first:
		t.Fatalf("first transaction did not reach the admin count: %v", err)
	}
	second := make(chan error, 1)
	go func() {
		second <- s.WithTx(ctx, func(tx store.StoreOps) error {
			count, err := tx.CountAdminUsers(ctx)
			if err != nil {
				return err
			}
			if count <= 1 {
				return lastAdmin
			}
			return remove(tx, admins[1])
		})
	}()
	var secondErr error
	select {
	case secondErr = <-second:
		close(release)
	case <-time.After(200 * time.Millisecond):
		close(release)
		secondErr = <-second
	}
	if err := <-first; err != nil {
		t.Errorf("first removal: %v", err)
	}
	if !errors.Is(secondErr, lastAdmin) {
		t.Errorf("second removal = %v, want last-admin rejection", secondErr)
	}
	if count, err := s.CountAdminUsers(ctx); err != nil || count != 1 {
		t.Errorf("active admins = %d, error = %v, want 1", count, err)
	}
}
