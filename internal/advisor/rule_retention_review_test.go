package advisor

import (
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

var retentionNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

// retentionMeta is a table partitioned by month whose data goes back 200
// days: one 10 GiB partition per month.
func retentionMeta() *clickhouse.AdvisorMetadata {
	meta := testMeta()
	meta.Table.PartitionKey = "toYYYYMM(ts)"
	oldest := retentionNow.Add(-200 * day)
	meta.OldestData = &oldest
	for start := oldest; start.Before(retentionNow); start = start.AddDate(0, 1, 0) {
		end := start.AddDate(0, 1, 0).Add(-time.Second)
		if end.After(retentionNow) {
			end = retentionNow
		}
		meta.Partitions = append(meta.Partitions, clickhouse.AdvisorPartition{Partition: start.Format("200601"), Parts: 3, Bytes: 10 * gib, MinTime: start, MaxTime: end})
	}
	return meta
}

// retentionUsage is 81 recorded days of 1,000 queries, 10 with no start
// time, that read back at most 30 days.
func retentionUsage() Usage {
	lookback := int64(30 * 86400)
	return Usage{Retention: models.QueryShapeCoverage{Days: 81, QueryCount: 1_000, NullLookbackCount: 10, MaxLookbackSeconds: &lookback}}
}

func TestRetentionReviewFires(t *testing.T) {
	report := Analyze(Facts{Metadata: retentionMeta(), TimestampColumn: "ts", Usage: retentionUsage(), Now: retentionNow})
	found := findingsOf(report, ruleRetentionReview)
	if len(found) != 1 {
		t.Fatalf("R8 findings = %+v, want 1", report.Findings)
	}
	f := found[0]
	if f.Impact != models.ImpactInfo || f.DDL == nil || len(f.DDL) != 0 || f.Rebuild || f.Candidate || f.Verify != "" {
		t.Errorf("finding = %+v, want info with no DDL", f)
	}
	if want := "Review retention: recorded queries read back 30 days, and the data goes back 200 days"; f.Title != want {
		t.Errorf("title = %q, want %q", f.Title, want)
	}
	// The cutoff is 2026-10-17 12:00. Of 7 partitions, starting on the 30th
	// of each month from April, the 5 that end by 2026-09-30 are older.
	want := []models.AdvisorEvidence{
		{Label: "Age of the oldest data", Value: "200 days (since 2026-04-30)", Source: "system.parts"},
		{Label: "Bytes in partitions older than the largest lookback", Value: "50.0 GiB", Source: "system.parts"},
		{Label: "Largest lookback of a recorded query", Value: "30 days", Source: "logchef"},
		{Label: "Days with recorded queries", Value: "81 of the last 90", Source: "logchef"},
		{Label: "Queries with no start time", Value: "at least 10 of 1,000 (1%); the true share can be higher", Source: "logchef"},
	}
	if !reflect.DeepEqual(f.Evidence, want) {
		t.Errorf("evidence = %+v\nwant %+v", f.Evidence, want)
	}
	if want := "Logchef sees only Explorer queries. Alerts, dashboards, exports and other clients may read older data. Choose a retention period from your own policy."; f.Cost != want {
		t.Errorf("cost = %q", f.Cost)
	}
}

func TestRetentionReviewGuards(t *testing.T) {
	for _, tt := range []struct {
		name  string
		meta  func(*clickhouse.AdvisorMetadata)
		usage func(*Usage)
	}{
		{name: "unknown age", meta: func(m *clickhouse.AdvisorMetadata) { m.OldestData = nil }},
		{name: "data 90 days old", meta: func(m *clickhouse.AdvisorMetadata) { oldest := retentionNow.Add(-90 * day); m.OldestData = &oldest }},
		{name: "a TTL exists", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Table.CreateQuery = "CREATE TABLE logs.app (`ts` DateTime) ENGINE = MergeTree PARTITION BY toYYYYMM(ts) ORDER BY (tenant, ts) TTL ts + toIntervalYear(2)"
		}},
		{name: "59 recorded days", usage: func(u *Usage) { u.Retention.Days = 59 }},
		{name: "199 queries", usage: func(u *Usage) { u.Retention.QueryCount, u.Retention.NullLookbackCount = 199, 0 }},
		{name: "5% with no start time", usage: func(u *Usage) { u.Retention.NullLookbackCount = 50 }},
		{name: "no query had a start time", usage: func(u *Usage) { u.Retention.MaxLookbackSeconds = nil }},
		{name: "lookback half the age", usage: func(u *Usage) { lookback := int64(100 * 86400); u.Retention.MaxLookbackSeconds = &lookback }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := retentionMeta()
			if tt.meta != nil {
				tt.meta(meta)
			}
			usage := retentionUsage()
			if tt.usage != nil {
				tt.usage(&usage)
			}
			if found := findingsOf(Analyze(Facts{Metadata: meta, TimestampColumn: "ts", Usage: usage, Now: retentionNow}), ruleRetentionReview); len(found) != 0 {
				t.Fatalf("R8 fired: %+v", found)
			}
		})
	}
}
