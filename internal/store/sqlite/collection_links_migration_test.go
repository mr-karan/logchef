package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// beforeLinkKindVersion is the last schema version before collection links
// got a kind. 000039 belongs to another branch, so the migrator jumps the gap.
const beforeLinkKindVersion = 38

// TestCollectionItemKindMigration seeds one link of each origin before
// 000040 and checks the backfill: links that the query's creator or a global
// admin added into a shared collection stay managed, every other link
// becomes a pin. It then reverts and re-applies the migration.
func TestCollectionItemKindMigration(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "links.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	m := newMigrator(t, db)
	if err := m.Migrate(beforeLinkKindVersion); err != nil {
		t.Fatalf("migrate to v%d: %v", beforeLinkKindVersion, err)
	}

	mustExec(t, db, `INSERT INTO users (id, email, full_name, role, status) VALUES
		(1, 'creator@test.dev', 'Creator', 'member', 'active'),
		(2, 'admin@test.dev', 'Admin', 'admin', 'active'),
		(3, 'other@test.dev', 'Other', 'member', 'active')`)
	mustExec(t, db, `INSERT INTO sources (id, name, _meta_is_auto_created, source_type, _meta_ts_field, connection_config, identity_key)
		VALUES (1, 'logs', 0, 'clickhouse', 'timestamp', '{}', 'clickhouse:ch:9000/logs/app')`)
	mustExec(t, db, `INSERT INTO saved_queries (id, source_id, name, query_language, editor_mode, query_content, created_by) VALUES
		(1, 1, 'by creator', 'logchefql', 'builder', '{}', 1),
		(2, 1, 'by admin', 'logchefql', 'builder', '{}', 1),
		(3, 1, 'by other', 'logchefql', 'builder', '{}', 1),
		(4, 1, 'by deleted user', 'logchefql', 'builder', '{}', 1),
		(5, 1, 'in personal', 'logchefql', 'builder', '{}', 1),
		(6, 1, 'creator gone', 'logchefql', 'builder', '{}', NULL),
		(7, 1, 'creator gone member pin', 'logchefql', 'builder', '{}', NULL)`)
	mustExec(t, db, `INSERT INTO collections (id, name, is_personal, created_by) VALUES
		(1, 'Shared', 0, 1),
		(2, 'My Collection', 1, 1)`)
	mustExec(t, db, `INSERT INTO collection_items (collection_id, saved_query_id, added_by) VALUES
		(1, 1, 1),
		(1, 2, 2),
		(1, 3, 3),
		(1, 4, NULL),
		(2, 5, 1),
		(1, 6, 2),
		(1, 7, 3)`)

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
	inventoryRows, err := db.Query(inventory)
	if err != nil {
		t.Fatal(err)
	}
	defer inventoryRows.Close()
	var inventoryIDs []int
	for inventoryRows.Next() {
		var collectionID, queryID int
		var addedBy sql.NullInt64
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
	if err := m.Migrate(40); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	if version, dirty, err := m.Version(); err != nil || dirty || version != 40 {
		t.Fatalf("version after up = %d dirty=%v err=%v, want 40", version, dirty, err)
	}
	assertLinkKinds(t, db)

	if err := m.Steps(-1); err != nil {
		t.Fatalf("migrate down: %v", err)
	}
	if _, err := db.Exec(`SELECT kind FROM collection_items LIMIT 1`); err == nil {
		t.Fatalf("kind column still present after down migration")
	}
	if got := countRows(t, db, "collection_items"); got != 7 {
		t.Fatalf("collection_items after down = %d, want 7", got)
	}
	if err := m.Migrate(40); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	assertLinkKinds(t, db)
}

func assertLinkKinds(t *testing.T, db *sql.DB) {
	t.Helper()
	want := map[string]string{
		"1/1": "managed", // the creator added it
		"1/2": "managed", // an admin added it
		"1/3": "pin",     // another member added it
		"1/4": "pin",     // the adder was deleted
		"2/5": "pin",     // a personal collection
		"1/7": "pin",
		"1/6": "managed", // an admin added a query whose creator is gone
	}
	for key, kind := range want {
		assertRow(t, db, `SELECT kind FROM collection_items WHERE collection_id || '/' || saved_query_id = '`+key+`'`, kind)
	}
	if _, err := db.Exec(`INSERT INTO collection_items (collection_id, saved_query_id, kind) VALUES (1, 1, 'bookmark')`); err == nil {
		t.Fatalf("kind check constraint accepted an unknown kind")
	}
	assertRow(t, db, `SELECT kind FROM collection_items WHERE collection_id = 1 AND saved_query_id = 1`, "managed")
}
