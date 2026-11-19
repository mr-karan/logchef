package advisor

import (
	"fmt"
	"reflect"
	"regexp"
	"testing"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

const gib = uint64(1) << 30

// testMeta is a readable MergeTree table logs.app ordered by (tenant, ts).
func testMeta(columns ...clickhouse.AdvisorColumn) *clickhouse.AdvisorMetadata {
	total := 100 * gib
	inputs := []models.AdvisorInput{}
	for _, name := range []string{clickhouse.AdvisorInputTables, clickhouse.AdvisorInputColumns, clickhouse.AdvisorInputIndexes, clickhouse.AdvisorInputParts, clickhouse.AdvisorInputQueryLog} {
		inputs = append(inputs, models.AdvisorInput{Name: name, State: models.AdvisorInputOK})
	}
	base := []clickhouse.AdvisorColumn{
		{Name: "ts", Type: "DateTime", InSortingKey: true, InPrimaryKey: true},
		{Name: "tenant", Type: "String", InSortingKey: true, InPrimaryKey: true},
	}
	return &clickhouse.AdvisorMetadata{
		ServerVersion: "26.8.2",
		Target:        &models.AdvisorTarget{Kind: models.AdvisorTargetTable, Database: "logs", Table: "app", Engine: "MergeTree"},
		Table:         &clickhouse.AdvisorTable{Database: "logs", Name: "app", Engine: "MergeTree", SortingKey: "tenant, ts", PrimaryKey: "tenant, ts", TotalBytes: &total},
		PassThrough:   clickhouse.AdvisorPassThrough{All: true},
		Columns:       append(base, columns...),
		Inputs:        inputs,
	}
}

func stringColumn(name string) clickhouse.AdvisorColumn {
	return clickhouse.AdvisorColumn{Name: name, Type: "String", CompressedBytes: 2 * gib, UncompressedBytes: 8 * gib}
}

func sampleOf(cards ...clickhouse.AdvisorCardinality) *clickhouse.AdvisorSample {
	return &clickhouse.AdvisorSample{Input: models.AdvisorInput{Name: clickhouse.AdvisorInputSample, State: models.AdvisorInputOK}, Columns: cards}
}

// usageOf records total queries: the given shapes, and the rest filtering on
// a column that no table holds, so they count toward Q.
func usageOf(total int64, shapes ...ShapeUsage) Usage {
	usage := Usage{Queries: total, Shapes: shapes}
	rest := total
	for _, s := range shapes {
		rest -= s.Queries
	}
	if rest > 0 {
		usage.Shapes = append(usage.Shapes, ShapeUsage{Shape: shape(plain("elsewhere", models.MatchEq)), Queries: rest})
	}
	return usage
}

func used(queries, meanMs int64, items ...models.FilterItem) ShapeUsage {
	return ShapeUsage{Shape: shape(items...), Queries: queries, DurationMs: queries * meanMs}
}

func findingsOf(report models.SchemaAdvisorReport, rule string) []models.SchemaFinding {
	var out []models.SchemaFinding
	for i := range report.Findings {
		if report.Findings[i].Rule == rule {
			out = append(out, report.Findings[i])
		}
	}
	return out
}

func TestReportStatus(t *testing.T) {
	denied := testMeta()
	denied.Inputs[1].State = models.AdvisorInputDenied
	view := testMeta()
	view.Unavailable, view.Target = clickhouse.AdvisorViewNotResolvable, nil
	limited := testMeta()
	limited.Inputs[4].State = models.AdvisorInputMissing
	for _, tt := range []struct {
		name       string
		facts      Facts
		wantStatus string
		wantReason string
	}{
		{"ok", Facts{Metadata: testMeta()}, models.AdvisorStatusOK, ""},
		{"unresolvable view", Facts{Metadata: view}, models.AdvisorStatusUnavailable, clickhouse.AdvisorViewNotResolvable},
		{"columns denied", Facts{Metadata: denied}, models.AdvisorStatusUnavailable, "system.columns denied"},
		{"query log missing", Facts{Metadata: limited}, models.AdvisorStatusPartial, ""},
		{"sample limited", Facts{Metadata: testMeta(), Sample: &clickhouse.AdvisorSample{Input: models.AdvisorInput{Name: clickhouse.AdvisorInputSample, State: models.AdvisorInputLimited}}}, models.AdvisorStatusPartial, ""},
		{"sample skipped", Facts{Metadata: testMeta(), Sample: &clickhouse.AdvisorSample{Input: models.AdvisorInput{Name: clickhouse.AdvisorInputSample, State: models.AdvisorInputSkipped}}}, models.AdvisorStatusOK, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report := Analyze(tt.facts)
			if report.Status != tt.wantStatus || report.Reason != tt.wantReason {
				t.Fatalf("status %q reason %q, want %q %q", report.Status, report.Reason, tt.wantStatus, tt.wantReason)
			}
			if report.Findings == nil {
				t.Fatal("findings is nil, want an empty list")
			}
		})
	}
}

func TestUsageGates(t *testing.T) {
	meta := testMeta(stringColumn("request_id"))
	sample := sampleOf(clickhouse.AdvisorCardinality{Column: "request_id", Distinct: 50_000})
	hot := func(q int64) ShapeUsage { return used(q, 10, plain("request_id", models.MatchEq)) }
	for _, tt := range []struct {
		name      string
		usage     Usage
		wantFires bool
		wantNotes []string
		analyzed  int64
	}{
		{"enough", usageOf(100, hot(100)), true, nil, 100},
		{"Q under 100", usageOf(99, hot(99)), false, []string{"too few recorded queries for usage rules"}, 99},
		{"queries without filters do not count toward Q", Usage{Queries: 150, Shapes: []ShapeUsage{hot(99), {Shape: models.FilterShape{}, Queries: 51}}}, false, []string{"too few recorded queries for usage rules"}, 150},
		{"incomplete record", func() Usage { u := usageOf(200, hot(160)); u.Queries, u.Unanalyzed, u.Overflow = 250, 30, 20; return u }(), false, []string{
			"20 queries were not analyzed because their source passed its daily limit of distinct filter shapes", "usage record is incomplete",
		}, 200},
		{"just under the incomplete bound", func() Usage { u := usageOf(200, hot(160)); u.Queries, u.Unanalyzed = 249, 49; return u }(), true, nil, 200},
		{"truncated", func() Usage { u := usageOf(100, hot(100)); u.Truncated = true; return u }(), true, []string{"usage truncated to the 1 most common shapes"}, 100},
		// 10,000 filtered queries, of which the list keeps 150: hot is 150 of
		// 10,000, not 150 of 150, and stays under the 5% share.
		{"shares use the window's total, not the listed shapes", Usage{Queries: 10_000, Truncated: true, Shapes: []ShapeUsage{hot(150)}}, false, []string{"usage truncated to the 1 most common shapes"}, 10_000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			report := Analyze(Facts{Metadata: meta, Usage: tt.usage, Sample: sample})
			if fires := len(findingsOf(report, ruleBloom)) == 1; fires != tt.wantFires {
				t.Errorf("R1 fires = %v, want %v", fires, tt.wantFires)
			}
			if !reflect.DeepEqual(report.Notes, tt.wantNotes) {
				t.Errorf("notes = %q, want %q", report.Notes, tt.wantNotes)
			}
			if report.QueriesAnalyzed != tt.analyzed {
				t.Errorf("queries analyzed = %d, want %d", report.QueriesAnalyzed, tt.analyzed)
			}
		})
	}
}

func TestFindingOrderAndCap(t *testing.T) {
	var columns []clickhouse.AdvisorColumn
	var wide []models.FilterItem
	var cards []clickhouse.AdvisorCardinality
	for i := range 55 {
		name := fmt.Sprintf("c%02d", i)
		columns = append(columns, stringColumn(name))
		wide = append(wide, plain(name, models.MatchEq))
		cards = append(cards, clickhouse.AdvisorCardinality{Column: name, Distinct: 20_000})
	}
	columns = append(columns, stringColumn("hot"), stringColumn("warm"))
	cards = append(cards, clickhouse.AdvisorCardinality{Column: "hot", Distinct: 20_000}, clickhouse.AdvisorCardinality{Column: "warm", Distinct: 20_000})
	// Of 1,000 queries: hot in 600 (high), warm in 300 (medium), each of
	// the 55 wide columns in 100 (low).
	usage := usageOf(1_000,
		used(600, 1, plain("hot", models.MatchEq)),
		used(300, 1, plain("warm", models.MatchIn)),
		used(100, 1, wide...),
	)
	report := Analyze(Facts{Metadata: testMeta(columns...), Usage: usage, Sample: sampleOf(cards...)})
	if len(report.Findings) != maxFindings {
		t.Fatalf("%d findings, want %d", len(report.Findings), maxFindings)
	}
	for i, want := range []struct{ column, impact string }{{"hot", models.ImpactHigh}, {"warm", models.ImpactMedium}, {"", models.ImpactLow}} {
		got := report.Findings[i]
		if want.column != "" && got.Column != want.column || got.Impact != want.impact {
			t.Errorf("finding %d = %s (%s), want %s (%s)", i, got.Column, got.Impact, want.column, want.impact)
		}
	}
	if want := []string{"7 more findings not shown"}; !reflect.DeepEqual(report.Notes, want) {
		t.Errorf("notes = %q, want %q", report.Notes, want)
	}
}

func TestFindingIDs(t *testing.T) {
	hexID := regexp.MustCompile(`^[0-9a-f]{16}$`)
	meta := testMeta(stringColumn("request_id"))
	a, b := &analysis{meta: meta}, &analysis{meta: testMeta()}
	b.meta.Target.Table = "other"
	ids := map[string]string{
		"bloom":         a.findingID(ruleBloom, "request_id", nil, ""),
		"other rule":    a.findingID(ruleMaterializedColumn, "request_id", nil, ""),
		"other table":   b.findingID(ruleBloom, "request_id", nil, ""),
		"path a.b":      a.findingID(ruleMaterializedColumn, "m", []string{"a.b"}, models.AccessJSONString),
		"path a, b":     a.findingID(ruleMaterializedColumn, "m", []string{"a", "b"}, models.AccessJSONString),
		"path as map":   a.findingID(ruleMaterializedColumn, "m", []string{"a.b"}, models.AccessMap),
		"column joined": a.findingID(ruleMaterializedColumn, "m|a", nil, ""),
	}
	seen := map[string]string{}
	for name, id := range ids {
		if !hexID.MatchString(id) {
			t.Errorf("%s: id %q is not 16 hex characters", name, id)
		}
		if other, ok := seen[id]; ok {
			t.Errorf("%s and %s share id %s", name, other, id)
		}
		seen[id] = name
	}
	if again := a.findingID(ruleBloom, "request_id", nil, ""); again != ids["bloom"] {
		t.Errorf("id changed between calls: %s then %s", ids["bloom"], again)
	}
}

func TestFormatting(t *testing.T) {
	for _, tt := range []struct{ got, want string }{
		{formatCount(int64(0)), "0"},
		{formatCount(int64(1900)), "1,900"},
		{formatCount(uint64(1234567)), "1,234,567"},
		{formatCount(int64(-1234)), "-1,234"},
		{formatBytes(512), "512 B"},
		{formatBytes(3 * gib / 2), "1.5 GiB"},
		{formatPercent(412.0 / 1900), "22%"},
		{generatedName("idx", "a`b c", "bloom"), "idx_a_b_c_bloom"},
	} {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
}

// Every finding ends with one line on the reads that system.query_log saw,
// and has no such line when the log was not read.
func TestQueryLogEvidence(t *testing.T) {
	queryLogLines := func(report models.SchemaAdvisorReport) []models.AdvisorEvidence {
		t.Helper()
		if len(report.Findings) == 0 {
			t.Fatalf("no findings: %+v", report)
		}
		var lines []models.AdvisorEvidence
		for _, f := range report.Findings {
			for _, e := range f.Evidence {
				if e.Source == clickhouse.AdvisorInputQueryLog {
					lines = append(lines, e)
				}
			}
		}
		return lines
	}
	for _, tt := range []struct {
		name string
		log  *clickhouse.AdvisorQueryLog
		want string
	}{
		{"reads", &clickhouse.AdvisorQueryLog{Queries: 1234, MedianDurationMs: 41.6, P95DurationMs: 1830, MedianReadRows: 2_500_000, MedianReadBytes: 3 << 20}, "1,234 on the server that answered: median 42 ms, p95 1,830 ms, median 2,500,000 rows and 3.0 MiB read"},
		{"no reads", &clickhouse.AdvisorQueryLog{}, "none on the server that answered"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta()
			meta.QueryLog = tt.log
			report := Analyze(Facts{Metadata: meta, TimestampColumn: "ts"})
			want := models.AdvisorEvidence{Label: "Reads of the source in 7 days, all clients", Value: tt.want, Source: clickhouse.AdvisorInputQueryLog}
			lines := queryLogLines(report)
			if len(lines) != len(report.Findings) {
				t.Fatalf("%d query log lines for %d findings", len(lines), len(report.Findings))
			}
			for _, f := range report.Findings {
				if got := f.Evidence[len(f.Evidence)-1]; got != want {
					t.Errorf("finding %s ends with %+v, want %+v", f.Rule, got, want)
				}
			}
		})
	}
	t.Run("log not read", func(t *testing.T) {
		meta := testMeta()
		meta.Inputs[4].State = models.AdvisorInputLimited
		if lines := queryLogLines(Analyze(Facts{Metadata: meta, TimestampColumn: "ts"})); len(lines) != 0 {
			t.Errorf("query log lines = %+v, want none", lines)
		}
	})
}
