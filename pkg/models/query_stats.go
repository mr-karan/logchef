package models

// query_stats_daily is an authoritative, non-pruned daily rollup of executed
// queries, incremented at record time. Unlike QueryHistory (capped per user, so
// a recent window only), it backs correct all-time usage analytics. The types
// below are the aggregate result rows the admin usage endpoint returns.

// SourceQueryStat is one row of the top-sources-by-query-count aggregate.
// SourceName is "" when the source row is gone (the rollup carries no FK and the
// name is resolved via a LEFT JOIN). AvgDurationMs is total_duration_ms /
// query_count as an integer (0 when count is 0).
type SourceQueryStat struct {
	SourceID      int64  `json:"source_id"`
	SourceName    string `json:"source_name"`
	QueryCount    int64  `json:"query_count"`
	AvgDurationMs int64  `json:"avg_duration_ms"`
}

// UserQueryStat is one row of the top-users-by-query-count aggregate.
type UserQueryStat struct {
	UserID     UserID `json:"user_id"`
	UserEmail  string `json:"user_email"`
	QueryCount int64  `json:"query_count"`
}

// DailyQueryVolume is one day's total query count, used for the volume-by-day
// series (ascending by date). Date is 'YYYY-MM-DD' (UTC).
type DailyQueryVolume struct {
	Date       string `json:"date"`
	QueryCount int64  `json:"query_count"`
}
