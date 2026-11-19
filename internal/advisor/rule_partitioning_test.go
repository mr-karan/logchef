package advisor

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

// manyPartitions returns n daily partitions.
func manyPartitions(n int) []clickhouse.AdvisorPartition {
	out := make([]clickhouse.AdvisorPartition, n)
	for i := range out {
		out[i] = clickhouse.AdvisorPartition{Partition: fmt.Sprint(i), Parts: 1, Rows: 10, Bytes: 100}
	}
	return out
}

func TestPartitioningUnpartitioned(t *testing.T) {
	for _, tt := range []struct {
		name, create, want string
		ttl                bool
	}{
		{"no TTL", "CREATE TABLE logs.app (`ts` DateTime) ENGINE = MergeTree ORDER BY (tenant, ts) SETTINGS index_granularity = 8192", "toYYYYMM(ts)", false},
		{"TTL under 90 days", "CREATE TABLE logs.app (`ts` DateTime) ENGINE = MergeTree ORDER BY (tenant, ts) TTL ts + toIntervalDay(30) SETTINGS index_granularity = 8192", "toDate(ts)", true},
		{"TTL of 90 days", "CREATE TABLE logs.app (`ts` DateTime) ENGINE = MergeTree ORDER BY (tenant, ts) TTL ts + toIntervalMonth(3)", "toYYYYMM(ts)", true},
		{"TTL with no interval", "CREATE TABLE logs.app (`ts` DateTime) ENGINE = MergeTree ORDER BY (tenant, ts) TTL toDateTime(ts) + 86400", "toYYYYMM(ts)", true},
		{"a column TTL is not the table's", "CREATE TABLE logs.app (`ts` DateTime, `msg` String TTL ts + toIntervalDay(7)) ENGINE = MergeTree ORDER BY (tenant, ts)", "toYYYYMM(ts)", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta()
			meta.Table.CreateQuery = tt.create
			report := Analyze(Facts{Metadata: meta, TimestampColumn: "ts"})
			found := findingsOf(report, rulePartitioning)
			if len(found) != 1 {
				t.Fatalf("R7 findings = %+v, want 1", report.Findings)
			}
			f := found[0]
			if f.DDL == nil || len(f.DDL) != 0 || !f.Rebuild || f.Candidate || f.Impact != models.ImpactMedium {
				t.Errorf("finding = %+v, want a medium rebuild with no DDL", f)
			}
			if f.Title != "Partition the table by "+tt.want {
				t.Errorf("title = %q", f.Title)
			}
			want := []models.AdvisorEvidence{
				{Label: "Partition key", Value: "none", Source: "system.tables"},
				{Label: "Active partitions", Value: "0", Source: "system.parts"},
				{Label: "Table size", Value: "100.0 GiB", Source: "system.tables"},
			}
			if tt.ttl {
				clause, _ := tableTTL(tt.create)
				want = append(want, models.AdvisorEvidence{Label: "Table TTL", Value: clause, Source: "system.tables"})
			}
			want = append(want, models.AdvisorEvidence{Label: "Proposed partition key", Value: tt.want, Source: "logchef"})
			if !reflect.DeepEqual(f.Evidence, want) {
				t.Errorf("evidence = %+v\nwant %+v", f.Evidence, want)
			}
		})
	}
}

func TestPartitioningTooMany(t *testing.T) {
	for _, tt := range []struct {
		name      string
		count     int
		truncated bool
		want      string
	}{
		{"1,001 partitions", 1_001, false, "1,001"},
		{"more than were read", 2_000, true, "more than 2,000"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta()
			meta.Table.PartitionKey = "toStartOfHour(ts)"
			meta.Partitions, meta.PartitionsTruncated = manyPartitions(tt.count), tt.truncated
			found := findingsOf(Analyze(Facts{Metadata: meta, TimestampColumn: "ts"}), rulePartitioning)
			if len(found) != 1 || found[0].Title != "Use a coarser partition key than toStartOfHour(ts)" || len(found[0].DDL) != 0 {
				t.Fatalf("R7 findings = %+v, want one coarser key", found)
			}
			if !slicesContainEvidence(found[0].Evidence, "Active partitions", tt.want) || !slicesContainEvidence(found[0].Evidence, "Proposed partition key", "toYYYYMM(ts)") {
				t.Errorf("evidence = %+v", found[0].Evidence)
			}
		})
	}
}

func TestPartitioningGuards(t *testing.T) {
	small := 100*gib - 1
	for _, tt := range []struct {
		name string
		meta func(*clickhouse.AdvisorMetadata)
		ts   string
	}{
		{name: "unpartitioned under 100 GiB", meta: func(m *clickhouse.AdvisorMetadata) { m.Table.TotalBytes = &small }, ts: "ts"},
		{name: "unpartitioned with an unknown size", meta: func(m *clickhouse.AdvisorMetadata) { m.Table.TotalBytes = nil }, ts: "ts"},
		{name: "1,000 partitions", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Table.PartitionKey, m.Partitions = "toStartOfHour(ts)", manyPartitions(1_000)
		}, ts: "ts"},
		{name: "partitions unreadable", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Table.PartitionKey, m.Partitions = "toStartOfHour(ts)", manyPartitions(1_001)
			m.Inputs[3].State = models.AdvisorInputLimited
		}, ts: "ts"},
		{name: "the proposed key is the current one", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Table.PartitionKey, m.Partitions = "toYYYYMM(ts)", manyPartitions(1_001)
		}, ts: "ts"},
		{name: "ReplacingMergeTree", meta: func(m *clickhouse.AdvisorMetadata) { m.Table.Engine = "ReplacingMergeTree" }, ts: "ts"},
		{name: "timestamp not a date", meta: func(m *clickhouse.AdvisorMetadata) { m.Columns[0].Type = "UInt64" }, ts: "ts"},
		{name: "timestamp not in the table", ts: "event_time"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta()
			if tt.meta != nil {
				tt.meta(meta)
			}
			if found := findingsOf(Analyze(Facts{Metadata: meta, TimestampColumn: tt.ts}), rulePartitioning); len(found) != 0 {
				t.Fatalf("R7 fired: %+v", found)
			}
		})
	}
}

func TestTableTTL(t *testing.T) {
	for _, tt := range []struct {
		create, clause string
		days           float64
		known          bool
	}{
		{"CREATE TABLE t (`ts` DateTime) ENGINE = MergeTree ORDER BY ts", "", 0, false},
		{"CREATE TABLE t (`ts` DateTime, `m` String TTL ts + toIntervalDay(1)) ENGINE = MergeTree ORDER BY ts", "", 0, false},
		{"CREATE TABLE t (`ts` DateTime) ENGINE = MergeTree ORDER BY ts TTL ts + toIntervalDay(14) SETTINGS index_granularity = 8192", "ts + toIntervalDay(14)", 14, true},
		{"CREATE TABLE t (`ts` DateTime) ENGINE = MergeTree ORDER BY ts TTL ts + toIntervalHour(36)", "ts + toIntervalHour(36)", 1.5, true},
		{"CREATE TABLE t (`ts` DateTime) ENGINE = MergeTree ORDER BY ts TTL ts + toIntervalDay(7) TO VOLUME 'cold', ts + toIntervalYear(1)", "ts + toIntervalDay(7) TO VOLUME 'cold', ts + toIntervalYear(1)", 365, true},
		{"CREATE TABLE t (`ts` DateTime) ENGINE = MergeTree ORDER BY ts TTL toDateTime(ts) + 86400", "toDateTime(ts) + 86400", 0, false},
	} {
		clause, ok := tableTTL(tt.create)
		if clause != tt.clause || ok != (tt.clause != "") {
			t.Errorf("tableTTL(%q) = %q, %v; want %q", tt.create, clause, ok, tt.clause)
			continue
		}
		if days, known := ttlDays(clause); days != tt.days || known != tt.known {
			t.Errorf("ttlDays(%q) = %v, %v; want %v, %v", clause, days, known, tt.days, tt.known)
		}
	}
}
