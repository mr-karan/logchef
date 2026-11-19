package advisor

import (
	"reflect"
	"testing"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

func typedColumn(name, typ string) clickhouse.AdvisorColumn {
	return clickhouse.AdvisorColumn{Name: name, Type: typ, CompressedBytes: 30 * gib, UncompressedBytes: 90 * gib}
}

func TestMaterializedColumnFires(t *testing.T) {
	for _, tt := range []struct {
		name     string
		column   clickhouse.AdvisorColumn
		item     models.FilterItem
		wantPath []string
		wantDDL  []string
		title    string
	}{
		{
			name:     "Map key",
			column:   typedColumn("attrs", "Map(String, String)"),
			item:     pathItem("attrs", models.AccessMap, models.MatchEq, "user.id"),
			wantPath: []string{"user.id"},
			wantDDL: []string{
				"ALTER TABLE `logs`.`app` ADD COLUMN IF NOT EXISTS `attrs_user_id` String MATERIALIZED `attrs`['user.id'];",
				"ALTER TABLE `logs`.`app` MATERIALIZE COLUMN `attrs_user_id`;",
			},
			title: "Add a materialized column for attrs['user.id']",
		},
		{
			name:     "LogchefQL nested field on a LowCardinality-keyed Map joins its segments",
			column:   typedColumn("attrs", "Map(LowCardinality(String), String)"),
			item:     pathItem("attrs", models.AccessNested, models.MatchNeq, "user", "id"),
			wantPath: []string{"user.id"},
			wantDDL: []string{
				"ALTER TABLE `logs`.`app` ADD COLUMN IF NOT EXISTS `attrs_user_id` String MATERIALIZED `attrs`['user.id'];",
				"ALTER TABLE `logs`.`app` MATERIALIZE COLUMN `attrs_user_id`;",
			},
			title: "Add a materialized column for attrs['user.id']",
		},
		{
			name:     "JSON path in a String",
			column:   typedColumn("body", "String"),
			item:     pathItem("body", models.AccessJSONString, models.MatchEq, "a", "b"),
			wantPath: []string{"a", "b"},
			wantDDL: []string{
				"ALTER TABLE `logs`.`app` ADD COLUMN IF NOT EXISTS `body_a_b` String MATERIALIZED JSONExtractString(`body`, 'a', 'b');",
				"ALTER TABLE `logs`.`app` MATERIALIZE COLUMN `body_a_b`;",
			},
			title: "Add a materialized column for JSONExtractString(body, 'a', 'b')",
		},
		{
			name:     "escaped column and key",
			column:   typedColumn("a`b", "String"),
			item:     pathItem("a`b", models.AccessNested, models.MatchRange, "it's"),
			wantPath: []string{"it's"},
			wantDDL: []string{
				"ALTER TABLE `logs`.`app` ADD COLUMN IF NOT EXISTS `a_b_it_s` String MATERIALIZED JSONExtractString(`a\\x60b`, 'it''s');",
				"ALTER TABLE `logs`.`app` MATERIALIZE COLUMN `a_b_it_s`;",
			},
			title: "Add a materialized column for JSONExtractString(`a\\x60b`, 'it''s')",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			usage := usageOf(500, used(100, 50, tt.item), used(30, 150, tt.item, plain("tenant", models.MatchEq)))
			found := findingsOf(Analyze(Facts{Metadata: testMeta(tt.column), Usage: usage}), ruleMaterializedColumn)
			if len(found) != 1 {
				t.Fatalf("R3 findings = %+v, want 1", found)
			}
			f := found[0]
			if !reflect.DeepEqual(f.DDL, tt.wantDDL) {
				t.Errorf("DDL = %q\nwant %q", f.DDL, tt.wantDDL)
			}
			if f.Title != tt.title || f.Column != tt.column.Name || !reflect.DeepEqual(f.Path, tt.wantPath) || f.Impact != models.ImpactMedium {
				t.Errorf("finding = %+v", f)
			}
			wantUsage := []models.AdvisorEvidence{
				{Label: "Queries that filter on " + f.Title[len("Add a materialized column for "):], Value: "130 of 500 (26%)", Source: "logchef"},
				{Label: "Mean duration of those queries", Value: "73 ms", Source: "logchef"},
				{Label: "Match kinds", Value: string(tt.item.Kind), Source: "logchef"},
				{Label: "Size of " + tt.column.Name, Value: "30.0 GiB compressed, 90.0 GiB uncompressed, 30% of the table", Source: "system.columns"},
			}
			if !reflect.DeepEqual(f.Evidence, wantUsage) {
				t.Errorf("evidence = %+v\nwant %+v", f.Evidence, wantUsage)
			}
		})
	}
}

func TestMaterializedColumnGuards(t *testing.T) {
	mapKey := pathItem("attrs", models.AccessMap, models.MatchEq, "k")
	for _, tt := range []struct {
		name    string
		columns []clickhouse.AdvisorColumn
		usage   Usage
	}{
		{"share under 10%", []clickhouse.AdvisorColumn{typedColumn("attrs", "Map(String, String)")}, usageOf(1_000, used(99, 1, mapKey))},
		{"under 20 queries", []clickhouse.AdvisorColumn{typedColumn("attrs", "Map(String, String)")}, usageOf(150, used(19, 1, mapKey))},
		{"Map with another value type", []clickhouse.AdvisorColumn{typedColumn("attrs", "Map(String, UInt64)")}, usageOf(100, used(100, 1, mapKey))},
		{"JSON path on a Map", []clickhouse.AdvisorColumn{typedColumn("attrs", "Map(String, String)")}, usageOf(100, used(100, 1, pathItem("attrs", models.AccessJSONString, models.MatchEq, "k")))},
		{"json_other", []clickhouse.AdvisorColumn{typedColumn("body", "String")}, usageOf(100, used(100, 1, pathItem("body", models.AccessJSONOther, models.MatchEq, "k")))},
		{"native JSON column", []clickhouse.AdvisorColumn{typedColumn("body", "JSON")}, usageOf(100, used(100, 1, pathItem("body", models.AccessNested, models.MatchEq, "k")))},
		{"Nullable String", []clickhouse.AdvisorColumn{typedColumn("body", "Nullable(String)")}, usageOf(100, used(100, 1, pathItem("body", models.AccessJSONString, models.MatchEq, "k")))},
		{"plain column", []clickhouse.AdvisorColumn{typedColumn("attrs", "Map(String, String)")}, usageOf(100, used(100, 1, plain("attrs", models.MatchEq)))},
		{"column missing", nil, usageOf(100, used(100, 1, mapKey))},
		{"generated name exists", []clickhouse.AdvisorColumn{typedColumn("attrs", "Map(String, String)"), typedColumn("attrs_k", "UInt8")}, usageOf(100, used(100, 1, mapKey))},
		{"a column already materializes the key", []clickhouse.AdvisorColumn{
			typedColumn("attrs", "Map(String, String)"),
			{Name: "k_of_attrs", Type: "String", DefaultKind: "MATERIALIZED", DefaultExpression: "attrs['k']"},
		}, usageOf(100, used(100, 1, mapKey))},
		{"a column already extracts the path", []clickhouse.AdvisorColumn{
			typedColumn("body", "String"),
			{Name: "ab", Type: "String", DefaultKind: "ALIAS", DefaultExpression: "JSONExtractString(body, 'a', 'b')"},
		}, usageOf(100, used(100, 1, pathItem("body", models.AccessNested, models.MatchEq, "a", "b")))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if found := findingsOf(Analyze(Facts{Metadata: testMeta(tt.columns...), Usage: tt.usage}), ruleMaterializedColumn); len(found) != 0 {
				t.Fatalf("R3 fired: %+v", found)
			}
		})
	}
}

// The JSON paths 'a.b' and 'a', 'b' are different expressions with one
// generated name: each gets its own finding id, and only the more used one a
// finding, so applying it never hides the other under IF NOT EXISTS.
func TestMaterializedColumnPathsStayDistinct(t *testing.T) {
	joined := pathItem("body", models.AccessJSONString, models.MatchEq, "a.b")
	split := pathItem("body", models.AccessJSONString, models.MatchEq, "a", "b")
	meta := testMeta(typedColumn("body", "String"))
	found := findingsOf(Analyze(Facts{Metadata: meta, Usage: usageOf(300, used(100, 1, joined), used(150, 1, split))}), ruleMaterializedColumn)
	if len(found) != 1 || !reflect.DeepEqual(found[0].Path, []string{"a", "b"}) {
		t.Fatalf("R3 findings = %+v, want one on the path a, b", found)
	}
	other := findingsOf(Analyze(Facts{Metadata: meta, Usage: usageOf(300, used(150, 1, joined), used(100, 1, split))}), ruleMaterializedColumn)
	if len(other) != 1 || !reflect.DeepEqual(other[0].Path, []string{"a.b"}) || other[0].ID == found[0].ID {
		t.Fatalf("R3 findings = %+v, want one on the path a.b with its own id", other)
	}
	if want := "ALTER TABLE `logs`.`app` ADD COLUMN IF NOT EXISTS `body_a_b` String MATERIALIZED JSONExtractString(`body`, 'a.b');"; other[0].DDL[0] != want {
		t.Errorf("DDL = %q, want %q", other[0].DDL[0], want)
	}
}

// A nested field and a SQL map access on one Map key are one subject.
func TestMaterializedColumnMergesAccesses(t *testing.T) {
	meta := testMeta(typedColumn("attrs", "Map(String, String)"))
	usage := usageOf(200,
		used(15, 1, pathItem("attrs", models.AccessMap, models.MatchEq, "user.id")),
		used(10, 1, pathItem("attrs", models.AccessNested, models.MatchPosition, "user", "id")),
	)
	found := findingsOf(Analyze(Facts{Metadata: meta, Usage: usage}), ruleMaterializedColumn)
	if len(found) != 1 || found[0].Evidence[0].Value != "25 of 200 (12%)" || found[0].Evidence[2].Value != "eq, position" {
		t.Fatalf("R3 findings = %+v, want one with 25 queries of both kinds", found)
	}
}
