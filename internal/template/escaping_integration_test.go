package template_test

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/template"
)

func TestTemplateValuesRoundTripClickHouse(t *testing.T) {
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
	for _, value := range []string{`\`, `'`, `\'`, `\' OR 1=1 --`, "`", `[[info]]`, `a\nb`, "a\nb\x00c", `a\\'b`} {
		t.Run(value, func(t *testing.T) {
			query, err := template.SubstituteVariables("SELECT {{value}} AS value", []template.Variable{{Name: "value", Type: template.TypeString, Value: value}})
			if err != nil {
				t.Fatal(err)
			}
			for _, throughBuilder := range []bool{false, true} {
				sql := query
				if throughBuilder {
					sql, err = clickhouse.NewExtendedQueryBuilder("", 0).BuildRawQuery(query, 0)
					if err != nil {
						t.Errorf("build %q: %v", query, err)
						continue
					}
				}
				var got string
				if err := conn.QueryRow(ctx, sql).Scan(&got); err != nil {
					t.Errorf("execute %q: %v", sql, err)
					continue
				}
				if got != value {
					t.Errorf("builder=%v got %q, want %q", throughBuilder, got, value)
				}
			}
		})
	}
	for _, value := range []string{" OR 1=1 --", `\'`, "a'\b", `[[info]]`} {
		t.Run("quoted/"+value, func(t *testing.T) {
			query, err := template.SubstituteVariables("SELECT '{{value}}' AS value", []template.Variable{{Name: "value", Type: template.TypeString, Value: value}})
			if err != nil {
				t.Fatal(err)
			}
			query, err = clickhouse.NewExtendedQueryBuilder("", 0).BuildRawQuery(query, 0)
			if err != nil {
				t.Fatal(err)
			}
			var got string
			if err := conn.QueryRow(ctx, query).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != value {
				t.Fatalf("got %q, want %q", got, value)
			}
		})
	}

	for _, query := range []string{`SELECT '%[[info]]%'`, `SELECT 'it''s [[info]]'`, `SELECT 'it\'s [[info]]'`, `SELECT /* outer /* inner */ [[info]] */ 'safe'`} {
		t.Run(query, func(t *testing.T) {
			sql, err := template.SubstituteVariables(query, []template.Variable{{Name: "unused", Type: template.TypeString, Value: "x"}})
			if err != nil {
				t.Fatal(err)
			}
			var want, got string
			if err := conn.QueryRow(ctx, query).Scan(&want); err != nil {
				t.Fatal(err)
			}
			if err := conn.QueryRow(ctx, sql).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
	query, err := template.SubstituteVariables("SELECT [[1,2],[3]]", []template.Variable{{Name: "unused", Type: template.TypeString, Value: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	var got [][]uint8
	if err := conn.QueryRow(ctx, query).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, [][]uint8{{1, 2}, {3}}) {
		t.Fatalf("array = %v", got)
	}
}
