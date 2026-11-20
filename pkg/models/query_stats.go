package models

// query_stats_daily is an authoritative, non-pruned daily rollup of executed
// queries, incremented at record time. Unlike QueryHistory (capped per user, so
// a recent window only), it backs correct all-time usage analytics. The types
// below are the aggregate rows the admin Query Activity page reads.

// QueryStatsFilter selects rollup rows for a current window and the window
// before it. Dates are 'YYYY-MM-DD' (UTC) and inclusive: rows from PriorSince
// to Until are read, and a row counts as current when bucket_date >= Since.
type QueryStatsFilter struct {
	PriorSince string
	Since      string
	Until      string
	// AccountType keeps only queries by users of that type; "" keeps every
	// query, including those of deleted users, whose type is unknown.
	AccountType UserAccountType
	SourceID    SourceID // 0 for every source
	UserID      UserID   // 0 for every user
}

// QueryTotals counts queries and their summed duration. The average latency
// is TotalDurationMs / QueryCount.
type QueryTotals struct {
	QueryCount      int64 `json:"query_count"`
	TotalDurationMs int64 `json:"total_duration_ms"`
}

// QueryStatsPeriod aggregates one window.
type QueryStatsPeriod struct {
	QueryTotals
	ActiveUsers   int64 `json:"active_users"`
	ActiveSources int64 `json:"active_sources"`
}

// QueryStatsSummary is the current window beside the one before it.
type QueryStatsSummary struct {
	Current QueryStatsPeriod
	Prior   QueryStatsPeriod
}

// AccountQueryCount is the query count of one account type in the current
// window. AccountType is "" for queries of deleted users.
type AccountQueryCount struct {
	AccountType UserAccountType
	QueryCount  int64
}

// DailyQueryStat is one UTC day of the rollup. Date is 'YYYY-MM-DD'.
type DailyQueryStat struct {
	Date        string `json:"date"`
	QueryCount  int64  `json:"query_count"`
	ActiveUsers int64  `json:"active_users"`
}

// UserQueryStat is one user's usage. Email, FullName and AccountType are ""
// when the user row is gone. ActiveDays counts current-window days with a
// query.
type UserQueryStat struct {
	UserID      UserID          `json:"user_id"`
	Email       string          `json:"email"`
	FullName    string          `json:"full_name"`
	AccountType UserAccountType `json:"account_type"`
	ActiveDays  int64           `json:"active_days"`
	Current     QueryTotals     `json:"current"`
	Prior       QueryTotals     `json:"prior"`
}

// SourceQueryStat is one source's usage. Current and Prior honor the
// filter's account type; ServiceQueryCount counts service-account queries in
// the current window whatever the account type. SourceName and SourceType are
// "" when the source row is gone. TeamID is a team that links the source
// now, to open it in Explorer, nil when no team does.
type SourceQueryStat struct {
	SourceID          SourceID    `json:"source_id"`
	SourceName        string      `json:"source_name"`
	SourceType        string      `json:"source_type"`
	TeamID            *TeamID     `json:"team_id"`
	ServiceQueryCount int64       `json:"service_query_count"`
	Current           QueryTotals `json:"current"`
	Prior             QueryTotals `json:"prior"`
}

// LanguageQueryCount is the current-window query count of one language.
type LanguageQueryCount struct {
	Language   QueryLanguage `json:"language"`
	QueryCount int64         `json:"query_count"`
}

// QueryShapeFilter selects query_shape_stats_daily rows, which carry no user,
// so only a source narrows them. Dates are as in QueryStatsFilter.
type QueryShapeFilter struct {
	PriorSince string
	Since      string
	Until      string
	SourceID   SourceID // 0 for every source
}

// QueryShapeStat is one analyzed filter shape of one source. Prior covers
// rows before Since, and is nil when the rollup no longer keeps that whole
// window. MaxLookbackSeconds covers the current window and is nil when no
// counted query had a start time.
type QueryShapeStat struct {
	SourceID           SourceID      `json:"source_id"`
	SourceName         string        `json:"source_name"`
	SourceType         string        `json:"source_type"`
	QueryLanguage      QueryLanguage `json:"query_language"`
	ShapeHash          string        `json:"shape_hash"`
	Shape              FilterShape   `json:"shape"`
	MaxLookbackSeconds *int64        `json:"max_lookback_s"`
	Current            QueryTotals   `json:"current"`
	Prior              *QueryTotals  `json:"prior"`
	// Scan covers the current window: ScannedRuns counts the runs on days
	// whose rows recorded a scan, and BytesRead and RowsRead sum it.
	Scan ShapeScan `json:"-"`
}

// ShapeScan is what a shape's runs scanned.
type ShapeScan struct {
	ScannedRuns int64
	RowsRead    int64
	BytesRead   int64
}

// LatencyBoundsMs are the inclusive upper bounds, in milliseconds, of the
// latency histogram buckets. A last, open bucket counts the slower runs.
// query_stats_daily keeps one column per bucket, in this order.
var LatencyBoundsMs = [...]int64{100, 250, 500, 1_000, 2_500, 5_000, 10_000, 30_000, 60_000}

// LatencyHistogram counts runs per bucket of LatencyBoundsMs, the open
// bucket last.
type LatencyHistogram [len(LatencyBoundsMs) + 1]int64

// LatencyHistogramOf is the histogram of one run that took ms.
func LatencyHistogramOf(ms int64) LatencyHistogram {
	var h LatencyHistogram
	for i, bound := range LatencyBoundsMs {
		if ms <= bound {
			h[i] = 1
			return h
		}
	}
	h[len(LatencyBoundsMs)] = 1
	return h
}

// Count is the number of runs in the histogram.
func (h LatencyHistogram) Count() int64 {
	var n int64
	for _, c := range h {
		n += c
	}
	return n
}

// Add adds o's counts to h.
func (h *LatencyHistogram) Add(o LatencyHistogram) {
	for i := range h {
		h[i] += o[i]
	}
}

// Percentile is a latency percentile estimated from a histogram.
type Percentile struct {
	Ms int64 `json:"ms"`
	// Open says that the percentile falls in the open bucket: the runs took
	// longer than Ms, the last bound.
	Open bool `json:"open"`
}

// Quantile estimates the q-quantile, 0 < q <= 1, by linear interpolation
// within the bucket that holds it. ok is false when the histogram is empty.
func (h LatencyHistogram) Quantile(q float64) (p Percentile, ok bool) {
	total := h.Count()
	if total == 0 {
		return Percentile{}, false
	}
	rank := q * float64(total)
	var below int64
	for i, bound := range LatencyBoundsMs {
		if h[i] > 0 && float64(below+h[i]) >= rank {
			lower := int64(0)
			if i > 0 {
				lower = LatencyBoundsMs[i-1]
			}
			within := (rank - float64(below)) / float64(h[i])
			return Percentile{Ms: lower + int64(within*float64(bound-lower)+0.5)}, true
		}
		below += h[i]
	}
	return Percentile{Ms: LatencyBoundsMs[len(LatencyBoundsMs)-1], Open: true}, true
}

// RunCost sums what successful runs cost. A run recorded before cost
// collection began adds nothing, so Latency.Count() is the number of runs
// the cost covers, and the averages divide by it.
type RunCost struct {
	Latency   LatencyHistogram
	RowsRead  int64
	BytesRead int64
}

// Add adds o to c.
func (c *RunCost) Add(o RunCost) {
	c.Latency.Add(o.Latency)
	c.RowsRead += o.RowsRead
	c.BytesRead += o.BytesRead
}

// QueryCostSummary is the cost of the current window beside the one before
// it.
type QueryCostSummary struct {
	Current RunCost
	Prior   RunCost
}

// SourceDayCost is one source's cost on one UTC day. Date is 'YYYY-MM-DD'.
type SourceDayCost struct {
	SourceID SourceID
	Date     string
	Cost     RunCost
}

// QueryStatsIncrement is one successful query run added to
// query_stats_daily. BucketDate is 'YYYY-MM-DD' (UTC).
type QueryStatsIncrement struct {
	BucketDate   string
	UserID       UserID
	TeamID       TeamID
	SourceID     SourceID
	Language     QueryLanguage
	DurationMs   int64
	RowsRead     int64
	BytesRead    int64
	RowsReturned int64
}

// QueryFailureIncrement is one failed query run added to
// query_failure_stats_daily. Reason is the query.run failure reason.
type QueryFailureIncrement struct {
	BucketDate string
	UserID     UserID
	TeamID     TeamID
	SourceID   SourceID
	Language   QueryLanguage
	Reason     string
}

// FailureReasonCount counts the failed runs of one reason in both windows.
type FailureReasonCount struct {
	Reason string
	Count  int64
	Prior  int64
}

// SourceFailureCount counts one source's failed runs in the current window.
type SourceFailureCount struct {
	SourceID SourceID
	Count    int64
}

// FailurePairStat counts the current-window failed runs of one user on one
// source. Names are "" when their row is gone.
type FailurePairStat struct {
	SourceID    SourceID        `json:"source_id"`
	SourceName  string          `json:"source_name"`
	SourceType  string          `json:"source_type"`
	UserID      UserID          `json:"user_id"`
	Email       string          `json:"email"`
	FullName    string          `json:"full_name"`
	AccountType UserAccountType `json:"account_type"`
	Count       int64           `json:"count"`
}
