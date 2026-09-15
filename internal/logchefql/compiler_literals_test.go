package logchefql

import (
	"strings"
	"testing"
)

func TestNumericLiteralPreservesLexeme(t *testing.T) {
	for _, tc := range []struct {
		literal string
		wantSQL string
		wantLog string
	}{
		{"9007199254740993", "`p`.`tcode` = 9007199254740993", "p.tcode:=9007199254740993"},
		{"-9007199254740993", "`p`.`tcode` = -9007199254740993", "p.tcode:=-9007199254740993"},
		{"+9007199254740993", "`p`.`tcode` = +9007199254740993", "p.tcode:=+9007199254740993"},
		{"1.2300", "`p`.`tcode` = 1.2300", "p.tcode:=1.2300"},
	} {
		t.Run(tc.literal, func(t *testing.T) {
			schema := &Schema{Columns: []ColumnInfo{{Name: "p", Type: "JSON"}}}
			sqlResult := Translate("p.tcode="+tc.literal, schema)
			if !sqlResult.Valid || sqlResult.SQL != tc.wantSQL {
				t.Fatalf("SQL = %q, want %q (result = %+v)", sqlResult.SQL, tc.wantSQL, sqlResult)
			}
			logsResult := TranslateToLogsQL("p.tcode="+tc.literal, nil)
			if !logsResult.Valid || logsResult.Query != tc.wantLog {
				t.Fatalf("LogsQL = %q, want %q (result = %+v)", logsResult.Query, tc.wantLog, logsResult)
			}
		})
	}
}

func TestSQLNullOperators(t *testing.T) {
	for _, tc := range []struct {
		operator Operator
		want     string
		valid    bool
	}{
		{OpEquals, "isNull(`field`)", true},
		{OpNotEquals, "isNotNull(`field`)", true},
		{OpGT, "", false},
		{OpLT, "", false},
		{OpGTE, "", false},
		{OpLTE, "", false},
		{OpRegex, "", false},
		{OpNotRegex, "", false},
	} {
		t.Run(string(tc.operator), func(t *testing.T) {
			result := Translate("field "+string(tc.operator)+" null", nil)
			if result.Valid != tc.valid {
				t.Fatalf("valid = %t, want %t, result = %+v", result.Valid, tc.valid, result)
			}
			if tc.valid && result.SQL != tc.want {
				t.Fatalf("SQL = %q, want %q", result.SQL, tc.want)
			}
		})
	}
}

func TestNumericLiteralRejectsStringFields(t *testing.T) {
	for _, tc := range []struct {
		columnType string
		query      string
	}{
		{"String", "p=123"},
		{"LowCardinality(Nullable(String))", "p>123"},
		{"String", "p.trade_id=123"},
		{"Map(String, String)", "p.trade_id=123"},
	} {
		t.Run(tc.columnType+"/"+tc.query, func(t *testing.T) {
			result := Translate(tc.query, &Schema{Columns: []ColumnInfo{{Name: "p", Type: tc.columnType}}})
			if result.Valid || result.Error == nil || !strings.Contains(result.Error.Message, "quote the value") {
				t.Fatalf("result = %+v, want actionable type error", result)
			}
		})
	}
}

func TestTypedContainerFieldResolution(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		field string
		type_ string
		part  string
		want  string
	}{
		{"map string regex", `attributes.value~"x"`, "attributes", "Map(String, String)", "positionCaseInsensitive", "`attributes`['value']"},
		{"map number regex", `attributes.value~"1"`, "attributes", "Map(String, UInt64)", "positionCaseInsensitive", "toString(`attributes`['value'])"},
		{"json typed regex", `payload.value~"1"`, "payload", "JSON(value UInt64)", "positionCaseInsensitive", "toString(`payload`.`value`)"},
		{"json dynamic regex", `payload.value~"x"`, "payload", "JSON", "positionCaseInsensitive", "toString(`payload`.`value`)"},
		{"nullable json dynamic regex", `payload.value~"x"`, "payload", "Nullable(JSON)", "positionCaseInsensitive", "toString(assumeNotNull(`payload`).`value`)"},
		{"nullable low cardinality string", `payload~"x"`, "payload", "LowCardinality(Nullable(String))", "positionCaseInsensitive", "`payload`"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := Translate(tc.query, &Schema{Columns: []ColumnInfo{{Name: tc.field, Type: tc.type_}}})
			if !result.Valid || !strings.Contains(result.SQL, tc.part) || !strings.Contains(result.SQL, tc.want) {
				t.Fatalf("result = %+v, want %q and %q", result, tc.part, tc.want)
			}
		})
	}
}

type unknownASTNode struct{}

func (unknownASTNode) nodeType() string { return "unknown" }

func TestGeneratorsRejectMalformedAST(t *testing.T) {
	for _, tc := range []struct {
		name string
		node ASTNode
	}{
		{"unknown SQL node", unknownASTNode{}},
		{"unknown operator", &ExpressionNode{Key: "field", Operator: "?", Value: "value"}},
		{"known scalar nested path", &ExpressionNode{Key: NestedField{Base: "count", Path: []string{"value"}}, Operator: OpEquals, Value: NumericLiteral("1")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := translateAST(tc.node, &Schema{Columns: []ColumnInfo{{Name: "count", Type: "UInt64"}}})
			if result.Valid || result.Error == nil {
				t.Fatalf("result = %+v, want a translation error", result)
			}
		})
	}
}

func BenchmarkSchemaAwareCompile(b *testing.B) {
	query := `payload.tcode=9007199254740993 and payload.component~"api" | payload.tcode payload.component`
	schema := &Schema{Columns: []ColumnInfo{{Name: "payload", Type: "JSON(tcode UInt64)"}}}
	params := QueryBuildParams{
		LogchefQL:      query,
		Schema:         schema,
		TableName:      "logs.events",
		TimestampField: "timestamp",
		StartTime:      "2026-01-01 00:00:00",
		EndTime:        "2026-01-01 01:00:00",
		Timezone:       "UTC",
		Limit:          100,
	}
	b.ReportAllocs()
	for b.Loop() {
		translated := Translate(query, schema)
		if !translated.Valid {
			b.Fatal(translated.Error)
		}
		if _, err := BuildFullQueryFromTranslation(params, translated); err != nil {
			b.Fatal(err)
		}
	}
}
