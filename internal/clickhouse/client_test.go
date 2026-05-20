package clickhouse

import (
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

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
