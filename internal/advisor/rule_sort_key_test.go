package advisor

import (
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

// keySample is an ok sample of 7 full slices, one a day from 2026-11-09
// 03:00 UTC, in which every slice holds each card's MaxSliceDistinct values.
func keySample(cards ...clickhouse.AdvisorCardinality) *clickhouse.AdvisorSample {
	sample := sampleOf(cards...)
	for i := range 7 {
		slice := clickhouse.AdvisorSlice{Start: time.Date(2026, 11, 9+i, 3, 0, 0, 0, time.UTC), Rows: 30_000}
		for _, card := range cards {
			slice.Distinct = append(slice.Distinct, card.MaxSliceDistinct)
		}
		sample.Slices = append(sample.Slices, slice)
	}
	return sample
}

func saturated(name string, distinct uint64) clickhouse.AdvisorCardinality {
	return clickhouse.AdvisorCardinality{Column: name, Distinct: distinct, MaxSliceDistinct: distinct, Saturated: true}
}

// keyUsage is 1,000 filtered queries over 20 days: service = in 700 of them,
// level IN in 600, the two together in 400.
func keyUsage() Usage {
	usage := usageOf(1_000,
		used(300, 50, plain("service", models.MatchEq)),
		used(200, 80, plain("level", models.MatchIn)),
		used(400, 20, plain("level", models.MatchIn), plain("service", models.MatchEq)),
	)
	usage.Days = 20
	return usage
}

func TestSortKeyFires(t *testing.T) {
	meta := testMeta(stringColumn("service"), stringColumn("level"))
	rows := uint64(5_000_000)
	meta.Table.TotalRows = &rows
	report := Analyze(Facts{Metadata: meta, TimestampColumn: "ts", Usage: keyUsage(), Sample: keySample(saturated("service", 40), saturated("level", 5))})
	found := findingsOf(report, ruleSortKey)
	if len(found) != 1 {
		t.Fatalf("R6 findings = %+v, want 1", report.Findings)
	}
	f := found[0]
	if f.DDL == nil || len(f.DDL) != 0 || !f.Rebuild || !f.Candidate || f.Impact != models.ImpactHigh || f.Column != "" {
		t.Errorf("finding = %+v, want a high rebuild candidate with no DDL", f)
	}
	if want := "Lead the sorting key with level, service"; f.Title != want {
		t.Errorf("title = %q, want %q", f.Title, want)
	}
	perSlice := func(n string) string {
		return "2026-11-09 03:00: 30,000 rows, " + n + " distinct; 2026-11-10 03:00: 30,000 rows, " + n + " distinct; 2026-11-11 03:00: 30,000 rows, " + n + " distinct; " +
			"2026-11-12 03:00: 30,000 rows, " + n + " distinct; 2026-11-13 03:00: 30,000 rows, " + n + " distinct; 2026-11-14 03:00: 30,000 rows, " + n + " distinct; " +
			"2026-11-15 03:00: 30,000 rows, " + n + " distinct"
	}
	want := []models.AdvisorEvidence{
		{Label: "level: Queries with = or IN", Value: "600 of 1,000 (60%)", Source: "logchef"},
		{Label: "level: Mean duration of those queries", Value: "40 ms", Source: "logchef"},
		{Label: "level: Distinct values in the sample", Value: "5 in 210,000 rows, from 7 slices of the last 7 days", Source: "sample"},
		{Label: "level: Rows and distinct values per slice (UTC)", Value: perSlice("5"), Source: "sample"},
		{Label: "service: Queries with = or IN", Value: "700 of 1,000 (70%)", Source: "logchef"},
		{Label: "service: Mean duration of those queries", Value: "32 ms", Source: "logchef"},
		{Label: "service: Distinct values in the sample", Value: "40 in 210,000 rows, from 7 slices of the last 7 days", Source: "sample"},
		{Label: "service: Rows and distinct values per slice (UTC)", Value: perSlice("40"), Source: "sample"},
		{Label: "Current sorting key", Value: "(tenant, ts)", Source: "system.tables"},
		{Label: "Table size", Value: "100.0 GiB, 5,000,000 rows", Source: "system.tables"},
		{Label: "Proposed sorting key", Value: "(level, service, tenant, ts)", Source: "logchef"},
	}
	if !reflect.DeepEqual(f.Evidence, want) {
		t.Errorf("evidence = %+v\nwant %+v", f.Evidence, want)
	}
}

func TestSortKeyProposals(t *testing.T) {
	for _, tt := range []struct {
		name string
		meta func(*clickhouse.AdvisorMetadata)
		want string
	}{
		{"a column after the timestamp moves to the front", func(m *clickhouse.AdvisorMetadata) {
			m.Table.SortingKey, m.Table.PrimaryKey = "tenant, ts, service", "tenant, ts, service"
		}, "(level, service, tenant, ts)"},
		{"quoted names in the current key stay quoted", func(m *clickhouse.AdvisorMetadata) {
			m.Columns = append(m.Columns, stringColumn("a-b"), stringColumn("c d"))
			m.Table.SortingKey = "`a-b`, toStartOfHour(`c d`), ts"
			m.Table.PrimaryKey = m.Table.SortingKey
		}, "(level, service, `a-b`, toStartOfHour(`c d`), ts)"},
		{"no timestamp in the key", func(m *clickhouse.AdvisorMetadata) {
			m.Table.SortingKey, m.Table.PrimaryKey = "host", "host"
		}, "(level, service, host)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta(stringColumn("service"), stringColumn("level"))
			tt.meta(meta)
			found := findingsOf(Analyze(Facts{Metadata: meta, TimestampColumn: "ts", Usage: keyUsage(), Sample: keySample(saturated("service", 40), saturated("level", 5))}), ruleSortKey)
			if len(found) != 1 {
				t.Fatalf("R6 findings = %+v, want 1", found)
			}
			if got := found[0].Evidence[len(found[0].Evidence)-1].Value; got != tt.want {
				t.Errorf("proposed key = %s, want %s", got, tt.want)
			}
		})
	}

	// Four columns qualify; the key takes the three with the fewest values.
	meta := testMeta(stringColumn("a b"), stringColumn("b"), stringColumn("c"), stringColumn("d"))
	usage := usageOf(1_000, used(1_000, 1, plain("a b", models.MatchEq), plain("b", models.MatchEq), plain("c", models.MatchEq), plain("d", models.MatchEq)))
	usage.Days = 14
	found := findingsOf(Analyze(Facts{Metadata: meta, TimestampColumn: "ts", Usage: usage,
		Sample: keySample(saturated("a b", 30), saturated("b", 9_999), saturated("c", 10), saturated("d", 20))}), ruleSortKey)
	if len(found) != 1 || found[0].Title != "Lead the sorting key with c, d, a b" {
		t.Fatalf("R6 findings = %+v, want c, d, a b", found)
	}
	if got, want := found[0].Evidence[len(found[0].Evidence)-1].Value, "(c, d, `a b`, tenant, ts)"; got != want {
		t.Errorf("proposed key = %s, want %s", got, want)
	}
}

func TestSortKeyGuards(t *testing.T) {
	sample := keySample(saturated("service", 40), saturated("level", 5))
	for _, tt := range []struct {
		name   string
		meta   func(*clickhouse.AdvisorMetadata)
		usage  func(*Usage)
		sample *clickhouse.AdvisorSample
	}{
		{name: "13 days of data", usage: func(u *Usage) { u.Days = 13 }, sample: sample},
		{name: "Q under 200", usage: func(u *Usage) {
			*u = usageOf(199, used(150, 1, plain("service", models.MatchEq)), used(149, 1, plain("level", models.MatchEq)))
			u.Days = 20
		}, sample: sample},
		{name: "ReplacingMergeTree", meta: func(m *clickhouse.AdvisorMetadata) { m.Table.Engine = "ReplacingMergeTree" }, sample: sample},
		{name: "SharedMergeTree", meta: func(m *clickhouse.AdvisorMetadata) { m.Table.Engine = "SharedMergeTree" }, sample: sample},
		{name: "a separate primary key", meta: func(m *clickhouse.AdvisorMetadata) { m.Table.PrimaryKey = "tenant" }, sample: sample},
		{name: "a truncated slice", sample: func() *clickhouse.AdvisorSample {
			// summarizeSample clears Saturated when any slice is truncated.
			s := keySample(clickhouse.AdvisorCardinality{Column: "service", Distinct: 40, MaxSliceDistinct: 40}, clickhouse.AdvisorCardinality{Column: "level", Distinct: 5, MaxSliceDistinct: 5})
			s.Slices[0].Rows, s.Slices[0].Truncated = clickhouse.AdvisorSliceRowLimit, true
			return s
		}()},
		{name: "10,000 distinct values", sample: keySample(saturated("service", 10_000), saturated("level", 10_000))},
		{name: "share under 50%", usage: func(u *Usage) {
			*u = usageOf(1_000, used(499, 1, plain("service", models.MatchEq)), used(499, 1, plain("level", models.MatchEq)))
			u.Days = 20
		}, sample: sample},
		{name: "!= is not a match", usage: func(u *Usage) {
			*u = usageOf(1_000, used(900, 1, plain("service", models.MatchNeq)))
			u.Days = 20
		}, sample: sample},
		{name: "columns already ahead of the timestamp", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Table.SortingKey, m.Table.PrimaryKey = "level, service, ts", "level, service, ts"
		}, sample: sample},
		{name: "columns in a key without the timestamp", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Table.SortingKey, m.Table.PrimaryKey = "level, service", "level, service"
		}, sample: sample},
		{name: "usage record incomplete", usage: func(u *Usage) { u.Queries, u.Unanalyzed = 1_300, 300 }, sample: sample},
		{name: "view does not pass the columns through", meta: func(m *clickhouse.AdvisorMetadata) {
			m.PassThrough = clickhouse.AdvisorPassThrough{Columns: []string{"ts", "tenant"}}
		}, sample: sample},
		{name: "no sample", sample: sampleOf()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta(stringColumn("service"), stringColumn("level"))
			if tt.meta != nil {
				tt.meta(meta)
			}
			usage := keyUsage()
			if tt.usage != nil {
				tt.usage(&usage)
			}
			if found := findingsOf(Analyze(Facts{Metadata: meta, TimestampColumn: "ts", Usage: usage, Sample: tt.sample}), ruleSortKey); len(found) != 0 {
				t.Fatalf("R6 fired: %+v", found)
			}
		})
	}
}

func TestSortKeyDistributed(t *testing.T) {
	meta := testMeta(stringColumn("service"), stringColumn("level"))
	meta.Target = &models.AdvisorTarget{Kind: models.AdvisorTargetDistributedLocal, Database: "logs", Table: "app_local", Cluster: "main", Engine: "ReplicatedMergeTree"}
	meta.Table.Engine = "ReplicatedMergeTree"
	found := findingsOf(Analyze(Facts{Metadata: meta, TimestampColumn: "ts", Usage: keyUsage(), Sample: keySample(saturated("service", 40), saturated("level", 5))}), ruleSortKey)
	if len(found) != 1 {
		t.Fatalf("R6 findings = %+v, want 1", found)
	}
	if !slicesContainEvidence(found[0].Evidence, "Sizes", "cover the connected replica only") {
		t.Errorf("evidence %+v lacks the replica line", found[0].Evidence)
	}
}

func slicesContainEvidence(evidence []models.AdvisorEvidence, label, value string) bool {
	for _, e := range evidence {
		if e.Label == label && e.Value == value {
			return true
		}
	}
	return false
}
