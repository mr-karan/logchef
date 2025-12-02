package clickhouse

import (
	"testing"

	clickhouseparser "github.com/AfterShip/clickhouse-sql-parser/parser"
)

func TestEnsureTimestampInQueryAddsMissingField(t *testing.T) {
	t.Parallel()

	client := &Client{}
	original := "SELECT host FROM logs.vector_logs"

	updated, err := client.ensureTimestampInQuery(original, "parsed_timestamp")
	if err != nil {
		t.Fatalf("ensureTimestampInQuery() returned error: %v", err)
	}

	items := mustSelectItems(t, updated)
	if len(items) != 2 {
		t.Fatalf("expected two select items, got %d", len(items))
	}

	if got := items[0].String(); got != "host" {
		t.Fatalf("unexpected first select item: %s", got)
	}

	if got := items[1].String(); got != "parsed_timestamp" {
		t.Fatalf("timestamp field was not appended correctly, got %s", got)
	}
}

func TestEnsureTimestampInQueryNoOpWhenTimestampPresent(t *testing.T) {
	t.Parallel()

	client := &Client{}
	original := "SELECT host, parsed_timestamp FROM logs.vector_logs"

	updated, err := client.ensureTimestampInQuery(original, "parsed_timestamp")
	if err != nil {
		t.Fatalf("ensureTimestampInQuery() returned error: %v", err)
	}

	if updated != original {
		t.Fatalf("expected query to remain unchanged; got %q", updated)
	}
}

func TestEnsureTimestampInQueryRespectsSelectStar(t *testing.T) {
	t.Parallel()

	client := &Client{}
	original := "SELECT * FROM logs.vector_logs LIMIT 100"

	updated, err := client.ensureTimestampInQuery(original, "parsed_timestamp")
	if err != nil {
		t.Fatalf("ensureTimestampInQuery() returned error: %v", err)
	}

	if updated != original {
		t.Fatalf("expected SELECT * query to remain unchanged; got %q", updated)
	}
}

func TestEnsureTimestampInQueryRespectsTableStar(t *testing.T) {
	t.Parallel()

	client := &Client{}
	original := "SELECT logs.* FROM logs.vector_logs AS logs"

	updated, err := client.ensureTimestampInQuery(original, "parsed_timestamp")
	if err != nil {
		t.Fatalf("ensureTimestampInQuery() returned error: %v", err)
	}

	if updated != original {
		t.Fatalf("expected table.* query to remain unchanged; got %q", updated)
	}
}

func TestEnsureTimestampInQueryRejectsEmptyQuery(t *testing.T) {
	t.Parallel()

	client := &Client{}
	if _, err := client.ensureTimestampInQuery("   ", "parsed_timestamp"); err == nil {
		t.Fatalf("expected error for empty query")
	}
}

func mustSelectItems(t *testing.T, sql string) []*clickhouseparser.SelectItem {
	t.Helper()

	parser := clickhouseparser.NewParser(sql)
	statements, err := parser.ParseStmts()
	if err != nil {
		t.Fatalf("failed to parse SQL: %v", err)
	}

	if len(statements) != 1 {
		t.Fatalf("expected single statement, got %d", len(statements))
	}

	selectQuery, ok := statements[0].(*clickhouseparser.SelectQuery)
	if !ok {
		t.Fatalf("statement is not a SELECT query")
	}

	return selectQuery.SelectItems
}
