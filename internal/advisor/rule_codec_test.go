package advisor

import (
	"reflect"
	"testing"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

// codecMeta is testMeta whose timestamp column and message column have
// the sizes R5 reads.
func codecMeta(ts, msg clickhouse.AdvisorColumn) *clickhouse.AdvisorMetadata {
	meta := testMeta(msg)
	meta.Columns[0] = ts
	return meta
}

var (
	bigTimestamp = clickhouse.AdvisorColumn{Name: "ts", Type: "DateTime64(3, 'UTC')", InSortingKey: true, InPrimaryKey: true, CompressedBytes: 2 * gib, UncompressedBytes: 8 * gib}
	smallTS      = clickhouse.AdvisorColumn{Name: "ts", Type: "DateTime", InSortingKey: true, InPrimaryKey: true, CompressedBytes: gib - 1, UncompressedBytes: 4 * gib}
	bigMessage   = clickhouse.AdvisorColumn{Name: "a`b", Type: "String", CompressedBytes: 60 * gib, UncompressedBytes: 150 * gib}
	smallMessage = clickhouse.AdvisorColumn{Name: "msg", Type: "String", CompressedBytes: gib, UncompressedBytes: 2 * gib}
)

func TestCodecFires(t *testing.T) {
	report := Analyze(Facts{Metadata: codecMeta(bigTimestamp, bigMessage), TimestampColumn: "ts"})
	found := findingsOf(report, ruleCodec)
	if len(found) != 2 {
		t.Fatalf("R5 findings = %+v, want 2", report.Findings)
	}
	message, ts := found[0], found[1]
	if want := []string{"ALTER TABLE `logs`.`app` MODIFY COLUMN `a\\x60b` CODEC(ZSTD(1));"}; !reflect.DeepEqual(message.DDL, want) {
		t.Errorf("trigger B DDL = %q\nwant %q", message.DDL, want)
	}
	if want := []string{"ALTER TABLE `logs`.`app` MODIFY COLUMN `ts` CODEC(Delta, ZSTD(1));"}; !reflect.DeepEqual(ts.DDL, want) {
		t.Errorf("trigger A DDL = %q\nwant %q", ts.DDL, want)
	}
	wantEvidence := []models.AdvisorEvidence{
		{Label: "Size of a`b", Value: "60.0 GiB compressed, 150.0 GiB uncompressed, 60% of the table", Source: "system.columns"},
		{Label: "Uncompressed to compressed ratio", Value: "2.5", Source: "system.columns"},
		{Label: "Current codec", Value: "none set, so the server default applies", Source: "system.columns"},
	}
	if !reflect.DeepEqual(message.Evidence, wantEvidence) {
		t.Errorf("evidence = %+v\nwant %+v", message.Evidence, wantEvidence)
	}
	if message.Impact != models.ImpactHigh || message.Title != "Compress a`b with ZSTD" || message.Candidate || message.Rebuild {
		t.Errorf("trigger B finding = %+v, want a high in-place finding", message)
	}
	if ts.Impact != models.ImpactLow || ts.Title != "Compress the timestamp column ts with Delta and ZSTD" {
		t.Errorf("trigger A finding = %+v, want a low finding on ts", ts)
	}
	if want := "More CPU on insert and merge. The ALTER changes metadata only: existing parts keep the old codec until a merge rewrites them. " +
		"This gains nothing where the server default is already ZSTD, as in ClickHouse Cloud, and Logchef cannot tell the two apart."; message.Cost != want {
		t.Errorf("trigger B cost = %q, want %q", message.Cost, want)
	}
}

func TestCodecTimestampTypes(t *testing.T) {
	for _, typ := range []string{"DateTime", "DateTime('UTC')", "DateTime64(3)", "DateTime64(9, 'Asia/Kolkata')"} {
		ts := bigTimestamp
		ts.Type = typ
		if found := findingsOf(Analyze(Facts{Metadata: codecMeta(ts, smallMessage), TimestampColumn: "ts"}), ruleCodec); len(found) != 1 || found[0].Column != "ts" {
			t.Errorf("%s: R5 findings = %+v, want one on ts", typ, found)
		}
	}
}

func TestCodecGuards(t *testing.T) {
	with := func(c clickhouse.AdvisorColumn, edit func(*clickhouse.AdvisorColumn)) clickhouse.AdvisorColumn {
		edit(&c)
		return c
	}
	for _, tt := range []struct {
		name    string
		ts, msg clickhouse.AdvisorColumn
		tsName  string
		meta    func(*clickhouse.AdvisorMetadata)
	}{
		{name: "timestamp under 1 GiB", ts: smallTS, msg: smallMessage, tsName: "ts"},
		{name: "timestamp with an explicit codec", ts: with(bigTimestamp, func(c *clickhouse.AdvisorColumn) { c.Codec = "CODEC(DoubleDelta, LZ4)" }), msg: smallMessage, tsName: "ts"},
		{name: "a DateTime that is not the source's timestamp", ts: bigTimestamp, msg: smallMessage, tsName: "event_time"},
		{name: "Nullable timestamp", ts: with(bigTimestamp, func(c *clickhouse.AdvisorColumn) { c.Type = "Nullable(DateTime)" }), msg: smallMessage, tsName: "ts"},
		{name: "String with an explicit codec", ts: smallTS, msg: with(bigMessage, func(c *clickhouse.AdvisorColumn) { c.Codec = "CODEC(LZ4HC(9))" }), tsName: "ts"},
		{name: "String under 10 GiB", ts: smallTS, msg: with(bigMessage, func(c *clickhouse.AdvisorColumn) { c.CompressedBytes, c.UncompressedBytes = 10*gib-1, 20*gib }), tsName: "ts"},
		{name: "String under 20% of the table", ts: smallTS, msg: with(bigMessage, func(c *clickhouse.AdvisorColumn) { c.CompressedBytes, c.UncompressedBytes = 19*gib, 40*gib }), tsName: "ts"},
		{name: "String that compresses 3 to 1", ts: smallTS, msg: with(bigMessage, func(c *clickhouse.AdvisorColumn) { c.UncompressedBytes = 180 * gib }), tsName: "ts"},
		{name: "Nullable(String)", ts: smallTS, msg: with(bigMessage, func(c *clickhouse.AdvisorColumn) { c.Type = "Nullable(String)" }), tsName: "ts"},
		{name: "table size unknown", ts: smallTS, msg: bigMessage, tsName: "ts", meta: func(m *clickhouse.AdvisorMetadata) { m.Table.TotalBytes = nil }},
		{name: "view does not pass the columns through", ts: bigTimestamp, msg: bigMessage, tsName: "ts", meta: func(m *clickhouse.AdvisorMetadata) {
			m.PassThrough = clickhouse.AdvisorPassThrough{Columns: []string{"tenant"}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := codecMeta(tt.ts, tt.msg)
			if tt.meta != nil {
				tt.meta(meta)
			}
			if found := findingsOf(Analyze(Facts{Metadata: meta, TimestampColumn: tt.tsName}), ruleCodec); len(found) != 0 {
				t.Fatalf("R5 fired: %+v", found)
			}
		})
	}
}
