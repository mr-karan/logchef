package alerts

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

// BacktestQuery runs the rule's query over the lookback that ends at at.
type BacktestQuery func(ctx context.Context, at time.Time) (*models.QueryResult, error)

// BacktestRule is the rule a backtest replays.
type BacktestRule struct {
	Rule
	GroupBy          []string
	Language         models.QueryLanguage
	FrequencySeconds int
	LookbackSeconds  int
	NotifyOnResolve  bool
}

// BacktestStep is the time between two replayed evaluations: the rule's
// frequency, or longer when the range would take more than
// models.MaxBacktestEvaluations of them.
func BacktestStep(frequencySeconds, rangeSeconds int) int {
	perEvaluation := (rangeSeconds + models.MaxBacktestEvaluations - 1) / models.MaxBacktestEvaluations
	return max(frequencySeconds, perEvaluation)
}

// Backtest replays rule over the rangeSeconds that end at end, moved back to a
// multiple of the step since the Unix epoch so the same request always reads
// the same instants. It runs query
// once per step, oldest first, each at its own instant, and feeds the samples
// through Transition with an in-memory set of instances, so grouping, the
// no-data policy and the forget rule behave as they do live. It writes and
// sends nothing, and silences are not applied.
//
// A failed evaluation leaves every state as it was, as it does live, and is
// counted in Summary.Errors; result.FirstErr keeps the first failure. When
// ctx reaches its deadline the replay stops and the result is partial. Any
// other end of ctx is returned as an error.
func Backtest(ctx context.Context, rule BacktestRule, end time.Time, rangeSeconds int, query BacktestQuery) (*models.BacktestResult, error) {
	rule.Grouped = len(rule.GroupBy) > 0
	step := BacktestStep(rule.FrequencySeconds, rangeSeconds)
	end = time.Unix(end.Unix()-end.Unix()%int64(step), 0).UTC()
	start := end.Add(-time.Duration(rangeSeconds) * time.Second)
	r := replay{
		rule:      rule,
		step:      step,
		end:       end,
		instances: make(map[string]Instance),
		groups:    make(map[string]*groupTally),
		result: &models.BacktestResult{
			Start:       start,
			End:         end,
			StepSeconds: step,
			Coverage:    min(1, float64(rule.LookbackSeconds)/float64(step)),
			Series:      make([]models.BacktestPoint, 0, rangeSeconds/step),
			Groups:      []models.BacktestGroup{},
			Warnings:    coverageWarnings(rule.FrequencySeconds, rule.LookbackSeconds, step),
		},
	}
	for i := 1; i <= rangeSeconds/step; i++ {
		at := start.Add(time.Duration(i*step) * time.Second)
		if stop, err := r.stopped(ctx); stop {
			if err != nil {
				return nil, err
			}
			break
		}
		result, err := query(ctx, at)
		if err != nil {
			if stop, ctxErr := r.stopped(ctx); stop {
				if ctxErr != nil {
					return nil, ctxErr
				}
				break
			}
			r.fail(at, err)
			continue
		}
		r.evaluate(at, result)
	}
	r.finish()
	return r.result, nil
}

// coverageWarnings says which part of the range no lookback window reads.
// When the rule's own frequency is longer than its lookback, live evaluation
// has the same gap. Otherwise the gap comes from the evaluation budget, and a
// shorter window replays in full.
func coverageWarnings(frequency, lookback, step int) []string {
	switch {
	case step <= lookback:
		return []string{}
	case frequency > lookback:
		return []string{fmt.Sprintf("The rule evaluates every %s and reads the last %s, so %.0f%% of the time is never read, live or in this replay: a burst that falls between two windows is missed. Make the lookback at least the frequency to read every minute.",
			compactDuration(frequency), compactDuration(lookback), 100*(1-float64(lookback)/float64(frequency)))}
	default:
		return []string{fmt.Sprintf("To stay within %d evaluations the replay evaluates every %s with the rule's %s lookback, so it reads %.0f%% of the range and can miss an episode shorter than %s. Replay at most %s at a time with end_time to read all of it.",
			models.MaxBacktestEvaluations, compactDuration(step), compactDuration(lookback), 100*float64(lookback)/float64(step), compactDuration(step), compactDuration(models.MaxBacktestEvaluations*lookback))}
	}
}

// compactDuration writes seconds as 45s, 12m, 1h24m or 2d3h.
func compactDuration(seconds int) string {
	d, rest := seconds/86400, seconds%86400
	h, rest := rest/3600, rest%3600
	m, s := rest/60, rest%60
	var b strings.Builder
	for _, part := range []struct {
		n    int
		unit string
	}{{d, "d"}, {h, "h"}, {m, "m"}, {s, "s"}} {
		if part.n > 0 {
			fmt.Fprintf(&b, "%d%s", part.n, part.unit)
		}
	}
	if b.Len() == 0 {
		return "0s"
	}
	return b.String()
}

// replay is the in-memory state of one backtest.
type replay struct {
	rule      BacktestRule
	step      int
	end       time.Time
	instances map[string]Instance
	groups    map[string]*groupTally
	result    *models.BacktestResult
}

type groupTally struct {
	group     map[string]string
	key       string
	episodes  int
	breaching int
	maxValue  *float64
	minValue  *float64
}

// stopped reports that ctx ended. A deadline makes the result partial;
// anything else, such as a cancelled request, is returned.
func (r *replay) stopped(ctx context.Context) (bool, error) {
	err := ctx.Err()
	switch {
	case err == nil:
		return false, nil
	case errors.Is(err, context.DeadlineExceeded):
		r.result.Partial = true
		return true, nil
	default:
		return true, err
	}
}

// evaluate reads one result and moves every group to its next state. A
// result the rule cannot read, such as a repeated group, fails the
// evaluation with a datasource.ValidationError, whose message is the user's
// to read.
func (r *replay) evaluate(at time.Time, result *models.QueryResult) {
	samples, err := SamplesFromResult(result, r.rule.GroupBy, r.rule.Language)
	if err == nil {
		var changes []Change
		if changes, err = Transition(r.rule.Rule, r.tracked(), samples, at); err == nil {
			r.apply(at, samples, changes)
			return
		}
	}
	r.fail(at, &datasource.ValidationError{Field: "result", Message: err.Error(), Err: err})
}

// apply counts one evaluation's changes and stores the new states.
func (r *replay) apply(at time.Time, samples []Sample, changes []Change) {
	point := models.BacktestPoint{T: at, Status: models.BacktestStatusOK}
	if !r.rule.Grouped && len(samples) == 1 {
		value := samples[0].Value
		point.Value = &value
	}
	noData := false
	for _, change := range changes {
		r.count(change)
		if change.To == models.AlertInstanceStateFiring {
			point.FiringGroups++
		}
		noData = noData || change.To == models.AlertInstanceStateNoData
		r.track(change, at)
	}
	switch {
	case point.FiringGroups > 0:
		point.Status = models.BacktestStatusFiring
	case noData:
		point.Status = models.BacktestStatusNoData
	}
	if noData {
		r.result.Summary.NoDataEvaluations++
	}
	r.record(point)
}

// count adds one change to the summary and to its group: a breach, a new
// episode, and each notification the evaluator would send for it.
func (r *replay) count(change Change) {
	summary := &r.result.Summary
	tally := r.tally(change)
	if change.Sampled && tally != nil {
		if tally.maxValue == nil || change.Value > *tally.maxValue {
			value := change.Value
			tally.maxValue = &value
		}
		if tally.minValue == nil || change.Value < *tally.minValue {
			value := change.Value
			tally.minValue = &value
		}
	}
	// The zero policy compares 0 for a tracked group without a row.
	if change.To == models.AlertInstanceStateFiring && (change.Sampled || r.rule.NoData == models.AlertNoDataZero) {
		summary.BreachingEvaluations++
		if tally != nil {
			tally.breaching++
		}
	}
	switch {
	case change.From == models.AlertInstanceStateOK && change.To != models.AlertInstanceStateOK:
		summary.Episodes++
		summary.Notifications++
		switch {
		case tally != nil:
			tally.episodes++
		case r.rule.Grouped:
			summary.EmptyResultEpisodes++
		}
	case change.From != change.To && change.To != models.AlertInstanceStateOK:
		// An open episode that moves between firing and no_data notifies its new state.
		summary.Notifications++
	case change.Closes() && r.rule.NotifyOnResolve:
		summary.Notifications++
	}
}

// fail records an evaluation that read nothing. Every group keeps its state.
func (r *replay) fail(at time.Time, err error) {
	r.result.Summary.Errors++
	if r.result.FirstErr == nil {
		r.result.FirstErr = err
	}
	point := models.BacktestPoint{T: at, Status: models.BacktestStatusError}
	for _, instance := range r.instances {
		if instance.State == models.AlertInstanceStateFiring {
			point.FiringGroups++
		}
	}
	r.record(point)
}

// record appends a point. The rule fires from this evaluation to the next
// one, or to the end of the range, whichever comes first.
func (r *replay) record(point models.BacktestPoint) {
	r.result.Evaluations++
	if point.FiringGroups > 0 {
		r.result.Summary.FiringSeconds += min(r.step, int(r.end.Sub(point.T)/time.Second))
	}
	r.result.Series = append(r.result.Series, point)
}

// tracked lists the instances in group key order, so a replay is repeatable.
func (r *replay) tracked() []Instance {
	instances := make([]Instance, 0, len(r.instances))
	for _, instance := range r.instances {
		instances = append(instances, instance)
	}
	slices.SortFunc(instances, func(a, b Instance) int { return cmp.Compare(a.GroupKey, b.GroupKey) })
	return instances
}

// track applies one change the way an evaluation stores it: an untracked
// group is deleted, any other keeps its new state, and LastSeen moves to at
// only when the result had a row for the group.
func (r *replay) track(change Change, at time.Time) {
	if change.Untracked {
		delete(r.instances, change.GroupKey)
		return
	}
	instance, ok := r.instances[change.GroupKey]
	if !ok {
		instance = Instance{GroupKey: change.GroupKey, Group: change.Group, LastSeen: at}
	}
	instance.State = change.To
	if change.Sampled {
		instance.LastSeen = at
	}
	r.instances[change.GroupKey] = instance
}

// tally returns the counters of a grouped rule's group, nil for an ungrouped
// rule and for the no-rows placeholder of a grouped one.
func (r *replay) tally(change Change) *groupTally {
	if !r.rule.Grouped || change.GroupKey == "" {
		return nil
	}
	tally, ok := r.groups[change.GroupKey]
	if !ok {
		tally = &groupTally{group: change.Group, key: change.GroupKey}
		r.groups[change.GroupKey] = tally
	}
	return tally
}

// finish lists the groups with the most breaching evaluations.
func (r *replay) finish() {
	tallies := make([]*groupTally, 0, len(r.groups))
	for _, tally := range r.groups {
		tallies = append(tallies, tally)
	}
	slices.SortFunc(tallies, func(a, b *groupTally) int {
		return cmp.Or(
			cmp.Compare(b.breaching, a.breaching),
			cmp.Compare(b.episodes, a.episodes),
			cmp.Compare(a.key, b.key),
		)
	})
	shown := tallies[:min(len(tallies), models.MaxBacktestGroups)]
	for _, tally := range shown {
		r.result.Groups = append(r.result.Groups, models.BacktestGroup{
			Group:                tally.group,
			Episodes:             tally.episodes,
			BreachingEvaluations: tally.breaching,
			MaxValue:             tally.maxValue,
			MinValue:             tally.minValue,
		})
	}
	r.result.GroupsOmitted = len(tallies) - len(shown)
}
