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

// testTeamForUpdate checks that GetTeamForUpdate returns the stored team,
// sees its own transaction's write, and reports a missing team.
func testTeamForUpdate(t *testing.T, ctx context.Context, s store.Store) {
	if _, err := s.GetTeamForUpdate(ctx, 987654); !models.IsNotFound(err) {
		t.Errorf("GetTeamForUpdate of a missing team: %v, want not found", err)
	}
	team := &models.Team{Name: "for-update-team", Description: "before"}
	if err := s.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		got, err := tx.GetTeamForUpdate(ctx, team.ID)
		if err != nil {
			return err
		}
		if got.Name != team.Name || got.Description != "before" {
			t.Errorf("GetTeamForUpdate = %q %q", got.Name, got.Description)
		}
		got.Description = "after"
		if err := tx.UpdateTeam(ctx, got); err != nil {
			return err
		}
		again, err := tx.GetTeamForUpdate(ctx, team.ID)
		if err != nil {
			return err
		}
		if again.Description != "after" {
			t.Errorf("GetTeamForUpdate inside the writing transaction = %q, want after", again.Description)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}

	user := mkUser(t, ctx, s, "for-update-member@test.dev")
	if m, err := s.GetTeamMemberForUpdate(ctx, team.ID, user.ID); err != nil || m != nil {
		t.Errorf("GetTeamMemberForUpdate of a non-member = %+v, %v; want nil, nil", m, err)
	}
	if err := s.AddTeamMember(ctx, team.ID, user.ID, models.TeamRoleEditor); err != nil {
		t.Fatalf("AddTeamMember: %v", err)
	}
	err = s.WithTx(ctx, func(tx store.StoreOps) error {
		m, err := tx.GetTeamMemberForUpdate(ctx, team.ID, user.ID)
		if err != nil {
			return err
		}
		if m == nil || m.TeamID != team.ID || m.UserID != user.ID || m.Role != models.TeamRoleEditor {
			t.Errorf("GetTeamMemberForUpdate = %+v, want an editor of team %d", m, team.ID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
}

// RunTeamForUpdateSerialized checks that GetTeamForUpdate holds a second
// reader until the first transaction ends, and that the second reader then
// sees the first one's write.
func RunTeamForUpdateSerialized(t *testing.T, s store.Store) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	team := &models.Team{Name: "for-update-serialized"}
	if err := s.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	started := make(chan struct{})
	seen := make(chan string, 1)
	result := make(chan error, 1)
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		locked, err := tx.GetTeamForUpdate(ctx, team.ID)
		if err != nil {
			return err
		}
		locked.Description = "written-under-lock"
		if err := tx.UpdateTeam(ctx, locked); err != nil {
			return err
		}
		go func() {
			result <- s.WithTx(ctx, func(second store.StoreOps) error {
				close(started)
				got, err := second.GetTeamForUpdate(ctx, team.ID)
				if err == nil {
					seen <- got.Description
				}
				return err
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
			return errors.Join(errors.New("second read did not wait for the first transaction"), err)
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
	if got := <-seen; got != "written-under-lock" {
		t.Errorf("second reader saw %q, want the first transaction's write", got)
	}
}

// testIdentityWriteCounts checks that InsertTeamMember and DeleteAPIToken say
// whether the call changed a row, so a request that lost a race records
// nothing.
func testIdentityWriteCounts(t *testing.T, ctx context.Context, s store.Store) {
	team := &models.Team{Name: "write-counts"}
	if err := s.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	user := mkUser(t, ctx, s, "write-counts@test.dev")
	if inserted, err := s.InsertTeamMember(ctx, team.ID, user.ID, models.TeamRoleEditor); err != nil || !inserted {
		t.Fatalf("first InsertTeamMember = %v, %v; want true", inserted, err)
	}
	if inserted, err := s.InsertTeamMember(ctx, team.ID, user.ID, models.TeamRoleMember); err != nil || inserted {
		t.Errorf("second InsertTeamMember = %v, %v; want false", inserted, err)
	}
	if m, err := s.GetTeamMember(ctx, team.ID, user.ID); err != nil || m == nil || m.Role != models.TeamRoleEditor {
		t.Errorf("member after a lost insert = %+v, %v; want the first role kept", m, err)
	}

	tokenID, err := s.CreateAPIToken(ctx, &models.APIToken{UserID: user.ID, Name: "write-counts", TokenHash: "write-counts-hash", Prefix: "logchef_x"})
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	if err := s.DeleteAPIToken(ctx, tokenID, user.ID); err != nil {
		t.Fatalf("first DeleteAPIToken: %v", err)
	}
	if err := s.DeleteAPIToken(ctx, tokenID, user.ID); !errors.Is(err, models.ErrNotFound) {
		t.Errorf("second DeleteAPIToken = %v, want ErrNotFound", err)
	}
}
