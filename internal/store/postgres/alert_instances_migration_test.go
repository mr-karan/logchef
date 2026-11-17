package postgres_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"
	"github.com/mr-karan/logchef/internal/store/storetest"
)

// TestAlertInstancesUpgrade applies every migration before add_alert_instances
// and records them as applied, seeds alert history, then lets postgres.New run
// the remaining migrations through the production runner.
func TestAlertInstancesUpgrade(t *testing.T) {
	dsn := postgrestest.DSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	const alertInstancesVersion = 17
	if _, err := conn.Exec(ctx, `CREATE TABLE schema_migrations (
		version BIGINT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrationsBefore(t, alertInstancesVersion) {
		if _, err := conn.Exec(ctx, readMigration(t, name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations (version) SELECT generate_series(1, $1::bigint)`, alertInstancesVersion-1); err != nil {
		t.Fatal(err)
	}
	for _, query := range storetest.AlertInstancesUpgradeSeed {
		if _, err := conn.Exec(ctx, query); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}

	storetest.CheckAlertInstancesUpgrade(t, opener(dsn)(t))
}

// TestAlertJobsUpgrade applies every migration before add_alert_evaluate_jobs
// and records them as applied, seeds alerts, then lets postgres.New run the
// remaining migrations through the production runner.
func TestAlertJobsUpgrade(t *testing.T) {
	dsn := postgrestest.DSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	const alertJobsVersion = 22
	if _, err := conn.Exec(ctx, `CREATE TABLE schema_migrations (
		version BIGINT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrationsBefore(t, alertJobsVersion) {
		if _, err := conn.Exec(ctx, readMigration(t, name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations (version) SELECT generate_series(1, $1::bigint)`, alertJobsVersion-1); err != nil {
		t.Fatal(err)
	}
	for _, query := range storetest.AlertJobsUpgradeSeed {
		if _, err := conn.Exec(ctx, query); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}

	storetest.CheckAlertJobsUpgrade(t, opener(dsn)(t))
}

// TestAlertDestinationsUpgrade applies every migration before
// add_notification_destinations and records them as applied, seeds rule
// targets, then lets postgres.New run the remaining migrations.
func TestAlertDestinationsUpgrade(t *testing.T) {
	dsn := postgrestest.DSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	const alertDestinationsVersion = 24
	if _, err := conn.Exec(ctx, `CREATE TABLE schema_migrations (
		version BIGINT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrationsBefore(t, alertDestinationsVersion) {
		if _, err := conn.Exec(ctx, readMigration(t, name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations (version) SELECT generate_series(1, $1::bigint)`, alertDestinationsVersion-1); err != nil {
		t.Fatal(err)
	}
	for _, query := range storetest.AlertDestinationsUpgradeSeed {
		if _, err := conn.Exec(ctx, query); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}

	storetest.CheckAlertDestinationsUpgrade(t, opener(dsn)(t))
}

// TestAlertDeliveriesUpgrade applies every migration before
// add_notification_deliveries and records them as applied, seeds open
// episodes with delivery bookkeeping, then lets postgres.New run the
// remaining migrations.
func TestAlertDeliveriesUpgrade(t *testing.T) {
	dsn := postgrestest.DSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	const alertDeliveriesVersion = 29
	if _, err := conn.Exec(ctx, `CREATE TABLE schema_migrations (
		version BIGINT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range upMigrationsBefore(t, alertDeliveriesVersion) {
		if _, err := conn.Exec(ctx, readMigration(t, name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := conn.Exec(ctx, `INSERT INTO schema_migrations (version) SELECT generate_series(1, $1::bigint)`, alertDeliveriesVersion-1); err != nil {
		t.Fatal(err)
	}
	for _, query := range storetest.AlertDeliveriesUpgradeSeed {
		if _, err := conn.Exec(ctx, query); err != nil {
			t.Fatalf("seed: %v\n%s", err, query)
		}
	}

	storetest.CheckAlertDeliveriesUpgrade(t, opener(dsn)(t))
}
