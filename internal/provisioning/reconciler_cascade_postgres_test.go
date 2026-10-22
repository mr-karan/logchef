package provisioning

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/pkg/models"
)

type beforeManagedTeams struct {
	store.StoreOps
	before func() error
}

func (s beforeManagedTeams) ListManagedTeams(ctx context.Context) ([]*models.Team, error) {
	if err := s.before(); err != nil {
		return nil, err
	}
	return s.StoreOps.ListManagedTeams(ctx)
}

type aroundSourceDeletion struct {
	store.StoreOps
	before func(models.SourceID) error
	after  func() error
}

func (s aroundSourceDeletion) DeleteSource(ctx context.Context, id models.SourceID) error {
	if err := s.before(id); err != nil {
		return err
	}
	if err := s.StoreOps.DeleteSource(ctx, id); err != nil {
		return err
	}
	return s.after()
}

func TestReconcilePostgresSourcePruneConcurrentDelete(t *testing.T) {
	for _, accountType := range []models.UserAccountType{models.UserAccountTypeHuman, models.UserAccountTypeService} {
		t.Run(string(accountType), func(t *testing.T) {
			db, dsn, observer := newProvisioningPostgres(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			creator := &models.User{Email: "creator@example.com", FullName: "Alert creator", Role: models.UserRoleMember, Status: models.UserStatusActive, AccountType: accountType}
			if err := db.CreateUser(ctx, creator); err != nil {
				t.Fatal(err)
			}
			sources := make([]*models.Source, 0, 2)
			for _, name := range []string{"first", "second"} {
				source, err := sourceFromConfig(config.ProvisionSource{Name: name, SourceType: models.SourceTypeVictoriaLogs, Connection: map[string]any{"base_url": "http://127.0.0.1:1/" + name}}, 0)
				if err != nil {
					t.Fatal(err)
				}
				if err := db.CreateSource(ctx, source); err != nil {
					t.Fatal(err)
				}
				if err := db.SetSourceManaged(ctx, source.ID, true, ""); err != nil {
					t.Fatal(err)
				}
				sources = append(sources, source)
			}
			result := make(chan error, 1)
			started := false
			wrapped := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps {
				return aroundSourceDeletion{StoreOps: tx, before: func(first models.SourceID) error {
					if started {
						return nil
					}
					other := sources[0].ID
					if other == first {
						other = sources[1].ID
					}
					// Opposite alert and source orders expose the foreign-key cascade cycle.
					for i, sourceID := range []models.SourceID{other, first} {
						alert := &models.Alert{SourceID: sourceID, Name: fmt.Sprint("alert-", i), QueryLanguage: models.QueryLanguageLogsQL, EditorMode: models.AlertEditorModeNative, Query: "SELECT 1", ThresholdOperator: models.AlertThresholdGreaterThan, Severity: models.AlertSeverityWarning, CreatedBy: &creator.ID}
						if err := db.CreateAlert(ctx, alert); err != nil {
							return err
						}
					}
					return nil
				}, after: func() error {
					if started {
						return nil
					}
					started = true
					go func() {
						if accountType == models.UserAccountTypeService {
							result <- core.DeleteServiceAccount(ctx, db, quietLogger(), creator.ID)
						} else {
							result <- core.DeleteUser(ctx, db, quietLogger(), creator.ID)
						}
					}()
					return waitForProvisioningContention(ctx, observer, applicationName(dsn), nil)
				}}
			}}
			reconcileErr := Reconcile(ctx, &config.ProvisioningConfig{ManageSources: true, Prune: true}, wrapped, nil, quietLogger(), nil)
			if !started {
				t.Fatalf("no source pruned: %v", reconcileErr)
			}
			deleteErr := <-result
			if reconcileErr != nil || deleteErr != nil {
				t.Fatalf("reconcile = %v; concurrent delete = %v", reconcileErr, deleteErr)
			}
			for _, source := range sources {
				if _, err := db.GetSource(ctx, source.ID); !models.IsNotFound(err) {
					t.Fatalf("source remains after prune: %v", err)
				}
				alerts, err := db.ListAlertsBySource(ctx, source.ID)
				if err != nil || len(alerts) != 0 {
					t.Fatalf("alerts remain after prune: count = %d; error = %v", len(alerts), err)
				}
			}
			if _, err := db.GetUser(ctx, creator.ID); !models.IsNotFound(err) {
				t.Fatalf("creator remains after delete: %v", err)
			}
		})
	}
}

func TestReconcilePostgresLateMemberConcurrentDelete(t *testing.T) {
	db, dsn, observer := newProvisioningPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg := &config.ProvisioningConfig{ManageTeams: true, Prune: true, Teams: []config.ProvisionTeam{{Name: "first"}, {Name: "second"}}}
	if err := Reconcile(ctx, cfg, db, nil, quietLogger(), nil); err != nil {
		t.Fatal(err)
	}
	user := &models.User{Email: "inserted@example.com", FullName: "Late member", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	started := false
	wrapped := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps {
		inserted := beforeManagedTeams{StoreOps: tx, before: func() error {
			for _, name := range []string{"second", "first"} {
				team, err := db.GetTeamByName(ctx, name)
				if err != nil {
					return err
				}
				if err := core.AddTeamMember(ctx, db, quietLogger(), team.ID, user.ID, models.TeamRoleMember); err != nil {
					return err
				}
			}
			return nil
		}}
		return afterMembershipRemoval{StoreOps: inserted, after: func() error {
			if started {
				return nil
			}
			started = true
			go func() { result <- core.DeleteUser(ctx, db, quietLogger(), user.ID) }()
			return waitForProvisioningContention(ctx, observer, applicationName(dsn), nil)
		}}
	}}
	reconcileErr := Reconcile(ctx, cfg, wrapped, nil, quietLogger(), nil)
	if !started {
		t.Fatalf("no membership removed: %v", reconcileErr)
	}
	deleteErr := <-result
	if reconcileErr != nil || deleteErr != nil {
		t.Fatalf("reconcile = %v; concurrent delete = %v", reconcileErr, deleteErr)
	}
	if _, err := db.GetUser(ctx, user.ID); !models.IsNotFound(err) {
		t.Fatalf("user remains after delete: %v", err)
	}
	for _, teamConfig := range cfg.Teams {
		team, err := db.GetTeamByName(ctx, teamConfig.Name)
		if err != nil {
			t.Fatal(err)
		}
		members, err := db.ListTeamMembersWithDetails(ctx, team.ID)
		if err != nil || len(members) != 0 {
			t.Fatalf("members remain after prune: count = %d; error = %v", len(members), err)
		}
	}
}

func TestReconcilePostgresConcurrentLoginAndMembershipWrites(t *testing.T) {
	db, dsn, observer := newProvisioningPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	user := &models.User{Email: "login@example.com", FullName: "Login user", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	addedTeam := &models.Team{Name: "added"}
	removedTeam := &models.Team{Name: "removed"}
	for _, team := range []*models.Team{addedTeam, removedTeam} {
		if err := db.CreateTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.AddTeamMember(ctx, removedTeam.ID, user.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	result := make(chan error, 1)
	started := false
	wrapped := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps {
		return afterUserManaged{StoreOps: tx, after: func() error {
			started = true
			go func() { result <- db.UpdateUserLastLogin(ctx, user.ID, stamp) }()
			if err := core.AddTeamMember(ctx, db, quietLogger(), addedTeam.ID, user.ID, models.TeamRoleEditor); err != nil {
				return err
			}
			if err := core.RemoveTeamMember(ctx, db, quietLogger(), removedTeam.ID, user.ID); err != nil {
				return err
			}
			return waitForProvisioningContention(ctx, observer, applicationName(dsn), nil)
		}}
	}}
	cfg := &config.ProvisioningConfig{ManageTeams: true, Prune: true, Teams: []config.ProvisionTeam{{Name: "managed", Members: []config.ProvisionMember{{Email: user.Email}}}}}
	reconcileErr := Reconcile(ctx, cfg, wrapped, nil, quietLogger(), nil)
	if !started {
		t.Fatalf("no user adopted: %v", reconcileErr)
	}
	loginErr := <-result
	if reconcileErr != nil || loginErr != nil {
		t.Fatalf("reconcile = %v; concurrent login = %v", reconcileErr, loginErr)
	}
	current, err := db.GetUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !current.Managed || current.LastLoginAt == nil || !current.LastLoginAt.Equal(stamp) {
		t.Fatalf("adoption or login lost: managed = %v; last login = %v", current.Managed, current.LastLoginAt)
	}
	member, err := db.GetTeamMember(ctx, addedTeam.ID, user.ID)
	if err != nil || member == nil || member.Role != models.TeamRoleEditor {
		t.Fatalf("concurrent membership lost: member = %v; error = %v", member, err)
	}
	member, err = db.GetTeamMember(ctx, removedTeam.ID, user.ID)
	if err != nil || member != nil {
		t.Fatalf("concurrent membership removal lost: member = %v; error = %v", member, err)
	}
}
