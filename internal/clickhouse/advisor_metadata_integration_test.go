package clickhouse

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/mr-karan/logchef/pkg/models"
)

// advisorTestCluster is the two-shard cluster of
// testdata/advisor_two_shards.xml, which CI copies into the server.
const advisorTestCluster = "logchef_two_shards"

func advisorFixture(ctx context.Context, t *testing.T, client *Client, ddl ...string) {
	t.Helper()
	for _, statement := range ddl {
		if err := client.conn.Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func dropAfter(t *testing.T, client *Client, names ...string) {
	t.Cleanup(func() {
		for _, name := range names {
			_ = client.conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+name)
		}
	})
}

func hasCluster(ctx context.Context, t *testing.T, client *Client) bool {
	t.Helper()
	var shards uint64
	if err := client.conn.QueryRow(ctx, "SELECT count() FROM system.clusters WHERE cluster = ?", advisorTestCluster).Scan(&shards); err != nil {
		t.Fatal(err)
	}
	return shards == 2
}

func knowsLeafTimeout(ctx context.Context, t *testing.T, client *Client) bool {
	t.Helper()
	modes, err := client.throwModes.get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, ok := modes["timeout_overflow_mode_leaf"]
	return ok
}

func inputState(t *testing.T, meta *AdvisorMetadata, name string) models.AdvisorInputState {
	t.Helper()
	input, ok := meta.Input(name)
	if !ok {
		t.Fatalf("no input %s in %+v", name, meta.Inputs)
	}
	return input.State
}

// TestAdvisorMetadataClickHouse reads back a MergeTree table with one skip
// index, and the same table through a view and a Distributed table.
func TestAdvisorMetadataClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	suffix := time.Now().UnixNano()
	table := fmt.Sprintf("logchef_advisor_%d", suffix)
	viewName, distName := table+"_v", table+"_d"
	dropAfter(t, client, viewName, distName, table)
	advisorFixture(ctx, t, client,
		"CREATE TABLE "+table+" (ts DateTime, tenant LowCardinality(String), user_id String CODEC(ZSTD(1)), msg String DEFAULT 'x',"+
			" INDEX idx_user user_id TYPE bloom_filter GRANULARITY 1) ENGINE = MergeTree PARTITION BY toDate(ts) ORDER BY (tenant, ts)"+
			" SETTINGS min_bytes_for_wide_part = 0",
		"INSERT INTO "+table+" (ts, tenant, user_id) SELECT toDateTime('2026-08-01 00:00:00', 'UTC') + number * 3600, 't1', toString(number) FROM numbers(72)",
		"CREATE VIEW "+viewName+" AS SELECT ts, user_id, tenant AS team FROM "+table,
		"SYSTEM FLUSH LOGS",
	)

	meta, err := client.AdvisorMetadata(ctx, "default", table, "ts")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Unavailable != "" || meta.Target == nil || meta.Target.Kind != models.AdvisorTargetTable {
		t.Fatalf("target %+v, unavailable %q", meta.Target, meta.Unavailable)
	}
	for _, name := range []string{AdvisorInputTables, AdvisorInputColumns, AdvisorInputIndexes, AdvisorInputParts, AdvisorInputQueryLog} {
		if state := inputState(t, meta, name); state != models.AdvisorInputOK {
			t.Fatalf("%s is %s: %+v", name, state, meta.Inputs)
		}
	}
	if meta.Table.SortingKey != "tenant, ts" || meta.Table.PartitionKey != "toDate(ts)" || meta.Table.TotalRows == nil || *meta.Table.TotalRows != 72 {
		t.Fatalf("table %+v", meta.Table)
	}
	columns := map[string]AdvisorColumn{}
	for _, column := range meta.Columns {
		columns[column.Name] = column
	}
	if c := columns["ts"]; !c.InSortingKey || !c.InPartitionKey || !c.InPrimaryKey {
		t.Fatalf("ts %+v", c)
	}
	if c := columns["user_id"]; c.Codec != "CODEC(ZSTD(1))" || c.InSortingKey || c.CompressedBytes == 0 {
		t.Fatalf("user_id %+v", c)
	}
	if c := columns["msg"]; c.DefaultKind != "DEFAULT" || c.DefaultExpression != "'x'" {
		t.Fatalf("msg %+v", c)
	}
	if len(meta.Indexes) != 1 || meta.Indexes[0].Name != "idx_user" || meta.Indexes[0].Expr != "user_id" || !strings.HasPrefix(meta.Indexes[0].Type, "bloom_filter") {
		t.Fatalf("indexes %+v", meta.Indexes)
	}
	if len(meta.Partitions) != 3 || meta.PartitionsTruncated {
		t.Fatalf("partitions %+v", meta.Partitions)
	}
	if want := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC); meta.OldestData == nil || !meta.OldestData.Equal(want) {
		t.Fatalf("oldest data %v, want %v", meta.OldestData, want)
	}
	if meta.QueryLog == nil || meta.ServerVersion == "" {
		t.Fatalf("query log %+v, version %q", meta.QueryLog, meta.ServerVersion)
	}

	viewMeta, err := client.AdvisorMetadata(ctx, "default", viewName, "ts")
	if err != nil {
		t.Fatal(err)
	}
	if viewMeta.Target == nil || viewMeta.Target.Kind != models.AdvisorTargetViewBase || viewMeta.Target.Table != table {
		t.Fatalf("view target %+v, unavailable %q", viewMeta.Target, viewMeta.Unavailable)
	}
	if !viewMeta.PassThrough.Has("user_id") || viewMeta.PassThrough.Has("tenant") || viewMeta.PassThrough.Has("team") {
		t.Fatalf("view passes %+v", viewMeta.PassThrough)
	}

	if !hasCluster(ctx, t, client) {
		t.Logf("cluster %s is not configured; skipping the Distributed read", advisorTestCluster)
		return
	}
	advisorFixture(ctx, t, client, fmt.Sprintf("CREATE TABLE %s AS %s ENGINE = Distributed(%s, default, %s)", distName, table, advisorTestCluster, table))
	distMeta, err := client.AdvisorMetadata(ctx, "default", distName, "ts")
	if err != nil {
		t.Fatal(err)
	}
	want := models.AdvisorTarget{Kind: models.AdvisorTargetDistributedLocal, Database: "default", Table: table, Cluster: advisorTestCluster, Engine: "MergeTree"}
	if distMeta.Target == nil || *distMeta.Target != want || distMeta.Sample != (AdvisorSampleTable{Database: "default", Table: distName, Leaf: true}) {
		t.Fatalf("distributed target %+v, sample %+v", distMeta.Target, distMeta.Sample)
	}
}

// TestAdvisorSampleTruncatedSlicesClickHouse fills each slice past its LIMIT
// on a table ordered by (tenant, ts). The first tenant has few user ids, so
// every truncated slice holds mostly its rows: the sample must not call the
// column saturated.
func TestAdvisorSampleTruncatedSlicesClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	table := fmt.Sprintf("logchef_advisor_sample_%d", time.Now().UnixNano())
	dropAfter(t, client, table)
	advisorFixture(ctx, t, client, "CREATE TABLE "+table+" (ts DateTime, tenant String, user_id String) ENGINE = MergeTree ORDER BY (tenant, ts)")
	now := time.Now()
	for _, start := range sliceStarts(now) {
		advisorFixture(ctx, t, client, fmt.Sprintf(`INSERT INTO %s SELECT toDateTime('%s', 'UTC') + number %% 600,
			if(number < 100000, 'a', 'b'), if(number < 100000, toString(number %% 10), toString(number)) FROM numbers(200000)`,
			table, start.UTC().Format(time.DateTime)))
	}
	meta, err := client.AdvisorMetadata(ctx, "default", table, "ts")
	if err != nil {
		t.Fatal(err)
	}
	sample, err := client.AdvisorSample(ctx, meta, "ts", []string{"user_id"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if sample.Input.State != models.AdvisorInputOK {
		t.Fatalf("sample %+v", sample.Input)
	}
	for _, slice := range sample.Slices {
		if !slice.Truncated {
			t.Fatalf("slice %+v holds 200000 rows and is not truncated", slice)
		}
	}
	if card := sample.Columns[0]; card.Saturated || card.Distinct >= 100000 {
		t.Fatalf("user_id %+v: want a biased lower bound and no saturation", card)
	}
	t.Logf("sample read %d rows and %d bytes for %d slices of %d rows (budget %d rows)",
		sample.Input.ReadRows, sample.Input.ReadBytes, len(sample.Slices), AdvisorSliceRowLimit, advisorSampleBudget.rows)
	if sample.Input.ReadRows == 0 {
		t.Fatal("the sample reported no rows read")
	}

	skipped := &AdvisorMetadata{Target: meta.Target, PassThrough: meta.PassThrough, Columns: []AdvisorColumn{{Name: "ts"}}}
	if sample, err := client.AdvisorSample(ctx, skipped, "ts", []string{"user_id"}, now); err != nil || sample.Input.State != models.AdvisorInputSkipped {
		t.Fatalf("a timestamp outside the keys: %+v, %v; want skipped", sample, err)
	}
}

// TestAdvisorSampleColumnNamesClickHouse samples columns named like the
// sample's own aliases: an alias of the same name would replace them.
func TestAdvisorSampleColumnNamesClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	table := fmt.Sprintf("logchef_advisor_names_%d", time.Now().UnixNano())
	dropAfter(t, client, table)
	advisorFixture(ctx, t, client, "CREATE TABLE "+table+" (ts DateTime, slice String, sampled_rows String, u0 String) ENGINE = MergeTree ORDER BY ts")
	now := time.Now()
	for _, start := range sliceStarts(now) {
		advisorFixture(ctx, t, client, fmt.Sprintf(`INSERT INTO %s SELECT toDateTime('%s', 'UTC') + number, toString(number), toString(number), toString(number) FROM numbers(100)`,
			table, start.UTC().Format(time.DateTime)))
	}
	meta, err := client.AdvisorMetadata(ctx, "default", table, "ts")
	if err != nil {
		t.Fatal(err)
	}
	sample, err := client.AdvisorSample(ctx, meta, "ts", []string{"slice", "sampled_rows", "u0"}, now)
	if err != nil || sample.Input.State != models.AdvisorInputOK {
		t.Fatalf("sample %+v, %v", sample, err)
	}
	for _, card := range sample.Columns {
		if card.Distinct != 100 || card.MaxSliceDistinct != 100 {
			t.Errorf("%s: %+v, want 100 distinct values", card.Column, card)
		}
	}
}

// TestAdvisorBudgetsClickHouse is gate G6: each budget stops its read with
// a state of limited, a short budget ends a read under a long context, and
// the sample through a Distributed table stays within its budget.
func TestAdvisorBudgetsClickHouse(t *testing.T) {
	client, _ := integrationClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	table := fmt.Sprintf("logchef_advisor_budget_%d", time.Now().UnixNano())
	distName := table + "_d"
	dropAfter(t, client, distName, table)
	advisorFixture(ctx, t, client,
		"CREATE TABLE "+table+" (ts DateTime, user_id String) ENGINE = MergeTree ORDER BY ts",
		"INSERT INTO "+table+" SELECT now() - number, toString(cityHash64(number)) FROM numbers(2000000)")

	drain := func(rows driver.Rows) error {
		for rows.Next() {
		}
		return rows.Err()
	}
	big := int64(1) << 40
	for _, tt := range []struct {
		name   string
		budget advisorBudget
		query  string
	}{
		{"rows", advisorBudget{seconds: 5, rows: 1000, bytes: big, memory: big}, "SELECT count() FROM " + table + " WHERE NOT ignore(user_id)"},
		{"bytes", advisorBudget{seconds: 5, rows: big, bytes: 1000, memory: big}, "SELECT count() FROM " + table + " WHERE NOT ignore(user_id)"},
		{"memory", advisorBudget{seconds: 5, rows: big, bytes: big, memory: 1 << 20}, "SELECT uniqExact(user_id) FROM " + table},
	} {
		t.Run(tt.name, func(t *testing.T) {
			read, err := client.queryBudgeted(ctx, tt.budget, false, drain, tt.query)
			if state, _ := advisorInputState(err); state != models.AdvisorInputLimited {
				t.Fatalf("state %s, err %v", state, err)
			}
			t.Logf("%s budget: read %d rows, %d bytes", tt.name, read.ReadRows, read.ReadBytes)
		})
	}

	t.Run("time under a long context", func(t *testing.T) {
		start := time.Now()
		slow := "SELECT count() FROM numbers(100000000000) WHERE NOT ignore(sipHash64(number))"
		_, err := client.queryBudgeted(ctx, advisorBudget{seconds: 2, memory: big}, false, drain, slow)
		elapsed := time.Since(start)
		if state, _ := advisorInputState(err); state != models.AdvisorInputLimited || elapsed > 2*time.Second+queryTimeoutGrace {
			t.Fatalf("state %s after %v, err %v", state, elapsed, err)
		}
	})

	if !hasCluster(ctx, t, client) {
		t.Logf("cluster %s is not configured; skipping the Distributed budgets", advisorTestCluster)
		return
	}
	advisorFixture(ctx, t, client, fmt.Sprintf("CREATE TABLE %s AS %s ENGINE = Distributed(%s, default, %s)", distName, table, advisorTestCluster, table))
	for _, preferLocal := range []int{0, 1} {
		t.Run(fmt.Sprintf("leaf rows prefer_localhost_replica=%d", preferLocal), func(t *testing.T) {
			leafClient := advisorClient(t, map[string]any{"prefer_localhost_replica": preferLocal})
			_, err := leafClient.queryBudgeted(ctx, advisorBudget{seconds: 5, rows: 1000, bytes: big, memory: big}, true, drain,
				"SELECT count() FROM "+distName+" WHERE NOT ignore(user_id)")
			if state, _ := advisorInputState(err); state != models.AdvisorInputLimited {
				t.Fatalf("state %s, err %v", state, err)
			}
		})
	}
	t.Run("leaf timeout overrides a larger source value", func(t *testing.T) {
		if !knowsLeafTimeout(ctx, t, client) {
			t.Skip("max_execution_time_leaf needs ClickHouse 23.11")
		}
		leafClient := advisorClient(t, map[string]any{"max_execution_time_leaf": 300, "prefer_localhost_replica": 0})
		queryID := fmt.Sprintf("logchef-advisor-leaf-%d", time.Now().UnixNano())
		start := time.Now()
		_, err := leafClient.queryBudgeted(ch.Context(ctx, ch.WithQueryID(queryID)), advisorBudget{seconds: 2, memory: big}, true, drain,
			"SELECT count() FROM "+distName+" WHERE NOT ignore(arrayMap(x -> sipHash64(x, user_id), range(2000))) SETTINGS max_block_size = 1024")
		elapsed := time.Since(start)
		if state, _ := advisorInputState(err); state != models.AdvisorInputLimited || elapsed > 2*time.Second+queryTimeoutGrace {
			t.Fatalf("state %s after %v, err %v", state, elapsed, err)
		}
		advisorFixture(ctx, t, client, "SYSTEM FLUSH LOGS")
		var leaf string
		if err := client.conn.QueryRow(ctx, "SELECT Settings['max_execution_time_leaf'] FROM system.query_log WHERE query_id = ? AND type != 'QueryStart'", queryID).Scan(&leaf); err != nil {
			t.Fatal(err)
		}
		if leaf != "2" {
			t.Fatalf("the server ran with max_execution_time_leaf %q, want the budget's 2", leaf)
		}
	})
	// The real seven-branch sample over more rows than a reduced budget
	// stops as limited, on the table and through both shards.
	sampleTable := AdvisorSampleTable{Database: "default", Table: table}
	limitedSample := sampleQuery(sampleTable, "ts", []string{"user_id"}, sliceStarts(time.Now()))
	sampleBudget := advisorBudget{seconds: 10, rows: 10_000, bytes: big, memory: big}
	read, err := client.queryBudgeted(ctx, sampleBudget, false, drain, limitedSample)
	if state, _ := advisorInputState(err); state != models.AdvisorInputLimited {
		t.Fatalf("sample over its budget on the table: state %s, err %v", state, err)
	}
	t.Logf("sample stopped at a %d-row budget on the table after %d rows read", sampleBudget.rows, read.ReadRows)
	sampleTable.Table, sampleTable.Leaf = distName, true
	limitedSample = sampleQuery(sampleTable, "ts", []string{"user_id"}, sliceStarts(time.Now()))
	for _, preferLocal := range []int{0, 1} {
		t.Run(fmt.Sprintf("sample over its budget prefer_localhost_replica=%d", preferLocal), func(t *testing.T) {
			leafClient := advisorClient(t, map[string]any{"prefer_localhost_replica": preferLocal})
			read, err := leafClient.queryBudgeted(ctx, sampleBudget, true, drain, limitedSample)
			if state, _ := advisorInputState(err); state != models.AdvisorInputLimited {
				t.Fatalf("state %s, err %v", state, err)
			}
			t.Logf("Distributed sample stopped at a %d-row budget after %d rows read", sampleBudget.rows, read.ReadRows)
		})
	}
	for _, preferLocal := range []int{0, 1} {
		t.Run(fmt.Sprintf("sample prefer_localhost_replica=%d", preferLocal), func(t *testing.T) {
			sampleClient := advisorClient(t, map[string]any{"prefer_localhost_replica": preferLocal})
			meta, err := sampleClient.AdvisorMetadata(ctx, "default", distName, "ts")
			if err != nil {
				t.Fatal(err)
			}
			sample, err := sampleClient.AdvisorSample(ctx, meta, "ts", []string{"user_id"}, time.Now())
			if err != nil || sample.Input.State != models.AdvisorInputOK {
				t.Fatalf("sample %+v, %v", sample, err)
			}
			t.Logf("Distributed sample: read %d rows, %d bytes (budget %d rows, %d bytes)",
				sample.Input.ReadRows, sample.Input.ReadBytes, advisorSampleBudget.rows, advisorSampleBudget.bytes)
		})
	}
}

// TestAdvisorGrantsClickHouse is gate G7: reads as a user with no grant on
// the table either succeed or come back denied, system.tables shows no row
// for the table, and an unknown system table is missing.
func TestAdvisorGrantsClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	suffix := time.Now().UnixNano()
	table, user := fmt.Sprintf("logchef_advisor_grants_%d", suffix), fmt.Sprintf("logchef_advisor_user_%d", suffix)
	dropAfter(t, client, table)
	advisorFixture(ctx, t, client, "CREATE TABLE "+table+" (ts DateTime) ENGINE = MergeTree ORDER BY ts")
	if err := client.conn.Exec(ctx, "CREATE USER "+user+" IDENTIFIED WITH no_password"); err != nil {
		if IsAccessDeniedError(err) {
			t.Skipf("the test user cannot manage users: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.conn.Exec(context.Background(), "DROP USER IF EXISTS "+user) })

	restricted := advisorClientAs(t, user, nil)
	meta, err := restricted.AdvisorMetadata(ctx, "default", table, "ts")
	if err != nil {
		t.Fatal(err)
	}
	if meta.Unavailable != AdvisorTargetNotReadable {
		t.Fatalf("unavailable %q, inputs %+v: want %s", meta.Unavailable, meta.Inputs, AdvisorTargetNotReadable)
	}
	for _, input := range []models.AdvisorInput{
		second(restricted.advisorIndexes(ctx, "default", table)),
		second(restricted.advisorPartitions(ctx, "default", table)),
		second(restricted.advisorQueryLog(ctx, "default."+table)),
	} {
		if input.State != models.AdvisorInputOK && input.State != models.AdvisorInputDenied && input.State != models.AdvisorInputMissing {
			t.Errorf("%s as a user without grants: %+v", input.Name, input)
		}
		t.Logf("%s as a user without grants: %s", input.Name, input.State)
	}

	_, err = client.queryBudgeted(ctx, advisorSystemBudget, false, func(driver.Rows) error { return nil },
		"SELECT count() FROM system.logchef_never_existed_log")
	if state, _ := advisorInputState(err); state != models.AdvisorInputMissing {
		t.Fatalf("unknown system table: state %s, err %v", state, err)
	}
}

// TestAdvisorQueryLogThroughViewAndDistributedClickHouse is gate G8:
// has(tables, 'db.table') matches a read through a view and through a
// Distributed table.
func TestAdvisorQueryLogThroughViewAndDistributedClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	table := fmt.Sprintf("logchef_advisor_log_%d", time.Now().UnixNano())
	viewName, distName := table+"_v", table+"_d"
	dropAfter(t, client, viewName, distName, table)
	advisorFixture(ctx, t, client,
		"CREATE TABLE "+table+" (ts DateTime, a String) ENGINE = MergeTree ORDER BY ts",
		"INSERT INTO "+table+" VALUES (now(), 'x')",
		"CREATE VIEW "+viewName+" AS SELECT * FROM "+table)
	sources := []string{viewName}
	if hasCluster(ctx, t, client) {
		advisorFixture(ctx, t, client, fmt.Sprintf("CREATE TABLE %s AS %s ENGINE = Distributed(%s, default, %s)", distName, table, advisorTestCluster, table))
		sources = append(sources, distName)
	}
	for _, source := range sources {
		var count uint64
		if err := client.conn.QueryRow(ctx, "SELECT count() FROM "+source+" WHERE a = 'x'").Scan(&count); err != nil {
			t.Fatal(err)
		}
	}
	advisorFixture(ctx, t, client, "SYSTEM FLUSH LOGS")
	for _, source := range sources {
		summary, input := client.advisorQueryLog(ctx, "default."+source)
		if input.State != models.AdvisorInputOK || summary.Queries == 0 {
			t.Fatalf("%s: %+v, %+v", source, input, summary)
		}
	}
}

func second[A, B any](_ A, b B) B { return b }

func advisorClient(t *testing.T, settings map[string]any) *Client {
	t.Helper()
	return advisorClientAs(t, "", settings)
}

func advisorClientAs(t *testing.T, user string, settings map[string]any) *Client {
	t.Helper()
	client, err := NewClient(ClientOptions{Host: os.Getenv("LOGCHEF_TEST_CLICKHOUSE_ADDR"), Username: user, QuerySettings: settings},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
