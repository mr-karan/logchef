package alerts

import (
	"github.com/mr-karan/logchef/internal/util"
	"github.com/mr-karan/logchef/pkg/models"
)

// Rule is the part of an alert that decides state.
type Rule struct {
	Operator  models.AlertThresholdOperator
	Threshold float64
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
}

// Closes reports that the change ends an episode.
func (c Change) Closes() bool {
	return c.From != models.AlertInstanceStateOK && c.To == models.AlertInstanceStateOK
}

// Transition computes the next state of every group under the zero policy: a
// sample is compared with the threshold, and a tracked group without a sample,
// or an ungrouped rule with no sample at all, compares 0. Only firing groups
// are tracked, so instances holds firing groups and an absent group is ok.
func Transition(rule Rule, instances []Instance, samples []Sample) []Change {
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
		changes = append(changes, Change{
			GroupKey: instance.GroupKey,
			Group:    instance.Group,
			From:     instance.State,
			To:       next(0),
		})
	}
	if len(changes) == 0 {
		changes = append(changes, Change{
			From: models.AlertInstanceStateOK,
			To:   next(0),
		})
	}
	return changes
}
