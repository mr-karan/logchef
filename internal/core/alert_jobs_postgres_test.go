package core

import (
	"context"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/alerts"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/jobs"
	"github.com/mr-karan/logchef/internal/store"
	"github.com/mr-karan/logchef/internal/store/postgres"
	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
	"github.com/mr-karan/logchef/pkg/models"
)

// TestAlertEditWaitsForEvaluationCommit: an evaluation's commit holds the
// rule's job row and then advances the alert row. An edit that arrives in
// between waits for the commit and then succeeds; with the opposite lock order
// Postgres aborts one of the two as a deadlock.
func TestAlertEditWaitsForEvaluationCommit(t *testing.T) {
	ctx := context.Background()
	log := discardLogger()
	db, err := postgres.New(ctx, postgres.Options{Logger: log, Config: config.PostgresConfig{DSN: postgrestest.DSN(t)}})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner := &models.User{Email: "deadlock-owner@example.com", FullName: "Owner", Role: models.UserRoleMember, Status: "active"}
	if err := db.CreateUser(ctx, owner); err != nil {
		t.Fatal(err)
	}
	source := &models.Source{Name: "deadlock", Connection: models.ConnectionInfo{Host: "ch:9000", Username: "default", Database: "default", TableName: "deadlock"}}
	if err := db.CreateSource(ctx, source); err != nil {
		t.Fatal(err)
	}
	ds := newFakeDatasourceService(db, log, nil)
	alert, err := CreateAlert(ctx, db, ds, log, source.ID, owner, newTestCreateAlertRequest())
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := db.ClaimJobs(ctx, string(alerts.EvaluateKind), "replica-a", time.Minute, 1)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim = %v, %v", claimed, err)
	}
	job := jobs.Job{ID: claimed[0].ID, Kind: alerts.EvaluateKind, Key: claimed[0].Key, LeaseToken: claimed[0].LeaseToken}

	held, release := make(chan struct{}), make(chan struct{})
	evaluation := make(chan error, 1)
	go func() {
		evaluation <- db.WithTx(ctx, func(tx store.StoreOps) error {
			if err := jobs.Hold(ctx, tx, job); err != nil {
				return err
			}
			close(held)
			<-release
			return tx.AdvanceAlertEvalSeq(ctx, alert.ID, alert.EvalSeq)
		})
	}()
	<-held
	edit := make(chan error, 1)
	name := "renamed"
	go func() {
		_, err := UpdateAlert(ctx, db, ds, log, owner, alert.ID, &models.UpdateAlertRequest{Name: &name})
		edit <- err
	}()
	// Let the edit reach its first lock before the evaluation goes on.
	time.Sleep(300 * time.Millisecond)
	close(release)
	if err := <-evaluation; err != nil {
		t.Errorf("evaluation commit: %v", err)
	}
	if err := <-edit; err != nil {
		t.Errorf("edit: %v", err)
	}
}
