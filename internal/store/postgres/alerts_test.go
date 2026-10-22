package postgres_test

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/mr-karan/logchef/internal/store/storetest"
)

func TestPruneAlertHistoryPlan(t *testing.T) {
	dsn := resetSchema(t)
	opener(dsn)(t)
	ctx := t.Context()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	query := storetest.GeneratedQuery(t, "sqlc/queries.sql.go", "pruneAlertHistory")
	rows, err := conn.Query(ctx, "EXPLAIN "+query, int64(1), int64(1), int32(1))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatal(err)
		}
		t.Log(detail)
		if strings.Contains(detail, "(SubPlan ") || strings.Contains(detail, "target.") {
			t.Errorf("pruning repeats a subquery per history row: %s", detail)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
