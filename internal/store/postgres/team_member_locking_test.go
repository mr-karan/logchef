package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/core"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
	"github.com/mr-karan/logchef/pkg/models"
)

// An add that loses the insert race to a concurrent add records the winner's
// membership as its before, not a new member with the winner's role.
func TestTeamMemberAddThatLosesTheInsertRecordsTheWinner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dsn := postgrestest.DSN(t)
	s := opener(dsn)(t)
	monitor := connect(t, ctx, dsn)
	user := createUser(t, ctx, s, "raced-member@test.dev", models.UserRoleMember)
	team := &models.Team{Name: "raced-team"}
	if err := s.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}

	type added struct {
		before, after *models.TeamMember
		err           error
	}
	result := make(chan added, 1)
	done := make(chan struct{})
	err := s.WithTx(ctx, func(tx store.StoreOps) error {
		if inserted, err := tx.InsertTeamMember(ctx, team.ID, user.ID, models.TeamRoleAdmin); err != nil || !inserted {
			t.Fatalf("winner insert = %v, %v", inserted, err)
		}
		go func() {
			before, after, err := core.AddTeamMember(ctx, s, discardLogger(), team.ID, user.ID, models.TeamRoleMember)
			result <- added{before, after, err}
			close(done)
		}()
		// The loser's insert waits on the winner's uncommitted row.
		awaitLockWaitOrDone(t, ctx, monitor, done)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got := <-result
	if got.err != nil {
		t.Fatalf("losing add: %v", got.err)
	}
	if got.before == nil || got.before.Role != models.TeamRoleAdmin {
		t.Errorf("losing add before = %+v, want the winner's admin membership", got.before)
	}
	if got.after == nil || got.after.Role != models.TeamRoleMember {
		t.Errorf("losing add after = %+v, want its own member role", got.after)
	}
}
