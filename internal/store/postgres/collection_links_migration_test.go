package postgres_test

import (
	"context"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/store/postgres/postgrestest"

	"github.com/jackc/pgx/v5"
)

// TestCollectionItemKindMigration applies every migration before 000014,
// seeds one link of each origin, applies 000014 and checks the backfill:
// links that the query's creator or a global admin added into a shared
// collection stay managed, every other link becomes a pin. It then reverts
// and re-applies the migration.
func TestCollectionItemKindMigration(t *testing.T) {
	dsn := postgrestest.DSN(t)
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	const kindVersion = 14
	for _, name := range upMigrationsBefore(t, kindVersion) {
		if _, err := conn.Exec(ctx, readMigration(t, name)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}

	seed := []string{
		`INSERT INTO users (id, email, full_name, role, status) VALUES
			(1, 'creator@test.dev', 'Creator', 'member', 'active'),
			(2, 'admin@test.dev', 'Admin', 'admin', 'active'),
			(3, 'other@test.dev', 'Other', 'member', 'active')`,
		`INSERT INTO sources (id, name, _meta_is_auto_created, _meta_ts_field, source_type, connection_config, identity_key)
			VALUES (1, 'logs', false, 'timestamp', 'clickhouse', '{}', 'clickhouse:ch:9000/logs/app')`,
		`INSERT INTO saved_queries (id, source_id, name, query_language, editor_mode, query_content, created_by) VALUES
			(1, 1, 'by creator', 'logchefql', 'builder', '{}', 1),
			(2, 1, 'by admin', 'logchefql', 'builder', '{}', 1),
			(3, 1, 'by other', 'logchefql', 'builder', '{}', 1),
			(4, 1, 'by deleted user', 'logchefql', 'builder', '{}', 1),
			(5, 1, 'in personal', 'logchefql', 'builder', '{}', 1),
			(6, 1, 'creator gone', 'logchefql', 'builder', '{}', NULL),
			(7, 1, 'creator gone member pin', 'logchefql', 'builder', '{}', NULL)`,
		`INSERT INTO collections (id, name, is_personal, created_by) VALUES
			(1, 'Shared', false, 1),
			(2, 'My Collection', true, 1)`,
		`INSERT INTO collection_items (collection_id, saved_query_id, added_by) VALUES
			(1, 1, 1),
			(1, 2, 2),
			(1, 3, 3),
			(1, 4, NULL),
			(2, 5, 1),
			(1, 6, 2),
			(1, 7, 3)`,
	}
	for _, q := range seed {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
	}

	notes, err := os.ReadFile("../../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(notes), "```sql\n")
	if !ok {
		t.Fatal("upgrade inventory SQL not found")
	}
	inventory, _, ok := strings.Cut(block, "```")
	if !ok {
		t.Fatal("upgrade inventory SQL has no closing fence")
	}
	inventory = strings.ReplaceAll(inventory, "c.is_personal = 0", "c.is_personal = false")
	inventoryRows, err := conn.Query(ctx, inventory)
	if err != nil {
		t.Fatal(err)
	}
	defer inventoryRows.Close()
	var inventoryIDs []int
	for inventoryRows.Next() {
		var collectionID, queryID int
		var addedBy *int64
		var createdAt any
		if err := inventoryRows.Scan(&collectionID, &queryID, &addedBy, &createdAt); err != nil {
			t.Fatal(err)
		}
		inventoryIDs = append(inventoryIDs, queryID)
	}
	if err := inventoryRows.Err(); err != nil {
		t.Fatal(err)
	}
	inventoryRows.Close()
	sort.Ints(inventoryIDs)
	if !reflect.DeepEqual(inventoryIDs, []int{3, 4, 7}) {
		t.Fatalf("inventory = %v, want [3 4 7]", inventoryIDs)
	}
	up := readMigration(t, "000014_add_collection_item_kind.up.sql")
	if _, err := conn.Exec(ctx, up); err != nil {
		t.Fatalf("up migration: %v", err)
	}
	assertPostgresLinkKinds(t, ctx, conn)

	if _, err := conn.Exec(ctx, readMigration(t, "000014_add_collection_item_kind.down.sql")); err != nil {
		t.Fatalf("down migration: %v", err)
	}
	if _, err := conn.Exec(ctx, `SELECT kind FROM collection_items LIMIT 1`); err == nil {
		t.Fatalf("kind column still present after down migration")
	}
	var links int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM collection_items`).Scan(&links); err != nil || links != 7 {
		t.Fatalf("collection_items after down = %d / %v, want 7", links, err)
	}
	if _, err := conn.Exec(ctx, up); err != nil {
		t.Fatalf("up migration again: %v", err)
	}
	assertPostgresLinkKinds(t, ctx, conn)
}

func assertPostgresLinkKinds(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	want := map[[2]int]string{
		{1, 1}: "managed", // the creator added it
		{1, 2}: "managed", // an admin added it
		{1, 3}: "pin",     // another member added it
		{1, 4}: "pin",     // the adder was deleted
		{2, 5}: "pin",     // a personal collection
		{1, 7}: "pin",
		{1, 6}: "managed", // an admin added a query whose creator is gone
	}
	for key, kind := range want {
		var got string
		if err := conn.QueryRow(ctx, `SELECT kind FROM collection_items WHERE collection_id = $1 AND saved_query_id = $2`, key[0], key[1]).Scan(&got); err != nil {
			t.Fatalf("read kind %v: %v", key, err)
		}
		if got != kind {
			t.Errorf("link %v kind = %q, want %q", key, got, kind)
		}
	}
	if _, err := conn.Exec(ctx, `INSERT INTO collection_items (collection_id, saved_query_id, kind) VALUES (1, 1, 'bookmark')`); err == nil {
		t.Fatalf("kind check constraint accepted an unknown kind")
	}
}

// upMigrationsBefore lists the up migration files with a version below v, in
// order.
func upMigrationsBefore(t *testing.T, v int) []string {
	t.Helper()
	entries, err := os.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(name, "_", 2)[0])
		if err != nil {
			t.Fatalf("parse version of %s: %v", name, err)
		}
		if version < v {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
