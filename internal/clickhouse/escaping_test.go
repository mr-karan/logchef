package clickhouse

import "testing"

func TestStatsIdentifiersRoundTripClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	for _, name := range []string{"a`b", `a\b`, `a\` + "`b", "`x`", "`x` + 1 AS `injected`", " spaced "} {
		t.Run(name, func(t *testing.T) {
			query := "SELECT 'safe' AS " + quoteIdentifier(name)
			rows, err := client.connection().Query(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			columns := rows.Columns()
			if len(columns) != 1 || columns[0] != name {
				t.Fatalf("columns=%q, want %q", columns, name)
			}
		})
	}
}
