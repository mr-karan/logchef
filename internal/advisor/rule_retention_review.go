package advisor

import (
	"fmt"
	"time"

	"github.com/mr-karan/logchef/internal/clickhouse"
	"github.com/mr-karan/logchef/pkg/models"
)

const ruleRetentionReview = "retention_review"

const day = 24 * time.Hour

// retentionReviewFindings is R8: data much older than any recorded query
// reads, in a table with no TTL. It generates nothing; it asks for a review.
func retentionReviewFindings(a *analysis) []finding {
	if a.meta.OldestData == nil {
		return nil
	}
	if _, ok := tableTTL(a.meta.Table.CreateQuery); ok {
		return nil
	}
	age := a.now.Sub(*a.meta.OldestData)
	coverage := a.usage.Retention
	if age <= retentionMinAgeDays*day || coverage.Days < retentionMinDays || coverage.QueryCount < retentionMinQueries ||
		coverage.MaxLookbackSeconds == nil ||
		float64(coverage.NullLookbackCount) >= retentionMaxNullShare*float64(coverage.QueryCount) {
		return nil
	}
	lookback := time.Duration(*coverage.MaxLookbackSeconds) * time.Second
	if lookback >= age/2 {
		return nil
	}
	cutoff := a.now.Add(-lookback)
	var older uint64
	for _, partition := range a.meta.Partitions {
		if partition.MaxTime.Before(cutoff) {
			older += partition.Bytes
		}
	}
	olderValue := formatBytes(older)
	if a.meta.Target.Kind == models.AdvisorTargetDistributedLocal {
		olderValue += ", on the connected replica only"
	}
	evidence := []models.AdvisorEvidence{
		{Label: "Age of the oldest data", Value: fmt.Sprintf("%s (since %s)", formatSpan(age), a.meta.OldestData.UTC().Format(time.DateOnly)), Source: clickhouse.AdvisorInputParts},
		{Label: "Bytes in partitions older than the largest lookback", Value: olderValue, Source: clickhouse.AdvisorInputParts},
		{Label: "Largest lookback of a recorded query", Value: formatSpan(lookback), Source: "logchef"},
		{Label: "Days with recorded queries", Value: fmt.Sprintf("%d of the last 90", coverage.Days), Source: "logchef"},
		// A day's rollup row that mixes queries with and without a start
		// time keeps the start time, so the count is a lower bound.
		{Label: "Queries with no start time", Value: fmt.Sprintf("at least %s of %s (%s); the true share can be higher",
			formatCount(coverage.NullLookbackCount), formatCount(coverage.QueryCount), formatPercent(float64(coverage.NullLookbackCount)/float64(coverage.QueryCount))), Source: "logchef"},
	}
	return []finding{{SchemaFinding: models.SchemaFinding{
		ID:       a.findingID(ruleRetentionReview, "", nil, ""),
		Rule:     ruleRetentionReview,
		Impact:   models.ImpactInfo,
		Title:    fmt.Sprintf("Review retention: recorded queries read back %s, and the data goes back %s", formatSpan(lookback), formatSpan(age)),
		Evidence: evidence,
		DDL:      []string{},
		Gain:     "None stated. The finding is a prompt to review retention.",
		Cost:     "Logchef sees only Explorer queries. Alerts, dashboards, exports and other clients may read older data. Choose a retention period from your own policy.",
	}}}
}

// formatSpan renders a duration in whole days, or in hours under two days.
func formatSpan(d time.Duration) string {
	if d < 2*day {
		return fmt.Sprintf("%d hours", int64(d/time.Hour))
	}
	return fmt.Sprintf("%d days", int64(d/day))
}
