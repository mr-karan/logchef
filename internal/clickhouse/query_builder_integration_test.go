package clickhouse

import (
	"errors"
	"reflect"
	"testing"

	"github.com/mr-karan/logchef/internal/queryescape"
)

func TestQueryBuilderClickHousePreservesLiteralValues(t *testing.T) {
	client, ctx := integrationClient(t)
	qb := NewExtendedQueryBuilder("", 0)

	built, err := qb.BuildRawQuery(
		"SELECT '___ESCAPED_QUOTE___' AS marker, 'it''s' AS escaped, '' AS empty",
		0,
	)
	if err != nil {
		t.Fatal(err)
	}

	var marker, escaped, empty string
	if err := client.conn.QueryRow(ctx, built).Scan(&marker, &escaped, &empty); err != nil {
		t.Fatalf("execute built query %q: %v", built, err)
	}
	if marker != "___ESCAPED_QUOTE___" || escaped != "it's" || empty != "" {
		t.Fatalf("built query returned marker=%q escaped=%q empty=%q", marker, escaped, empty)
	}

	withoutLimit, err := qb.RemoveLimitClause(built)
	if err != nil {
		t.Fatal(err)
	}
	marker, escaped, empty = "", "", "not empty"
	if err := client.conn.QueryRow(ctx, withoutLimit).Scan(&marker, &escaped, &empty); err != nil {
		t.Fatalf("execute limit-free query %q: %v", withoutLimit, err)
	}
	if marker != "___ESCAPED_QUOTE___" || escaped != "it's" || empty != "" {
		t.Fatalf("limit-free query returned marker=%q escaped=%q empty=%q", marker, escaped, empty)
	}
}

func TestQuotedIdentifiersMatchClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	qb := NewExtendedQueryBuilder("", 0)
	query := func(t *testing.T, sql string) ([]string, []any) {
		t.Helper()
		rows, err := client.conn.Query(ctx, sql)
		if err != nil {
			t.Fatalf("execute %q: %v", sql, err)
		}
		defer rows.Close()
		var values []any
		for rows.Next() {
			row := make([]any, len(rows.ColumnTypes()))
			for i, column := range rows.ColumnTypes() {
				row[i] = reflect.New(column.ScanType()).Interface()
			}
			if err := rows.Scan(row...); err != nil {
				t.Fatal(err)
			}
			for _, value := range row {
				values = append(values, reflect.ValueOf(value).Elem().Interface())
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return rows.Columns(), values
	}

	var queries []string
	for _, tc := range acceptedQuotedIdentifiers {
		ident := tc.quote + tc.body + tc.quote
		source := " FROM (SELECT 11 AS " + queryescape.ClickHouseIdentifier(tc.name) + ", 22 AS marker)"
		queries = append(queries,
			"SELECT 1 AS "+ident+", 2 AS marker",
			"SELECT "+ident+", marker"+source,
			"SELECT marker "+ident+source,
			"SELECT marker"+source+" WHERE "+ident+" = 11",
		)
	}
	queries = append(queries,
		"SELECT `a`\"b\" FROM (SELECT 11 AS a, 22 AS b)",
		"SELECT \"a\"`b` FROM (SELECT 11 AS a, 22 AS b)",
		"SELECT `a` `b` FROM (SELECT 11 AS a, 22 AS b)",
		"SELECT `a`,`b` FROM (SELECT 11 AS a, 22 AS b)",
	)
	queries = append(queries, acceptedNamedArguments...)
	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			built, err := qb.BuildRawQuery(sql, 0)
			if err != nil {
				t.Fatalf("BuildRawQuery: %v", err)
			}
			wantColumns, wantValues := query(t, sql)
			gotColumns, gotValues := query(t, built)
			if !reflect.DeepEqual(gotColumns, wantColumns) || !reflect.DeepEqual(gotValues, wantValues) {
				t.Fatalf("built %q returned %q %v, want %q %v", built, gotColumns, gotValues, wantColumns, wantValues)
			}
		})
	}

	// ClickHouse reads each as one identifier holding an embedded quote, so it
	// returns 11. The parser splits it into a column and an alias that selects 22.
	for _, sql := range []string{
		"SELECT `a``b` FROM (SELECT 11 AS \"a`b\", 22 AS a)",
		"SELECT `a\\x60``b` FROM (SELECT 11 AS \"a``b\", 22 AS \"a`\")",
		"SELECT `a\\\\``b` FROM (SELECT 11 AS \"a\\\\`b\", 22 AS \"a\\\\\")",
		"SELECT \"a\\\\\"\"b\" FROM (SELECT 11 AS `a\\\\\"b`, 22 AS `a\\\\`)",
		"SELECT \"a\"\"b\" FROM (SELECT 11 AS `a\"b`, 22 AS a)",
	} {
		t.Run(sql, func(t *testing.T) {
			if _, values := query(t, sql); !reflect.DeepEqual(values, []any{uint8(11)}) {
				t.Fatalf("ClickHouse returned %v, want [11]", values)
			}
			if built, err := qb.BuildRawQuery(sql, 0); !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("BuildRawQuery = %q, %v; want ErrInvalidQuery", built, err)
			}
		})
	}

	for _, sql := range rejectedNamedArguments {
		t.Run(sql, func(t *testing.T) {
			if rows, err := client.conn.Query(ctx, sql); err == nil {
				rows.Close()
				t.Fatalf("ClickHouse ran %q, want an unknown identifier error", sql)
			}
			if built, err := qb.BuildRawQuery(sql, 0); !errors.Is(err, ErrInvalidQuery) {
				t.Fatalf("BuildRawQuery = %q, %v; want ErrInvalidQuery", built, err)
			}
		})
	}
}
