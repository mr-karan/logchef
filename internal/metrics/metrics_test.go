package metrics

import (
	"testing"

	"github.com/VictoriaMetrics/metrics"
)

func TestRecordRateLimitRejection(t *testing.T) {
	const label = `logchef_rate_limit_rejections_total{scope="auth"}`

	before := metrics.GetOrCreateCounter(label).Get()
	RecordRateLimitRejection("auth")
	after := metrics.GetOrCreateCounter(label).Get()

	if after != before+1 {
		t.Fatalf("counter = %d, want %d", after, before+1)
	}

	// A different scope must use a distinct series and not affect the first.
	queryBefore := metrics.GetOrCreateCounter(`logchef_rate_limit_rejections_total{scope="query"}`).Get()
	RecordRateLimitRejection("query")
	if got := metrics.GetOrCreateCounter(label).Get(); got != after {
		t.Fatalf("auth counter changed after recording query rejection: %d, want %d", got, after)
	}
	if got := metrics.GetOrCreateCounter(`logchef_rate_limit_rejections_total{scope="query"}`).Get(); got != queryBefore+1 {
		t.Fatalf("query counter = %d, want %d", got, queryBefore+1)
	}
}
