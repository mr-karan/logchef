package datasource

import (
	"testing"

	"github.com/mr-karan/logchef/internal/clickhouse"
)

func TestHasLeadingTimestampSortKey(t *testing.T) {
	tests := []struct {
		name  string
		keys  []string
		field string
		want  bool
	}{
		{name: "leading field matches", keys: []string{"event_time", "service"}, field: "event_time", want: true},
		{name: "quoted leading field matches", keys: []string{"`event_time`"}, field: "event_time", want: true},
		{name: "non-leading field is rejected", keys: []string{"service", "event_time"}, field: "event_time", want: false},
		{name: "missing metadata is rejected", field: "event_time", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasLeadingTimestampSortKey(&clickhouse.TableInfo{SortKeys: tt.keys}, tt.field); got != tt.want {
				t.Fatalf("hasLeadingTimestampSortKey() = %t, want %t", got, tt.want)
			}
		})
	}
}
