package models

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestParseSavedViewDefinitionAcceptsTheRFCExample(t *testing.T) {
	raw := `{
  "version": 1,
  "columns": [{ "name": "timestamp", "width": 190 }, { "name": "level" }, { "name": "msg", "width": 640 }],
  "sort": [{ "column": "timestamp", "desc": true }],
  "display_mode": "table",
  "filter": "level = \"error\"",
  "time_range": "1h"
}`
	got, err := ParseSavedViewDefinition([]byte(raw))
	if err != nil {
		t.Fatalf("ParseSavedViewDefinition: %v", err)
	}
	w190, w640 := 190, 640
	mode := DisplayModeTable
	filter, tr := `level = "error"`, "1h"
	want := SavedViewDefinition{
		Version:     1,
		Columns:     []SavedViewColumn{{Name: "timestamp", Width: &w190}, {Name: "level"}, {Name: "msg", Width: &w640}},
		Sort:        []SavedViewSort{{Column: "timestamp", Desc: true}},
		DisplayMode: &mode,
		Filter:      &filter,
		TimeRange:   &tr,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed = %+v, want %+v", got, want)
	}
}

func TestParseSavedViewDefinitionAcceptsOnlyAVersion(t *testing.T) {
	if _, err := ParseSavedViewDefinition([]byte(`{"version":1}`)); err != nil {
		t.Fatalf("a definition with only a version is valid: %v", err)
	}
}

// One case per row of the RFC 0007 definition rule table, each at the bound
// that passes and the first value past it.
func TestParseSavedViewDefinitionRules(t *testing.T) {
	columns := func(n int) string {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf(`{"name":"c%d"}`, i)
		}
		return `[` + strings.Join(parts, ",") + `]`
	}
	quoted := func(s string) string { return `"` + s + `"` }
	cases := []struct {
		name  string
		body  string
		valid bool
	}{
		{"version missing", `{}`, false},
		{"version 2", `{"version":2}`, false},
		{"version 0", `{"version":0}`, false},

		{"columns empty", `{"version":1,"columns":[]}`, false},
		{"columns 200", `{"version":1,"columns":` + columns(200) + `}`, true},
		{"columns 201", `{"version":1,"columns":` + columns(201) + `}`, false},

		{"column name empty", `{"version":1,"columns":[{"name":""}]}`, false},
		{"column name missing", `{"version":1,"columns":[{"width":100}]}`, false},
		{"column name 512 bytes", `{"version":1,"columns":[{"name":` + quoted(strings.Repeat("a", 512)) + `}]}`, true},
		{"column name 513 bytes", `{"version":1,"columns":[{"name":` + quoted(strings.Repeat("a", 513)) + `}]}`, false},
		{"column name 512 bytes of multibyte runes", `{"version":1,"columns":[{"name":` + quoted(strings.Repeat("é", 256)) + `}]}`, true},
		{"column name 514 bytes of multibyte runes", `{"version":1,"columns":[{"name":` + quoted(strings.Repeat("é", 257)) + `}]}`, false},
		{"duplicate column", `{"version":1,"columns":[{"name":"a"},{"name":"a"}]}`, false},
		{"column names differ in case", `{"version":1,"columns":[{"name":"a"},{"name":"A"}]}`, true},

		{"width absent", `{"version":1,"columns":[{"name":"a"}]}`, true},
		{"width 50", `{"version":1,"columns":[{"name":"a","width":50}]}`, true},
		{"width 49", `{"version":1,"columns":[{"name":"a","width":49}]}`, false},
		{"width 3000", `{"version":1,"columns":[{"name":"a","width":3000}]}`, true},
		{"width 3001", `{"version":1,"columns":[{"name":"a","width":3001}]}`, false},
		{"width fractional", `{"version":1,"columns":[{"name":"a","width":100.5}]}`, false},

		{"sort empty", `{"version":1,"sort":[]}`, false},
		{"sort 3", `{"version":1,"sort":[{"column":"a"},{"column":"b","desc":true},{"column":"c"}]}`, true},
		{"sort 4", `{"version":1,"sort":[{"column":"a"},{"column":"b"},{"column":"c"},{"column":"d"}]}`, false},
		{"sort column empty", `{"version":1,"sort":[{"column":"","desc":true}]}`, false},
		{"sort column 513 bytes", `{"version":1,"sort":[{"column":` + quoted(strings.Repeat("a", 513)) + `}]}`, false},
		{"sort desc not boolean", `{"version":1,"sort":[{"column":"a","desc":"yes"}]}`, false},

		{"display_mode table", `{"version":1,"display_mode":"table"}`, true},
		{"display_mode compact", `{"version":1,"display_mode":"compact"}`, true},
		{"display_mode json", `{"version":1,"display_mode":"json"}`, true},
		{"display_mode other", `{"version":1,"display_mode":"grid"}`, false},

		{"filter 8192 bytes", `{"version":1,"filter":` + quoted(strings.Repeat("a", 8192)) + `}`, true},
		{"filter 8193 bytes", `{"version":1,"filter":` + quoted(strings.Repeat("a", 8193)) + `}`, false},

		{"time_range 15m", `{"version":1,"time_range":"15m"}`, true},
		{"time_range 7d", `{"version":1,"time_range":"7d"}`, true},
		{"time_range 2w", `{"version":1,"time_range":"2w"}`, true},
		{"time_range unit missing", `{"version":1,"time_range":"15"}`, false},
		{"time_range unknown unit", `{"version":1,"time_range":"1y"}`, false},
		{"time_range absolute", `{"version":1,"time_range":"2026-11-15T00:00:00Z"}`, false},
		{"time_range empty", `{"version":1,"time_range":""}`, false},

		{"unknown top-level key", `{"version":1,"layout":"x"}`, false},
		{"unknown column key", `{"version":1,"columns":[{"name":"a","hidden":true}]}`, false},
		{"unknown sort key", `{"version":1,"sort":[{"column":"a","nulls":"last"}]}`, false},
		{"data after the object", `{"version":1} {}`, false},
		{"not an object", `[]`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseSavedViewDefinition([]byte(tc.body))
			if tc.valid && err != nil {
				t.Fatalf("want valid, got %v", err)
			}
			if !tc.valid {
				if err == nil {
					t.Fatal("want an error, got none")
				}
				if !errors.Is(err, ErrInvalidSavedViewDefinition) {
					t.Fatalf("error %v does not wrap ErrInvalidSavedViewDefinition", err)
				}
			}
		})
	}
}

func TestSavedViewDefinitionSizeLimit(t *testing.T) {
	// 200 columns of 400-byte names are each within their own bound but
	// together exceed the 65,536-byte definition limit.
	cols := make([]SavedViewColumn, MaxSavedViewColumns)
	for i := range cols {
		cols[i] = SavedViewColumn{Name: fmt.Sprintf("%03d%s", i, strings.Repeat("x", 397))}
	}
	def := SavedViewDefinition{Version: 1, Columns: cols}
	if err := def.Validate(); !errors.Is(err, ErrInvalidSavedViewDefinition) {
		t.Fatalf("Validate of an oversized definition = %v, want ErrInvalidSavedViewDefinition", err)
	}

	padded := `{"version":1,"filter":"a"` + strings.Repeat(" ", MaxSavedViewDefinitionBytes) + `}`
	if _, err := ParseSavedViewDefinition([]byte(padded)); !errors.Is(err, ErrInvalidSavedViewDefinition) {
		t.Fatalf("Parse of an oversized body = %v, want ErrInvalidSavedViewDefinition", err)
	}
}
