package provisioning

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

type observedStore struct {
	store.Store
	observe func(store.StoreOps) store.StoreOps
}

func (s observedStore) WithTx(ctx context.Context, fn func(store.StoreOps) error) error {
	return s.Store.WithTx(ctx, func(tx store.StoreOps) error { return fn(s.observe(tx)) })
}

type lookupFailure struct {
	store.StoreOps
	target  string
	creates int
}

func canceledLookupContext(parent context.Context) context.Context {
	ctx, cancel := context.WithCancel(parent)
	cancel()
	return ctx
}

func (s *lookupFailure) GetSourceByNameForProvisioning(ctx context.Context, name string) (*models.Source, error) {
	if s.target == "source" {
		ctx = canceledLookupContext(ctx)
	}
	return s.StoreOps.GetSourceByNameForProvisioning(ctx, name)
}

func (s *lookupFailure) GetTeamByName(ctx context.Context, name string) (*models.Team, error) {
	if s.target == "team" {
		ctx = canceledLookupContext(ctx)
	}
	return s.StoreOps.GetTeamByName(ctx, name)
}

func (s *lookupFailure) LockProvisioningUsers(ctx context.Context, emails, teamNames []string, prune bool) ([]*models.User, error) {
	if s.target == "user" {
		ctx = canceledLookupContext(ctx)
	}
	return s.StoreOps.LockProvisioningUsers(ctx, emails, teamNames, prune)
}

func (s *lookupFailure) CreateSource(ctx context.Context, src *models.Source) error {
	s.creates++
	return s.StoreOps.CreateSource(ctx, src)
}

func (s *lookupFailure) CreateTeam(ctx context.Context, team *models.Team) error {
	s.creates++
	return s.StoreOps.CreateTeam(ctx, team)
}

func (s *lookupFailure) CreateUser(ctx context.Context, user *models.User) error {
	s.creates++
	return s.StoreOps.CreateUser(ctx, user)
}

func TestReconcileLookupErrors(t *testing.T) {
	for _, target := range []string{"source", "team", "user"} {
		t.Run(target, func(t *testing.T) {
			db := newReconcileTestDB(t)
			cfg := &config.ProvisioningConfig{ManageTeams: true, Teams: []config.ProvisionTeam{{Name: "existing", Members: []config.ProvisionMember{{Email: "member@example.com"}}}}}
			if err := db.CreateTeam(context.Background(), &models.Team{Name: "existing"}); err != nil {
				t.Fatal(err)
			}
			if target == "source" {
				cfg.ManageTeams = false
				cfg.ManageSources = true
				cfg.Sources = []config.ProvisionSource{{Name: "source", Connection: map[string]any{"host": "127.0.0.1:1", "database": "default", "table_name": "logs"}}}
			}
			var observed *lookupFailure
			wrapped := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps {
				observed = &lookupFailure{StoreOps: tx, target: target}
				return observed
			}}
			err := Reconcile(context.Background(), cfg, wrapped, newTestDatasourceService(db), quietLogger(), nil)
			if !errors.Is(err, context.Canceled) {
				t.Errorf("lookup error = %v, want context.Canceled", err)
			}
			if observed.creates != 0 {
				t.Errorf("attempted %d creates after a failed %s lookup", observed.creates, target)
			}
		})
	}
}

func TestReconcileNormalizedEmailDoesNotFlap(t *testing.T) {
	db := newReconcileTestDB(t)
	cfg := &config.ProvisioningConfig{ManageTeams: true, Prune: true, Teams: []config.ProvisionTeam{{Name: "team", Members: []config.ProvisionMember{{Email: " Alice@Example.com ", Role: "editor"}}}}}
	ctx := context.Background()
	var first *models.TeamMember
	for run := 1; run <= 2; run++ {
		if err := Reconcile(ctx, cfg, db, newTestDatasourceService(db), quietLogger(), []string{" ALICE@EXAMPLE.COM "}); err != nil {
			t.Fatal(err)
		}
		user, err := db.GetUserByEmail(ctx, "alice@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if user.Role != models.UserRoleAdmin {
			t.Errorf("run %d: global role = %q, want admin", run, user.Role)
		}
		team, err := db.GetTeamByName(ctx, "team")
		if err != nil {
			t.Fatal(err)
		}
		member, err := db.GetTeamMember(ctx, team.ID, user.ID)
		if err != nil || member == nil {
			t.Fatalf("run %d: membership flapped: %v", run, err)
		}
		if member.Role != models.TeamRoleEditor {
			t.Errorf("run %d: role = %q", run, member.Role)
		}
		if first != nil && !first.CreatedAt.Equal(member.CreatedAt) {
			t.Fatal("membership was replaced")
		}
		first = member
	}
}

type unavailableUserLookup struct{ store.StoreOps }

func (s unavailableUserLookup) GetUser(ctx context.Context, id models.UserID) (*models.User, error) {
	return s.StoreOps.GetUser(canceledLookupContext(ctx), id)
}

func TestReconcilePrunesUsingMemberDetails(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()
	cfg := &config.ProvisioningConfig{ManageTeams: true, Prune: true, Teams: []config.ProvisionTeam{{Name: "team", Members: []config.ProvisionMember{{Email: "former@example.com"}}}}}
	if err := Reconcile(ctx, cfg, db, newTestDatasourceService(db), quietLogger(), nil); err != nil {
		t.Fatal(err)
	}
	cfg.Teams[0].Members = nil
	wrapped := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps { return unavailableUserLookup{tx} }}
	if err := Reconcile(ctx, cfg, wrapped, newTestDatasourceService(db), quietLogger(), nil); err != nil {
		t.Fatal(err)
	}
	team, err := db.GetTeamByName(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	members, err := db.ListTeamMembersWithDetails(ctx, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 0 {
		t.Fatalf("unavailable per-user lookup left %d stale memberships", len(members))
	}
}

func TestReconcileValidationDoesNotHoldWriteConnection(t *testing.T) {
	db := newReconcileTestDB(t)
	var once sync.Once
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		once.Do(func() {
			result <- db.CreateTeam(ctx, &models.Team{Name: "concurrent-write"})
		})
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	ds := datasource.NewService(db, quietLogger())
	ds.Register(victorialogs.NewProvider(quietLogger()))
	cfg := &config.ProvisioningConfig{ManageSources: true, Sources: []config.ProvisionSource{{Name: "remote", SourceType: models.SourceTypeVictoriaLogs, Connection: map[string]any{"base_url": server.URL}}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := Reconcile(ctx, cfg, db, ds, quietLogger(), nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("network validation blocked an independent metadata write: %v", err)
		}
	default:
		t.Fatal("datasource validation did not run")
	}
}

func TestReconcileRejectsNormalizedMemberEmails(t *testing.T) {
	for _, emails := range [][]string{{"A@example.com", " a@example.com "}, {"   "}} {
		t.Run(fmt.Sprint(emails), func(t *testing.T) {
			cfg := &config.ProvisioningConfig{ManageTeams: true, Teams: []config.ProvisionTeam{{Name: "team"}}}
			for _, email := range emails {
				cfg.Teams[0].Members = append(cfg.Teams[0].Members, config.ProvisionMember{Email: email})
			}
			db := newReconcileTestDB(t)
			err := Reconcile(context.Background(), cfg, db, nil, quietLogger(), nil)
			if err == nil || (!strings.Contains(err.Error(), "duplicate email") && !strings.Contains(err.Error(), "email is required")) {
				t.Fatalf("validation = %v", err)
			}
		})
	}
}

func TestReconcileSharedMember(t *testing.T) {
	db := newReconcileTestDB(t)
	ctx := context.Background()
	cfg := &config.ProvisioningConfig{ManageTeams: true, Prune: true, Teams: []config.ProvisionTeam{
		{Name: "first", Members: []config.ProvisionMember{{Email: " Shared@example.com ", Role: "admin"}}},
		{Name: "second", Members: []config.ProvisionMember{{Email: "shared@EXAMPLE.com", Role: "editor"}}},
	}}
	for range 2 {
		if err := Reconcile(ctx, cfg, db, nil, quietLogger(), nil); err != nil {
			t.Fatal(err)
		}
		users, err := db.ListUsers(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(users) != 1 || !users[0].Managed || users[0].Email != "shared@example.com" {
			t.Fatalf("expected one normalized managed user, got %d users", len(users))
		}
		for i, role := range []models.TeamRole{models.TeamRoleAdmin, models.TeamRoleEditor} {
			team, err := db.GetTeamByName(ctx, cfg.Teams[i].Name)
			if err != nil {
				t.Fatal(err)
			}
			members, err := db.ListTeamMembersWithDetails(ctx, team.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(members) != 1 || members[0].UserID != users[0].ID || members[0].Role != role {
				t.Fatalf("team %q does not have the shared user with role %q", team.Name, role)
			}
		}
	}
}
