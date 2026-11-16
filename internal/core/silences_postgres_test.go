package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/core/access"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
	"github.com/mr-karan/logchef/pkg/models"
)

// countBarrier holds each silence count until a second count arrives or the
// wait runs out, so two creates read the count at the same time unless
// something orders them.
type countBarrier struct {
	store.Store
	mu      sync.Mutex
	arrived int
	both    chan struct{}
}

func (b *countBarrier) WithTx(ctx context.Context, fn func(tx store.StoreOps) error) error {
	return b.Store.WithTx(ctx, func(tx store.StoreOps) error { return fn(&barrierTx{StoreOps: tx, b: b}) })
}

type barrierTx struct {
	store.StoreOps
	b *countBarrier
}

func (tx *barrierTx) CountAlertSilences(ctx context.Context, sourceID models.SourceID) (int, error) {
	tx.b.mu.Lock()
	tx.b.arrived++
	if tx.b.arrived == 2 {
		close(tx.b.both)
	}
	tx.b.mu.Unlock()
	select {
	case <-tx.b.both:
	case <-time.After(2 * time.Second):
	}
	return tx.StoreOps.CountAlertSilences(ctx, sourceID)
}

// TestSilenceLimitHoldsUnderConcurrency: with 99 silences on a source, two
// creates at once leave exactly 100. Without the source row lock both read 99
// and both insert.
func TestSilenceLimitHoldsUnderConcurrency(t *testing.T) {
	ctx := context.Background()
	log := discardLogger()
	pg, err := postgres.New(ctx, postgres.Options{Logger: log, Config: config.PostgresConfig{DSN: postgrestest.DSN(t)}})
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	user := &models.User{Email: "silence-race@example.com", FullName: "On call", Role: models.UserRoleMember, Status: models.UserStatusActive}
	if err := pg.CreateUser(ctx, user); err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "silence-race", Connection: models.ConnectionInfo{Host: "ch:9000", Username: "default", Database: "default", TableName: "silence_race"}}
	if err := pg.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	team, err := CreateTeam(ctx, pg, log, "silence-race", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := AddTeamMember(ctx, pg, log, team.ID, user.ID, models.TeamRoleMember); err != nil {
		t.Fatal(err)
	}
	if err := pg.AddTeamSource(ctx, team.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	end := now.Add(time.Hour)
	for range models.MaxSilencesPerSource - 1 {
		s := &models.AlertSilence{SourceID: source.ID, Comment: "seed", ScheduleKind: models.SilenceOnce, StartsAt: &now, EndsAt: &end}
		if err := pg.CreateAlertSilence(ctx, s); err != nil {
			t.Fatal(err)
		}
	}

	db := &countBarrier{Store: pg, both: make(chan struct{})}
	req := &models.AlertSilenceRequest{SourceID: source.ID, Comment: "race", ScheduleKind: models.SilenceOnce, StartsAt: &now, EndsAt: &end}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = CreateSilenceForPrincipal(ctx, db, log, access.SessionPrincipal(user), req, now)
		})
	}
	wg.Wait()

	count, err := pg.CountAlertSilences(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	refused := 0
	for _, err := range errs {
		switch {
		case errors.Is(err, ErrInvalidSilence):
			refused++
		case err != nil:
			t.Errorf("create err = %v", err)
		}
	}
	if count != models.MaxSilencesPerSource || refused != 1 {
		t.Fatalf("silences = %d with %d create refused, want %d with one refused (errors %v)", count, refused, models.MaxSilencesPerSource, errs)
	}
}
