package postgres_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestAdminDemotionDoesNotDeadlockWithProvisioning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dsn := resetSchema(t)
	s := opener(dsn)(t)
	monitor := connect(t, ctx, dsn)
	a := createUser(t, ctx, s, "provision-a@test.dev", models.UserRoleAdmin)
	b := createUser(t, ctx, s, "provision-b@test.dev", models.UserRoleAdmin)

	heldB := make(chan struct{})
	proceed := make(chan struct{})
	provisioned := make(chan error, 1)
	go func() {
		provisioned <- s.WithTx(ctx, func(tx store.StoreOps) error {
			if err := tx.SetUserManaged(ctx, b.ID, true); err != nil {
				return err
			}
			close(heldB)
			select {
			case <-proceed:
			case <-ctx.Done():
				return ctx.Err()
			}
			return tx.SetUserManaged(ctx, a.ID, true)
		})
	}()
	select {
	case <-heldB:
	case err := <-provisioned:
		t.Fatalf("provisioning ended before holding the second admin: %v", err)
	}

	demoted := make(chan error, 1)
	demoteDone := make(chan struct{})
	go func() {
		demoted <- core.UpdateUser(ctx, s, discardLogger(), access.SystemPrincipal(), a.ID, models.User{Role: models.UserRoleMember})
		close(demoteDone)
	}()
	awaitLockWaitOrDone(t, ctx, monitor, demoteDone)
	close(proceed)

	if err := <-provisioned; err != nil {
		t.Errorf("provisioning: %v", err)
	}
	if err := <-demoted; err != nil {
		t.Errorf("demotion: %v", err)
	}
	if got, err := s.GetUser(ctx, a.ID); err != nil || got.Role != models.UserRoleMember || !got.Managed {
		t.Errorf("user a = %+v, error = %v, want a managed member", got, err)
	}
}

func TestConcurrentEditCannotReactivateUser(t *testing.T) {
	edits := map[string]func(ctx context.Context, s store.Store, id models.UserID) error{
		"name edit": func(ctx context.Context, s store.Store, id models.UserID) error {
			return core.UpdateUser(ctx, s, discardLogger(), access.SystemPrincipal(), id, models.User{FullName: "Renamed User"})
		},
		"last login": func(ctx context.Context, s store.Store, id models.UserID) error {
			return s.UpdateUserLastLogin(ctx, id, time.Now().UTC())
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			dsn := resetSchema(t)
			s := opener(dsn)(t)
			monitor := connect(t, ctx, dsn)
			user := createUser(t, ctx, s, "reactivate@test.dev", models.UserRoleMember)

			deactivation, err := connect(t, ctx, dsn).Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = deactivation.Rollback(context.Background()) }()
			if _, err := deactivation.Exec(ctx, `UPDATE users SET status = 'inactive', updated_at = now() WHERE id = $1`, int64(user.ID)); err != nil {
				t.Fatal(err)
			}

			edited := make(chan error, 1)
			editDone := make(chan struct{})
			go func() {
				edited <- edit(ctx, s, user.ID)
				close(editDone)
			}()
			awaitLockWaitOrDone(t, ctx, monitor, editDone)
			if err := deactivation.Commit(ctx); err != nil {
				t.Fatal(err)
			}

			if err := <-edited; err != nil {
				t.Fatalf("edit: %v", err)
			}
			if got, err := s.GetUser(ctx, user.ID); err != nil || got.Status != models.UserStatusInactive {
				t.Errorf("user = %+v, error = %v, want inactive", got, err)
			}
		})
	}
}

func createUser(t *testing.T, ctx context.Context, s store.Store, email string, role models.UserRole) *models.User {
	t.Helper()
	u := &models.User{Email: email, FullName: "Test User", Role: role, Status: models.UserStatusActive}
	if err := s.CreateUser(ctx, u); err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	return u
}

func connect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(context.WithoutCancel(ctx)) })
	return conn
}

// awaitLockWaitOrDone returns once another backend waits on a lock or done closes.
func awaitLockWaitOrDone(t *testing.T, ctx context.Context, monitor *pgx.Conn, done <-chan struct{}) {
	t.Helper()
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			t.Fatal("timed out waiting for a lock wait")
		case <-time.After(10 * time.Millisecond):
		}
		var waiting bool
		err := monitor.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()
		)`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAdminTransitionUsesLockedState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dsn := resetSchema(t)
	s := opener(dsn)(t)
	monitor := connect(t, ctx, dsn)
	user := createUser(t, ctx, s, "admin-transition@test.dev", models.UserRoleAdmin)

	deactivation, err := connect(t, ctx, dsn).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deactivation.Rollback(context.Background()) }()
	if _, err := deactivation.Exec(ctx, `UPDATE users SET status = 'inactive', updated_at = now() WHERE id = $1`, int64(user.ID)); err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		principal := access.APITokenPrincipal(nil, &models.APIToken{Scopes: []models.TokenScope{models.TokenScopeUsersWrite}})
		result <- core.UpdateUser(ctx, s, discardLogger(), principal, user.ID, models.User{Status: models.UserStatusActive})
		close(done)
	}()
	awaitLockWaitOrDone(t, ctx, monitor, done)
	if err := deactivation.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, core.ErrAdminGrantForbidden) {
		t.Fatalf("reactivation error = %v, want admin grant rejection", err)
	}
	stored, err := s.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != models.UserStatusInactive {
		t.Fatalf("stored status = %s, want inactive", stored.Status)
	}
}
