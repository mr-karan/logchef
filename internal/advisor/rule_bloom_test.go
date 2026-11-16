package advisor

import (
	"reflect"
	"testing"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

func TestBloomFires(t *testing.T) {
	const column = "a`b"
	meta := testMeta(stringColumn(column))
	meta.Indexes = []clickhouse.AdvisorIndex{{Name: "idx_tenant", Type: "set(100)", Expr: "tenant"}}
	usage := usageOf(1_900,
		used(300, 400, plain(column, models.MatchEq)),
		used(112, 200, plain(column, models.MatchIn), plain("tenant", models.MatchEq)),
	)
	report := Analyze(Facts{Metadata: meta, Usage: usage, Sample: sampleOf(clickhouse.AdvisorCardinality{Column: column, Distinct: 25_000})})
	found := findingsOf(report, ruleBloom)
	if len(found) != 1 {
		t.Fatalf("R1 findings = %+v, want 1", report.Findings)
	}
	f := found[0]
	wantDDL := []string{
		"ALTER TABLE `logs`.`app` ADD INDEX IF NOT EXISTS `idx_a_b_bloom` `a\\x60b` TYPE bloom_filter GRANULARITY 1;",
		"ALTER TABLE `logs`.`app` MATERIALIZE INDEX `idx_a_b_bloom`;",
	}
	if !reflect.DeepEqual(f.DDL, wantDDL) {
		t.Errorf("DDL = %q\nwant %q", f.DDL, wantDDL)
	}
	if want := "EXPLAIN indexes = 1 SELECT count() FROM `logs`.`app` WHERE `a\\x60b` = '<value>'"; f.Verify != want {
		t.Errorf("verify = %q, want %q", f.Verify, want)
	}
	wantEvidence := []models.AdvisorEvidence{
		{Label: "Queries with = or IN", Value: "412 of 1,900 (22%)", Source: "logchef"},
		{Label: "Mean duration of those queries", Value: "345 ms", Source: "logchef"},
		{Label: "Distinct values in the sample", Value: "at least 25,000", Source: "sample"},
		{Label: "Size of a`b", Value: "2.0 GiB compressed, 8.0 GiB uncompressed, 2% of the table", Source: "system.columns"},
		{Label: "Existing skip indexes", Value: "idx_tenant (set(100) on tenant)", Source: "system.data_skipping_indices"},
	}
	if !reflect.DeepEqual(f.Evidence, wantEvidence) {
		t.Errorf("evidence = %+v\nwant %+v", f.Evidence, wantEvidence)
	}
	if f.Impact != models.ImpactMedium || f.Column != column || f.Rebuild || f.Candidate || f.Path != nil {
		t.Errorf("finding = %+v, want a medium in-place finding on %s", f, column)
	}
}

func TestBloomDistributedTarget(t *testing.T) {
	meta := testMeta(stringColumn("request_id"))
	meta.Target = &models.AdvisorTarget{Kind: models.AdvisorTargetDistributedLocal, Database: "logs", Table: "app_local", Cluster: "prod's", Engine: "ReplicatedMergeTree"}
	report := Analyze(Facts{Metadata: meta, Usage: usageOf(100, used(100, 1, plain("request_id", models.MatchEq))),
		Sample: sampleOf(clickhouse.AdvisorCardinality{Column: "request_id", Distinct: 10_000, Saturated: true})})
	found := findingsOf(report, ruleBloom)
	if len(found) != 1 {
		t.Fatalf("R1 findings = %+v, want 1", report.Findings)
	}
	if want := "ALTER TABLE `logs`.`app_local` ON CLUSTER 'prod''s' MATERIALIZE INDEX `idx_request_id_bloom`;"; found[0].DDL[1] != want {
		t.Errorf("DDL = %q, want %q", found[0].DDL[1], want)
	}
	var replica, distinct bool
	for _, e := range found[0].Evidence {
		replica = replica || e.Label == "Sizes" && e.Value == "cover the connected replica only"
		distinct = distinct || e.Label == "Distinct values in the sample" && e.Value == "10,000"
	}
	if !replica || !distinct {
		t.Errorf("evidence %+v lacks the replica line or the exact distinct count", found[0].Evidence)
	}
}

func TestBloomGuards(t *testing.T) {
	hot := func(items ...models.FilterItem) Usage { return usageOf(200, used(100, 1, items...)) }
	eq := plain("request_id", models.MatchEq)
	card := clickhouse.AdvisorCardinality{Column: "request_id", Distinct: 50_000}
	for _, tt := range []struct {
		name   string
		meta   func(*clickhouse.AdvisorMetadata)
		usage  Usage
		sample *clickhouse.AdvisorSample
	}{
		{name: "share under 5%", usage: usageOf(1_000, used(49, 1, eq)), sample: sampleOf(card)},
		{name: "under 20 queries", usage: usageOf(200, used(19, 1, eq)), sample: sampleOf(card)},
		{name: "!= only", usage: hot(plain("request_id", models.MatchNeq)), sample: sampleOf(card)},
		{name: "other only", usage: hot(plain("request_id", models.MatchOther)), sample: sampleOf(card)},
		{name: "a path, not a plain column", usage: hot(pathItem("request_id", models.AccessSubcolumn, models.MatchEq, "x")), sample: sampleOf(card)},
		{name: "first sort key column", usage: hot(plain("tenant", models.MatchEq)), sample: sampleOf(clickhouse.AdvisorCardinality{Column: "tenant", Distinct: 50_000})},
		{name: "column missing from the target", meta: func(m *clickhouse.AdvisorMetadata) { m.Columns = m.Columns[:2] }, usage: hot(eq), sample: sampleOf(card)},
		{name: "view does not pass the column through", meta: func(m *clickhouse.AdvisorMetadata) {
			m.PassThrough = clickhouse.AdvisorPassThrough{Columns: []string{"ts", "tenant"}}
		}, usage: hot(eq), sample: sampleOf(card)},
		{name: "an index covers the column", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Indexes = []clickhouse.AdvisorIndex{{Name: "rid", Type: "set(0)", Expr: "request_id"}}
		}, usage: hot(eq), sample: sampleOf(card)},
		{name: "an index has the generated name", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Indexes = []clickhouse.AdvisorIndex{{Name: "idx_request_id_bloom", Type: "minmax", Expr: "lower(request_id)"}}
		}, usage: hot(eq), sample: sampleOf(card)},
		{name: "indexes unreadable", meta: func(m *clickhouse.AdvisorMetadata) { m.Inputs[2].State = models.AdvisorInputDenied }, usage: hot(eq), sample: sampleOf(card)},
		{name: "low cardinality", usage: hot(eq), sample: sampleOf(clickhouse.AdvisorCardinality{Column: "request_id", Distinct: 9_999, Saturated: true})},
		{name: "cardinality not sampled", usage: hot(eq), sample: sampleOf()},
		{name: "sample limited", usage: hot(eq), sample: &clickhouse.AdvisorSample{
			Input:   models.AdvisorInput{Name: clickhouse.AdvisorInputSample, State: models.AdvisorInputLimited},
			Columns: []clickhouse.AdvisorCardinality{card},
		}},
		{name: "no sample yet", usage: hot(eq)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta(stringColumn("request_id"))
			if tt.meta != nil {
				tt.meta(meta)
			}
			if found := findingsOf(Analyze(Facts{Metadata: meta, Usage: tt.usage, Sample: tt.sample}), ruleBloom); len(found) != 0 {
				t.Fatalf("R1 fired: %+v", found)
			}
		})
	}
}

func TestBloomIndexNameCollision(t *testing.T) {
	// a`b and a_b both map to idx_a_b_bloom; the more used column gets it.
	meta := testMeta(stringColumn("a`b"), stringColumn("a_b"))
	usage := usageOf(200, used(60, 1, plain("a`b", models.MatchEq)), used(80, 1, plain("a_b", models.MatchEq)))
	sample := sampleOf(clickhouse.AdvisorCardinality{Column: "a`b", Distinct: 50_000}, clickhouse.AdvisorCardinality{Column: "a_b", Distinct: 50_000})
	found := findingsOf(Analyze(Facts{Metadata: meta, Usage: usage, Sample: sample}), ruleBloom)
	if len(found) != 1 || found[0].Column != "a_b" {
		t.Fatalf("R1 findings = %+v, want one on a_b", found)
	}
}

func TestSampleColumns(t *testing.T) {
	meta := testMeta(stringColumn("request_id"), stringColumn("trace_id"), stringColumn("rare"))
	usage := usageOf(1_000,
		used(100, 1, plain("trace_id", models.MatchEq)),
		used(300, 1, plain("request_id", models.MatchIn)),
		used(10, 1, plain("rare", models.MatchEq)),
		used(200, 1, plain("tenant", models.MatchEq)),
	)
	facts := Facts{Metadata: meta, Usage: usage, Sample: sampleOf()}
	if got, want := SampleColumns(facts), []string{"request_id", "trace_id"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SampleColumns = %q, want %q", got, want)
	}

	var columns []clickhouse.AdvisorColumn
	var wide []models.FilterItem
	for i := range 25 {
		name := string(rune('a'+i)) + "_id"
		columns = append(columns, stringColumn(name))
		wide = append(wide, plain(name, models.MatchEq))
	}
	if got := SampleColumns(Facts{Metadata: testMeta(columns...), Usage: usageOf(100, used(100, 1, wide...))}); len(got) != clickhouse.AdvisorMaxSampleColumns {
		t.Errorf("SampleColumns returned %d columns, want the cap of %d", len(got), clickhouse.AdvisorMaxSampleColumns)
	}
}
