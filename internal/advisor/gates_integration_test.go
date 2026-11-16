package advisor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/internal/queryescape"
	"github.com/mr-karan/logchef/pkg/models"
)

// The build gates of RFC 0010 for R1 and R3. Each executes the DDL that
// Analyze generates on a table with existing parts. A rule whose gate fails
// is removed from the rule set.

func gateClickHouse(t *testing.T) (*clickhouse.Client, driver.Conn, context.Context) {
	t.Helper()
	addr := os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR")
	if addr == "" {
		t.Skip("set LOGCHEF_TEST_CLICKHOUSE_ADDR to run against ClickHouse")
	}
	client, err := clickhouse.NewClient(clickhouse.ClientOptions{Host: addr}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	conn, err := ch.Open(&ch.Options{Addr: []string{addr}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	// Mutations finish before ALTER returns, so a comparison reads their result.
	return client, conn, ch.Context(ctx, ch.WithSettings(ch.Settings{"mutations_sync": 2}))
}

func gateExec(ctx context.Context, t *testing.T, conn driver.Conn, statements ...string) {
	t.Helper()
	for _, statement := range statements {
		if err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func gateString(ctx context.Context, t *testing.T, conn driver.Conn, query string) string {
	t.Helper()
	var out string
	if err := conn.QueryRow(ctx, query).Scan(&out); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return out
}

func gateFacts(ctx context.Context, t *testing.T, client *clickhouse.Client, table string, usage Usage, sample *clickhouse.AdvisorSample) Facts {
	t.Helper()
	meta, err := client.AdvisorMetadata(ctx, "default", table, "ts")
	if err != nil {
		t.Fatal(err)
	}
	if reason := unavailable(meta); reason != "" {
		t.Fatalf("metadata unavailable: %s", reason)
	}
	return Facts{Metadata: meta, Usage: usage, Sample: sample}
}

// TestGateBloomFilter is G1 for bloom_filter: an index that R1 adds with ADD
// INDEX and MATERIALIZE INDEX serves = and IN on parts written before it,
// and results do not change. The column name holds a backtick.
func TestGateBloomFilter(t *testing.T) {
	client, conn, ctx := gateClickHouse(t)
	const column = "request`id"
	table := fmt.Sprintf("logchef_gate_bloom_%d", time.Now().UnixNano())
	quoted := queryescape.ClickHouseIdentifier(column)
	t.Cleanup(func() { _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })
	gateExec(ctx, t, conn,
		// One partition per insert keeps three parts that no merge combines.
		"CREATE TABLE "+table+" (ts DateTime, tenant String, "+quoted+" String) ENGINE = MergeTree PARTITION BY tenant ORDER BY (tenant, ts) SETTINGS index_granularity = 256",
	)
	for part := range 3 {
		gateExec(ctx, t, conn, fmt.Sprintf("INSERT INTO %s SELECT toDateTime('2026-11-01 00:00:00') + number, 't%d', concat('id-', toString(number + %d)) FROM numbers(20000)", table, part, part*20000))
	}
	results := []string{
		"SELECT arrayStringConcat(groupArray(tenant), ',') FROM (SELECT tenant FROM " + table + " WHERE " + quoted + " = 'id-25000' ORDER BY tenant)",
		"SELECT arrayStringConcat(groupArray(" + quoted + "), ',') FROM (SELECT " + quoted + " FROM " + table + " WHERE " + quoted + " IN ('id-5', 'id-45000', 'nope') ORDER BY " + quoted + ")",
	}
	before := make([]string, len(results))
	for i, query := range results {
		before[i] = gateString(ctx, t, conn, query)
	}
	if before[0] != "t1" || before[1] != "id-45000,id-5" {
		t.Fatalf("fixture results = %q", before)
	}

	usage := Usage{Queries: 200, Shapes: []ShapeUsage{{Shape: models.FilterShape{{Column: column, Kind: models.MatchEq}}, Queries: 200, DurationMs: 200 * 900}}}
	sample := &clickhouse.AdvisorSample{
		Input:   models.AdvisorInput{Name: clickhouse.AdvisorInputSample, State: models.AdvisorInputOK},
		Columns: []clickhouse.AdvisorCardinality{{Column: column, Distinct: 60000, MaxSliceDistinct: 20000}},
	}
	facts := gateFacts(ctx, t, client, table, usage, sample)
	if got := SampleColumns(facts); len(got) != 1 || got[0] != column {
		t.Fatalf("SampleColumns = %q, want the bloom candidate", got)
	}
	found := findingsOf(Analyze(facts), ruleBloom)
	if len(found) != 1 {
		t.Fatalf("R1 findings = %+v, want 1", found)
	}
	gateExec(ctx, t, conn, found[0].DDL...)

	for i, query := range results {
		if got := gateString(ctx, t, conn, query); got != before[i] {
			t.Errorf("after the index, %s = %q, want %q", query, got, before[i])
		}
	}
	index := generatedName("idx", column, "bloom")
	for _, predicate := range []string{quoted + " = 'id-25000'", quoted + " IN ('id-5', 'id-45000', 'nope')"} {
		if read, total := skipIndexGranules(ctx, t, conn, "EXPLAIN indexes = 1 SELECT count() FROM "+table+" WHERE "+predicate, index); read >= total {
			t.Errorf("index %s kept %d of %d granules for %s, want fewer", index, read, total, predicate)
		}
	}
	if again := findingsOf(Analyze(gateFacts(ctx, t, client, table, usage, sample)), ruleBloom); len(again) != 0 {
		t.Errorf("R1 fires again after its DDL: %+v", again)
	}
}

var granules = regexp.MustCompile(`Granules: (\d+)/(\d+)`)

// skipIndexGranules returns the granules the named skip index kept, of
// those it was offered, from EXPLAIN indexes = 1.
func skipIndexGranules(ctx context.Context, t *testing.T, conn driver.Conn, explain, index string) (read, total int) {
	t.Helper()
	rows, err := conn.Query(ctx, explain)
	if err != nil {
		t.Fatalf("%s: %v", explain, err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	for i, line := range lines {
		if !strings.Contains(line, "Name: "+index) {
			continue
		}
		for _, next := range lines[i+1:] {
			if m := granules.FindStringSubmatch(next); m != nil {
				read, _ = strconv.Atoi(m[1])
				total, _ = strconv.Atoi(m[2])
				return read, total
			}
		}
	}
	t.Fatalf("index %s is not in the plan:\n%s", index, strings.Join(lines, "\n"))
	return 0, 0
}

// TestGateMaterializedColumn is G3: a column that R3 adds with ADD COLUMN
// ... MATERIALIZED reads the same value as its expression, for a Map key and
// a JSON path, on old and new parts, before and after MATERIALIZE COLUMN.
func TestGateMaterializedColumn(t *testing.T) {
	client, conn, ctx := gateClickHouse(t)
	table := fmt.Sprintf("logchef_gate_materialized_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })
	gateExec(ctx, t, conn,
		// One partition per insert keeps parts that no merge combines.
		"CREATE TABLE "+table+" (batch UInt32, ts DateTime, attrs Map(String, String), labels Map(LowCardinality(String), String), payload String) ENGINE = MergeTree PARTITION BY batch ORDER BY ts",
	)
	insert := func(offset int) {
		t.Helper()
		gateExec(ctx, t, conn, fmt.Sprintf("INSERT INTO %s SELECT %d, toDateTime('2026-11-01 00:00:00') + number, "+
			"if(number %% 3 = 0, map('k', toString(number)), map('other', 'x')), "+
			"map('a.b', concat('l', toString(number))), "+
			"if(number %% 2 = 0, concat('{\"a\":{\"b\":\"', toString(number), '\"},\"a.b\":\"flat\"}'), 'not json') "+
			"FROM numbers(%d, 1000)", table, offset, offset))
	}
	for part := range 3 {
		insert(part * 1000)
	}

	shape := models.FilterShape{
		{Column: "attrs", Path: []string{"k"}, Access: models.AccessMap, Kind: models.MatchEq},
		{Column: "labels", Path: []string{"a", "b"}, Access: models.AccessNested, Kind: models.MatchEq},
		{Column: "payload", Path: []string{"a", "b"}, Access: models.AccessJSONString, Kind: models.MatchEq},
	}
	usage := Usage{Queries: 200, Shapes: []ShapeUsage{{Shape: shape, Queries: 200, DurationMs: 200 * 500}}}
	found := findingsOf(Analyze(gateFacts(ctx, t, client, table, usage, nil)), ruleMaterializedColumn)
	if len(found) != 3 {
		t.Fatalf("R3 findings = %+v, want 3", found)
	}
	checks := map[string]string{
		"attrs_k":     "attrs['k']",
		"labels_a_b":  "labels['a.b']",
		"payload_a_b": "JSONExtractString(payload, 'a', 'b')",
	}
	compare := func(stage string) {
		t.Helper()
		for name, expr := range checks {
			query := fmt.Sprintf("SELECT toString(countIf(%s != %s)) || '/' || toString(countIf(%s != '')) FROM %s", name, expr, name, table)
			got := gateString(ctx, t, conn, query)
			if !strings.HasPrefix(got, "0/") || got == "0/0" {
				t.Errorf("%s: %s = %s, want no mismatch and some values", stage, query, got)
			}
		}
	}
	for _, f := range found {
		gateExec(ctx, t, conn, f.DDL[0]) // ADD COLUMN only: old parts compute the value
	}
	compare("old parts before MATERIALIZE COLUMN")
	insert(3000)
	compare("a new part before MATERIALIZE COLUMN")
	for _, f := range found {
		gateExec(ctx, t, conn, f.DDL[1])
	}
	compare("after MATERIALIZE COLUMN")
	if again := findingsOf(Analyze(gateFacts(ctx, t, client, table, usage, nil)), ruleMaterializedColumn); len(again) != 0 {
		t.Errorf("R3 fires again after its DDL: %+v", again)
	}
}
