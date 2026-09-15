package clickhouse

import (
	"fmt"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestEmptyValueFilterOnlyAppliesToStringColumns(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		want     string
	}{
		{name: "string", typeName: "String", want: "`level` != ''"},
		{name: "nullable string", typeName: "Nullable(String)", want: "`level` != ''"},
		{name: "low cardinality nullable string", typeName: "LowCardinality(Nullable(String))", want: "`level` != ''"},
		{name: "nullable low cardinality string", typeName: "Nullable(LowCardinality(String))", want: "`level` != ''"},
		{name: "enum", typeName: "Enum8('' = 0, 'info' = 1)", want: "1"},
		{name: "nullable enum", typeName: "Nullable(Enum8('' = 0, 'info' = 1))", want: "1"},
		{name: "low cardinality enum", typeName: "LowCardinality(Enum8('' = 0, 'info' = 1))", want: "1"},
		{name: "number", typeName: "Nullable(UInt64)", want: "1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := emptyValueFilter(tt.typeName, "`level`"); got != tt.want {
				t.Fatalf("emptyValueFilter(%q) = %q, want %q", tt.typeName, got, tt.want)
			}
		})
	}
}

func TestExtractFieldValuesPreservesEmptyEnumValue(t *testing.T) {
	result := &models.QueryResult{Logs: []map[string]any{
		{"value": "", "cnt": uint64(2)},
		{"value": "info", "cnt": uint64(3)},
	}}

	if got := extractFieldValues(result, false); len(got) != 2 || got[0].Value != "" || got[0].Count != 2 {
		t.Fatalf("enum values = %+v, want empty and info values", got)
	}
	if got := extractFieldValues(result, true); len(got) != 1 || got[0].Value != "info" {
		t.Fatalf("string values = %+v, want only info", got)
	}
}

func TestEnumFieldValuesClickHouse(t *testing.T) {
	client, ctx := integrationClient(t)
	table := fmt.Sprintf("logchef_enum_values_test_%d", time.Now().UnixNano())
	conn := client.connection()
	if err := conn.Exec(ctx, "CREATE TABLE "+table+" (ts DateTime64(9), level Enum8('' = 0, 'INFO' = 1)) ENGINE=MergeTree ORDER BY ts"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP TABLE "+table)
	if err := conn.Exec(ctx, "INSERT INTO "+table+" VALUES (toDateTime64('2026-09-15 10:00:00', 9), ''), (toDateTime64('2026-09-15 10:00:01', 9), 'INFO')"); err != nil {
		t.Fatal(err)
	}

	result, err := client.GetFieldDistinctValues(ctx, "default", table, FieldValuesParams{
		FieldName: "level", FieldType: "Enum8('' = 0, 'INFO' = 1)", TimestampField: "ts",
		StartTime: time.Date(2026, 9, 15, 9, 59, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 9, 15, 10, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalDistinct != 2 || len(result.Values) != 2 {
		t.Fatalf("enum values = %+v, want two values including empty enum", result)
	}
}

func TestFieldValuesMemoryClickHouseWithJSONFilter(t *testing.T) {
	for _, columnType := range []string{"String", "JSON"} {
		t.Run(columnType, func(t *testing.T) {
			testFieldValuesMemoryJSONFilter(t, columnType)
		})
	}
}

func testFieldValuesMemoryJSONFilter(t *testing.T, columnType string) {
	t.Helper()
	client, ctx := integrationClient(t)
	table := fmt.Sprintf("logchef_memory_field_values_test_%d", time.Now().UnixNano())
	conn := client.connection()
	if columnType == "JSON" {
		version, err := conn.ServerVersion()
		if err != nil {
			t.Fatal(err)
		}
		if version.Version.Major < 25 || version.Version.Major == 25 && version.Version.Minor < 3 {
			t.Skip("native JSON requires ClickHouse 25.3 or newer")
		}
	}
	if err := conn.Exec(ctx, "CREATE TABLE "+table+" (ts DateTime64(9), p "+columnType+", level Enum8('' = 0, 'info' = 1)) ENGINE=Memory"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(ctx, "DROP TABLE "+table)
	if err := conn.Exec(ctx, "INSERT INTO "+table+" VALUES (toDateTime64('2026-09-15 10:00:00', 9), '{\"trade_id\":\"204036076\"}', 'info'), (toDateTime64('2026-09-15 10:00:01', 9), '{\"trade_id\":\"7\"}', 'info')"); err != nil {
		t.Fatal(err)
	}

	result, err := client.GetFieldDistinctValues(ctx, "default", table, FieldValuesParams{
		FieldName: "level", FieldType: "Enum8('' = 0, 'info' = 1)", TimestampField: "ts",
		StartTime: time.Date(2026, 9, 15, 9, 59, 0, 0, time.UTC),
		EndTime:   time.Date(2026, 9, 15, 10, 1, 0, 0, time.UTC),
		LogchefQL: `p.trade_id="204036076"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.TotalDistinct != 1 || len(result.Values) != 1 || result.Values[0].Value != "info" || result.Values[0].Count != 1 {
		t.Fatalf("filtered enum values = %+v, want one info value", result)
	}
}
