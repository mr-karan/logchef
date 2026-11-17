package advisor

import (
	"reflect"
	"slices"
	"testing"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

const textIndexNote = "text index advice needs ClickHouse 26.8"

func TestTextIndexFires(t *testing.T) {
	const column = "a`b"
	meta := testMeta(stringColumn(column))
	meta.Indexes = []clickhouse.AdvisorIndex{{Name: "idx_ab_bloom", Type: "bloom_filter", Expr: "`a\\x60b`"}}
	usage := usageOf(1_000,
		used(150, 400, plain(column, models.MatchToken)),
		used(60, 200, plain(column, models.MatchLikeToken), plain("tenant", models.MatchEq)),
		used(40, 100, plain(column, models.MatchToken), plain(column, models.MatchLikeToken)),
		// A plain LIKE does not count toward the trigger or the evidence.
		used(300, 50, plain(column, models.MatchLike)),
	)
	report := Analyze(Facts{Metadata: meta, Usage: usage})
	found := findingsOf(report, ruleTextIndex)
	if len(found) != 1 {
		t.Fatalf("R2 findings = %+v, want 1", report.Findings)
	}
	f := found[0]
	wantDDL := []string{
		"ALTER TABLE `logs`.`app` ADD INDEX IF NOT EXISTS `idx_a_b_text` `a\\x60b` TYPE text(tokenizer = splitByNonAlpha);",
		"ALTER TABLE `logs`.`app` MATERIALIZE INDEX `idx_a_b_text`;",
	}
	if !reflect.DeepEqual(f.DDL, wantDDL) {
		t.Errorf("DDL = %q\nwant %q", f.DDL, wantDDL)
	}
	if want := "EXPLAIN indexes = 1 SELECT count() FROM `logs`.`app` WHERE hasToken(`a\\x60b`, '<token>')"; f.Verify != want {
		t.Errorf("verify = %q, want %q", f.Verify, want)
	}
	wantEvidence := []models.AdvisorEvidence{
		{Label: "Queries with a token search", Value: "250 of 1,000 (25%)", Source: "logchef"},
		{Label: "Mean duration of those queries", Value: "304 ms", Source: "logchef"},
		{Label: "Queries with hasToken, hasAllTokens or hasAnyTokens", Value: "190 of 1,000 (19%)", Source: "logchef"},
		{Label: "Queries with a LIKE on a complete token", Value: "100 of 1,000 (10%)", Source: "logchef"},
		{Label: "Size of a`b", Value: "2.0 GiB compressed, 8.0 GiB uncompressed, 2% of the table", Source: "system.columns"},
		{Label: "Existing skip indexes", Value: "idx_ab_bloom (bloom_filter on `a\\x60b`)", Source: "system.data_skipping_indices"},
	}
	if !reflect.DeepEqual(f.Evidence, wantEvidence) {
		t.Errorf("evidence = %+v\nwant %+v", f.Evidence, wantEvidence)
	}
	if f.Impact != models.ImpactMedium || f.Column != column || f.Rebuild || f.Candidate || f.Path != nil {
		t.Errorf("finding = %+v, want a medium in-place finding on %s", f, column)
	}
	if slices.Contains(report.Notes, textIndexNote) {
		t.Errorf("notes = %q, want no version note on 26.8", report.Notes)
	}
}

func TestTextIndexVersion(t *testing.T) {
	usage := usageOf(200, used(100, 1, plain("msg", models.MatchLikeToken)))
	for _, tt := range []struct {
		version   string
		wantFires bool
	}{
		{"26.8.0", true},
		{"26.8.2", true},
		{"27.1.1", true},
		{"26.7.9", false},
		{"25.12.3", false},
		{"24.1.2", false},
		{"", false},
	} {
		t.Run(tt.version, func(t *testing.T) {
			meta := testMeta(stringColumn("msg"))
			meta.ServerVersion = tt.version
			report := Analyze(Facts{Metadata: meta, Usage: usage})
			if fires := len(findingsOf(report, ruleTextIndex)) == 1; fires != tt.wantFires {
				t.Errorf("R2 fires = %v, want %v", fires, tt.wantFires)
			}
			if noted := slices.Contains(report.Notes, textIndexNote); noted == tt.wantFires {
				t.Errorf("notes = %q, want the version note only when R2 cannot fire", report.Notes)
			}
		})
	}

	// An old server with no token search gets no note.
	meta := testMeta(stringColumn("msg"))
	meta.ServerVersion = "24.1.2"
	if report := Analyze(Facts{Metadata: meta, Usage: usageOf(200, used(100, 1, plain("msg", models.MatchEq)))}); len(report.Notes) != 0 {
		t.Errorf("notes = %q, want none without token search", report.Notes)
	}
}

func TestTextIndexGuards(t *testing.T) {
	hot := func(items ...models.FilterItem) Usage { return usageOf(200, used(100, 1, items...)) }
	token := plain("msg", models.MatchToken)
	for _, tt := range []struct {
		name  string
		meta  func(*clickhouse.AdvisorMetadata)
		usage Usage
	}{
		{name: "like only", usage: hot(plain("msg", models.MatchLike))},
		{name: "prefix only", usage: hot(plain("msg", models.MatchPrefix))},
		{name: "regex only", usage: hot(plain("msg", models.MatchRegex))},
		{name: "position only", usage: hot(plain("msg", models.MatchPosition))},
		{name: "ilike only", usage: hot(plain("msg", models.MatchILike))},
		{name: "like, regex, position and ilike together", usage: hot(
			plain("msg", models.MatchLike), plain("msg", models.MatchRegex), plain("msg", models.MatchPosition), plain("msg", models.MatchILike),
		)},
		{name: "share under 10%", usage: usageOf(1_000, used(99, 1, token))},
		{name: "under 20 queries", usage: usageOf(100, used(19, 1, token))},
		{name: "usage rules off", usage: usageOf(99, used(99, 1, token))},
		{name: "a path, not a plain column", usage: hot(pathItem("msg", models.AccessJSONString, models.MatchToken, "x"))},
		{name: "not a String column", meta: func(m *clickhouse.AdvisorMetadata) { m.Columns[2].Type = "LowCardinality(String)" }, usage: hot(token)},
		{name: "column missing from the target", meta: func(m *clickhouse.AdvisorMetadata) { m.Columns = m.Columns[:2] }, usage: hot(token)},
		{name: "view does not pass the column through", meta: func(m *clickhouse.AdvisorMetadata) {
			m.PassThrough = clickhouse.AdvisorPassThrough{Columns: []string{"ts", "tenant"}}
		}, usage: hot(token)},
		{name: "a text index reads the column", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Indexes = []clickhouse.AdvisorIndex{{Name: "t", Type: "text(tokenizer = splitByNonAlpha, preprocessor = lower(msg))", Expr: "lower(msg)"}}
		}, usage: hot(token)},
		{name: "a tokenbf_v1 index reads the column", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Indexes = []clickhouse.AdvisorIndex{{Name: "t", Type: "tokenbf_v1(32768, 3, 0)", Expr: "msg"}}
		}, usage: hot(token)},
		{name: "an ngrambf_v1 index reads the column", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Indexes = []clickhouse.AdvisorIndex{{Name: "t", Type: "ngrambf_v1(3, 256, 2, 0)", Expr: "`msg`"}}
		}, usage: hot(token)},
		{name: "an index has the generated name", meta: func(m *clickhouse.AdvisorMetadata) {
			m.Indexes = []clickhouse.AdvisorIndex{{Name: "idx_msg_text", Type: "minmax", Expr: "length(other)"}}
		}, usage: hot(token)},
		{name: "indexes unreadable", meta: func(m *clickhouse.AdvisorMetadata) { m.Inputs[2].State = models.AdvisorInputDenied }, usage: hot(token)},
		{name: "indexes truncated", meta: func(m *clickhouse.AdvisorMetadata) { m.IndexesTruncated = true }, usage: hot(token)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta(stringColumn("msg"))
			if tt.meta != nil {
				tt.meta(meta)
			}
			if found := findingsOf(Analyze(Facts{Metadata: meta, Usage: tt.usage}), ruleTextIndex); len(found) != 0 {
				t.Fatalf("R2 fired: %+v", found)
			}
		})
	}
}

func TestTextIndexIgnoresOtherIndexes(t *testing.T) {
	meta := testMeta(stringColumn("msg"))
	meta.Indexes = []clickhouse.AdvisorIndex{
		{Name: "idx_msg_bloom", Type: "bloom_filter", Expr: "msg"},
		{Name: "idx_message", Type: "tokenbf_v1(32768, 3, 0)", Expr: "message"},
	}
	usage := usageOf(200, used(100, 1, plain("msg", models.MatchToken)))
	if found := findingsOf(Analyze(Facts{Metadata: meta, Usage: usage}), ruleTextIndex); len(found) != 1 {
		t.Fatalf("R2 findings = %+v, want 1 despite a bloom filter on msg and a token index on another column", found)
	}
}

func TestTextIndexNameCollision(t *testing.T) {
	// a`b and a_b both map to idx_a_b_text; the more used column gets it.
	meta := testMeta(stringColumn("a`b"), stringColumn("a_b"))
	usage := usageOf(200, used(60, 1, plain("a`b", models.MatchToken)), used(80, 1, plain("a_b", models.MatchToken)))
	found := findingsOf(Analyze(Facts{Metadata: meta, Usage: usage}), ruleTextIndex)
	if len(found) != 1 || found[0].Column != "a_b" {
		t.Fatalf("R2 findings = %+v, want one on a_b", found)
	}
}

func TestTextIndexPositionNote(t *testing.T) {
	logchefQL := func(queries int64, items ...models.FilterItem) ShapeUsage {
		u := used(queries, 1, items...)
		u.Language = models.QueryLanguageLogchefQL
		return u
	}
	sql := func(queries int64, items ...models.FilterItem) ShapeUsage {
		u := used(queries, 1, items...)
		u.Language = models.QueryLanguageClickHouseSQL
		return u
	}
	const note = "1,200 LogchefQL queries search `msg` with `~`, which compiles to `positionCaseInsensitive`. No index serves it."
	for _, tt := range []struct {
		name      string
		version   string
		usage     Usage
		wantNotes []string
	}{
		{"LogchefQL ~ passes the trigger", "26.8.2", usageOf(4_000, logchefQL(1_200, plain("msg", models.MatchPosition))), []string{note}},
		{"also on an older server", "24.1.2", usageOf(4_000, logchefQL(1_200, plain("msg", models.MatchPosition))), []string{note}},
		{"SQL position functions do not count", "26.8.2", usageOf(4_000, sql(1_200, plain("msg", models.MatchPosition))), nil},
		{"share under 10%", "26.8.2", usageOf(4_000, logchefQL(399, plain("msg", models.MatchPosition))), nil},
		{"a path, not a plain column", "26.8.2", usageOf(200, logchefQL(100, pathItem("msg", models.AccessNested, models.MatchPosition, "x"))), nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta := testMeta(stringColumn("msg"))
			meta.ServerVersion = tt.version
			report := Analyze(Facts{Metadata: meta, Usage: tt.usage})
			if !reflect.DeepEqual(report.Notes, tt.wantNotes) {
				t.Errorf("notes = %q, want %q", report.Notes, tt.wantNotes)
			}
			if found := findingsOf(report, ruleTextIndex); len(found) != 0 {
				t.Errorf("R2 fired on position items: %+v", found)
			}
		})
	}
}
