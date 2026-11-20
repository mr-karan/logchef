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
}
