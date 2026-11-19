package advisor

import (
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

// slicedSample is an ok sample of 7 slices that start on 2026-11-09 03:00
// UTC, one a day, 3 hours apart. Slice i holds rows[i] rows and, for each
// card, card.MaxSliceDistinct values.
func slicedSample(rows []uint64, cards ...clickhouse.AdvisorCardinality) *clickhouse.AdvisorSample {
	sample := sampleOf(cards...)
	start := time.Date(2026, 11, 9, 3, 0, 0, 0, time.UTC)
	for i, n := range rows {
		slice := clickhouse.AdvisorSlice{Start: start.AddDate(0, 0, i).Add(time.Duration(i*3) * time.Hour), Rows: n, Truncated: n >= clickhouse.AdvisorSliceRowLimit}
		for _, card := range cards {
			slice.Distinct = append(slice.Distinct, card.MaxSliceDistinct)
		}
		sample.Slices = append(sample.Slices, slice)
	}
	return sample
}

var fullSlices = []uint64{24_000, 25_000, 21_000, 30_000, 22_000, 26_000, 9_000}

func repetitive(name string, distinct uint64) clickhouse.AdvisorCardinality {
	return clickhouse.AdvisorCardinality{Column: name, Distinct: distinct, MaxSliceDistinct: distinct, Saturated: true}
}

func TestLowCardinalityFires(t *testing.T) {
	const column = "a`b"
	big := clickhouse.AdvisorColumn{Name: column, Type: "String", CompressedBytes: 30 * gib, UncompressedBytes: 200 * gib}
	report := Analyze(Facts{Metadata: testMeta(big), Sample: slicedSample(fullSlices, repetitive(column, 12))})
	found := findingsOf(report, ruleLowCardinality)
	if len(found) != 1 {
		t.Fatalf("R4 findings = %+v, want 1", report.Findings)
	}
	f := found[0]
	if want := []string{"ALTER TABLE `logs`.`app` MODIFY COLUMN `a\\x60b` LowCardinality(String);"}; !reflect.DeepEqual(f.DDL, want) {
		t.Errorf("DDL = %q\nwant %q", f.DDL, want)
	}
	if want := "SELECT uniq(`a\\x60b`) FROM `logs`.`app`"; f.Verify != want {
		t.Errorf("verify = %q, want %q", f.Verify, want)
	}
	wantEvidence := []models.AdvisorEvidence{
		{Label: "Distinct values in the sample", Value: "12 in 157,000 rows, from 7 slices of the last 7 days", Source: "sample"},
		{Label: "Rows and distinct values per slice (UTC)", Value: "2026-11-09 03:00: 24,000 rows, 12 distinct; 2026-11-10 06:00: 25,000 rows, 12 distinct; " +
			"2026-11-11 09:00: 21,000 rows, 12 distinct; 2026-11-12 12:00: 30,000 rows, 12 distinct; 2026-11-13 15:00: 22,000 rows, 12 distinct; " +
			"2026-11-14 18:00: 26,000 rows, 12 distinct; 2026-11-15 21:00: 9,000 rows, 12 distinct", Source: "sample"},
		{Label: "Size of a`b", Value: "30.0 GiB compressed, 200.0 GiB uncompressed, 30% of the table", Source: "system.columns"},
	}
	if !reflect.DeepEqual(f.Evidence, wantEvidence) {
		t.Errorf("evidence = %+v\nwant %+v", f.Evidence, wantEvidence)
	}
	if f.Impact != models.ImpactMedium || f.Column != column || !f.Candidate || f.Rebuild || f.Title != "Store a`b as LowCardinality(String)" {
		t.Errorf("finding = %+v, want a medium candidate on %s", f, column)
	}
}

func TestLowCardinalityVariants(t *testing.T) {
	for _, tt := range []struct {
		name    string
		column  clickhouse.AdvisorColumn
		meta    func(*clickhouse.AdvisorMetadata)
		wantDDL string
	}{
		{
			name:    "Nullable(String)",
			column:  clickhouse.AdvisorColumn{Name: "level", Type: "Nullable(String)", CompressedBytes: gib / 4, UncompressedBytes: 2 * gib},
			wantDDL: "ALTER TABLE `logs`.`app` MODIFY COLUMN `level` LowCardinality(Nullable(String));",
		},
		{
			name:   "under 1 GiB but 1% of the table",
			column: clickhouse.AdvisorColumn{Name: "level", Type: "String", CompressedBytes: 1 << 20, UncompressedBytes: 11 << 20},
			meta: func(m *clickhouse.AdvisorMetadata) {
				m.Columns = append(m.Columns, clickhouse.AdvisorColumn{Name: "msg", Type: "String", UncompressedBytes: 989 << 20})
			},
			wantDDL: "ALTER TABLE `logs`.`app` MODIFY COLUMN `level` LowCardinality(String);",
		},
		{
			name:   "Distributed target",
			column: clickhouse.AdvisorColumn{Name: "level", Type: "String", CompressedBytes: gib / 4, UncompressedBytes: 2 * gib},
			meta: func(m *clickhouse.AdvisorMetadata) {
				m.Target = &models.AdvisorTarget{Kind: models.AdvisorTargetDistributedLocal, Database: "logs", Table: "app_local", Cluster: "main", Engine: "ReplicatedMergeTree"}
			},
			wantDDL: "ALTER TABLE `logs`.`app_local` ON CLUSTER 'main' MODIFY COLUMN `level` LowCardinality(String);",
		},
		{
			name:   "an index on another column",
			column: clickhouse.AdvisorColumn{Name: "level", Type: "String", CompressedBytes: gib / 4, UncompressedBytes: 2 * gib},
			meta: func(m *clickhouse.AdvisorMetadata) {
				m.Indexes = []clickhouse.AdvisorIndex{{Name: "idx_levels", Type: "set(10)", Expr: "lower(level_name)"}}
				m.Table.CreateQuery = "CREATE TABLE logs.app (`level` String, PROJECTION p (SELECT tenant, count() GROUP BY tenant)) ENGINE = MergeTree ORDER BY (tenant, ts)"
			},
			wantDDL: "ALTER TABLE `logs`.`app` MODIFY COLUMN `level` LowCardinality(String);",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta(tt.column)
			if tt.meta != nil {
				tt.meta(meta)
			}
			found := findingsOf(Analyze(Facts{Metadata: meta, Sample: slicedSample(fullSlices, repetitive("level", 999))}), ruleLowCardinality)
			if len(found) != 1 || len(found[0].DDL) != 1 || found[0].DDL[0] != tt.wantDDL {
				t.Fatalf("R4 findings = %+v, want one with %q", found, tt.wantDDL)
			}
		})
	}
}

func TestLowCardinalityGuards(t *testing.T) {
	level := clickhouse.AdvisorColumn{Name: "level", Type: "String", CompressedBytes: gib / 4, UncompressedBytes: 2 * gib}
	saturated := slicedSample(fullSlices, repetitive("level", 12))
	for _, tt := range []struct {
		name   string
		column clickhouse.AdvisorColumn
		meta   func(*clickhouse.AdvisorMetadata)
		sample *clickhouse.AdvisorSample
	}{
		{name: "sorting key column", column: func() clickhouse.AdvisorColumn { c := level; c.InSortingKey = true; return c }(), sample: saturated},
		{name: "primary key column", column: func() clickhouse.AdvisorColumn { c := level; c.InPrimaryKey = true; return c }(), sample: saturated},
		{name: "partition key column", column: func() clickhouse.AdvisorColumn { c := level; c.InPartitionKey = true; return c }(), sample: saturated},
		{name: "already LowCardinality", column: func() clickhouse.AdvisorColumn { c := level; c.Type = "LowCardinality(String)"; return c }(), sample: saturated},
		{name: "not a string", column: func() clickhouse.AdvisorColumn { c := level; c.Type = "UInt8"; return c }(), sample: saturated},
		{name: "under 1 GiB and under 1% of the table", column: func() clickhouse.AdvisorColumn { c := level; c.UncompressedBytes = gib - 1; return c }(), meta: func(m *clickhouse.AdvisorMetadata) {
			m.Columns = append(m.Columns, clickhouse.AdvisorColumn{Name: "msg", Type: "String", UncompressedBytes: 100 * gib})
		}, sample: saturated},
		{name: "1% of a truncated column list", column: func() clickhouse.AdvisorColumn { c := level; c.UncompressedBytes = 10 << 20; return c }(), meta: func(m *clickhouse.AdvisorMetadata) {
			m.ColumnsTruncated = true
		}, sample: saturated},
		{name: "1,000 distinct values", column: level, sample: slicedSample(fullSlices, repetitive("level", 1_000))},
		{name: "unsaturated", column: level, sample: slicedSample(fullSlices, clickhouse.AdvisorCardinality{Column: "level", Distinct: 12, MaxSliceDistinct: 6})},
		// summarizeSample clears Saturated when any slice is truncated.
		{name: "a truncated slice", column: level, sample: slicedSample([]uint64{150_000, 25_000, 21_000, 30_000, 22_000, 26_000, 9_000}, clickhouse.AdvisorCardinality{Column: "level", Distinct: 12, MaxSliceDistinct: 12})},
		{name: "not sampled", column: level, sample: sampleOf()},
		{name: "sample skipped", column: level, sample: &clickhouse.AdvisorSample{Input: models.AdvisorInput{Name: clickhouse.AdvisorInputSample, State: models.AdvisorInputSkipped}}},
		{name: "sample limited", column: level, sample: func() *clickhouse.AdvisorSample {
			s := slicedSample(fullSlices, repetitive("level", 12))
			s.Input.State = models.AdvisorInputLimited
			return s
		}()},
		{name: "no sample yet", column: level},
		{name: "view does not pass the column through", column: level, meta: func(m *clickhouse.AdvisorMetadata) {
			m.PassThrough = clickhouse.AdvisorPassThrough{Columns: []string{"ts", "tenant"}}
		}, sample: saturated},
		{name: "a skip index reads the column", column: level, meta: func(m *clickhouse.AdvisorMetadata) {
			m.Indexes = []clickhouse.AdvisorIndex{{Name: "idx_level", Type: "set(10)", Expr: "lower(level)"}}
		}, sample: saturated},
		{name: "a projection reads the column", column: level, meta: func(m *clickhouse.AdvisorMetadata) {
			m.Table.CreateQuery = "CREATE TABLE logs.app (`ts` DateTime, `level` String, PROJECTION p (SELECT `level`, count() GROUP BY `level`)) ENGINE = MergeTree ORDER BY (tenant, ts)"
		}, sample: saturated},
		{name: "indexes unreadable", column: level, meta: func(m *clickhouse.AdvisorMetadata) { m.Inputs[2].State = models.AdvisorInputDenied }, sample: saturated},
		// The index left out of the list may read the column.
		{name: "index list truncated", column: level, meta: func(m *clickhouse.AdvisorMetadata) { m.IndexesTruncated = true }, sample: saturated},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta(tt.column)
			if tt.meta != nil {
				tt.meta(meta)
			}
			if found := findingsOf(Analyze(Facts{Metadata: meta, Sample: tt.sample}), ruleLowCardinality); len(found) != 0 {
				t.Fatalf("R4 fired: %+v", found)
			}
		})
	}
}

func TestLowCardinalitySampleColumns(t *testing.T) {
	small := clickhouse.AdvisorColumn{Name: "small", Type: "String", CompressedBytes: 1 << 20, UncompressedBytes: 1 << 20}
	meta := testMeta(
		clickhouse.AdvisorColumn{Name: "level", Type: "String", CompressedBytes: gib, UncompressedBytes: 2 * gib},
		clickhouse.AdvisorColumn{Name: "service", Type: "Nullable(String)", CompressedBytes: 3 * gib, UncompressedBytes: 9 * gib},
		small,
	)
	if got, want := SampleColumns(Facts{Metadata: meta}), []string{"service", "level"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SampleColumns = %q, want %q", got, want)
	}
}

func TestMentions(t *testing.T) {
	for _, tt := range []struct {
		expr, column string
		want         bool
	}{
		{"level", "level", true},
		{"lower(level)", "level", true},
		{"lower(`level`)", "level", true},
		{"concat(level_name, 'x')", "level", false},
		{"concat(msg, 'level')", "level", false},
		{"concat(msg, 'it''s level')", "level", false},
		{"lower(`a\\`b`)", "a`b", true},
		{`lower("level")`, "level", true},
		{"n.level", "n.level", true},
		{"length(n.level)", "n", true},
		{"t.level", "level", true},
		{"levels", "level", false},
	} {
		if got := mentions(tt.expr, tt.column); got != tt.want {
			t.Errorf("mentions(%q, %q) = %v, want %v", tt.expr, tt.column, got, tt.want)
		}
	}
}
