package clickhouse

import (
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

func TestTruncationState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		opts   QueryOptions
		rows   int
		reason string
		want   models.Truncation
	}{
		{name: "a probe row proves the cut", opts: QueryOptions{MaxRows: 100, Probed: true, LimitSource: models.LimitSourceRequest}, rows: 100, reason: "row_limit", want: models.TruncationRows},
		{name: "the byte budget is a proven cut", opts: QueryOptions{MaxRows: 100, Probed: true, LimitSource: models.LimitSourceRequest}, rows: 7, reason: "byte_limit", want: models.TruncationBytes},
		{name: "a probed cap filled without the extra row is complete", opts: QueryOptions{MaxRows: 100, Probed: true, LimitSource: models.LimitSourceRequest}, rows: 100, want: models.TruncationNone},
		{name: "fewer rows than the cap is complete", opts: QueryOptions{MaxRows: 100, LimitSource: models.LimitSourceRequest}, rows: 99, want: models.TruncationNone},
		{name: "an unprobed cap filled may be incomplete", opts: QueryOptions{MaxRows: 100, LimitSource: models.LimitSourceRequest}, rows: 100, want: models.TruncationUnknown},
		{name: "the statement's own limit filled is what the author asked", opts: QueryOptions{MaxRows: 20, LimitSource: models.LimitSourceStatement}, rows: 20, want: models.TruncationNone},
		{name: "no cap is never a cut", opts: QueryOptions{}, rows: 5000, want: models.TruncationNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.opts.truncation(tc.rows, tc.reason); got != tc.want {
				t.Fatalf("truncation(%d, %q) = %q, want %q", tc.rows, tc.reason, got, tc.want)
			}
		})
	}
}
