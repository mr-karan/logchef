package clickhouse

import (
	"reflect"
	"testing"
)

func TestParseSortKeysPreservesExpressions(t *testing.T) {
	for _, test := range []struct {
		input string
		want  []string
	}{
		{"tuple(`@timestamp`, cityHash64(user, host))", []string{"@timestamp", "cityHash64(user, host)"}},
		{"toDate(ts), ts", []string{"toDate(ts)", "ts"}},
		{"tuple()", nil},
	} {
		if got := ParseSortKeys(test.input); !reflect.DeepEqual(got, test.want) {
			t.Errorf("ParseSortKeys(%q) = %v, want %v", test.input, got, test.want)
		}
	}
}
