package alerts

import (
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

const (
	// MaxTrackedGroups bounds the instances one rule keeps.
	MaxTrackedGroups = 200
	// ForgetAfter is how long a group may return no row before it is forgotten.
	ForgetAfter = 24 * time.Hour
)

// ErrTooManyGroups is the evaluation error of a rule that would track more
// than MaxTrackedGroups groups.
var ErrTooManyGroups = fmt.Errorf("the rule would track more than %d groups. Narrow the query or the group_by columns", MaxTrackedGroups)

// Rule is the part of an alert that decides state.
type Rule struct {
	Operator  models.AlertThresholdOperator
	Threshold float64
	NoData    models.AlertNoDataPolicy
	// Grouped is set for a rule with group_by columns.
	Grouped bool
}

// Sample is the value that one evaluation read for one group.
type Sample struct {
	GroupKey string
	Group    map[string]string
	Value    float64
}

// Instance is the stored state of one group.
type Instance struct {
	GroupKey string
	Group    map[string]string
	State    models.AlertInstanceState
	// LastSeen is the last evaluation that returned a row for the group, or
	// the start of its state when it never had one.
	LastSeen time.Time
}

// Change is the outcome of one evaluation for one group. From and To are equal
// when the group keeps its state.
type Change struct {
	GroupKey string
	Group    map[string]string
	From     models.AlertInstanceState
	To       models.AlertInstanceState
	Value    float64
	// Sampled reports that the result had a row for the group.
	Sampled bool
	// Untracked reports that the group's instance is deleted: a group
	// forgotten after ForgetAfter without a row, an ok group under the zero
	// policy, or the no-data placeholder of a grouped rule whose query
	// returns rows again.
	Untracked bool
	// Forgotten reports that the group returned no row for ForgetAfter.
	Forgotten bool
}

// Closes reports that the change ends an episode.
func (c Change) Closes() bool {
	return c.From != models.AlertInstanceStateOK && c.To == models.AlertInstanceStateOK
}

// Transition computes the next state of every group at now.
//
// A sample is compared with the threshold. A tracked group without a sample,
// and an ungrouped rule with no sample at all, follow the rule's no-data
// policy: zero compares 0, no_data moves to no_data, keep changes nothing.
// Under zero only firing and no-data groups stay tracked; the other policies
// also track ok groups, so that a group that stops reporting is noticed. A
// grouped rule with no rows and no tracked group uses the group key "" for
// its no_data state.
//
// Under no_data and keep, a group that returned no row for ForgetAfter is
// forgotten: it moves to ok and is untracked. The "" group is never
// forgotten, because the next evaluation would track it again.
//
// It returns ErrTooManyGroups when more than MaxTrackedGroups groups would
// stay tracked.
func Transition(rule Rule, instances []Instance, samples []Sample, now time.Time) ([]Change, error) {
	tracked := make(map[string]Instance, len(instances))
	for _, instance := range instances {
		tracked[instance.GroupKey] = instance
	}
	state := func(groupKey string) models.AlertInstanceState {
		if instance, ok := tracked[groupKey]; ok {
			return instance.State
		}
		return models.AlertInstanceStateOK
	}
	next := func(value float64) models.AlertInstanceState {
		if util.CompareAlertThreshold(value, rule.Threshold, rule.Operator) {
			return models.AlertInstanceStateFiring
		}
		return models.AlertInstanceStateOK
	}

	changes := make([]Change, 0, len(samples)+len(instances))
	sampled := make(map[string]struct{}, len(samples))
	for _, sample := range samples {
		sampled[sample.GroupKey] = struct{}{}
		changes = append(changes, Change{
			GroupKey: sample.GroupKey,
			Group:    sample.Group,
			From:     state(sample.GroupKey),
			To:       next(sample.Value),
			Value:    sample.Value,
			Sampled:  true,
		})
	}
	for _, instance := range instances {
		if _, ok := sampled[instance.GroupKey]; ok {
			continue
		}
		change := Change{GroupKey: instance.GroupKey, Group: instance.Group, From: instance.State}
		switch {
		case rule.Grouped && instance.GroupKey == "" && len(samples) > 0:
			// The placeholder for "no rows at all" ends once rows return.
			change.To = models.AlertInstanceStateOK
			change.Untracked = true
		case rule.NoData != models.AlertNoDataZero && instance.GroupKey != "" && now.Sub(instance.LastSeen) >= ForgetAfter:
			change.To = models.AlertInstanceStateOK
			change.Untracked = true
			change.Forgotten = true
		default:
			change.To = noSample(rule, instance.State, next)
		}
		changes = append(changes, change)
	}
	if len(changes) == 0 {
		to := noSample(rule, models.AlertInstanceStateOK, next)
		changes = append(changes, Change{
			From:      models.AlertInstanceStateOK,
			To:        to,
			Untracked: to == models.AlertInstanceStateOK,
		})
	}

	kept := 0
	for i := range changes {
		change := &changes[i]
		if change.To == models.AlertInstanceStateOK && rule.NoData == models.AlertNoDataZero {
			change.Untracked = true
		}
		if !change.Untracked {
			kept++
		}
	}
	if kept > MaxTrackedGroups {
		return nil, ErrTooManyGroups
	}
	return changes, nil
}

// noSample is the next state of a group in state from that had no sample.
func noSample(rule Rule, from models.AlertInstanceState, next func(float64) models.AlertInstanceState) models.AlertInstanceState {
	switch rule.NoData {
	case models.AlertNoDataNoData:
		return models.AlertInstanceStateNoData
	case models.AlertNoDataKeep:
		return from
	default:
		return next(0)
	}
}
