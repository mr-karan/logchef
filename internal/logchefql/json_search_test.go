package logchefql

import "testing"

func TestWholeColumnJSONSearch(t *testing.T) {
	for _, columnType := range []string{"JSON", "JSON(max_dynamic_paths=256)", "String", "LowCardinality(String)"} {
		for _, operator := range []string{"~", "!~"} {
			t.Run(columnType+operator, func(t *testing.T) {
				result := Translate(`p`+operator+`"500"`, &Schema{Columns: []ColumnInfo{{Name: "p", Type: columnType}}})
				column := "`p`"
				if columnType == "JSON" || columnType == "JSON(max_dynamic_paths=256)" {
					column = "toString(" + column + ")"
				}
				comparison := " > 0"
				if operator == "!~" {
					comparison = " = 0"
				}
				want := "positionCaseInsensitive(" + column + ", '500')" + comparison
				if !result.Valid || result.SQL != want {
					t.Fatalf("got %+v, want %s", result, want)
				}
			})
		}
	}
}
