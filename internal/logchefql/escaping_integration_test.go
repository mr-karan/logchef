package logchefql_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/logchefql"
)

func TestLogchefQLIdentifiersRoundTripClickHouse(t *testing.T) {
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
	for _, field := range []string{"a`b", `a\b`, `a\` + "`b", "`x`", "a' OR 1=1 --", `[[field]]`, " spaced "} {
		t.Run(field, func(t *testing.T) {
			value := "value_" + field
			result := logchefql.Translate(quoteLogchefQL(field)+"="+quoteLogchefQL(value)+" | "+quoteLogchefQL(field), &logchefql.Schema{Columns: []logchefql.ColumnInfo{{Name: field, Type: "String"}}})
			if !result.Valid {
				t.Fatalf("translate: %+v", result.Error)
			}
			inner := "SELECT unhex('" + hex.EncodeToString([]byte(value)) + "') AS \"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(field) + "\""
			query := "SELECT " + result.SelectClause + " FROM (" + inner + ") WHERE " + result.SQL
			built, err := clickhouse.NewExtendedQueryBuilder("", 0).BuildRawQuery(query, 0)
			if err != nil {
				t.Fatalf("build %q: %v", query, err)
			}
			rows, err := conn.Query(ctx, built)
			if err != nil {
				t.Fatalf("execute %q: %v", built, err)
			}
			defer rows.Close()
			if columns := rows.Columns(); len(columns) != 1 || columns[0] != field {
				t.Fatalf("columns = %q, want %q", columns, field)
			}
			if !rows.Next() {
				t.Fatalf("no row: %v", rows.Err())
			}
			var got string
			if err := rows.Scan(&got); err != nil {
				t.Fatal(err)
			}
			if got != value || rows.Next() {
				t.Fatalf("value = %q, want one row containing %q", got, value)
			}
		})
	}
}

func TestLogchefQLIdentifiersAndValuesRoundTripVictoriaLogs(t *testing.T) {
	endpoint := os.Getenv("LOGCHEF_TEST_VICTORIALOGS_URL")
	if endpoint == "" {
		t.Skip("set LOGCHEF_TEST_VICTORIALOGS_URL to run against VictoriaLogs")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, value := range []string{`\`, `'`, `\'`, `\' OR 1=1 --`, "`", `[[info]]`, `a\nb`, "a\nb\x00c", `a"b`} {
		t.Run(value, func(t *testing.T) {
			field := "field" + value
			result := logchefql.TranslateToLogsQL(quoteLogchefQL(field)+"="+quoteLogchefQL(value), nil)
			if !result.Valid {
				t.Fatalf("translate: %+v", result.Error)
			}
			query := "* | generate_sequence 1 | format " + strconv.Quote(value) + " as " + strconv.Quote(field) + " | filter " + result.Query + " | fields " + strconv.Quote(field)
			body := url.Values{"query": {query}, "start": {"2026-01-01T00:00:00Z"}, "end": {"2026-01-01T00:00:01Z"}}
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(endpoint, "/")+"/select/logsql/query", strings.NewReader(body.Encode()))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("query %q: HTTP %d %s", query, resp.StatusCode, data)
			}
			var got map[string]string
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("decode %q: %v", data, err)
			}
			if len(got) != 1 || got[field] != value {
				t.Fatalf("got %q, want field %q value %q", got, field, value)
			}
		})
	}
}

func quoteLogchefQL(value string) string {
	return `"` + strings.NewReplacer("\\", "\\\\", `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(value) + `"`
}
