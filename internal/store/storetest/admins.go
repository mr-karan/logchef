package storetest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

const adminRemovalRounds = 25

func testConcurrentAdminRemovals(t *testing.T, s store.Store) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	removals := map[string]func(ctx context.Context, id models.UserID) error{
		"demote": func(ctx context.Context, id models.UserID) error {
			return core.UpdateUser(ctx, s, log, id, models.User{Role: models.UserRoleMember})
		},
		"deactivate": func(ctx context.Context, id models.UserID) error {
			return core.UpdateUser(ctx, s, log, id, models.User{Status: models.UserStatusInactive})
		},
		"delete": func(ctx context.Context, id models.UserID) error {
			return core.DeleteUser(ctx, s, log, id)
		},
	}
	for name, remove := range removals {
		t.Run(name, func(t *testing.T) {
			for round := range adminRemovalRounds {
				testConcurrentAdminRemoval(t, s, fmt.Sprintf("%s-%d", name, round), remove)
			}
		})
	}
}

func testConcurrentAdminRemoval(t *testing.T, s store.Store, label string, remove func(context.Context, models.UserID) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	before, err := s.CountAdminUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	admins := make([]*models.User, 2)
	defer func() {
		for _, admin := range admins {
			if admin != nil {
				_ = s.DeleteUser(ctx, admin.ID)
			}
		}
	}()
	for i := range admins {
		user := &models.User{Email: fmt.Sprintf("%s-%d@test.dev", label, i), FullName: "Admin", Role: models.UserRoleAdmin, Status: models.UserStatusActive}
		if err := s.CreateUser(ctx, user); err != nil {
			t.Fatal(err)
		}
		admins[i] = user
	}

	start := make(chan struct{})
	results := make(chan error, len(admins))
	for _, admin := range admins {
		go func() {
			<-start
			results <- remove(ctx, admin.ID)
		}()
	}
	close(start)
	removed := 0
	for range admins {
		switch err := <-results; {
		case err == nil:
			removed++
		case !errors.Is(err, core.ErrCannotDeleteLastAdmin):
			t.Fatalf("%s: unexpected error: %v", label, err)
		}
	}

	after, err := s.CountAdminUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if want := max(before, 1); after != want || after != before+len(admins)-removed {
		t.Fatalf("%s: active admins before = %d, removed = %d, after = %d, want %d", label, before, removed, after, want)
	}
}
