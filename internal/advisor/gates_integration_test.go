package advisor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"slices"
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

// The build gates of RFC 0010 for R1, R3, R4 and R5. Each executes the DDL that
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
	return Facts{Metadata: meta, TimestampColumn: "ts", Usage: usage, Sample: sample}
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

// TestGateLowCardinality is G4: the MODIFY COLUMN that R4 generates changes
// only the type. It keeps the default expression, comment and codec, runs
// as one mutation per column and keeps every value. The column name holds a
// backtick. Columns that a skip index or a projection reads get no finding,
// since ClickHouse before 26.8 and from 24.1 on, in turn, refuses the change.
func TestGateLowCardinality(t *testing.T) {
	client, conn, ctx := gateClickHouse(t)
	const level = "le`vel"
	quoted := queryescape.ClickHouseIdentifier(level)
	table := fmt.Sprintf("logchef_gate_low_cardinality_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })
	gateExec(ctx, t, conn,
		// One partition per insert keeps three parts that no merge combines.
		"CREATE TABLE "+table+" (ts DateTime, tenant String, "+quoted+" String DEFAULT 'none' COMMENT 'the level' CODEC(ZSTD(3)), "+
			"service Nullable(String) DEFAULT NULL COMMENT 'the service', indexed String, projected String, "+
			"INDEX idx_indexed lower(indexed) TYPE set(10) GRANULARITY 1, PROJECTION by_projected (SELECT projected, count() GROUP BY projected)) "+
			"ENGINE = MergeTree PARTITION BY tenant ORDER BY (tenant, ts)",
	)
	for part := range 3 {
		gateExec(ctx, t, conn, fmt.Sprintf("INSERT INTO %s SELECT toDateTime('2026-11-01 00:00:00') + number, 't%d', "+
			"['debug', 'info', 'warning', 'error'][number %% 4 + 1], if(number %% 5 = 0, NULL, concat('svc-', toString(number %% 7))), "+
			"concat('host-', toString(number %% 3)), concat('zone-', toString(number %% 2)) FROM numbers(20000)", table, part))
	}
	// The sum of row hashes ignores row order, which a merge may change.
	values := "SELECT toString(count()) || ':' || toString(sum(cityHash64(toString((ts, tenant, " + quoted + ", service, indexed, projected))))) FROM " + table
	definitions := "SELECT arrayStringConcat(groupArray(concat(name, ' ', default_kind, ' ', default_expression, ' ', compression_codec, ' ', comment)), '\n') " +
		"FROM (SELECT * FROM system.columns WHERE database = currentDatabase() AND table = '" + table + "' ORDER BY position)"
	beforeValues, beforeDefinitions := gateString(ctx, t, conn, values), gateString(ctx, t, conn, definitions)

	cards := []clickhouse.AdvisorCardinality{
		{Column: level, Distinct: 4, MaxSliceDistinct: 4, Saturated: true},
		{Column: "service", Distinct: 7, MaxSliceDistinct: 7, Saturated: true},
		{Column: "indexed", Distinct: 3, MaxSliceDistinct: 3, Saturated: true},
		{Column: "projected", Distinct: 2, MaxSliceDistinct: 2, Saturated: true},
	}
	sample := &clickhouse.AdvisorSample{Input: models.AdvisorInput{Name: clickhouse.AdvisorInputSample, State: models.AdvisorInputOK}, Columns: cards}
	for range 7 {
		sample.Slices = append(sample.Slices, clickhouse.AdvisorSlice{Start: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Rows: 20_000, Distinct: []uint64{4, 7, 3, 2}})
	}
	facts := gateFacts(ctx, t, client, table, Usage{}, sample)
	if got := SampleColumns(facts); !slices.Contains(got, level) || !slices.Contains(got, "service") || slices.Contains(got, "indexed") || slices.Contains(got, "projected") {
		t.Fatalf("SampleColumns = %q, want the two R4 candidates and neither indexed nor projected", got)
	}
	found := findingsOf(Analyze(facts), ruleLowCardinality)
	if len(found) != 2 {
		t.Fatalf("R4 findings = %+v, want 2", found)
	}
	for _, f := range found {
		gateExec(ctx, t, conn, f.DDL...)
	}

	if got := gateString(ctx, t, conn, values); got != beforeValues {
		t.Errorf("values changed: hash %s, want %s", got, beforeValues)
	}
	if got := gateString(ctx, t, conn, definitions); got != beforeDefinitions {
		t.Errorf("column definitions changed:\n%s\nwant\n%s", got, beforeDefinitions)
	}
	types := gateString(ctx, t, conn, "SELECT arrayStringConcat(groupArray(type), ', ') FROM (SELECT type FROM system.columns WHERE database = currentDatabase() AND table = '"+table+"' ORDER BY position)")
	if want := "DateTime, String, LowCardinality(String), LowCardinality(Nullable(String)), String, String"; types != want {
		t.Errorf("types = %s, want %s", types, want)
	}
	mutations := gateString(ctx, t, conn, "SELECT toString(count()) || '/' || toString(countIf(is_done)) FROM system.mutations WHERE database = currentDatabase() AND table = '"+table+"'")
	if mutations != "2/2" {
		t.Errorf("mutations (all/done) = %s, want one finished mutation per column, 2/2", mutations)
	}
	gateExec(ctx, t, conn, "INSERT INTO "+table+" (ts, tenant) VALUES ('2026-11-02 00:00:00', 't9')")
	if got := gateString(ctx, t, conn, "SELECT "+quoted+" || '/' || toString(isNull(service)) FROM "+table+" WHERE tenant = 't9'"); got != "none/1" {
		t.Errorf("defaults after the change = %s, want none/1", got)
	}
	if again := findingsOf(Analyze(gateFacts(ctx, t, client, table, Usage{}, sample)), ruleLowCardinality); len(again) != 0 {
		t.Errorf("R4 fires again after its DDL: %+v", again)
	}
}

// TestGateCodec is G5: the MODIFY COLUMN ... CODEC that R5 generates is
// accepted on a DateTime64 sorting key column and on a String column, runs
// no mutation and leaves existing parts as they are until a merge rewrites
// them. A column with an empty compression_codec uses the part's default.
// The tables are too small for R5's size floors, so the gate scales the
// sizes it read before it runs the rules.
func TestGateCodec(t *testing.T) {
	client, conn, ctx := gateClickHouse(t)
	table := fmt.Sprintf("logchef_gate_codec_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table) })
	gateExec(ctx, t, conn,
		"CREATE TABLE "+table+" (ts DateTime64(3, 'UTC'), tenant String, msg String) ENGINE = MergeTree ORDER BY (tenant, ts) "+
			// Compact parts report no per-column size.
			"SETTINGS min_bytes_for_wide_part = 0",
		"SYSTEM STOP MERGES "+table,
	)
	for part := range 3 {
		gateExec(ctx, t, conn, fmt.Sprintf("INSERT INTO %s SELECT toDateTime64('2026-11-01 00:00:00', 3, 'UTC') + number * 0.137, 't', "+
			"concat('request ', toString(number %% 97), ' took ', toString(number %% 1000), ' ms') FROM numbers(%d, 100000)", table, part*100000))
	}
	parts := "SELECT arrayStringConcat(groupArray(concat(name, ':', column, ':', toString(column_data_compressed_bytes))), ',') " +
		"FROM (SELECT * FROM system.parts_columns WHERE database = currentDatabase() AND table = '" + table + "' AND active ORDER BY name, column)"
	values := "SELECT toString(count()) || ':' || toString(sum(cityHash64(toString((ts, tenant, msg))))) FROM " + table
	beforeParts, beforeValues := gateString(ctx, t, conn, parts), gateString(ctx, t, conn, values)
	codecs := "SELECT arrayStringConcat(groupArray(concat(name, '=', compression_codec)), ',') FROM (SELECT * FROM system.columns WHERE database = currentDatabase() AND table = '" + table + "' AND name != 'tenant' ORDER BY name)"
	if got := gateString(ctx, t, conn, codecs); got != "msg=,ts=" {
		t.Fatalf("codecs = %s, want none set", got)
	}
	if got := gateString(ctx, t, conn, "SELECT arrayStringConcat(groupUniqArray(default_compression_codec), ',') FROM system.parts WHERE database = currentDatabase() AND table = '"+table+"' AND active"); got != "LZ4" {
		t.Fatalf("parts' default codec = %s, want LZ4", got)
	}
	tsBefore := gateString(ctx, t, conn, "SELECT toString(sum(column_data_compressed_bytes)) FROM system.parts_columns WHERE database = currentDatabase() AND table = '"+table+"' AND active AND column = 'ts'")

	facts := gateFacts(ctx, t, client, table, Usage{}, nil)
	total := 100 * gib
	facts.Metadata.Table.TotalBytes = &total
	for i, column := range facts.Metadata.Columns {
		switch column.Name {
		case "ts":
			facts.Metadata.Columns[i].CompressedBytes, facts.Metadata.Columns[i].UncompressedBytes = 2*gib, 8*gib
		case "msg":
			facts.Metadata.Columns[i].CompressedBytes, facts.Metadata.Columns[i].UncompressedBytes = 60*gib, 150*gib
		}
	}
	found := findingsOf(Analyze(facts), ruleCodec)
	if len(found) != 2 {
		t.Fatalf("R5 findings = %+v, want 2", found)
	}
	for _, f := range found {
		gateExec(ctx, t, conn, f.DDL...)
	}

	if got := gateString(ctx, t, conn, codecs); got != "msg=CODEC(ZSTD(1)),ts=CODEC(Delta(8), ZSTD(1))" {
		t.Errorf("codecs = %s, want the generated ones", got)
	}
	if got := gateString(ctx, t, conn, "SELECT toString(count()) FROM system.mutations WHERE database = currentDatabase() AND table = '"+table+"'"); got != "0" {
		t.Errorf("%s mutations, want none", got)
	}
	if got := gateString(ctx, t, conn, parts); got != beforeParts {
		t.Errorf("parts changed before a merge:\n%s\nwant\n%s", got, beforeParts)
	}
	gateExec(ctx, t, conn, "SYSTEM START MERGES "+table, "OPTIMIZE TABLE "+table+" FINAL")
	tsAfter := gateString(ctx, t, conn, "SELECT toString(sum(column_data_compressed_bytes)) FROM system.parts_columns WHERE database = currentDatabase() AND table = '"+table+"' AND active AND column = 'ts'")
	if after, before := mustAtoi(t, tsAfter), mustAtoi(t, tsBefore); after >= before {
		t.Errorf("ts takes %d compressed bytes after the merge, want fewer than %d", after, before)
	}
	if got := gateString(ctx, t, conn, values); got != beforeValues {
		t.Errorf("values changed: hash %s, want %s", got, beforeValues)
	}
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestRebuildRulesOnClickHouse runs R6, R7 and R8 on metadata that
// ClickHouse returns, which no gate covers because they generate no DDL: the
// sorting and partition keys, the TTL clause, the partition count and the
// age of the data. Usage and the sample are given; the unpartitioned table
// is far below R7's size floor, so its size is scaled.
func TestRebuildRulesOnClickHouse(t *testing.T) {
	client, conn, ctx := gateClickHouse(t)
	suffix := time.Now().UnixNano()
	unpartitioned := fmt.Sprintf("logchef_rebuild_unpartitioned_%d", suffix)
	hourly := fmt.Sprintf("logchef_rebuild_hourly_%d", suffix)
	monthly := fmt.Sprintf("logchef_rebuild_monthly_%d", suffix)
	t.Cleanup(func() {
		for _, table := range []string{unpartitioned, hourly, monthly} {
			_ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+table)
		}
	})
	gateExec(ctx, t, conn,
		"CREATE TABLE "+unpartitioned+" (ts DateTime, msg String TTL ts + INTERVAL 1 DAY) ENGINE = MergeTree ORDER BY ts TTL ts + INTERVAL 30 DAY",
		"INSERT INTO "+unpartitioned+" SELECT now() - number, 'x' FROM numbers(1000)",
		"CREATE TABLE "+hourly+" (ts DateTime, msg String) ENGINE = MergeTree PARTITION BY toStartOfHour(ts) ORDER BY ts",
		"INSERT INTO "+hourly+" SETTINGS max_partitions_per_insert_block = 0 SELECT toDateTime('2026-01-01 00:00:00') + number * 3600, 'x' FROM numbers(1001)",
		"CREATE TABLE "+monthly+" (ts DateTime, service String, msg String) ENGINE = MergeTree PARTITION BY toYYYYMM(ts) ORDER BY ts",
		"INSERT INTO "+monthly+" SELECT now() - number * 3600, concat('svc-', toString(number % 7)), 'x' FROM numbers(200 * 24)",
	)
	noDDL := func(t *testing.T, found []models.SchemaFinding) {
		t.Helper()
		for _, f := range found {
			if f.DDL == nil || len(f.DDL) != 0 {
				t.Errorf("%s carries DDL %q", f.Rule, f.DDL)
			}
		}
	}

	t.Run("R7 A with a TTL under 90 days", func(t *testing.T) {
		facts := gateFacts(ctx, t, client, unpartitioned, Usage{}, nil)
		total := 100 * gib
		facts.Metadata.Table.TotalBytes = &total
		report := Analyze(facts)
		found := findingsOf(report, rulePartitioning)
		if len(found) != 1 || found[0].Title != "Partition the table by toDate(ts)" {
			t.Fatalf("R7 findings = %+v, want toDate(ts)", found)
		}
		if !slicesContainEvidence(found[0].Evidence, "Table TTL", "ts + toIntervalDay(30)") {
			t.Errorf("evidence %+v lacks the table TTL", found[0].Evidence)
		}
		noDDL(t, found)
		if r8 := findingsOf(report, ruleRetentionReview); len(r8) != 0 {
			t.Errorf("R8 fired on a table with a TTL: %+v", r8)
		}
	})

	t.Run("R7 B with 1,001 partitions", func(t *testing.T) {
		found := findingsOf(Analyze(gateFacts(ctx, t, client, hourly, Usage{}, nil)), rulePartitioning)
		if len(found) != 1 || found[0].Title != "Use a coarser partition key than toStartOfHour(ts)" {
			t.Fatalf("R7 findings = %+v, want a coarser key", found)
		}
		if !slicesContainEvidence(found[0].Evidence, "Active partitions", "1,001") || !slicesContainEvidence(found[0].Evidence, "Proposed partition key", "toYYYYMM(ts)") {
			t.Errorf("evidence = %+v", found[0].Evidence)
		}
		noDDL(t, found)
	})

	t.Run("R6 and R8 on 200 days of data", func(t *testing.T) {
		lookback := int64(7 * 86400)
		usage := Usage{
			Queries: 1000, Days: 30,
			Shapes:    []ShapeUsage{{Shape: models.FilterShape{{Column: "service", Kind: models.MatchEq}}, Queries: 1000, DurationMs: 1000 * 300}},
			Retention: models.QueryShapeCoverage{Days: 85, QueryCount: 3000, NullLookbackCount: 20, MaxLookbackSeconds: &lookback},
		}
		sample := &clickhouse.AdvisorSample{
			Input:   models.AdvisorInput{Name: clickhouse.AdvisorInputSample, State: models.AdvisorInputOK},
			Columns: []clickhouse.AdvisorCardinality{{Column: "service", Distinct: 7, MaxSliceDistinct: 7, Saturated: true}},
		}
		for i := range 7 {
			sample.Slices = append(sample.Slices, clickhouse.AdvisorSlice{Start: time.Date(2026, 11, 1+i, 3, 0, 0, 0, time.UTC), Rows: 30_000, Distinct: []uint64{7}})
		}
		facts := gateFacts(ctx, t, client, monthly, usage, sample)
		facts.Now = time.Now()
		if facts.Metadata.OldestData == nil {
			t.Fatal("the age of a table partitioned by month on its timestamp is unknown")
		}
		report := Analyze(facts)
		sortKey := findingsOf(report, ruleSortKey)
		if len(sortKey) != 1 || !slicesContainEvidence(sortKey[0].Evidence, "Proposed sorting key", "(service, ts)") || !sortKey[0].Rebuild {
			t.Fatalf("R6 findings = %+v, want (service, ts)", sortKey)
		}
		retention := findingsOf(report, ruleRetentionReview)
		if len(retention) != 1 || retention[0].Impact != models.ImpactInfo {
			t.Fatalf("R8 findings = %+v, want one", retention)
		}
		noDDL(t, append(sortKey, retention...))
	})
}
