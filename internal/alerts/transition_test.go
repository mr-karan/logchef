package alerts

import (
	"reflect"
	"testing"

	"github.com/mr-karan/logchef/pkg/models"
)

// TestTransitionZeroPolicy covers each row of the RFC 0006 policy table for
// the zero policy.
func TestTransitionZeroPolicy(t *testing.T) {
	const (
		ok     = models.AlertInstanceStateOK
		firing = models.AlertInstanceStateFiring
	)
	above := Rule{Operator: models.AlertThresholdGreaterThan, Threshold: 10}
	below := Rule{Operator: models.AlertThresholdLessThan, Threshold: 5}
	firingGroup := func(key string) Instance { return Instance{GroupKey: key, State: firing} }

	for _, tc := range []struct {
		name      string
		rule      Rule
		instances []Instance
		samples   []Sample
		want      []Change
	}{
		{
			name:    "sample meets the threshold, untracked group fires",
			rule:    above,
			samples: []Sample{{Value: 12}},
			want:    []Change{{From: ok, To: firing, Value: 12, Sampled: true}},
		},
		{
			name:      "sample meets the threshold, firing group keeps firing",
			rule:      above,
			instances: []Instance{firingGroup("")},
			samples:   []Sample{{Value: 12}},
			want:      []Change{{From: firing, To: firing, Value: 12, Sampled: true}},
		},
		{
			name:      "sample misses the threshold, firing group resolves",
			rule:      above,
			instances: []Instance{firingGroup("")},
			samples:   []Sample{{Value: 3}},
			want:      []Change{{From: firing, To: ok, Value: 3, Sampled: true}},
		},
		{
			name:    "sample misses the threshold, untracked group stays ok",
			rule:    above,
			samples: []Sample{{Value: 3}},
			want:    []Change{{From: ok, To: ok, Value: 3, Sampled: true}},
		},
		{
			name:      "no sample, tracked group compares 0 and resolves",
			rule:      above,
			instances: []Instance{firingGroup("")},
			want:      []Change{{From: firing, To: ok}},
		},
		{
			name:      "no sample, tracked group compares 0 and keeps firing",
			rule:      below,
			instances: []Instance{firingGroup("")},
			want:      []Change{{From: firing, To: firing}},
		},
		{
			name: "no sample at all, ungrouped rule compares 0 and stays ok",
			rule: above,
			want: []Change{{From: ok, To: ok}},
		},
		{
			name: "no sample at all, ungrouped rule compares 0 and fires",
			rule: below,
			want: []Change{{From: ok, To: firing}},
		},
		{
			name:      "each group is decided on its own",
			rule:      above,
			instances: []Instance{firingGroup(`["a"]`), {GroupKey: `["c"]`, Group: map[string]string{"service": "c"}, State: firing}},
			samples:   []Sample{{GroupKey: `["a"]`, Value: 3}, {GroupKey: `["b"]`, Value: 11}},
			want: []Change{
				{GroupKey: `["a"]`, From: firing, To: ok, Value: 3, Sampled: true},
				{GroupKey: `["b"]`, From: ok, To: firing, Value: 11, Sampled: true},
				{GroupKey: `["c"]`, Group: map[string]string{"service": "c"}, From: firing, To: ok},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Transition(tc.rule, tc.instances, tc.samples)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Transition = %+v, want %+v", got, tc.want)
			}
		})
	}
}
