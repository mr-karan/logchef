package clickhouse

import "testing"

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
	if err := client.connection().QueryRow(ctx, built).Scan(&marker, &escaped, &empty); err != nil {
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
	if err := client.connection().QueryRow(ctx, withoutLimit).Scan(&marker, &escaped, &empty); err != nil {
		t.Fatalf("execute limit-free query %q: %v", withoutLimit, err)
	}
	if marker != "___ESCAPED_QUOTE___" || escaped != "it's" || empty != "" {
		t.Fatalf("limit-free query returned marker=%q escaped=%q empty=%q", marker, escaped, empty)
	}
}
