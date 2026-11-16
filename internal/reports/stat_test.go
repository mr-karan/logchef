package reports

import (
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

func statResult() *models.QueryResult {
	return &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "ts", Type: "Nullable(DateTime64(3))"}, {Name: "n", Type: "Nullable(UInt64)"}},
		Logs: []map[string]any{
			{"ts": time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC), "n": 5},
			{"ts": time.Date(2026, 11, 15, 8, 0, 0, 0, time.UTC), "n": 3},
			{"ts": nil, "n": 7},
			{"ts": time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC), "n": nil},
		},
	}
}

func TestReduceStat(t *testing.T) {
	plain := models.DashboardNumberFormat{}
	grouped0 := models.DashboardNumberFormat{Decimals: new(0)}
	four := models.DashboardNumberFormat{Decimals: new(4)}
	empty := &models.QueryResult{Columns: []models.ColumnInfo{{Name: "n", Type: "UInt64"}}}
	wide := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "n", Type: "UInt256"}},
		Logs:    []map[string]any{{"n": "123456789012345678901234567890"}, {"n": "1"}},
	}
	thirds := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "n", Type: "Int32"}},
		Logs:    []map[string]any{{"n": 1}, {"n": 1}, {"n": 2}},
	}
	floats := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "n", Type: "Float64"}},
		Logs:    []map[string]any{{"n": 0.1}, {"n": 0.2}},
	}
	untyped := &models.QueryResult{
		Columns: []models.ColumnInfo{{Name: "n", Type: "String"}},
		Logs:    []map[string]any{{"n": "10"}, {"n": "20"}},
	}
	for _, tc := range []struct {
		name    string
		res     *models.QueryResult
		opts    StatOptions
		value   string
		label   string
		wantErr string
	}{
		{name: "sum skips NULL", res: statResult(), opts: StatOptions{Value: "n", Reduce: "sum"}, value: "15"},
		{name: "avg skips NULL", res: statResult(), opts: StatOptions{Value: "n", Reduce: "avg"}, value: "5"},
		{name: "min", res: statResult(), opts: StatOptions{Value: "n", Reduce: "min"}, value: "3"},
		{name: "max", res: statResult(), opts: StatOptions{Value: "n", Reduce: "max"}, value: "7"},
		{name: "first by order_by drops a NULL order", res: statResult(), opts: StatOptions{Value: "n", Reduce: "first", OrderBy: "ts"}, value: "3", label: "2026-11-15 08:00:00 UTC"},
		{name: "last by order_by selects a NULL value", res: statResult(), opts: StatOptions{Value: "n", Reduce: "last", OrderBy: "ts"}, label: "2026-11-15 10:00:00 UTC"},
		{name: "first in result order", res: statResult(), opts: StatOptions{Value: "n", Reduce: "first"}, value: "5"},
		{name: "last in result order is NULL", res: statResult(), opts: StatOptions{Value: "n", Reduce: "last"}},
		{name: "no rows sums to no data", res: empty, opts: StatOptions{Value: "n", Reduce: "sum"}},
		{name: "no rows has no last", res: empty, opts: StatOptions{Value: "n", Reduce: "last"}},
		{name: "no columns and no rows", res: &models.QueryResult{}, opts: StatOptions{Value: "n", Reduce: "sum"}},
		{name: "30-digit integer keeps every digit", res: wide, opts: StatOptions{Value: "n", Reduce: "sum", Format: grouped0}, value: "123,456,789,012,345,678,901,234,567,891"},
		{name: "avg rounds to ten places", res: thirds, opts: StatOptions{Value: "n", Reduce: "avg", Format: four}, value: "1.3333"},
		{name: "float column sums in binary", res: floats, opts: StatOptions{Value: "n", Reduce: "sum"}, value: "0.3"},
		{name: "untyped column needs a confirmed kind", res: untyped, opts: StatOptions{Value: "n", Reduce: "sum", Untyped: true}, wantErr: "must be a number"},
		{name: "confirmed number kind", res: untyped, opts: StatOptions{Value: "n", Reduce: "sum", Untyped: true, Columns: map[string]string{"n": "number"}}, value: "30"},
		{name: "missing value column", res: statResult(), opts: StatOptions{Value: "count", Reduce: "sum"}, wantErr: `"count" is missing`},
		{name: "missing order column", res: statResult(), opts: StatOptions{Value: "n", Reduce: "last", OrderBy: "at"}, wantErr: `"at" is missing`},
		{name: "a text column is not a number", res: statResult(), opts: StatOptions{Value: "ts", Reduce: "sum"}, wantErr: "must be a number"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, label, err := ReduceStat(tc.res, tc.opts, time.UTC)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if f := tc.opts.Format; f == (models.DashboardNumberFormat{}) {
				tc.opts.Format = plain
			}
			tile := FormatTile(n, label, tc.opts.Format)
			if tile.Value != tc.value || tile.RowLabel != tc.label {
				t.Errorf("tile = %+v, want value %q label %q", tile, tc.value, tc.label)
			}
		})
	}
}

func TestReduceCount(t *testing.T) {
	at := func(h int) time.Time { return time.Date(2026, 11, 15, h, 0, 0, 0, time.UTC) }
	rows := []CountRow{{At: at(9), Count: 4}, {At: at(10), Count: 0}, {At: at(11), Count: 6}}
	for _, tc := range []struct {
		reduce models.DashboardStatReduce
		rows   []CountRow
		value  string
		label  string
	}{
		{"sum", rows, "10", ""},
		{"avg", rows, "3.33", ""},
		{"min", rows, "0", ""},
		{"max", rows, "6", ""},
		{"first", rows, "4", "2026-11-15 09:00:00 UTC"},
		{"last", rows, "6", "2026-11-15 11:00:00 UTC"},
		{"sum", nil, "0", ""},
		{"last", nil, "0", ""},
	} {
		n, label := ReduceCount(tc.rows, tc.reduce, time.UTC)
		if got := FormatNumber(n, models.DashboardNumberFormat{}); got != tc.value || label != tc.label {
			t.Errorf("%s over %d rows = %q %q, want %q %q", tc.reduce, len(tc.rows), got, label, tc.value, tc.label)
		}
	}
}
