package alerts

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

const (
	stOK     = models.AlertInstanceStateOK
	stFiring = models.AlertInstanceStateFiring
	stNoData = models.AlertInstanceStateNoData
)

// TestTransitionPolicyTable covers every cell of the RFC 0006 policy table,
// for each of the three no-data policies.
func TestTransitionPolicyTable(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Minute)
	above := func(policy models.AlertNoDataPolicy) Rule {
		return Rule{Operator: models.AlertThresholdGreaterThan, Threshold: 10, NoData: policy}
	}
	below := func(policy models.AlertNoDataPolicy) Rule {
		return Rule{Operator: models.AlertThresholdLessThan, Threshold: 5, NoData: policy}
	}
	tracked := func(key string, state models.AlertInstanceState) Instance {
		return Instance{GroupKey: key, State: state, LastSeen: recent}
	}

	type tc struct {
		name      string
		rule      Rule
		instances []Instance
		samples   []Sample
		want      []Change
	}
	var cases []tc
	for _, policy := range []models.AlertNoDataPolicy{models.AlertNoDataZero, models.AlertNoDataNoData, models.AlertNoDataKeep} {
		zero := policy == models.AlertNoDataZero
		cases = append(cases,
			tc{
				name:    "sample meets the threshold, untracked group fires",
				rule:    above(policy),
				samples: []Sample{{Value: 12}},
				want:    []Change{{From: stOK, To: stFiring, Value: 12, Sampled: true}},
			},
			tc{
				name:      "sample meets the threshold, no-data group fires",
				rule:      above(policy),
				instances: []Instance{tracked("", stNoData)},
				samples:   []Sample{{Value: 12}},
				want:      []Change{{From: stNoData, To: stFiring, Value: 12, Sampled: true}},
			},
			tc{
				name:      "sample misses the threshold, firing group resolves",
				rule:      above(policy),
				instances: []Instance{tracked("", stFiring)},
				samples:   []Sample{{Value: 3}},
				// Only the zero policy stops tracking an ok group.
				want: []Change{{From: stFiring, To: stOK, Value: 3, Sampled: true, Untracked: zero}},
			},
			tc{
				name:    "sample misses the threshold, untracked group stays ok",
				rule:    above(policy),
				samples: []Sample{{Value: 3}},
				want:    []Change{{From: stOK, To: stOK, Value: 3, Sampled: true, Untracked: zero}},
			},
		)
	}

	cases = append(cases,
		// No sample, group is tracked.
		tc{
			name:      "zero: tracked firing group compares 0 and resolves",
			rule:      above(models.AlertNoDataZero),
			instances: []Instance{tracked("", stFiring)},
			want:      []Change{{From: stFiring, To: stOK, Untracked: true}},
		},
		tc{
			name:      "zero: tracked firing group compares 0 and keeps firing",
			rule:      below(models.AlertNoDataZero),
			instances: []Instance{tracked("", stFiring)},
			want:      []Change{{From: stFiring, To: stFiring}},
		},
		tc{
			name:      "no_data: tracked firing group has no data",
			rule:      above(models.AlertNoDataNoData),
			instances: []Instance{tracked(`["a"]`, stFiring)},
			want:      []Change{{GroupKey: `["a"]`, From: stFiring, To: stNoData}},
		},
		tc{
			name:      "no_data: tracked ok group has no data",
			rule:      above(models.AlertNoDataNoData),
			instances: []Instance{tracked(`["a"]`, stOK)},
			want:      []Change{{GroupKey: `["a"]`, From: stOK, To: stNoData}},
		},
		tc{
			name:      "keep: tracked firing group keeps firing",
			rule:      above(models.AlertNoDataKeep),
			instances: []Instance{tracked(`["a"]`, stFiring)},
			want:      []Change{{GroupKey: `["a"]`, From: stFiring, To: stFiring}},
		},
		tc{
			name:      "keep: tracked ok group stays ok and tracked",
			rule:      below(models.AlertNoDataKeep),
			instances: []Instance{tracked(`["a"]`, stOK)},
			want:      []Change{{GroupKey: `["a"]`, From: stOK, To: stOK}},
		},
		// No sample at all, ungrouped rule.
		tc{
			name: "zero: no sample at all compares 0 and stays ok",
			rule: above(models.AlertNoDataZero),
			want: []Change{{From: stOK, To: stOK, Untracked: true}},
		},
		tc{
			name: "zero: no sample at all compares 0 and fires",
			rule: below(models.AlertNoDataZero),
			want: []Change{{From: stOK, To: stFiring}},
		},
		tc{
			name: "no_data: no sample at all has no data",
			rule: below(models.AlertNoDataNoData),
			want: []Change{{From: stOK, To: stNoData}},
		},
		tc{
			name: "keep: no sample at all changes nothing",
			rule: below(models.AlertNoDataKeep),
			want: []Change{{From: stOK, To: stOK, Untracked: true}},
		},
		tc{
			name: "no_data: grouped rule with no rows and no tracked group uses the empty key",
			rule: Rule{Operator: models.AlertThresholdGreaterThan, Threshold: 10, NoData: models.AlertNoDataNoData, Grouped: true},
			want: []Change{{From: stOK, To: stNoData}},
		},
		tc{
			name:      "no_data: grouped placeholder ends when rows return",
			rule:      Rule{Operator: models.AlertThresholdGreaterThan, Threshold: 10, NoData: models.AlertNoDataNoData, Grouped: true},
			instances: []Instance{tracked("", stNoData)},
			samples:   []Sample{{GroupKey: `["a"]`, Value: 3}},
			want: []Change{
				{GroupKey: `["a"]`, From: stOK, To: stOK, Value: 3, Sampled: true},
				{From: stNoData, To: stOK, Untracked: true},
			},
		},
		tc{
			name:      "no_data: grouped rule tracks each silent group, not the empty key",
			rule:      Rule{Operator: models.AlertThresholdGreaterThan, Threshold: 10, NoData: models.AlertNoDataNoData, Grouped: true},
			instances: []Instance{tracked(`["a"]`, stOK), tracked(`["b"]`, stFiring)},
			want: []Change{
				{GroupKey: `["a"]`, From: stOK, To: stNoData},
				{GroupKey: `["b"]`, From: stFiring, To: stNoData},
			},
		},
	)

	for _, c := range cases {
		t.Run(string(c.rule.NoData)+"/"+c.name, func(t *testing.T) {
			got, err := Transition(c.rule, c.instances, c.samples, now)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Transition = %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestTransitionForget: under no_data and keep, a group without a row for 24
// hours is forgotten in each of its three states. Under zero, and for the
// empty key, nothing is forgotten.
func TestTransitionForget(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-ForgetAfter)
	fresh := now.Add(-ForgetAfter + time.Second)
	rule := func(policy models.AlertNoDataPolicy) Rule {
		return Rule{Operator: models.AlertThresholdGreaterThan, Threshold: 10, NoData: policy, Grouped: true}
	}
	forgotten := func(from models.AlertInstanceState) []Change {
		return []Change{{GroupKey: `["a"]`, From: from, To: stOK, Untracked: true, Forgotten: true}}
	}

	for _, c := range []struct {
		name     string
		rule     Rule
		instance Instance
		want     []Change
	}{
		{"no_data: ok group", rule(models.AlertNoDataNoData), Instance{GroupKey: `["a"]`, State: stOK, LastSeen: stale}, forgotten(stOK)},
		{"no_data: no-data group", rule(models.AlertNoDataNoData), Instance{GroupKey: `["a"]`, State: stNoData, LastSeen: stale}, forgotten(stNoData)},
		{"keep: firing group", rule(models.AlertNoDataKeep), Instance{GroupKey: `["a"]`, State: stFiring, LastSeen: stale}, forgotten(stFiring)},
		{"keep: ok group", rule(models.AlertNoDataKeep), Instance{GroupKey: `["a"]`, State: stOK, LastSeen: stale}, forgotten(stOK)},
		{
			name:     "no_data: a group seen inside 24 hours is kept",
			rule:     rule(models.AlertNoDataNoData),
			instance: Instance{GroupKey: `["a"]`, State: stNoData, LastSeen: fresh},
			want:     []Change{{GroupKey: `["a"]`, From: stNoData, To: stNoData}},
		},
		{
			name:     "zero: a silent firing group is never forgotten",
			rule:     Rule{Operator: models.AlertThresholdLessThan, Threshold: 5, NoData: models.AlertNoDataZero, Grouped: true},
			instance: Instance{GroupKey: `["a"]`, State: stFiring, LastSeen: stale},
			want:     []Change{{GroupKey: `["a"]`, From: stFiring, To: stFiring}},
		},
		{
			name:     "no_data: the empty key is never forgotten",
			rule:     rule(models.AlertNoDataNoData),
			instance: Instance{State: stNoData, LastSeen: stale},
			want:     []Change{{From: stNoData, To: stNoData}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Transition(c.rule, []Instance{c.instance}, nil, now)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Transition = %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestTransitionTrackedGroupCap: a rule tracks at most 200 groups. Groups that
// leave the tracked set do not count.
func TestTransitionTrackedGroupCap(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	rule := Rule{Operator: models.AlertThresholdGreaterThan, Threshold: 10, NoData: models.AlertNoDataNoData, Grouped: true}
	instances := func(n int, lastSeen time.Time) []Instance {
		out := make([]Instance, n)
		for i := range out {
			out[i] = Instance{GroupKey: fmt.Sprintf(`["old-%d"]`, i), State: stOK, LastSeen: lastSeen}
		}
		return out
	}
	newSample := []Sample{{GroupKey: `["new"]`, Value: 1}}

	if _, err := Transition(rule, instances(MaxTrackedGroups-1, now), newSample, now); err != nil {
		t.Fatalf("200 tracked groups err = %v, want nil", err)
	}
	if _, err := Transition(rule, instances(MaxTrackedGroups, now), newSample, now); !errors.Is(err, ErrTooManyGroups) {
		t.Fatalf("201 tracked groups err = %v, want ErrTooManyGroups", err)
	}
	if _, err := Transition(rule, instances(MaxTrackedGroups, now.Add(-ForgetAfter)), newSample, now); err != nil {
		t.Fatalf("200 forgotten groups and one new group err = %v, want nil", err)
	}
	zero := rule
	zero.NoData = models.AlertNoDataZero
	if _, err := Transition(zero, instances(MaxTrackedGroups, now), newSample, now); err != nil {
		t.Fatalf("zero policy with 200 ok groups err = %v, want nil: ok groups are not tracked", err)
	}
}
