package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mr-karan/logchef/pkg/models"
)

func mergeTree(database, name string) *AdvisorTable {
	return &AdvisorTable{Database: database, Name: name, Engine: "MergeTree"}
}

func view(name, query string) *AdvisorTable {
	return &AdvisorTable{Database: "logs", Name: name, Engine: "View",
		CreateQuery: fmt.Sprintf("CREATE VIEW logs.%s (`a` String, `b` String) AS %s", name, query)}
}

func lookupIn(tables ...*AdvisorTable) func(database, table string) (*AdvisorTable, error) {
	return func(database, table string) (*AdvisorTable, error) {
		for _, t := range tables {
			if t.Database == database && t.Name == table {
				return t, nil
			}
		}
		return nil, nil
	}
}

func TestResolveAdvisorTarget(t *testing.T) {
	base := mergeTree("logs", "base")
	tests := []struct {
		name   string
		tables []*AdvisorTable
		source string
		want   advisorResolution
	}{
		{
			name: "table", tables: []*AdvisorTable{base}, source: "base",
			want: advisorResolution{
				target: &models.AdvisorTarget{Kind: models.AdvisorTargetTable, Database: "logs", Table: "base", Engine: "MergeTree"},
				table:  base, pass: AdvisorPassThrough{All: true}, sample: AdvisorSampleTable{Database: "logs", Table: "base"},
			},
		},
		{
			name: "select star view", tables: []*AdvisorTable{base, view("v", "SELECT * FROM logs.base")}, source: "v",
			want: advisorResolution{
				target: &models.AdvisorTarget{Kind: models.AdvisorTargetViewBase, Database: "logs", Table: "base", Engine: "MergeTree"},
				table:  base, pass: AdvisorPassThrough{All: true}, sample: AdvisorSampleTable{Database: "logs", Table: "base"},
			},
		},
		{
			name: "renamed column does not pass through", tables: []*AdvisorTable{base, view("v", "SELECT a, b AS c, `d` AS d, lower(e) AS e FROM base")}, source: "v",
			want: advisorResolution{
				target: &models.AdvisorTarget{Kind: models.AdvisorTargetViewBase, Database: "logs", Table: "base", Engine: "MergeTree"},
				table:  base, pass: AdvisorPassThrough{Columns: []string{"a", "d"}}, sample: AdvisorSampleTable{Database: "logs", Table: "base"},
			},
		},
		{
			name: "nested view keeps the columns both pass", source: "outer",
			tables: []*AdvisorTable{base, view("inner", "SELECT a, b FROM logs.base"), view("outer", "SELECT a, c FROM logs.inner")},
			want: advisorResolution{
				target: &models.AdvisorTarget{Kind: models.AdvisorTargetViewBase, Database: "logs", Table: "base", Engine: "MergeTree"},
				table:  base, pass: AdvisorPassThrough{Columns: []string{"a"}}, sample: AdvisorSampleTable{Database: "logs", Table: "base"},
			},
		},
		{
			name: "view on another database", source: "v",
			tables: []*AdvisorTable{mergeTree("raw", "base"), view("v", "SELECT * FROM raw.base AS r")},
			want: advisorResolution{
				target: &models.AdvisorTarget{Kind: models.AdvisorTargetViewBase, Database: "raw", Table: "base", Engine: "MergeTree"},
				table:  mergeTree("raw", "base"), pass: AdvisorPassThrough{All: true}, sample: AdvisorSampleTable{Database: "raw", Table: "base"},
			},
		},
		{
			name: "distributed", source: "dist",
			tables: []*AdvisorTable{base, {Database: "logs", Name: "dist", Engine: "Distributed", EngineFull: "Distributed('prod', 'logs', 'base', rand())"}},
			want: advisorResolution{
				target: &models.AdvisorTarget{Kind: models.AdvisorTargetDistributedLocal, Database: "logs", Table: "base", Cluster: "prod", Engine: "MergeTree"},
				table:  base, pass: AdvisorPassThrough{All: true}, sample: AdvisorSampleTable{Database: "logs", Table: "dist", Leaf: true},
			},
		},
		{name: "join", source: "v", tables: []*AdvisorTable{base, view("v", "SELECT * FROM logs.base AS x JOIN logs.other AS y ON x.a = y.a")},
			want: advisorResolution{reason: AdvisorViewNotResolvable}},
		{name: "union", source: "v", tables: []*AdvisorTable{base, view("v", "SELECT a FROM logs.base UNION ALL SELECT a FROM logs.base")},
			want: advisorResolution{reason: AdvisorViewNotResolvable}},
		{name: "subquery", source: "v", tables: []*AdvisorTable{base, view("v", "SELECT * FROM (SELECT * FROM logs.base)")},
			want: advisorResolution{reason: AdvisorViewNotResolvable}},
		{name: "table function", source: "v", tables: []*AdvisorTable{view("v", "SELECT * FROM numbers(10)")},
			want: advisorResolution{reason: AdvisorViewNotResolvable}},
		{name: "cte shadowing the base", source: "v", tables: []*AdvisorTable{base, view("v", "WITH base AS (SELECT 'x' AS a) SELECT * FROM base")},
			want: advisorResolution{reason: AdvisorViewNotResolvable}},
		{
			name: "unrelated cte", source: "v", tables: []*AdvisorTable{base, view("v", "WITH 5 AS n SELECT * FROM base")},
			want: advisorResolution{
				target: &models.AdvisorTarget{Kind: models.AdvisorTargetViewBase, Database: "logs", Table: "base", Engine: "MergeTree"},
				table:  base, pass: AdvisorPassThrough{All: true}, sample: AdvisorSampleTable{Database: "logs", Table: "base"},
			},
		},
		{name: "cycle", source: "v1", tables: []*AdvisorTable{view("v1", "SELECT * FROM logs.v2"), view("v2", "SELECT * FROM logs.v1")},
			want: advisorResolution{reason: AdvisorViewNotResolvable}},
		{name: "more than three hops", source: "v4", tables: []*AdvisorTable{base,
			view("v1", "SELECT * FROM logs.base"), view("v2", "SELECT * FROM logs.v1"),
			view("v3", "SELECT * FROM logs.v2"), view("v4", "SELECT * FROM logs.v3")},
			want: advisorResolution{reason: AdvisorViewNotResolvable}},
		{name: "base table not readable", source: "v", tables: []*AdvisorTable{view("v", "SELECT * FROM logs.base")},
			want: advisorResolution{reason: AdvisorTargetNotReadable}},
		{name: "source not readable", source: "base", want: advisorResolution{reason: AdvisorTargetNotReadable}},
		{name: "distributed local not readable", source: "dist",
			tables: []*AdvisorTable{{Database: "logs", Name: "dist", Engine: "Distributed", EngineFull: "Distributed('prod', 'logs', 'base')"}},
			want:   advisorResolution{reason: AdvisorTargetNotReadable}},
		{name: "materialized view", source: "mv", tables: []*AdvisorTable{{Database: "logs", Name: "mv", Engine: "MaterializedView"}},
			want: advisorResolution{reason: AdvisorUnsupportedEngine}},
		{name: "merge", source: "m", tables: []*AdvisorTable{{Database: "logs", Name: "m", Engine: "Merge"}},
			want: advisorResolution{reason: AdvisorUnsupportedEngine}},
		{name: "view over distributed", source: "v", tables: []*AdvisorTable{view("v", "SELECT * FROM logs.dist"),
			{Database: "logs", Name: "dist", Engine: "Distributed", EngineFull: "Distributed('prod', 'logs', 'base')"}},
			want: advisorResolution{reason: AdvisorUnsupportedEngine}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveAdvisorTarget(lookupIn(tt.tables...), "logs", tt.source)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestResolveAdvisorTargetPassesLookupErrors(t *testing.T) {
	denied := &clickhouse.Exception{Code: chExceptionAccessDenied}
	_, err := resolveAdvisorTarget(func(string, string) (*AdvisorTable, error) { return nil, denied }, "logs", "base")
	if !errors.Is(err, denied) {
		t.Fatalf("err = %v, want the lookup's error", err)
	}
}

func TestAdvisorInputState(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want models.AdvisorInputState
	}{
		{"no error", nil, models.AdvisorInputOK},
		{"access denied", &clickhouse.Exception{Code: 497}, models.AdvisorInputDenied},
		{"unknown table", &clickhouse.Exception{Code: 60}, models.AdvisorInputMissing},
		{"rows budget", &clickhouse.Exception{Code: 158}, models.AdvisorInputLimited},
		{"bytes budget", &clickhouse.Exception{Code: 307}, models.AdvisorInputLimited},
		{"time budget", &clickhouse.Exception{Code: 159}, models.AdvisorInputLimited},
		{"memory budget", &clickhouse.Exception{Code: 241}, models.AdvisorInputLimited},
		{"budget deadline", fmt.Errorf("read: %w", context.DeadlineExceeded), models.AdvisorInputLimited},
		{"wrapped public error", publicQueryError(&clickhouse.Exception{Code: 60, Message: "Table system.query_log does not exist"}), models.AdvisorInputMissing},
		{"other exception", &clickhouse.Exception{Code: 62, Message: "Syntax error"}, models.AdvisorInputError},
		{"transport", errors.New("connection refused"), models.AdvisorInputError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, detail := advisorInputState(tt.err)
			if got != tt.want {
				t.Fatalf("state %q, want %q", got, tt.want)
			}
			if (got == models.AdvisorInputOK) != (detail == "") {
				t.Fatalf("detail %q for state %q", detail, got)
			}
		})
	}
}

func TestOldestData(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 8, d, 0, 0, 0, 0, time.UTC) }
	partitioned := []AdvisorColumn{{Name: "ts", InPartitionKey: true, InSortingKey: true}}
	parts := []AdvisorPartition{{Partition: "20260802", MinTime: day(2)}, {Partition: "20260801", MinTime: day(1)}}
	all := AdvisorPassThrough{All: true}
	if got := oldestData(partitioned, parts, false, "ts", all); got == nil || !got.Equal(day(1)) {
		t.Fatalf("partitioned by ts: got %v, want %v", got, day(1))
	}
	tests := []struct {
		name       string
		columns    []AdvisorColumn
		partitions []AdvisorPartition
		truncated  bool
		pass       AdvisorPassThrough
	}{
		{"unpartitioned", []AdvisorColumn{{Name: "ts", InSortingKey: true}}, []AdvisorPartition{{Partition: "tuple()", MinTime: time.Unix(0, 0)}}, false, all},
		{"partitioned by another time column", []AdvisorColumn{{Name: "ts", InSortingKey: true}, {Name: "created", InPartitionKey: true}}, parts, false, all},
		{"partition key without a time", partitioned, []AdvisorPartition{{Partition: "a", MinTime: time.Unix(0, 0)}}, false, all},
		{"one partition without a time", partitioned, append(parts, AdvisorPartition{Partition: "x", MinTime: time.Unix(0, 0)}), false, all},
		{"partitions truncated", partitioned, parts, true, all},
		{"no parts", partitioned, nil, false, all},
		{"timestamp hidden by the view", partitioned, parts, false, AdvisorPassThrough{Columns: []string{"msg"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := oldestData(tt.columns, tt.partitions, tt.truncated, "ts", tt.pass); got != nil {
				t.Fatalf("age is %v, want unknown", got)
			}
		})
	}
}

func TestSliceStarts(t *testing.T) {
	starts := sliceStarts(time.Date(2026, 11, 16, 9, 30, 0, 0, time.UTC))
	days, hours := map[int]bool{}, map[int]bool{}
	for _, start := range starts {
		if !start.Before(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)) || start.Before(time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("slice %v is outside the last 7 days", start)
		}
		days[start.YearDay()], hours[start.Hour()] = true, true
	}
	if len(starts) != 7 || len(days) != 7 || len(hours) != 7 {
		t.Fatalf("starts %v: want 7 slices on 7 days at 7 hours", starts)
	}
}

func TestSampleQueryEscapesAndNumbersSlices(t *testing.T) {
	query := sampleQuery(AdvisorSampleTable{Database: "logs", Table: "t`x"}, "ts", []string{"a`b", "c"}, sliceStarts(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)))
	for _, want := range []string{"`a\\x60b` AS __logchef_c0", "`c` AS __logchef_c1", "uniqCombined(__logchef_c1) AS __logchef_u1", "FROM `logs`.`t\\x60x`",
		"SELECT 1 AS __logchef_slice", "SELECT 7 AS __logchef_slice", "LIMIT 150000", "GROUP BY __logchef_slice WITH ROLLUP", "`ts` >= toDateTime('2026-11-15 03:00:00', 'UTC')"} {
		if !strings.Contains(query, want) {
			t.Fatalf("query lacks %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "SELECT 0 AS __logchef_slice") {
		t.Fatal("slice 0 is the ROLLUP total")
	}
}

func TestSummarizeSample(t *testing.T) {
	starts := sliceStarts(time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC))
	columns := []string{"level", "request_id"}
	full := func(rows, level, request uint64) []uint64 { return []uint64{rows, level, request} }

	saturated := map[uint8][]uint64{0: full(150_000, 4, 120_000)}
	for slice := uint8(1); slice <= 7; slice++ {
		saturated[slice] = full(21_000, 3, 17_000)
	}
	_, cards := summarizeSample(columns, starts, saturated)
	if !cards[0].Saturated || cards[0].Distinct != 4 || cards[0].MaxSliceDistinct != 3 {
		t.Fatalf("level: %+v, want saturated with 4 distinct", cards[0])
	}
	if cards[1].Saturated {
		t.Fatalf("request_id: %+v, a total over 1.5 times the largest slice is not saturated", cards[1])
	}

	truncated := map[uint8][]uint64{0: full(1_050_000, 6, 900_000)}
	for slice := uint8(1); slice <= 7; slice++ {
		truncated[slice] = full(AdvisorSliceRowLimit, 5, 140_000)
	}
	slices, cards := summarizeSample(columns, starts, truncated)
	if !slices[0].Truncated || cards[0].Saturated || cards[1].Saturated {
		t.Fatalf("truncated slices: %+v %+v, want no saturation", slices[0], cards)
	}
	if cards[1].Distinct != 900_000 {
		t.Fatalf("request_id lower bound %d, want 900000", cards[1].Distinct)
	}

	sparse := map[uint8][]uint64{0: full(80_000, 3, 10), 1: full(20_000, 3, 10), 2: full(20_000, 3, 10), 3: full(20_000, 3, 10), 4: full(20_000, 3, 10)}
	slices, cards = summarizeSample(columns, starts, sparse)
	if cards[0].Saturated || slices[6].Rows != 0 {
		t.Fatalf("four full slices: %+v, want no saturation and empty slices kept", cards[0])
	}
}

func TestBudgetSettings(t *testing.T) {
	source := clickhouse.Settings{"max_rows_to_read": int64(1000), "max_execution_time_leaf": 300, "max_bytes_to_read_leaf": int64(10)}
	withLeafTimeout := clientWithModes(source, clickhouse.Settings{"timeout_overflow_mode_leaf": "throw"})
	got, err := withLeafTimeout.budgetSettings(context.Background(), advisorSampleBudget, true)
	if err != nil {
		t.Fatal(err)
	}
	want := clickhouse.Settings{
		"max_rows_to_read": int64(1000), "max_bytes_to_read": advisorSampleBudget.bytes, "max_memory_usage": advisorSampleBudget.memory,
		"max_threads": 2, "max_rows_to_read_leaf": advisorSampleBudget.rows, "max_bytes_to_read_leaf": int64(10), "max_execution_time_leaf": int64(10),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("leaf settings\n got %v\nwant %v", got, want)
	}

	older := clientWithModes(nil, clickhouse.Settings{})
	got, err = older.budgetSettings(context.Background(), advisorSampleBudget, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["max_execution_time_leaf"]; ok {
		t.Fatal("a server before 23.11 does not know max_execution_time_leaf")
	}
	got, err = older.budgetSettings(context.Background(), advisorSystemBudget, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["max_rows_to_read_leaf"]; ok || got["max_rows_to_read"] != advisorSystemBudget.rows {
		t.Fatalf("non-leaf settings %v", got)
	}
}
