package alerts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mr-karan/logchef/pkg/models"
)

// Two replays that end now, asked within one step, read the same instants,
// so whether a burst is caught never depends on the second asked. An end the
// caller chose is evaluated as given.
func TestBacktestAlignsInstants(t *testing.T) {
	// 24h at a 60s frequency steps every 720s, and 10:12 is a multiple of it.
	want := backtestEnd.Add(12 * time.Minute)
	for _, asked := range []time.Duration{13*time.Minute + 5*time.Second, 23*time.Minute + 50*time.Second} {
		if got := AlignBacktestEnd(backtestEnd.Add(asked), 60, 86400); !got.Equal(want) {
			t.Errorf("end asked at +%s aligned to %s, want %s", asked, got, want)
		}
	}

	chosen := backtestEnd.Add(13*time.Minute + 5*time.Second)
	var ats []time.Time
	if _, err := Backtest(t.Context(), ungroupedRule(true), chosen, 86400, seriesQuery(make([]float64, 200), &ats)); err != nil {
		t.Fatal(err)
	}
	if len(ats) != 120 || !ats[119].Equal(chosen) {
		t.Fatalf("last instant %s, want the chosen end %s", ats[len(ats)-1], chosen)
	}
}

func TestBacktestCoverageWarnings(t *testing.T) {
	for _, tc := range []struct {
		frequency, lookback, rangeSeconds int
		want                              []string // substrings of the one warning; none means no warning
	}{
		{60, 300, 3600, nil},
		{60, 300, 86400, []string{"every 12m", "5m lookback", "reads 42% of the range", "Replay at most 10h"}},
		{900, 300, 3600, []string{"evaluates every 15m and reads the last 5m", "67% of the time is never read"}},
	} {
		rule := ungroupedRule(true)
		rule.FrequencySeconds, rule.LookbackSeconds = tc.frequency, tc.lookback
		var ats []time.Time
		got, err := Backtest(t.Context(), rule, backtestEnd, tc.rangeSeconds, seriesQuery(make([]float64, 200), &ats))
		if err != nil {
			t.Fatal(err)
		}
		if tc.want == nil {
			if got.Warnings == nil || len(got.Warnings) != 0 {
				t.Errorf("%+v: warnings %#v, want an empty list", tc, got.Warnings)
			}
			continue
		}
		if len(got.Warnings) != 1 {
			t.Fatalf("%+v: warnings %q, want one", tc, got.Warnings)
		}
		for _, part := range tc.want {
			if !strings.Contains(got.Warnings[0], part) {
				t.Errorf("%+v: warning %q lacks %q", tc, got.Warnings[0], part)
			}
		}
	}
}

// A grouped rule whose whole result is empty before any group reports opens
// a no_data episode that belongs to no group (a tracked group without a row
// opens its own). The summary names it, so the episodes add up.
func TestBacktestEmptyResultEpisodes(t *testing.T) {
	rule := ungroupedRule(true)
	rule.GroupBy = []string{"service"}
	rule.NoData = models.AlertNoDataNoData
	calls := 0
	query := func(context.Context, time.Time) (*models.QueryResult, error) {
		calls++
		switch {
		case calls < 5:
			return groupedResult(), nil
		case calls >= 20 && calls < 25:
			return groupedResult(map[string]any{"service": "api", "value": 9}), nil
		default:
			return groupedResult(map[string]any{"service": "api", "value": 1}), nil
		}
	}
	got, err := Backtest(t.Context(), rule, backtestEnd, 3600, query)
	if err != nil {
		t.Fatal(err)
	}
	inGroups := 0
	for _, group := range got.Groups {
		inGroups += group.Episodes
	}
	if got.Summary.EmptyResultEpisodes != 1 || got.Summary.Episodes != inGroups+got.Summary.EmptyResultEpisodes {
		t.Fatalf("episodes %d, in groups %d, empty result %d", got.Summary.Episodes, inGroups, got.Summary.EmptyResultEpisodes)
	}
}

// A group keeps its lowest value next to its highest, which is the value
// that matters for an lt or lte rule.
func TestBacktestGroupMinValue(t *testing.T) {
	rule := ungroupedRule(true)
	rule.GroupBy = []string{"service"}
	rule.Operator, rule.Threshold = models.AlertThresholdLessThan, 3
	calls := 0
	query := func(context.Context, time.Time) (*models.QueryResult, error) {
		calls++
		return groupedResult(map[string]any{"service": "api", "value": float64(calls % 7)}), nil
	}
	got, err := Backtest(t.Context(), rule, backtestEnd, 3600, query)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Groups) != 1 || got.Groups[0].MinValue == nil || *got.Groups[0].MinValue != 0 || *got.Groups[0].MaxValue != 6 {
		t.Fatalf("groups = %+v, want api with min 0 and max 6", got.Groups)
	}
}
