package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/mr-karan/logchef/internal/store/storetest"
	"github.com/mr-karan/logchef/pkg/models"
)

func createTestSource(t *testing.T, ctx context.Context, db *DB) models.SourceID {
	t.Helper()
	src := &models.Source{
		Name:        "logs.alerts",
		MetaTSField: "timestamp",
		Connection:  models.ConnectionInfo{Host: "localhost:9000", Database: "logs", TableName: "alerts"},
	}
	if err := src.SyncConnectionConfig(); err != nil {
		t.Fatalf("SyncConnectionConfig: %v", err)
	}
	if err := db.CreateSource(ctx, src); err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	return src.ID
}

func TestPruneAlertHistoryPlan(t *testing.T) {
	db := newTxTestDB(t)
	query := storetest.GeneratedQuery(t, "sqlc/queries.sql.go", "pruneAlertHistory")
	args := make([]any, strings.Count(query, "?"))
	for i := range args {
		args[i] = 1
	}
	rows, err := db.writeDB.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "CORRELATED") {
			t.Errorf("pruning repeats a subquery per history row: %s", detail)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
