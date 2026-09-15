package logchefql

import (
	"context"
	"os"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
)

func TestSchemaAwareQueriesClickHouse(t *testing.T) {
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	conn, err := ch.Open(&ch.Options{Addr: []string{addr}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	version, err := conn.ServerVersion()
	if err != nil {
		t.Fatal(err)
	}
	hasNativeJSON := version.Version.Major > 25 || version.Version.Major == 25 && version.Version.Minor >= 3

	for _, tt := range []struct {
		name       string
		columnType string
		expression string
		query      string
		want       uint64
		minVersion string
	}{
		{"JSON contains", "JSON", `CAST('{"order_number":"500","tcode":10023}', 'JSON')`, `p~"500" | p`, 1, "25.3"},
		{"JSON excludes", "JSON", `CAST('{"order_number":"500"}', 'JSON')`, `p!~"500" | p`, 0, "25.3"},
		{"JSON path contains", "JSON", `CAST('{"order_number":"500"}', 'JSON')`, `p.order_number~"500" | p.order_number`, 1, "25.3"},
		{"JSON numeric equality", "JSON", `CAST('{"tcode":10023}', 'JSON')`, `p.tcode=10023 | p.tcode`, 1, "25.3"},
		{"JSON numeric comparison", "JSON", `CAST('{"tcode":10023}', 'JSON')`, `p.tcode>10000 | p.tcode`, 1, "25.3"},
		{"JSON typed path", "JSON(tcode UInt64)", `CAST('{"tcode":10023}', 'JSON(tcode UInt64)')`, `p.tcode~"10023" | p.tcode`, 1, "25.3"},
		{"Nullable JSON path", "Nullable(JSON)", `CAST('{"tcode":10023}', 'Nullable(JSON)')`, `p.tcode=10023 | p.tcode`, 1, "25.3"},
		{"String contains", "String", `'{"order_number":"500"}'`, `p~"500" | p`, 1, ""},
		{"String excludes", "String", `'{"order_number":"500"}'`, `p!~"500" | p`, 0, ""},
		{"String JSON path", "String", `'{"order_number":"500"}'`, `p.order_number="500" | p.order_number`, 1, ""},
		{"String JSON path contains", "String", `'{"order_number":"500"}'`, `p.order_number~"500" | p.order_number`, 1, ""},
		{"Map key", "Map(String, String)", `map('order_number','500')`, `p.order_number="500" | p.order_number`, 1, ""},
		{"Map key contains", "Map(String, String)", `map('order_number','500')`, `p.order_number~"500" | p.order_number`, 1, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.minVersion == "25.3" && !hasNativeJSON {
				t.Skipf("native JSON requires ClickHouse 25.3 or newer, got %s", version.Version)
			}
			result := Translate(tt.query, &Schema{Columns: []ColumnInfo{{Name: "p", Type: tt.columnType}}})
			if !result.Valid {
				t.Fatalf("translation failed: %+v", result.Error)
			}
			query := "SELECT count() FROM (SELECT " + result.SelectClause + " FROM (SELECT " + tt.expression + " AS p) WHERE " + result.SQL + ")"
			var count uint64
			if err := conn.QueryRow(ctx, query).Scan(&count); err != nil {
				t.Fatalf("executing %s: %v", query, err)
			}
			if count != tt.want {
				t.Fatalf("got %d rows, want %d", count, tt.want)
			}
		})
	}
}
