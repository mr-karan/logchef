package clickhouse

import (
	"testing"

	"github.com/mr-karan/logchef/internal/queryescape"
)

func TestStatsIdentifiersRoundTripClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	for _, name := range []string{"a`b", `a\b`, `a\` + "`b", "`x`", "`x` + 1 AS `injected`", " spaced "} {
		t.Run(name, func(t *testing.T) {
			query := "SELECT 'safe' AS " + queryescape.ClickHouseIdentifier(name)
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

func TestTableStatsEscapesNamesClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	for _, name := range []string{"missing'", `missing\`, `missing\' OR 1=1 --`} {
		t.Run(name, func(t *testing.T) {
			stats, err := client.TableStats(ctx, name, name)
			if err != nil {
				t.Fatal(err)
			}
			if stats.Database != name || stats.Table != name || stats.Rows != 0 {
				t.Fatalf("unexpected stats: %+v", stats)
			}
		})
	}
}
