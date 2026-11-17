package alerts

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/internal/datasource"
	"github.com/mr-karan/logchef/pkg/models"
)

var backtestEnd = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

// seriesQuery answers the i-th evaluation, counted from 0, with values[i]
// as an ungrouped result, and records each instant it was asked for.
func seriesQuery(values []float64, ats *[]time.Time) BacktestQuery {
	return func(_ context.Context, at time.Time) (*models.QueryResult, error) {
		*ats = append(*ats, at)
		return ungroupedResult(map[string]any{"value": values[len(*ats)-1]}), nil
	}
}

func ungroupedRule(notifyOnResolve bool) BacktestRule {
	return BacktestRule{
		Rule:             Rule{Operator: models.AlertThresholdGreaterThan, Threshold: 5, NoData: models.AlertNoDataZero},
		FrequencySeconds: 60,
		LookbackSeconds:  300,
		NotifyOnResolve:  notifyOnResolve,
	}
}

// TestBacktestFixedSeries is the RFC's check: a fixed series of values gives
// the expected episodes, firing time, notifications and coverage.
func TestBacktestFixedSeries(t *testing.T) {
	values := make([]float64, 60)
	for _, i := range []int{10, 11, 12, 30, 31} {
		values[i] = 9
	}
	for _, tc := range []struct {
		notifyOnResolve bool
		notifications   int
	}{{true, 4}, {false, 2}} {
		var ats []time.Time
		got, err := Backtest(t.Context(), ungroupedRule(tc.notifyOnResolve), backtestEnd, 3600, seriesQuery(values, &ats))
		if err != nil {
			t.Fatal(err)
		}
		want := models.BacktestSummary{Episodes: 2, FiringSeconds: 300, BreachingEvaluations: 5, Notifications: tc.notifications}
		if got.Summary != want {
			t.Errorf("notify_on_resolve=%v: summary = %+v, want %+v", tc.notifyOnResolve, got.Summary, want)
		}
		if got.StepSeconds != 60 || got.Evaluations != 60 || got.Coverage != 1 || got.Partial {
			t.Errorf("step=%d evaluations=%d coverage=%v partial=%v, want 60, 60, 1, false", got.StepSeconds, got.Evaluations, got.Coverage, got.Partial)
		}
		if !got.Start.Equal(backtestEnd.Add(-time.Hour)) || !got.End.Equal(backtestEnd) {
			t.Errorf("window = %s to %s", got.Start, got.End)
		}
		if p := got.Series[10]; p.Status != models.BacktestStatusFiring || p.Value == nil || *p.Value != 9 || p.FiringGroups != 1 {
			t.Errorf("series[10] = %+v, want firing with value 9", p)
		}
		if p := got.Series[13]; p.Status != models.BacktestStatusOK || p.Value == nil || *p.Value != 0 {
			t.Errorf("series[13] = %+v, want ok with value 0", p)
		}
	}
}

// TestBacktestFiringTimeEndsAtRange: an evaluation at the end of the range
// adds no firing time, and one short of it adds only the time left.
func TestBacktestFiringTimeEndsAtRange(t *testing.T) {
	rule := ungroupedRule(true)
	rule.FrequencySeconds = 3600
	var ats []time.Time
	got, err := Backtest(t.Context(), rule, backtestEnd, 3600, seriesQuery([]float64{9}, &ats))
	if err != nil {
		t.Fatal(err)
	}
	if got.Evaluations != 1 || got.Summary.Episodes != 1 || got.Summary.FiringSeconds != 0 {
		t.Fatalf("evaluations=%d summary=%+v, want one episode and no firing time", got.Evaluations, got.Summary)
	}

	// 3700 s in 2000 s steps: one evaluation 1700 s before the end.
	rule.FrequencySeconds = 2000
	ats = nil
	got, err = Backtest(t.Context(), rule, backtestEnd, 3700, seriesQuery([]float64{9}, &ats))
	if err != nil {
		t.Fatal(err)
	}
	if got.Evaluations != 1 || got.Summary.FiringSeconds != 1700 {
		t.Fatalf("evaluations=%d firing_seconds=%d, want 1 and 1700", got.Evaluations, got.Summary.FiringSeconds)
	}
}

// TestBacktestInstants: each evaluation runs at its own instant, one step
// apart, oldest first, ending at end.
func TestBacktestInstants(t *testing.T) {
	var ats []time.Time
	got, err := Backtest(t.Context(), ungroupedRule(true), backtestEnd, 3600, seriesQuery(make([]float64, 60), &ats))
	if err != nil {
		t.Fatal(err)
	}
	if len(ats) != 60 || !ats[0].Equal(backtestEnd.Add(-59*time.Minute)) || !ats[59].Equal(backtestEnd) {
		t.Fatalf("instants: %d, first %s, last %s", len(ats), ats[0], ats[len(ats)-1])
	}
	for i, at := range ats {
		if !got.Series[i].T.Equal(at) {
			t.Fatalf("series[%d].t = %s, queried at %s", i, got.Series[i].T, at)
		}
	}
}

// TestBacktestStepAndCoverage: the step grows past the frequency to keep a
// range within 120 evaluations, and coverage is the share the lookback reads.
func TestBacktestStepAndCoverage(t *testing.T) {
	cases := []struct {
		frequency, lookback, rangeSeconds int
		step, evaluations                 int
		coverage                          float64
	}{
		{60, 300, 3600, 60, 60, 1},
		{60, 300, 86400, 720, 120, 300.0 / 720},
		{900, 300, 86400, 900, 96, 300.0 / 900},
		{60, 60, 604800, 5040, 120, 60.0 / 5040},
		{10, 3600, 3600, 30, 120, 1},
	}
	for _, tc := range cases {
		rule := ungroupedRule(true)
		rule.FrequencySeconds, rule.LookbackSeconds = tc.frequency, tc.lookback
		var ats []time.Time
		got, err := Backtest(t.Context(), rule, backtestEnd, tc.rangeSeconds, seriesQuery(make([]float64, 200), &ats))
		if err != nil {
			t.Fatal(err)
		}
		if got.StepSeconds != tc.step || got.Evaluations != tc.evaluations || len(ats) != tc.evaluations || got.Coverage != tc.coverage {
			t.Errorf("%+v: step=%d evaluations=%d queries=%d coverage=%v", tc, got.StepSeconds, got.Evaluations, len(ats), got.Coverage)
		}
	}
}

// TestBacktestErrorKeepsState: a failed evaluation changes no state, as it
// does live, so a firing rule keeps firing through it and resolves once.
func TestBacktestErrorKeepsState(t *testing.T) {
	calls := 0
	query := func(context.Context, time.Time) (*models.QueryResult, error) {
		calls++
		switch calls {
		case 2:
			return ungroupedResult(map[string]any{"value": 9}), nil
		case 3, 4:
			return nil, fmt.Errorf("upstream down")
		}
		return ungroupedResult(map[string]any{"value": 1}), nil
	}
	got, err := Backtest(t.Context(), ungroupedRule(true), backtestEnd, 3600, query)
	if err != nil {
		t.Fatal(err)
	}
	want := models.BacktestSummary{Episodes: 1, FiringSeconds: 180, BreachingEvaluations: 1, Notifications: 2, Errors: 2}
	if got.Summary != want {
		t.Errorf("summary = %+v, want %+v", got.Summary, want)
	}
	if p := got.Series[2]; p.Status != models.BacktestStatusError || p.FiringGroups != 1 {
		t.Errorf("series[2] = %+v, want an error that keeps one firing group", p)
	}
	if got.FirstErr == nil || got.FirstErr.Error() != "upstream down" {
		t.Errorf("first error = %v", got.FirstErr)
	}
}

// TestBacktestResultError: a result the rule cannot read is an evaluation
// error carried as a validation error, whose message the user may read.
func TestBacktestResultError(t *testing.T) {
	rule := ungroupedRule(true)
	rule.GroupBy = []string{"service"}
	query := func(context.Context, time.Time) (*models.QueryResult, error) {
		return groupedResult(row("checkout", 1), row("checkout", 2)), nil
	}
	got, err := Backtest(t.Context(), rule, backtestEnd, 3600, query)
	if err != nil {
		t.Fatal(err)
	}
	resultErr, ok := errors.AsType[*datasource.ValidationError](got.FirstErr)
	if got.Summary.Errors != 60 || !ok || !strings.Contains(resultErr.Message, "more than one row for the group") {
		t.Fatalf("errors=%d first=%v", got.Summary.Errors, got.FirstErr)
	}
}

// TestBacktestDeadline: a replay that reaches its deadline returns what it
// evaluated, marked partial; a cancelled request is an error.
func TestBacktestDeadline(t *testing.T) {
	stallAfter := func(n int) BacktestQuery {
		calls := 0
		return func(ctx context.Context, _ time.Time) (*models.QueryResult, error) {
			calls++
			if calls > n {
				<-ctx.Done()
				return nil, fmt.Errorf("query: %w", ctx.Err())
			}
			return ungroupedResult(map[string]any{"value": 9}), nil
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	got, err := Backtest(ctx, ungroupedRule(true), backtestEnd, 3600, stallAfter(2))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Partial || got.Evaluations != 2 || len(got.Series) != 2 || got.Summary.Errors != 0 || got.Summary.Episodes != 1 {
		t.Fatalf("partial=%v evaluations=%d series=%d summary=%+v", got.Partial, got.Evaluations, len(got.Series), got.Summary)
	}

	ctx, cancel = context.WithCancel(t.Context())
	time.AfterFunc(50*time.Millisecond, cancel)
	if _, err := Backtest(ctx, ungroupedRule(true), backtestEnd, 3600, stallAfter(2)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled backtest returned %v, want context.Canceled", err)
	}
}

// TestBacktestGroups lists the 20 groups with the most breaching
// evaluations and counts the rest.
func TestBacktestGroups(t *testing.T) {
	rule := ungroupedRule(true)
	rule.GroupBy = []string{"service"}
	query := func(context.Context, time.Time) (*models.QueryResult, error) {
		rows := make([]map[string]any, 25)
		for i := range rows {
			// s00 never breaches; s24 breaches every evaluation.
			rows[i] = map[string]any{"service": fmt.Sprintf("s%02d", i), "value": uint64(i)}
		}
		return groupedResult(rows...), nil
	}
	rule.Threshold = 4.5
	got, err := Backtest(t.Context(), rule, backtestEnd, 3600, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != models.MaxBacktestGroups || got.GroupsOmitted != 5 {
		t.Fatalf("groups=%d omitted=%d", len(got.Groups), got.GroupsOmitted)
	}
	first := got.Groups[0]
	if first.Group["service"] != "s05" || first.BreachingEvaluations != 60 || first.Episodes != 1 || first.MaxValue == nil || *first.MaxValue != 5 {
		t.Errorf("first group = %+v", first)
	}
	for _, group := range got.Groups {
		if group.BreachingEvaluations != 60 {
			t.Errorf("group %v breached %d times, want 60", group.Group, group.BreachingEvaluations)
		}
	}
	if got.Summary.Episodes != 20 || got.Summary.BreachingEvaluations != 20*60 || got.Series[0].FiringGroups != 20 || got.Series[0].Value != nil {
		t.Errorf("summary=%+v first point=%+v", got.Summary, got.Series[0])
	}
}

// TestBacktestNoData: under no_data an empty result opens a no_data episode,
// which notifies, and data below the threshold resolves it.
func TestBacktestNoData(t *testing.T) {
	rule := ungroupedRule(true)
	rule.NoData = models.AlertNoDataNoData
	calls := 0
	query := func(context.Context, time.Time) (*models.QueryResult, error) {
		calls++
		if calls >= 5 && calls < 8 {
			return ungroupedResult(), nil
		}
		return ungroupedResult(map[string]any{"value": 1}), nil
	}
	got, err := Backtest(t.Context(), rule, backtestEnd, 3600, query)
	if err != nil {
		t.Fatal(err)
	}
	want := models.BacktestSummary{Episodes: 1, Notifications: 2, NoDataEvaluations: 3}
	if got.Summary != want {
		t.Errorf("summary = %+v, want %+v", got.Summary, want)
	}
	if p := got.Series[4]; p.Status != models.BacktestStatusNoData || p.Value != nil {
		t.Errorf("series[4] = %+v, want no_data without a value", p)
	}
}

// TestBacktestMatchesLiveEvaluation replays one random grouped series
// through the backtest and through the live evaluator on a real store, under
// each no-data policy, and requires the same firing groups at every
// evaluation, the same notifications and the same final instances. The
// series spans seven days, so groups are also forgotten.
func TestBacktestMatchesLiveEvaluation(t *testing.T) {
	for _, policy := range []models.AlertNoDataPolicy{models.AlertNoDataZero, models.AlertNoDataNoData, models.AlertNoDataKeep} {
		t.Run(string(policy), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(7, uint64(len(policy))))
			results := make([]*models.QueryResult, models.MaxBacktestEvaluations)
			for i := range results {
				var rows []map[string]any
				if rng.IntN(10) > 0 {
					for _, service := range []string{"checkout", "search", "auth"} {
						// A group is missing for long stretches, so some are forgotten.
						if (i/20+len(service))%3 != 0 && rng.IntN(4) > 0 {
							rows = append(rows, row(service, rng.IntN(20)))
						}
					}
				}
				results[i] = groupedResult(rows...)
			}

			rule := BacktestRule{
				Rule:             Rule{Operator: models.AlertThresholdGreaterThan, Threshold: 10, NoData: policy},
				GroupBy:          []string{"service"},
				FrequencySeconds: 3600,
				LookbackSeconds:  3600,
				NotifyOnResolve:  true,
			}
			var ats []time.Time
			backtest, err := Backtest(t.Context(), rule, backtestEnd, models.MaxBacktestRangeSeconds, func(_ context.Context, at time.Time) (*models.QueryResult, error) {
				ats = append(ats, at)
				return results[len(ats)-1], nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if backtest.Evaluations != len(results) {
				t.Fatalf("evaluations = %d", backtest.Evaluations)
			}

			f := newGroupedFixture(t)
			f.withPolicy(t, policy, true, "service")
			sent := 0
			for i, at := range ats {
				sent += len(f.tickAt(t, at, results[i]))
				firing := 0
				for _, state := range f.instances(t) {
					if state == models.AlertInstanceStateFiring {
						firing++
					}
				}
				if got := backtest.Series[i].FiringGroups; got != firing {
					t.Fatalf("evaluation %d: backtest has %d firing groups, live has %d", i, got, firing)
				}
			}
			if backtest.Summary.Notifications != sent {
				t.Errorf("backtest counts %d notifications, live sent %d", backtest.Summary.Notifications, sent)
			}
			if backtest.Summary.Episodes == 0 || backtest.Summary.Notifications <= backtest.Summary.Episodes {
				t.Errorf("summary %+v: the series should open and close episodes", backtest.Summary)
			}
			if live, replayed := f.instances(t), lastInstances(rule, results, ats); !maps.Equal(live, replayed) {
				t.Errorf("final instances: live %v, backtest %v", live, replayed)
			}
		})
	}
}

// lastInstances replays results and returns the instances it ends with, by
// service.
func lastInstances(rule BacktestRule, results []*models.QueryResult, ats []time.Time) map[string]models.AlertInstanceState {
	rule.Grouped = true
	r := replay{rule: rule, step: 1, instances: map[string]Instance{}, groups: map[string]*groupTally{}, result: &models.BacktestResult{}}
	for i, at := range ats {
		r.evaluate(at, results[i])
	}
	states := make(map[string]models.AlertInstanceState, len(r.instances))
	for _, instance := range r.instances {
		states[instance.Group["service"]] = instance.State
	}
	return states
}
