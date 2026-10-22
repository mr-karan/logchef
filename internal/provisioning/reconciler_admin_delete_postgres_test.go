package provisioning

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/victorialogs"
	"github.com/mr-karan/logchef/pkg/models"
)

type afterMemberRoleUpdate struct {
	store.StoreOps
	after func() error
}

func (s afterMemberRoleUpdate) UpdateTeamMemberRole(ctx context.Context, team models.TeamID, user models.UserID, role models.TeamRole) error {
	if err := s.StoreOps.UpdateTeamMemberRole(ctx, team, user, role); err != nil {
		return err
	}
	return s.after()
}

type afterSourceUnlink struct {
	store.StoreOps
	after func() error
}

func (s afterSourceUnlink) RemoveTeamSource(ctx context.Context, team models.TeamID, source models.SourceID) error {
	if err := s.StoreOps.RemoveTeamSource(ctx, team, source); err != nil {
		return err
	}
	return s.after()
}

type aroundTeamDeletion struct {
	store.StoreOps
	before func(models.TeamID) error
	after  func() error
}

func (s aroundTeamDeletion) DeleteTeam(ctx context.Context, team models.TeamID) error {
	if err := s.before(team); err != nil {
		return err
	}
	if err := s.StoreOps.DeleteTeam(ctx, team); err != nil {
		return err
	}
	return s.after()
}

func TestReconcilePostgresConcurrentAdminTeamDelete(t *testing.T) {
	db, dsn, observer := newProvisioningPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	source, err := sourceFromConfig(config.ProvisionSource{Name: "logs", SourceType: models.SourceTypeVictoriaLogs, Connection: map[string]any{"base_url": "http://127.0.0.1:1"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	cfg := &config.ProvisioningConfig{ManageTeams: true, Teams: []config.ProvisionTeam{{Name: "team", Members: []config.ProvisionMember{{Email: "member@example.com"}}}}}
	if err := Reconcile(ctx, cfg, db, nil, quietLogger(), nil); err != nil {
		t.Fatal(err)
	}
	team, err := db.GetTeamByName(ctx, "team")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Teams[0].Members[0].Role = "admin"
	cfg.Teams[0].Sources = []string{"logs"}
	result := make(chan error, 1)
	started := false
	wrapped := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps {
		return afterMemberRoleUpdate{StoreOps: tx, after: func() error {
			started = true
			go func() { result <- core.DeleteTeam(ctx, db, quietLogger(), team.ID) }()
			return waitForProvisioningContention(ctx, observer, applicationName(dsn), nil)
		}}
	}}
	reconcileErr := Reconcile(ctx, cfg, wrapped, nil, quietLogger(), nil)
	if !started {
		t.Fatalf("no member role changed: %v", reconcileErr)
	}
	deleteErr := <-result
	if reconcileErr != nil || deleteErr != nil {
		t.Fatalf("reconcile = %v; concurrent team delete = %v", reconcileErr, deleteErr)
	}
	if _, err := db.GetTeam(ctx, team.ID); !models.IsNotFound(err) {
		t.Fatalf("team remains after delete: %v", err)
	}
}

func TestReconcilePostgresConcurrentAdminSourceDelete(t *testing.T) {
	db, dsn, observer := newProvisioningPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	source, err := sourceFromConfig(config.ProvisionSource{Name: "logs", SourceType: models.SourceTypeVictoriaLogs, Connection: map[string]any{"base_url": "http://127.0.0.1:1"}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	cfg := &config.ProvisioningConfig{ManageTeams: true, Prune: true, Teams: []config.ProvisionTeam{{Name: "first", Sources: []string{"logs"}}, {Name: "second"}}}
	if err := Reconcile(ctx, cfg, db, nil, quietLogger(), nil); err != nil {
		t.Fatal(err)
	}
	cfg.Teams[0].Sources = nil
	cfg.Teams[1].Sources = []string{"logs"}
	service := datasource.NewService(db, quietLogger())
	service.Register(victorialogs.NewProvider(quietLogger()))
	result := make(chan error, 1)
	started := false
	wrapped := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps {
		return afterSourceUnlink{StoreOps: tx, after: func() error {
			started = true
			go func() { result <- service.DeleteSource(ctx, source.ID) }()
			return waitForProvisioningContention(ctx, observer, applicationName(dsn), nil)
		}}
	}}
	reconcileErr := Reconcile(ctx, cfg, wrapped, nil, quietLogger(), nil)
	if !started {
		t.Fatalf("no source unlinked: %v", reconcileErr)
	}
	deleteErr := <-result
	if reconcileErr != nil || deleteErr != nil {
		t.Fatalf("reconcile = %v; concurrent source delete = %v", reconcileErr, deleteErr)
	}
	if _, err := db.GetSource(ctx, source.ID); !models.IsNotFound(err) {
		t.Fatalf("source remains after delete: %v", err)
	}
}

func TestReconcilePostgresConcurrentCollectionDelete(t *testing.T) {
	db, dsn, observer := newProvisioningPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg := &config.ProvisioningConfig{ManageTeams: true, Prune: true, Teams: []config.ProvisionTeam{{Name: "first"}, {Name: "second"}}}
	if err := Reconcile(ctx, cfg, db, nil, quietLogger(), nil); err != nil {
		t.Fatal(err)
	}
	teams, err := db.ListManagedTeams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	owner := &models.User{Email: "owner@example.com", FullName: "Owner", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := db.CreateUser(ctx, owner); err != nil {
		t.Fatal(err)
	}
	collection, err := core.CreateCollection(ctx, db, quietLogger(), "shared", "", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	started := false
	wrapped := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps {
		return aroundTeamDeletion{StoreOps: tx, before: func(first models.TeamID) error {
			if started {
				return nil
			}
			other := teams[0].ID
			if other == first {
				other = teams[1].ID
			}
			// Opposite share and prune orders expose the cascade lock cycle.
			for _, id := range []models.TeamID{other, first} {
				if err := db.AddCollectionTeam(ctx, collection.ID, id, &owner.ID); err != nil {
					return err
				}
			}
			return nil
		}, after: func() error {
			if started {
				return nil
			}
			started = true
			go func() { result <- core.DeleteCollection(ctx, db, quietLogger(), collection.ID, owner.ID) }()
			return waitForProvisioningContention(ctx, observer, applicationName(dsn), nil)
		}}
	}}
	cfg.Teams = nil
	reconcileErr := Reconcile(ctx, cfg, wrapped, nil, quietLogger(), nil)
	if !started {
		t.Fatalf("no team pruned: %v", reconcileErr)
	}
	deleteErr := <-result
	if reconcileErr != nil || deleteErr != nil {
		t.Fatalf("reconcile = %v; concurrent collection delete = %v", reconcileErr, deleteErr)
	}
	if _, err := db.GetCollection(ctx, collection.ID); !models.IsNotFound(err) {
		t.Fatalf("collection remains after delete: %v", err)
	}
	for _, team := range teams {
		if _, err := db.GetTeam(ctx, team.ID); !models.IsNotFound(err) {
			t.Fatalf("team remains after prune: %v", err)
		}
	}
}

type afterSourceDeletion struct {
	store.StoreOps
	after func() error
}

func (s afterSourceDeletion) DeleteSource(ctx context.Context, source models.SourceID) error {
	if err := s.StoreOps.DeleteSource(ctx, source); err != nil {
		return err
	}
	return s.after()
}

func TestReconcilePostgresConcurrentAdminTeamSourceWrite(t *testing.T) {
	for _, operation := range []string{"add", "remove", "remove_missing"} {
		t.Run(operation, func(t *testing.T) {
			db, dsn, observer := newProvisioningPostgres(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			source, err := sourceFromConfig(config.ProvisionSource{Name: "logs", SourceType: models.SourceTypeVictoriaLogs, Connection: map[string]any{"base_url": "http://127.0.0.1:1"}}, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.CreateSource(ctx, source); err != nil {
				t.Fatal(err)
			}
			if err := db.SetSourceManaged(ctx, source.ID, true, ""); err != nil {
				t.Fatal(err)
			}
			cfg := &config.ProvisioningConfig{ManageTeams: true, Prune: true, Teams: []config.ProvisionTeam{{Name: "team"}}}
			if operation == "remove" {
				cfg.Teams[0].Sources = []string{"logs"}
			}
			if err := Reconcile(ctx, cfg, db, nil, quietLogger(), nil); err != nil {
				t.Fatal(err)
			}
			team, err := db.GetTeamByName(ctx, "team")
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			progress := make(chan struct{})
			started := false
			wrapped := observedStore{Store: db, observe: func(tx store.StoreOps) store.StoreOps {
				return afterSourceDeletion{StoreOps: tx, after: func() error {
					started = true
					go func() {
						if operation == "add" {
							result <- core.AddTeamSource(ctx, db, quietLogger(), team.ID, source.ID)
						} else {
							result <- core.RemoveTeamSource(ctx, db, quietLogger(), team.ID, source.ID)
						}
						close(progress)
					}()
					return waitForProvisioningContention(ctx, observer, applicationName(dsn), progress)
				}}
			}}
			cfg.ManageSources = true
			cfg.Teams = nil
			reconcileErr := Reconcile(ctx, cfg, wrapped, nil, quietLogger(), nil)
			if !started {
				t.Fatalf("no source pruned: %v", reconcileErr)
			}
			writeErr := <-result
			if reconcileErr != nil {
				t.Fatalf("reconcile = %v; concurrent %s = %v", reconcileErr, operation, writeErr)
			}
			if operation == "add" {
				if !errors.Is(writeErr, core.ErrTeamNotFound) {
					t.Fatalf("concurrent add = %v, want team not found after prune", writeErr)
				}
			} else if writeErr != nil {
				t.Fatalf("concurrent %s = %v", operation, writeErr)
			}
			if _, err := db.GetTeam(ctx, team.ID); !models.IsNotFound(err) {
				t.Fatalf("team remains after prune: %v", err)
			}
			if _, err := db.GetSource(ctx, source.ID); !models.IsNotFound(err) {
				t.Fatalf("source remains after prune: %v", err)
			}
		})
	}
}
