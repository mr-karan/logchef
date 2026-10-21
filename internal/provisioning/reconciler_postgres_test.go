package provisioning

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/pkg/models"
)

func newProvisioningPostgres(t *testing.T) (*postgres.Store, string, *pgx.Conn) {
	t.Helper()
	dsn := os.Getenv("LOGCHEF_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("LOGCHEF_TEST_POSTGRES_DSN not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("provisioning_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		_ = conn.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := conn.Exec(ctx, "DROP SCHEMA "+identifier+" CASCADE"); err != nil {
			t.Error(err)
		}
		_ = conn.Close(ctx)
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	query.Set("application_name", schema)
	parsed.RawQuery = query.Encode()
	dsn = parsed.String()
	db, err := postgres.New(ctx, postgres.Options{Logger: quietLogger(), Config: config.PostgresConfig{DSN: dsn}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dsn, conn
}

type afterMembershipRemoval struct {
	store.StoreOps
	after func() error
}

func (s afterMembershipRemoval) RemoveTeamMember(ctx context.Context, team models.TeamID, user models.UserID) error {
	if err := s.StoreOps.RemoveTeamMember(ctx, team, user); err != nil {
		return err
	}
	return s.after()
}

func TestReconcilePostgresConcurrentDelete(t *testing.T) {
	for iteration := range 5 {
		t.Run(fmt.Sprint(iteration), func(t *testing.T) {
			db, dsn, observer := newProvisioningPostgres(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cfg := &config.ProvisioningConfig{ManageTeams: true, Prune: true, Teams: []config.ProvisionTeam{
				{Name: "old", Members: []config.ProvisionMember{{Email: "moving@example.com"}}},
				{Name: "new"},
			}}
			if err := Reconcile(ctx, cfg, db, nil, quietLogger(), nil); err != nil {
				t.Fatal(err)
			}
			user, err := db.GetUserByEmail(ctx, "moving@example.com")
			if err != nil {
				t.Fatal(err)
			}
			cfg.Teams[0].Members = nil
			cfg.Teams[1].Members = []config.ProvisionMember{{Email: user.Email}}
			deleting, err := postgres.New(ctx, postgres.Options{Logger: quietLogger(), Config: config.PostgresConfig{DSN: dsn}})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = deleting.Close() }()
			deleteResult := make(chan error, 1)
			started := false
			wrapped := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps {
				return afterMembershipRemoval{StoreOps: tx, after: func() error {
					started = true
					go func() { deleteResult <- core.DeleteUser(ctx, deleting, quietLogger(), user.ID) }()
					return waitForProvisioningContention(ctx, observer, applicationName(dsn), nil)
				}}
			}}
			reconcileErr := Reconcile(ctx, cfg, wrapped, nil, quietLogger(), nil)
			if !started {
				t.Fatal("reconciliation did not remove membership")
			}
			deleteErr := <-deleteResult
			if reconcileErr != nil || deleteErr != nil {
				t.Fatalf("reconcile = %v; concurrent delete = %v", reconcileErr, deleteErr)
			}
			if _, err := db.GetUser(ctx, user.ID); !models.IsNotFound(err) {
				t.Fatalf("user remains after delete: %v", err)
			}
		})
	}
}

func applicationName(dsn string) string {
	parsed, _ := url.Parse(dsn)
	return parsed.Query().Get("application_name")
}

func waitForProvisioningContention(ctx context.Context, observer *pgx.Conn, name string, progress <-chan struct{}) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := observer.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname = current_database() AND application_name = $1 AND wait_event_type = 'Lock')`, name).Scan(&blocked); err != nil {
			return err
		}
		if blocked {
			return nil
		}
		select {
		case <-progress:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

type afterUserManaged struct {
	store.StoreOps
	after func() error
}

func (s afterUserManaged) SetUserManaged(ctx context.Context, id models.UserID, managed bool) error {
	if err := s.StoreOps.SetUserManaged(ctx, id, managed); err != nil {
		return err
	}
	return s.after()
}

func TestReconcilePostgresConcurrentAdoption(t *testing.T) {
	db, dsn, observer := newProvisioningPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, name := range []string{"first", "second"} {
		cfg := &config.ProvisioningConfig{ManageTeams: true, Teams: []config.ProvisionTeam{{Name: name}}}
		if err := Reconcile(ctx, cfg, db, nil, quietLogger(), nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, email := range []string{"a@example.com", "b@example.com"} {
		if err := db.CreateUser(ctx, &models.User{Email: email, FullName: email, Role: models.UserRoleMember}); err != nil {
			t.Fatal(err)
		}
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	name := applicationName(dsn) + "_second"
	query.Set("application_name", name)
	parsed.RawQuery = query.Encode()
	second, err := postgres.New(ctx, postgres.Options{Logger: quietLogger(), Config: config.PostgresConfig{DSN: parsed.String()}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	firstConfig := &config.ProvisioningConfig{ManageTeams: true, Teams: []config.ProvisionTeam{{Name: "first", Members: []config.ProvisionMember{{Email: "a@example.com"}, {Email: "b@example.com"}}}}}
	secondConfig := &config.ProvisioningConfig{ManageTeams: true, Teams: []config.ProvisionTeam{{Name: "second", Members: []config.ProvisionMember{{Email: "b@example.com"}, {Email: "a@example.com"}}}}}
	progress := make(chan struct{}, 1)
	secondObserved := observedStore{Store: second, observe: func(tx store.StoreOps) store.StoreOps {
		return afterUserManaged{StoreOps: tx, after: func() error {
			select {
			case progress <- struct{}{}:
			default:
			}
			return nil
		}}
	}}
	result := make(chan error, 1)
	started := false
	firstObserved := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps {
		return afterUserManaged{StoreOps: tx, after: func() error {
			if started {
				return nil
			}
			started = true
			go func() { result <- Reconcile(ctx, secondConfig, secondObserved, nil, quietLogger(), nil) }()
			return waitForProvisioningContention(ctx, observer, name, progress)
		}}
	}}
	firstErr := Reconcile(ctx, firstConfig, firstObserved, nil, quietLogger(), nil)
	if !started {
		t.Fatal("reconciliation did not adopt users")
	}
	secondErr := <-result
	if firstErr != nil || secondErr != nil {
		t.Fatalf("first reconciliation = %v; second reconciliation = %v", firstErr, secondErr)
	}
	for _, name := range []string{"first", "second"} {
		team, err := db.GetTeamByName(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		members, err := db.ListTeamMembersWithDetails(ctx, team.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(members) != 2 {
			t.Fatalf("team %q has %d members, want 2", name, len(members))
		}
	}
}
