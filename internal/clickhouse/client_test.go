package clickhouse

import (
	"encoding/json"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

// TestJSONStringSizeNeverUnderCounts guards the response byte-budget: the fast
// arithmetic estimate must never report FEWER bytes than the real JSON encoding,
// otherwise a response could exceed MaxResponseBytes and blow up memory.
func TestJSONStringSizeNeverUnderCounts(t *testing.T) {
	cases := []string{
		"",
		"plain ascii log line",
		`has "quotes" and \backslashes\`,
		"newlines\nand\ttabs\r\n",
		"control\x00\x01\x02\x1f bytes",
		"html <script>&amp;</script> chars",
		"unicode: café — 日本語 — 🚀",
		`{"nested":"json","as":"a string"}`,
	}
	for _, s := range cases {
		enc, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("json.Marshal(%q): %v", s, err)
		}
		if got := jsonStringSize(s); got < len(enc) {
			t.Errorf("jsonStringSize(%q) = %d, under-counts real JSON size %d", s, got, len(enc))
		}
	}
}

func TestWithColumnDescriptions(t *testing.T) {
	columns := []models.ColumnInfo{
		{Name: "ts", Type: "DateTime64(3)"},
		{Name: "msg", Type: "String"},
	}
	extColumns := []ExtendedColumnInfo{
		{Name: "ts", Comment: "event time"},
		{Name: "msg", Comment: "message body"},
	}

	got := withColumnDescriptions(columns, extColumns)

	if got[0].Description != "event time" {
		t.Fatalf("expected ts description, got %q", got[0].Description)
	}
	if got[1].Description != "message body" {
		t.Fatalf("expected msg description, got %q", got[1].Description)
	}
}
